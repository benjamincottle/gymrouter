package engine

import (
	"context"
	"fmt"
	"runtime/debug"
	"sort"
	"time"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/lines"
	"github.com/benjamincottle/gymrouter/internal/raptor"
	"github.com/benjamincottle/gymrouter/internal/timetable"
)

// CatalogStop is a stop served by at least one public line today, and the lines that serve it.
type CatalogStop struct {
	ID, Name, Station, StationID string
	Pos                          geo.Point
	Lines                        []lines.Key
}

// Catalog lists every served stop with its lines. Setup uses it to find the stops near a place before
// any lines have been chosen, so it covers the whole network (the routable timetable only holds
// the lines devices have asked about).
type Catalog struct {
	Date  time.Time
	Stops []CatalogStop
	// Lines is every public line in the timetable, whether or not it runs on Date.
	Lines lines.Set
}

// NearStop is a catalogue stop with the estimated walk to it.
type NearStop struct {
	CatalogStop
	WalkS int32
}

// Near returns the stops within radiusM of p, nearest first.
func (c *Catalog) Near(p geo.Point, radiusM float64, o raptor.Options) []NearStop {
	var out []NearStop
	for _, s := range c.Stops {
		if d := geo.DistanceM(p, s.Pos); d <= radiusM {
			out = append(out, NearStop{CatalogStop: s, WalkS: o.WalkSecs(d)})
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].WalkS != out[b].WalkS {
			return out[a].WalkS < out[b].WalkS
		}
		return out[a].ID < out[b].ID
	})
	return out
}

// NewCatalog summarises a day's stops and the lines that call at them. Replacement buses are left out: they come
// with the train lines they stand in for, nobody picks them.
func NewCatalog(d *gtfs.Day) *Catalog {
	perStop := make([]lines.Set, len(d.Stops))
	routeLine := make([]lines.Key, len(d.Routes))
	for i, r := range d.Routes {
		routeLine[i] = lines.Of(r.Type, r.ShortName)
	}
	for i := range d.Trips {
		t := &d.Trips[i]
		k := routeLine[t.Route]
		if k.Mode == lines.ReplacementBus {
			continue
		}
		for _, st := range t.StopTimes {
			if perStop[st.Stop] == nil {
				perStop[st.Stop] = lines.Set{}
			}
			perStop[st.Stop][k] = true
		}
	}
	c := &Catalog{Date: d.Date, Lines: lines.Set{}}
	for _, k := range routeLine {
		if k.Mode != lines.ReplacementBus {
			c.Lines[k] = true
		}
	}
	for si, set := range perStop {
		if len(set) == 0 {
			continue
		}
		st := d.Stops[si]
		cs := CatalogStop{ID: st.ID, Name: st.Name, Pos: st.Pos}
		if st.Parent != "" {
			if pi, ok := d.StopIndex[st.Parent]; ok {
				cs.Station, cs.StationID = d.Stops[pi].Name, st.Parent
			}
		}
		for k := range set {
			cs.Lines = append(cs.Lines, k)
		}
		sort.Slice(cs.Lines, func(a, b int) bool { return cs.Lines[a].String() < cs.Lines[b].String() })
		c.Stops = append(c.Stops, cs)
	}
	return c
}

// Catalog returns the stop catalogue, or nil while it's still being built.
func (e *Engine) Catalog() *Catalog { return e.catalog.Load() }

// BuildCatalog loads the whole network for today, summarises it and lets the big timetable go.
func (e *Engine) BuildCatalog(ctx context.Context) {
	select {
	case e.heavy <- struct{}{}:
	case <-ctx.Done():
		return
	}
	restore := frugalGC()
	defer func() {
		restore()
		<-e.heavy
	}()
	start := e.now()
	date := e.LocalDate(e.now())
	d, err := timetable.LoadAll(date, e.paths)
	if err != nil {
		e.log.Error("building the stop catalogue failed", "err", err)
		return
	}
	c := NewCatalog(d)
	unknown := timetable.UnknownTrackwork(d, e.Lines())
	d = nil
	e.rtMu.Lock()
	e.unknownTrackwork = unknown
	e.rtMu.Unlock()
	if len(unknown) > 0 {
		e.log.Warn("trackwork buses with an unknown line code are left out of searches", "buses", fmt.Sprint(unknown))
	}
	debug.FreeOSMemory()
	e.catalog.Store(c)
	e.log.Info("stop catalogue built", "stops", len(c.Stops), "took", e.now().Sub(start).Round(time.Millisecond).String())
}
