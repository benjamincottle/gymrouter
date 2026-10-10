package engine

import (
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/lines"
)

// shapeTolM is the simplification tolerance for route shapes (metres).
const shapeTolM = 4

// trackM is how near a train's track must pass the platforms of a ride to be the line drawn for it. The complete
// bundle's lines end within 7 m of their platforms; one that doesn't come this near isn't the line of this trip.
const trackM = 25

// shapeSet is the lines rides are drawn along.
type shapeSet struct {
	byID map[string]gtfs.Shape
	// track is the shape that follows a train's own track to its platforms, by trip. A train's own shape, from the
	// trains bundle, is one line for its whole route, whichever platform it uses: at Central that line passes up to
	// 140 m from the platform. The complete bundle has the same trip (most of them) with a shape along its track.
	track map[string]string
}

// LoadShapes reads route shapes for today's trips (trains from the trains bundle, with their tracks and the rest
// from the complete bundle). It's slow (the shapes file is ~1 GB), so Start runs it in the background;
// until it finishes, geometry falls back to straight lines between stops.
func (e *Engine) LoadShapes() {
	s := e.today.Load()
	if s == nil {
		return
	}
	start := e.now()
	wantTrains, wantOther, trains := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, t := range s.Static.Day.Trips {
		r := s.Static.Day.Routes[t.Route]
		byTrains := lines.ModeOf(r.Type) == lines.Train && e.paths.Trains != ""
		if byTrains {
			trains[t.ID] = true
		}
		if t.Shape == "" {
			continue
		}
		if byTrains {
			wantTrains[t.Shape] = true
		} else {
			wantOther[t.Shape] = true
		}
	}
	set := shapeSet{byID: map[string]gtfs.Shape{}}
	if len(trains) > 0 {
		track, err := gtfs.TripShapes(e.paths.Complete, trains)
		if err != nil {
			e.log.Warn("loading train tracks failed", "err", err)
		}
		set.track = track
		for _, id := range track {
			wantOther[id] = true
		}
	}
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
			set.byID[k] = v
		}
	}
	e.shapes.Store(&set)
	e.log.Info("shapes loaded", "shapes", len(set.byID), "train_tracks", len(set.track),
		"took", e.now().Sub(start).Round(time.Millisecond).String())
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
	calls := trip.StopTimes[fi : ti+1]
	if set := e.shapes.Load(); set != nil {
		// A train's track, when it is this ride's: the two bundles can disagree about a trip (one cut short for
		// trackwork, say), and then its track doesn't reach the platforms.
		if sh, ok := set.byID[set.track[trip.ID]]; ok {
			if path, stops, ok := besideLine(d, calls, sh.Pts, trackM); ok {
				return path, stops, true
			}
		}
		if sh, ok := set.byID[trip.Shape]; ok {
			if sh.Dist != nil && len(sh.Pts) > 1 && distsKnown(calls) {
				// The feed says how far along the shape each call is: cut there, and put the stops passed there too.
				// Placing stops by where they sit beside the line goes wrong where a bus runs both ways along a street.
				a, b := geo.AtDist(sh.Pts, sh.Dist, calls[0].Dist), geo.AtDist(sh.Pts, sh.Dist, calls[len(calls)-1].Dist)
				for _, st := range calls[1 : len(calls)-1] {
					stops = append(stops, geo.AtDist(sh.Pts, sh.Dist, st.Dist).Pt)
				}
				return geo.Cut(sh.Pts, a, b), stops, true
			}
			// No distances (the trains feed).
			if path, stops, ok := besideLine(d, calls, sh.Pts, math.Inf(1)); ok {
				return path, stops, true
			}
		}
	}
	for _, st := range calls[1 : len(calls)-1] {
		stops = append(stops, d.Stops[st.Stop].Pos)
	}
	path = make([]geo.Point, 0, len(calls))
	for _, st := range calls {
		path = append(path, d.Stops[st.Stop].Pos)
	}
	return path, stops, true
}

// besideLine cuts a line where the first and last of a ride's calls sit beside it, and puts the stops passed on it.
// It cuts beside each stop, not at the nearest corner: shapes are simplified, so the nearest corner can be past the
// stop and the line would run on and double back. PlaceLeg also copes with a shape that passes the boarding stop
// twice (Hornsby, on a train out round the North Shore and back). ok is false when the line doesn't make the ride,
// or passes further than withinM from where it starts or ends.
func besideLine(d *gtfs.Day, calls []gtfs.StopTime, pts []geo.Point, withinM float64) (path, stops []geo.Point, ok bool) {
	if len(pts) < 2 {
		return nil, nil, false
	}
	from, to := d.Stops[calls[0].Stop].Pos, d.Stops[calls[len(calls)-1].Stop].Pos
	a, b, ok := geo.PlaceLeg(pts, from, to)
	if !ok || geo.DistanceM(from, a.Pt) > withinM || geo.DistanceM(to, b.Pt) > withinM {
		return nil, nil, false
	}
	path = geo.Cut(pts, a, b)
	// Stop positions are at the kerb; put each one on the line, in order, so the map draws it on the line.
	at := geo.Along{}
	for _, st := range calls[1 : len(calls)-1] {
		at = geo.Project(path, d.Stops[st.Stop].Pos, at)
		stops = append(stops, at.Pt)
	}
	return path, stops, true
}

// distsKnown reports whether every call says how far along the shape it is, never going back, and the ride goes
// somewhere.
func distsKnown(calls []gtfs.StopTime) bool {
	for i, st := range calls {
		if !(st.Dist >= 0 && (i == 0 || st.Dist >= calls[i-1].Dist)) { // NaN (not given) fails too
			return false
		}
	}
	return calls[len(calls)-1].Dist > calls[0].Dist
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
