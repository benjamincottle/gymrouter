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

func TestGridWithinAndNearest(t *testing.T) {
	pts := []Point{{-33.8, 151.0}, {-33.801, 151.0}, {-33.9, 151.0}}
	g := NewGrid(pts, 500)
	var got []int32
	g.Within(Point{-33.8, 151.0}, 200, func(i int32, _ float64) { got = append(got, i) })
	if len(got) != 2 {
		t.Errorf("within: %v", got)
	}
	if Nearest(pts, Point{-33.899, 151.0}, 0) != 2 || Nearest(pts, Point{-33.8, 151.0}, 1) != 1 {
		t.Error("nearest")
	}
}
