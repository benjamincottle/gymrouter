package engine

import (
	"math"
	"sync"
	"sync/atomic"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/lines"
	"github.com/benjamincottle/gymrouter/internal/raptor"
	"github.com/benjamincottle/gymrouter/internal/stand"
	"github.com/benjamincottle/gymrouter/internal/walk"
)

// stationsOf is a day's stations indexed for boarding advice, with the advice already worked out. It's kept while the
// day's stops and pathways and the street network are the same ones: a day is copied each time live times are
// applied to it, but those are shared between the copies, and the advice doesn't depend on the times.
type stationsOf struct {
	stops    *gtfs.Stop
	pathways int
	walker   *walk.Graph
	st       *stand.Stations
	memo     sync.Map // boardKey -> boardAnswer
	held     atomic.Int32
}

// boardKey is everything a ride's advice depends on. A trip being followed asks for the same few again and again.
type boardKey struct {
	trip           string
	from, to, then int32
	stay           bool
	lat, lon       int32 // where the trip ends, to about 10 m
}

type boardAnswer struct {
	adv stand.Advice
	ok  bool
}

// maxBoardMemo bounds the answers kept; past it they're dropped and worked out again as asked for.
const maxBoardMemo = 4096

func (e *Engine) stationsFor(d *gtfs.Day, g *walk.Graph) *stationsOf {
	if len(d.Stops) == 0 {
		return nil
	}
	if c := e.stations.Load(); c != nil && c.stops == &d.Stops[0] && c.pathways == len(d.Pathways) && c.walker == g {
		return c
	}
	c := &stationsOf{stops: &d.Stops[0], pathways: len(d.Pathways), walker: g, st: stand.NewStations(d)}
	e.stations.Store(c)
	return c
}

// headingM is how much of a ride's line, from the platform, says which way the train runs there.
const headingM = 150

// Boarding works out where to board the ride that is leg i of a trip so as to step off beside the way on: the
// carriage, and where on the platform it stops. place is where the trip ends. ok is false for anything but a train or
// metro ride, and when there's nothing to say or nothing known (see stand.Advise).
func (e *Engine) Boarding(s *Snapshot, legs []raptor.Leg, i int, place geo.Point) (stand.Advice, bool) {
	g := e.Walker()
	if g == nil || i < 0 || i >= len(legs) || legs[i].Kind == raptor.Walk {
		return stand.Advice{}, false
	}
	d := s.Day
	l := legs[i]
	route := d.Routes[l.Route]
	mode := lines.Of(route.Type, route.ShortName).Mode
	if mode != lines.Train && mode != lines.RegionalTrain && mode != lines.Metro {
		return stand.Advice{}, false
	}
	trip := &d.Trips[l.Trip]
	r := stand.Ride{Board: l.From, Alight: l.To, Metro: mode == lines.Metro, Then: -1, Goal: place}
	if r.Cars = stand.Cars(trip.ID, r.Metro); r.Cars == 0 {
		return stand.Advice{}, false
	}
	switch {
	case i+1 >= len(legs):
		return stand.Advice{}, false // a trip doesn't end on a train
	case legs[i+1].Kind != raptor.Walk:
		r.Stay = true
	case legs[i+1].To >= 0:
		r.Then, r.Goal = legs[i+1].To, d.Stops[legs[i+1].To].Pos
	}
	c := e.stationsFor(d, g)
	if c == nil {
		return stand.Advice{}, false
	}
	key := boardKey{trip: trip.ID, from: l.From, to: l.To, then: r.Then, stay: r.Stay, lat: int32(math.Round(r.Goal.Lat * 1e4)), lon: int32(math.Round(r.Goal.Lon * 1e4))}
	if a, ok := c.memo.Load(key); ok {
		return a.(boardAnswer).adv, a.(boardAnswer).ok
	}
	adv, ok := e.board(s, c, trip, r)
	if c.held.Add(1) > maxBoardMemo {
		c.memo.Clear()
		c.held.Store(0)
	}
	c.memo.Store(key, boardAnswer{adv, ok})
	return adv, ok
}

// board works out a ride's advice: which way the train runs at each platform, from its line, then where to board.
func (e *Engine) board(s *Snapshot, c *stationsOf, trip *gtfs.Trip, r stand.Ride) (stand.Advice, bool) {
	d := s.Day
	path, _, ok := e.LegGeometry(s, trip.ID, d.Stops[r.Board].ID, d.Stops[r.Alight].ID)
	if !ok || len(path) < 2 {
		return stand.Advice{}, false
	}
	r.HeadingBoard = heading(path[0], along(path, headingM))
	back := make([]geo.Point, len(path))
	for k, p := range path {
		back[len(path)-1-k] = p
	}
	r.HeadingAlight = heading(along(back, headingM), back[0])
	return c.st.Advise(c.walker, r)
}

// along is the point so many metres along a line from its start, or its end if it's shorter.
func along(pts []geo.Point, m float64) geo.Point {
	for k := 1; k < len(pts); k++ {
		d := geo.DistanceM(pts[k-1], pts[k])
		if d >= m && d > 0 {
			t := m / d
			return geo.Point{Lat: pts[k-1].Lat + (pts[k].Lat-pts[k-1].Lat)*t, Lon: pts[k-1].Lon + (pts[k].Lon-pts[k-1].Lon)*t}
		}
		m -= d
	}
	return pts[len(pts)-1]
}

// heading is the direction from a to b, in degrees clockwise from north.
func heading(a, b geo.Point) float64 {
	x := (b.Lon - a.Lon) * math.Cos(a.Lat*math.Pi/180)
	return math.Mod(math.Atan2(x, b.Lat-a.Lat)*180/math.Pi+360, 360)
}
