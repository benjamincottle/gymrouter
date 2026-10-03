package api

import (
	"testing"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/gtfs"
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
