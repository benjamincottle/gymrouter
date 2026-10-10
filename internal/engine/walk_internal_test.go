package engine

import (
	"math"
	"testing"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/walk"
)

// Two streets 100 m apart with a gully between them, crossed 8 steps west of the home end (83 m a step).
func gullyEngine(t *testing.T) *Engine {
	t.Helper()
	b := &walk.Builder{MinComponent: 1}
	id := func(x, y int) int64 { return int64(1 + y*100 + x) }
	var south, north []int64
	for x := 0; x <= 20; x++ {
		south, north = append(south, id(x, 0)), append(north, id(x, 1))
	}
	b.AddWay(map[string]string{"highway": "residential"}, south)
	b.AddWay(map[string]string{"highway": "residential"}, north)
	b.AddWay(map[string]string{"highway": "footway"}, []int64{id(12, 0), id(12, 1)})
	b.Prepare()
	for y := 0; y <= 1; y++ {
		for x := 0; x <= 20; x++ {
			b.AddNode(id(x, y), gully(float64(x), float64(y)).Lat, gully(float64(x), float64(y)).Lon)
		}
	}
	g, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{}
	e.walker.Store(g)
	return e
}

func gully(x, y float64) geo.Point { return geo.Point{Lat: -33.9 + y*0.0009, Lon: 151.2 + x*0.0009} }

// A stop across the gully is near in a straight line but a long way on foot: it mustn't crowd out the stops that are
// nearer to walk to, and the walks to those are what the longest walk is measured against.
func TestNearbyMeasuresTheLongestWalkOnFoot(t *testing.T) {
	e := gullyEngine(t)
	home := gully(20, 0)
	across, mid, far := gully(20, 1), gully(10, 0), gully(5, 0) // ~1.4 km, ~830 m and ~1.25 km on foot
	kept := func(n nearby) map[int]float64 {
		out := map[int]float64{}
		for k, i := range n.idx {
			out[i] = n.metres[k]
		}
		return out
	}

	n := e.nearby(home, []geo.Point{across, mid, far}, 800)
	got := kept(n)
	if _, ok := got[0]; ok || len(got) != 2 || !n.streets {
		t.Fatalf("kept %v (streets %v); want the two on this side, not the one across the gully", got, n.streets)
	}
	if math.Abs(n.stretched-got[1]) > 1 || n.stretched <= 800 || n.nearest != 1 {
		t.Errorf("stretched %.0f m to candidate %d; the nearest on foot is candidate 1 at %.0f m, beyond the 800 m longest walk", n.stretched, n.nearest, got[1])
	}

	// Within the longest walk on foot: not stretched, and the band reaches 500 m past the nearest.
	near := gully(15, 0) // ~415 m
	got = kept(e.nearby(home, []geo.Point{across, mid, far, near}, 800))
	if _, ok := got[2]; len(got) != 2 || got[3] == 0 || got[1] == 0 || ok {
		t.Errorf("kept %v; want the stop ~415 m away and the one within 500 m further", got)
	}
	if s := e.nearby(home, []geo.Point{near}, 800).stretched; s != 0 {
		t.Errorf("stretched %.0f m with a stop within the longest walk", s)
	}
}
