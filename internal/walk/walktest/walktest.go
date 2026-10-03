// Package walktest builds small street networks for tests.
package walktest

import (
	"testing"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/walk"
)

// Grid returns a street network made of a square grid of residential streets (41 by 41 nodes, 0.0005 degrees
// apart: about 55 m north-south and 46 m east-west) around each centre.
func Grid(t testing.TB, centres ...geo.Point) *walk.Graph {
	t.Helper()
	const n, d = 41, 0.0005
	b := &walk.Builder{MinComponent: 1}
	id := func(c, i, j int) int64 { return int64(1 + c*10_000_000 + i*1000 + j) }
	for c := range centres {
		for i := 0; i < n; i++ {
			var row, col []int64
			for j := 0; j < n; j++ {
				row, col = append(row, id(c, i, j)), append(col, id(c, j, i))
			}
			b.AddWay(map[string]string{"highway": "residential"}, row)
			b.AddWay(map[string]string{"highway": "residential"}, col)
		}
	}
	b.Prepare()
	for c, ctr := range centres {
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				b.AddNode(id(c, i, j), ctr.Lat+float64(i-n/2)*d, ctr.Lon+float64(j-n/2)*d)
			}
		}
	}
	g, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	return g
}
