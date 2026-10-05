package engine

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/raptor"
	"github.com/benjamincottle/gymrouter/internal/suggest"
	"github.com/benjamincottle/gymrouter/internal/timetable"
)

// ErrNoStops is returned (wrapped) when a place has no stops to start or end a trip.
var ErrNoStops = errors.New("no stops")

// SuggestPlace is one end of a suggestion search: a position, and optionally the stops (with measured
// walk times) to use instead of everything within the radius.
type SuggestPlace struct {
	Pos    geo.Point
	Access []StopWalk
}

// StopWalk is a stop and the time to walk to it.
type StopWalk struct {
	Stop  string
	WalkS int32
}

// SuggestWindow summarises one searched window.
type SuggestWindow struct {
	Label      string
	Date       time.Time
	Departures int
	BestS      int32 // median door-to-door time of the quickest itinerary; 0 if none
}

// SuggestResult is the outcome of Suggest.
type SuggestResult struct {
	Windows     []SuggestWindow
	Lines       []suggest.LineScore
	Itineraries []suggest.Itinerary
}

// suggestWindows are the usual times people travel: a weekday afternoon and a Sunday morning.
var suggestWindows = []struct {
	label      string
	weekday    time.Weekday
	start, end int32
}{
	{"Weekday afternoon", time.Tuesday, 16 * 3600, 19 * 3600},
	{"Sunday morning", time.Sunday, 8 * 3600, 11 * 3600},
}

const suggestStep = 10 * 60

// frugalGC makes the collector run eagerly while a whole-network pass is in progress. That pass is mostly
// short-lived garbage, and collecting it promptly keeps the peak well under the container's limit. The
// returned function restores the previous setting.
func frugalGC() func() {
	prev := debug.SetGCPercent(20)
	return func() { debug.SetGCPercent(prev) }
}

// MaxSuggestTargets bounds how many destinations one Suggest call searches (enough for every gym a device can hold).
const MaxSuggestTargets = 12

// Suggest searches the whole network from one place to each of the targets and reports, for each, which lines
// appear in the best options. It loads the full timetable for each window's day once for all targets, for the
// duration of the call, and runs one at a time (ErrBusy if another is running), so the server's memory stays bounded.
// progress, if not nil, is told how many of the steps (reading a day's timetable, searching one target on it) are done.
func (e *Engine) Suggest(ctx context.Context, from SuggestPlace, targets []SuggestPlace, radiusM float64,
	progress func(done, of int)) ([]*SuggestResult, error) {
	if len(targets) == 0 || len(targets) > MaxSuggestTargets {
		return nil, fmt.Errorf("%w: need 1 to %d destinations", ErrNoStops, MaxSuggestTargets)
	}
	select {
	case e.heavy <- struct{}{}:
	default:
		return nil, ErrBusy
	}
	restore := frugalGC()
	defer func() {
		restore()
		<-e.heavy
		debug.FreeOSMemory()
	}()

	if progress == nil {
		progress = func(int, int) {}
	}
	steps, done := len(suggestWindows)*(1+len(targets)), 0
	step := func() {
		done++
		progress(done, steps)
	}
	opts := e.routingOptions()
	today := e.LocalDate(e.now())
	results := make([]*SuggestResult, len(targets))
	perTarget := make([][]suggest.WindowResult, len(targets))
	for i := range results {
		results[i] = &SuggestResult{}
	}
	for _, w := range suggestWindows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		date := today
		for {
			date = date.AddDate(0, 0, 1)
			if date.Weekday() == w.weekday {
				break
			}
		}
		runs, err := e.suggestWindow(ctx, date, w.label, w.start, w.end, from, targets, radiusM, opts, step)
		if err != nil {
			return nil, err
		}
		debug.FreeOSMemory() // drop this day's network before loading the next
		for i, wr := range runs {
			sw := SuggestWindow{Label: w.label, Date: date, Departures: wr.Departures}
			if len(wr.Itineraries) > 0 {
				sw.BestS = wr.Itineraries[0].MedianS
			}
			results[i].Windows = append(results[i].Windows, sw)
			results[i].Itineraries = append(results[i].Itineraries, wr.Itineraries...)
			perTarget[i] = append(perTarget[i], wr)
		}
	}
	for i := range results {
		results[i].Lines = suggest.Scores(perTarget[i])
	}
	return results, nil
}

// suggestWindow searches one window on one day's whole network, once per target. It returns nothing if the
// feed has no trips that day.
func (e *Engine) suggestWindow(ctx context.Context, date time.Time, label string, start, end int32, from SuggestPlace,
	targets []SuggestPlace, radiusM float64, opts raptor.Options, step func()) ([]suggest.WindowResult, error) {
	day, err := timetable.LoadAll(date, e.paths)
	if err != nil {
		return nil, err
	}
	step()
	if len(day.Trips) == 0 {
		for range targets {
			step()
		}
		return nil, nil
	}
	net := raptor.Build(day, opts)
	access, err := e.suggestAccess(net, from, radiusM, opts)
	if err != nil {
		return nil, fmt.Errorf("the start: %w", err)
	}
	out := make([]suggest.WindowResult, 0, len(targets))
	for i, to := range targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		egress, err := e.suggestAccess(net, to, radiusM, opts)
		if err != nil {
			return nil, fmt.Errorf("destination %d: %w", i+1, err)
		}
		out = append(out, suggest.Run(net, access, egress, suggest.Window{Label: label, Start: start, End: end, Step: suggestStep},
			suggest.DefaultParams()))
		step()
	}
	return out, nil
}

func (e *Engine) suggestAccess(net *raptor.Network, p SuggestPlace, radiusM float64, o raptor.Options) ([]raptor.Access, error) {
	if len(p.Access) > 0 {
		var out []raptor.Access
		for _, a := range p.Access {
			if si, ok := net.Day.StopIndex[a.Stop]; ok {
				out = append(out, raptor.Access{Stop: si, Secs: a.WalkS})
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("%w: none of the chosen stops run on that day", ErrNoStops)
		}
		return out, nil
	}
	// The same stops the planner would use, so every suggested line is one it can reach.
	out := e.Approach(net, p.Pos, radiusM, o).Access
	if len(out) == 0 {
		return nil, fmt.Errorf("%w within %.0f km", ErrNoStops, BandCapM/1000.0)
	}
	return out, nil
}
