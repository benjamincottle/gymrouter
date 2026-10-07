package engine

import (
	"math"
	"testing"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/gtfs"
)

// A bus out along a street to a turning loop and back the same way: getting on and off on the way back is a short
// ride, not out to the loop and back.
func TestLegGeometryAlongAStreetBothWays(t *testing.T) {
	m := 1.0 / 111320
	k := math.Cos(-33.8 * math.Pi / 180)
	at := func(x, y float64) geo.Point { return geo.Point{Lat: -33.8 + y*m, Lon: 151 + x*m/k} }
	d := &gtfs.Day{TripIndex: map[string][]int32{"t": {0}}}
	var calls []gtfs.StopTime
	// Kerbside stops: out on the north side, back on the south.
	for i, s := range []struct {
		x, y float64
		dist float32
	}{{0, 6, 0}, {1000, 6, 1000}, {2000, 0, 2000}, {1500, -6, 2500}, {1000, -6, 3000}, {0, -6, 4000}} {
		d.Stops = append(d.Stops, gtfs.Stop{ID: string(rune('a' + i)), Pos: at(s.x, s.y)})
		calls = append(calls, gtfs.StopTime{Stop: int32(i), Dist: s.dist})
	}
	d.Trips = []gtfs.Trip{{ID: "t", Shape: "s", StopTimes: calls}}
	street := []geo.Point{at(0, 0), at(1000, 0), at(2000, 0), at(1000, 0), at(0, 0)}
	e := &Engine{}
	snap := &Snapshot{Day: d}
	for _, c := range []struct {
		name string
		dist []float32
		long bool // drawn out to the loop and back
	}{
		{"by distance", []float32{0, 1000, 2000, 3000, 4000}, false},
		{"by position (no distances in the feed)", nil, true}, // why the feed's distances are used
	} {
		e.shapes.Store(&map[string]gtfs.Shape{"s": {Pts: street, Dist: c.dist}})
		path, stops, ok := e.LegGeometry(snap, "t", "e", "f") // 1000 m on the way back
		if !ok {
			t.Fatalf("%s: no geometry", c.name)
		}
		l := 0.0
		for i := 1; i < len(path); i++ {
			l += geo.DistanceM(path[i-1], path[i])
		}
		if got := l > 2000; got != c.long || (!c.long && math.Abs(l-1000) > 5) {
			t.Errorf("%s: path %.0f m: %v", c.name, l, path)
		}
		if len(stops) != 0 {
			t.Errorf("%s: stops passed %v", c.name, stops)
		}
	}
	// Stops passed are placed by distance too.
	e.shapes.Store(&map[string]gtfs.Shape{"s": {Pts: street, Dist: []float32{0, 1000, 2000, 3000, 4000}}})
	if _, stops, _ := e.LegGeometry(snap, "t", "c", "f"); len(stops) != 2 || geo.DistanceM(stops[0], at(1500, 0)) > 1 ||
		geo.DistanceM(stops[1], at(1000, 0)) > 1 {
		t.Errorf("stops passed %v", stops)
	}
}
