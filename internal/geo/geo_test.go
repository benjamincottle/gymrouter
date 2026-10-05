package geo

import (
	"math"
	"testing"
)

func TestDistance(t *testing.T) {
	// ~1.11 km per 0.01° latitude
	d := DistanceM(Point{-33.80, 151.0}, Point{-33.81, 151.0})
	if math.Abs(d-1112) > 5 {
		t.Errorf("distance %f", d)
	}
}

func TestSimplifyKeepsShape(t *testing.T) {
	// A straight line with tiny wiggles and one real corner.
	var pts []Point
	for i := 0; i <= 50; i++ {
		pts = append(pts, Point{-33.8 + float64(i)*0.0001, 151.0 + float64(i%2)*0.000001})
	}
	corner := Point{-33.795, 151.01}
	pts = append(pts, corner)
	s := Simplify(pts, 5)
	if len(s) != 3 || s[0] != pts[0] || s[1] != pts[50] || s[2] != corner {
		t.Fatalf("simplified to %d points: %v", len(s), s)
	}
}

func TestGridWithin(t *testing.T) {
	pts := []Point{{-33.8, 151.0}, {-33.801, 151.0}, {-33.9, 151.0}}
	g := NewGrid(pts, 500)
	var got []int32
	g.Within(Point{-33.8, 151.0}, 200, func(i int32, _ float64) { got = append(got, i) })
	if len(got) != 2 {
		t.Errorf("within: %v", got)
	}
}

func TestProjectAndCutStopBesideTheLine(t *testing.T) {
	// A straight road east with corners every ~1 km; a stop beside it 300 m before the third corner.
	m := 1.0 / 111320
	k := math.Cos(-33.8 * math.Pi / 180)
	at := func(x, y float64) Point { return Point{Lat: -33.8 + y*m, Lon: 151 + x*m/k} }
	road := []Point{at(0, 0), at(1000, 0), at(2000, 0), at(3000, 0)}
	from := Project(road, at(250, 8), Along{})
	to := Project(road, at(1700, -6), from)
	if from.Seg != 0 || math.Abs(from.T-0.25) > 0.01 || to.Seg != 1 || math.Abs(to.T-0.7) > 0.01 || !from.Before(to) {
		t.Fatalf("projections %+v %+v", from, to)
	}
	cut := Cut(road, from, to)
	if len(cut) != 3 || DistanceM(cut[2], at(1700, 0)) > 1 {
		t.Fatalf("cut %v", cut)
	}
	// The cut never goes past the stop and back: each point is further east than the last.
	for i := 1; i < len(cut); i++ {
		if cut[i].Lon <= cut[i-1].Lon {
			t.Fatalf("doubles back at %d: %v", i, cut)
		}
	}
	// Looking only after a point: a stop "behind" it lands on that point, not earlier.
	if back := Project(road, at(100, 0), to); back.Before(to) {
		t.Errorf("projected before the starting point: %+v", back)
	}
}

func TestPlaceLegOnALoop(t *testing.T) {
	// A loop round a 2 km square, starting and ending at the same corner, like a train that runs out and back round a
	// loop. Boarding at that corner means the start of the loop, not its end, even when the end is a little nearer.
	m := 1.0 / 111320
	k := math.Cos(-33.8 * math.Pi / 180)
	at := func(x, y float64) Point { return Point{Lat: -33.8 + y*m, Lon: 151 + x*m/k} }
	loop := []Point{at(0, 0), at(2000, 0), at(2000, 2000), at(0, 2000), at(0, 1)}
	from, to := at(-3, 3), at(1000, 2005)
	if got := Project(loop, from, Along{}); got.Seg != 3 {
		t.Fatalf("test setup: on its own, the boarding stop should be nearest the loop's end: %+v", got)
	}
	a, b, ok := PlaceLeg(loop, from, to)
	if !ok || a.Seg != 0 || b.Seg != 2 || !a.Before(b) {
		t.Fatalf("leg placed at %+v to %+v (ok %v)", a, b, ok)
	}
	// Getting off back at the start corner is the loop's end.
	if _, b, ok := PlaceLeg(loop, at(2000, 1000), at(-3, 3)); !ok || b.Seg != 3 {
		t.Fatalf("back to the start: %+v (ok %v)", b, ok)
	}
}
