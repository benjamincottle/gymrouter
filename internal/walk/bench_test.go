package walk

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/benjamincottle/gymrouter/internal/geo"
)

// The walking benchmarks run over a real street network, which isn't in the repo: point GYMROUTER_BENCH_DATA at a
// server data directory holding walk.graph.

var benchGraph struct {
	once sync.Once
	g    *Graph
	err  error
}

func loadBenchGraph(b *testing.B) *Graph {
	b.Helper()
	dir := os.Getenv("GYMROUTER_BENCH_DATA")
	if dir == "" {
		b.Skip("GYMROUTER_BENCH_DATA not set")
	}
	benchGraph.once.Do(func() { benchGraph.g, benchGraph.err = Load(filepath.Join(dir, "walk.graph")) })
	if benchGraph.err != nil {
		b.Fatal(benchGraph.err)
	}
	return benchGraph.g
}

// BenchmarkFrom walks out from the Lane Cove gym as far as a plan does for a place without curated stops (1 km
// straight line, so 2.5 km along the streets), then times and draws the walk to a stop.
func BenchmarkFrom(b *testing.B) {
	g := loadBenchGraph(b)
	gym := geo.Point{Lat: -33.807948, Lon: 151.150629}
	stop := geo.Point{Lat: -33.80435, Lon: 151.1561}
	for b.Loop() {
		r, ok := g.From(gym, 2500)
		if !ok {
			b.Fatal("gym not on the network")
		}
		if _, ok := r.Metres(stop); !ok {
			b.Fatal("stop unreachable")
		}
		r.Path(stop)
	}
}

// BenchmarkFromFar is the curated-place case: paths drawn within up to 6 km.
func BenchmarkFromFar(b *testing.B) {
	g := loadBenchGraph(b)
	gym := geo.Point{Lat: -33.807948, Lon: 151.150629}
	for b.Loop() {
		if _, ok := g.From(gym, 6000); !ok {
			b.Fatal("gym not on the network")
		}
	}
}
