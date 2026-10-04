package engine

import (
	"os"
	"path/filepath"
	"time"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/lines"
)

// shapeTolM is the simplification tolerance for route shapes (metres).
const shapeTolM = 4

// LoadShapes reads route shapes for today's trips (trains from the trains bundle, the rest from the
// complete bundle). It's slow (the shapes file is ~1 GB), so Start runs it in the background;
// until it finishes, geometry falls back to straight lines between stops.
func (e *Engine) LoadShapes() {
	s := e.today.Load()
	if s == nil {
		return
	}
	start := e.now()
	wantTrains, wantOther := map[string]bool{}, map[string]bool{}
	for _, t := range s.Static.Day.Trips {
		if t.Shape == "" {
			continue
		}
		r := s.Static.Day.Routes[t.Route]
		if lines.ModeOf(r.Type) == lines.Train && e.paths.Trains != "" {
			wantTrains[t.Shape] = true
		} else {
			wantOther[t.Shape] = true
		}
	}
	all := map[string][]geo.Point{}
	for path, want := range map[string]map[string]bool{e.paths.Complete: wantOther, e.paths.Trains: wantTrains} {
		if len(want) == 0 {
			continue
		}
		m, err := gtfs.LoadShapes(path, want, shapeTolM)
		if err != nil {
			e.log.Warn("loading shapes failed", "err", err)
			continue
		}
		for k, v := range m {
			all[k] = v
		}
	}
	e.shapes.Store(&all)
	e.log.Info("shapes loaded", "shapes", len(all), "took", e.now().Sub(start).Round(time.Millisecond).String())
}

// LegGeometry returns the path of a trip between two of its stops (along the route shape when known,
// otherwise straight lines between its stops) and the positions of the stops it calls at in between (placed on the
// path when it follows the shape).
func (e *Engine) LegGeometry(s *Snapshot, tripID, fromStop, toStop string) (path, stops []geo.Point, ok bool) {
	d := s.Day
	var trip *gtfs.Trip
	for _, ti := range d.TripIndex[tripID] {
		trip = &d.Trips[ti]
		if trip.DayOffset == 0 {
			break
		}
	}
	if trip == nil {
		return nil, nil, false
	}
	fi, ti := callRange(d, trip, fromStop, toStop)
	if fi < 0 {
		return nil, nil, false
	}
	for _, st := range trip.StopTimes[fi+1 : ti] {
		stops = append(stops, d.Stops[st.Stop].Pos)
	}
	from, to := d.Stops[trip.StopTimes[fi].Stop].Pos, d.Stops[trip.StopTimes[ti].Stop].Pos
	if shapes := e.shapes.Load(); shapes != nil {
		if pts, ok := (*shapes)[trip.Shape]; ok && len(pts) > 1 {
			// Cut the shape where each stop sits beside it, not at the nearest corner: shapes are simplified, so the
			// nearest corner can be past the stop and the line would run on and double back.
			a := geo.Project(pts, from, geo.Along{})
			b := geo.Project(pts, to, a)
			if a.Before(b) {
				path = geo.Cut(pts, a, b)
				// Stop positions are at the kerb; put each one on the line, in order, so the map draws it on the line.
				at := geo.Along{}
				for i, p := range stops {
					at = geo.Project(path, p, at)
					stops[i] = at.Pt
				}
				return path, stops, true
			}
		}
	}
	path = make([]geo.Point, 0, ti-fi+1)
	for _, st := range trip.StopTimes[fi : ti+1] {
		path = append(path, d.Stops[st.Stop].Pos)
	}
	return path, stops, true
}

// callRange finds where a ride boards and gets off: the call indexes of fromStop and of the first toStop after it,
// or -1, -1 if the trip doesn't make that journey.
func callRange(d *gtfs.Day, trip *gtfs.Trip, fromStop, toStop string) (int, int) {
	fi := -1
	for i, st := range trip.StopTimes {
		id := d.Stops[st.Stop].ID
		if fi < 0 && id == fromStop {
			fi = i
		} else if fi >= 0 && id == toStop {
			return fi, i
		}
	}
	return -1, -1
}

// MapFile returns the path of the self-hosted basemap (PMTiles), if present.
func (e *Engine) MapFile() (string, bool) {
	p := filepath.Join(e.cfg.Server.DataDir, "map.pmtiles")
	fi, err := os.Stat(p)
	return p, err == nil && fi.Mode().IsRegular()
}
