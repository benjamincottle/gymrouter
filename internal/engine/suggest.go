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

// SuggestPlace is one end of a suggestion search: a position, and optionally the stops chosen for it (by ID) to use
// instead of everything within the radius.
type SuggestPlace struct {
	Pos    geo.Point
	Access []string
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

// SuggestProgress is how far a Suggest call has got: what it's doing, and how much of the whole is done.
type SuggestProgress struct {
	Stage    string  // SuggestReading or SuggestSearching
	Fraction float64 // 0 to 1
}

// What a Suggest call does, for each day it searches: reads the day's whole timetable, then searches it.
const (
	SuggestReading   = "reading"
	SuggestSearching = "searching"
)

// readWeight is how long reading a day's timetable takes, counted in searches of one destination on it: reading is
// nearly all of the work (measured: about 5 s against 0.7 s).
const readWeight = 7

// MaxSuggestTargets bounds how many destinations one Suggest call searches (enough for every gym a device can hold).
const MaxSuggestTargets = 12

// Suggest searches the whole network from one place to each of the targets and reports, for each, which lines
// appear in the best options. It loads the full timetable for each window's day once for all targets, for the
// duration of the call, and runs one at a time (ErrBusy if another is running), so the server's memory stays bounded.
// progress, if not nil, is told how far it has got as it goes: through each read, and after every departure time searched.
func (e *Engine) Suggest(ctx context.Context, from SuggestPlace, targets []SuggestPlace, radiusM float64,
	progress func(SuggestProgress)) ([]*SuggestResult, error) {
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
		progress = func(SuggestProgress) {}
	}
	// Each day is a read and then a search for each target, in units of one target's search.
	perDay := float64(readWeight + len(targets))
	opts := e.routingOptions()
	today := e.LocalDate(e.now())
	results := make([]*SuggestResult, len(targets))
	perTarget := make([][]suggest.WindowResult, len(targets))
	for i := range results {
		results[i] = &SuggestResult{}
	}
	for wi, w := range suggestWindows {
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
		report := func(stage string, units float64) {
			progress(SuggestProgress{Stage: stage, Fraction: (float64(wi)*perDay + units) / (float64(len(suggestWindows)) * perDay)})
		}
		runs, err := e.suggestWindow(ctx, date, w.label, w.start, w.end, from, targets, radiusM, opts, report)
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
// feed has no trips that day. report is told the stage and how much of the day's work is done, in units of one
// target's search (readWeight for the read, then one for each target).
func (e *Engine) suggestWindow(ctx context.Context, date time.Time, label string, start, end int32, from SuggestPlace,
	targets []SuggestPlace, radiusM float64, opts raptor.Options, report func(stage string, units float64)) ([]suggest.WindowResult, error) {
	report(SuggestReading, 0)
	// The last tenth of the read is building the network from what was read.
	day, err := timetable.LoadAll(date, e.paths, func(read float64) { report(SuggestReading, 0.9*readWeight*read) })
	if err != nil {
		return nil, err
	}
	if len(day.Trips) == 0 {
		report(SuggestSearching, float64(readWeight+len(targets)))
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
		win := suggest.Window{Label: label, Start: start, End: end, Step: suggestStep}
		searched := 0
		report(SuggestSearching, float64(readWeight+i))
		out = append(out, suggest.Run(net, access, egress, win, suggest.DefaultParams(), func() {
			searched++
			report(SuggestSearching, float64(readWeight+i)+float64(searched)/float64(win.Departures()))
		}))
	}
	return out, nil
}

func (e *Engine) suggestAccess(net *raptor.Network, p SuggestPlace, radiusM float64, o raptor.Options) ([]raptor.Access, error) {
	if len(p.Access) > 0 {
		var stops []int32
		for _, id := range p.Access {
			if si, ok := net.Day.StopIndex[id]; ok {
				stops = append(stops, si)
			}
		}
		if len(stops) == 0 {
			return nil, fmt.Errorf("%w: none of the chosen stops run on that day", ErrNoStops)
		}
		return e.ApproachTo(net, p.Pos, stops, o).Access, nil
	}
	// The same stops the planner would use, so every suggested line is one it can reach.
	out := e.Approach(net, p.Pos, radiusM, o, nil).Access
	if len(out) == 0 {
		return nil, fmt.Errorf("%w within %.0f km", ErrNoStops, BandCapM/1000.0)
	}
	return out, nil
}
