package engine

import (
	"os"
	"path/filepath"
	"sort"
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

// LegGeometry returns the path of a trip between two of its stops: along the route shape when
// known, otherwise straight lines between its stops.
func (e *Engine) LegGeometry(s *Snapshot, tripID, fromStop, toStop string) ([]geo.Point, bool) {
	d := s.Day
	var trip *gtfs.Trip
	for _, ti := range d.TripIndex[tripID] {
		trip = &d.Trips[ti]
		if trip.DayOffset == 0 {
			break
		}
	}
	if trip == nil {
		return nil, false
	}
	fi, ti := -1, -1
	for i, st := range trip.StopTimes {
		id := d.Stops[st.Stop].ID
		if fi < 0 && id == fromStop {
			fi = i
		} else if fi >= 0 && id == toStop {
			ti = i
			break
		}
	}
	if fi < 0 || ti < 0 {
		return nil, false
	}
	from, to := d.Stops[trip.StopTimes[fi].Stop].Pos, d.Stops[trip.StopTimes[ti].Stop].Pos
	if shapes := e.shapes.Load(); shapes != nil {
		if pts, ok := (*shapes)[trip.Shape]; ok && len(pts) > 1 {
			a := geo.Nearest(pts, from, 0)
			b := geo.Nearest(pts, to, a)
			if b > a {
				out := append([]geo.Point{from}, pts[a:b+1]...)
				return append(out, to), true
			}
		}
	}
	out := make([]geo.Point, 0, ti-fi+1)
	for _, st := range trip.StopTimes[fi : ti+1] {
		out = append(out, d.Stops[st.Stop].Pos)
	}
	return out, true
}

// LineShape is the geometry of one line for drawing the gym's network.
type LineShape struct {
	Line   lines.Key
	Color  string
	Coords [][]geo.Point
}

// LineShapes returns, for each line in set, its most common shapes today (up to 2 per direction),
// simplified further for overview drawing.
func (e *Engine) LineShapes(set lines.Set) []LineShape {
	s := e.today.Load()
	shapes := e.shapes.Load()
	if s == nil || shapes == nil {
		return nil
	}
	d := s.Static.Day
	type key struct {
		line  lines.Key
		shape string
	}
	count := map[key]int{}
	color := map[lines.Key]string{}
	for _, t := range d.Trips {
		r := d.Routes[t.Route]
		k := lines.Of(r.Type, r.ShortName)
		if !set[k] || t.Shape == "" {
			continue
		}
		count[key{k, t.Shape}]++
		color[k] = r.Color
	}
	byLine := map[lines.Key][]key{}
	for k := range count {
		byLine[k.line] = append(byLine[k.line], k)
	}
	var out []LineShape
	for line, ks := range byLine {
		sort.Slice(ks, func(a, b int) bool {
			if count[ks[a]] != count[ks[b]] {
				return count[ks[a]] > count[ks[b]]
			}
			return ks[a].shape < ks[b].shape
		})
		ls := LineShape{Line: line, Color: color[line]}
		for _, k := range ks {
			if len(ls.Coords) >= 4 {
				break
			}
			if pts, ok := (*shapes)[k.shape]; ok {
				ls.Coords = append(ls.Coords, geo.Simplify(pts, 15))
			}
		}
		if len(ls.Coords) > 0 {
			out = append(out, ls)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Line.String() < out[b].Line.String() })
	return out
}

// MapFile returns the path of the self-hosted basemap (PMTiles), if present.
func (e *Engine) MapFile() (string, bool) {
	p := filepath.Join(e.cfg.Server.DataDir, "map.pmtiles")
	fi, err := os.Stat(p)
	return p, err == nil && fi.Mode().IsRegular()
}
