package api

import (
	"slices"
	"testing"

	"github.com/benjamincottle/gymrouter/internal/engine"
	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/raptor"
)

func TestChangePathRules(t *testing.T) {
	at := func(parent string, metresEast float64) gtfs.Stop {
		return gtfs.Stop{Parent: parent, Pos: geo.Point{Lat: -33.8, Lon: 151.2 + metresEast/92000}}
	}
	// A route of the given length (metres) between the two points, as a two-point line with a detour in the middle.
	route := func(length float64) func(a, b geo.Point) ([]geo.Point, bool) {
		return func(a, b geo.Point) ([]geo.Point, bool) {
			mid := geo.Point{Lat: a.Lat + (length/2-geo.DistanceM(a, b)/2)/111000, Lon: (a.Lon + b.Lon) / 2}
			return []geo.Point{a, mid, b}, true
		}
	}
	none := func(a, b geo.Point) ([]geo.Point, bool) { return nil, false }

	for name, tc := range map[string]struct {
		a, b gtfs.Stop
		walk func(a, b geo.Point) ([]geo.Point, bool)
		want bool
	}{
		"different stops, drawn":                   {at("A", 0), at("B", 300), route(420), true},
		"different stops, short, still drawn":      {at("A", 0), at("B", 20), route(25), true},
		"stops with no parent, drawn":              {at("", 0), at("", 150), route(180), true},
		"platform to platform in a station":        {at("S", 0), at("S", 16), route(20), false},
		"platform to a bus stand outside":          {at("S", 0), at("S", 200), route(260), true},
		"in a station but the route loops far out": {at("S", 0), at("S", 100), route(900), false},
		"no street route":                          {at("A", 0), at("B", 300), none, false},
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := changePath(tc.a, tc.b, tc.walk); ok != tc.want {
				t.Errorf("drawn = %v, want %v", ok, tc.want)
			}
		})
	}
}

// A walk joined from a change and the final walk is drawn through the stop between them, each piece along the
// streets where there's a route and straight where there isn't.
func TestWalkLegPathGoesThroughTheStopsItPasses(t *testing.T) {
	stop := func(east float64) gtfs.Stop { return gtfs.Stop{Pos: geo.Point{Lat: -33.8, Lon: 151.2 + east/92000}} }
	d := &gtfs.Day{Stops: []gtfs.Stop{stop(0), stop(300)}}
	gym := engine.Approach{Place: geo.Point{Lat: -33.81, Lon: 151.2}}
	bend := geo.Point{Lat: -33.799, Lon: 151.2015}
	walk := func(a, b geo.Point) ([]geo.Point, bool) { return []geo.Point{a, bend, b}, true }

	// Off at stop 0, along the streets to stop 1, then (no street route known) straight on to the gym.
	pts, ok := walkLegPath(d, raptor.Leg{Kind: raptor.Walk, From: 0, To: -1, Via: []int32{1}}, engine.Approach{}, gym, walk)
	want := []geo.Point{d.Stops[0].Pos, bend, d.Stops[1].Pos, gym.Place}
	if !ok || !slices.Equal(pts, want) {
		t.Errorf("through the stop: %v %v, want %v", pts, ok, want)
	}
	// A single walk with no route is left to the map's straight line.
	if pts, ok := walkLegPath(d, raptor.Leg{Kind: raptor.Walk, From: 1, To: -1}, engine.Approach{}, gym, walk); ok {
		t.Errorf("no route: got %v", pts)
	}
}
