// Package walk routes pedestrians over a street network built from OpenStreetMap. It exists because
// "straight line times 1.3" is a poor guide to how long it takes to walk to a stop, in both directions.
//
// The graph keeps every node of every walkable way (no compression), as flat arrays: a few million
// nodes cost tens of megabytes.
package walk

import (
	"errors"
	"fmt"
	"math"
	"os"
	"sort"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/osmpbf"
)

const (
	// degScale stores coordinates as int32 in units of 1e-7 degrees (about 1 cm).
	degScale = 1e7
	// minComponent drops tiny disconnected islands (a path inside a private compound) that a snap could land on.
	minComponent = 100
	// SnapM is how far from a street a point may be and still be placed on it.
	SnapM = 250
	// snapDetour makes the walk from a point to its nearest street node a little longer than the straight line.
	snapDetour = 1.2
)

// Graph is an undirected walking network.
type Graph struct {
	lat, lon []int32   // per node
	start    []int32   // CSR offsets, len = nodes+1
	to       []int32   // neighbour per half-edge
	cost     []float32 // effective metres per half-edge

	idx index
	// Bounds of the data, for deciding whether a place is covered.
	MinLat, MaxLat, MinLon, MaxLon float64
}

// Nodes returns the number of nodes.
func (g *Graph) Nodes() int { return len(g.lat) }

// Edges returns the number of half-edges (two per street segment).
func (g *Graph) Edges() int { return len(g.to) }

func (g *Graph) point(n int32) geo.Point {
	return geo.Point{Lat: float64(g.lat[n]) / degScale, Lon: float64(g.lon[n]) / degScale}
}

// Builder accumulates ways and node positions, then makes a Graph.
type Builder struct {
	refs []int64 // all walkable ways' node ids, concatenated
	ways []wayRec
	ids  []int64 // sorted unique node ids (after Prepare)
	lat  []int32
	lon  []int32
	have []bool

	// MinComponent overrides the smallest connected island kept (default minComponent); tests use small networks.
	MinComponent int
}

type wayRec struct {
	off, n int32
	cost   float32
}

// AddWay records a way if pedestrians can use it.
func (b *Builder) AddWay(tags map[string]string, refs []int64) {
	cost, ok := classify(tags)
	if !ok || len(refs) < 2 {
		return
	}
	b.ways = append(b.ways, wayRec{off: int32(len(b.refs)), n: int32(len(refs)), cost: cost})
	b.refs = append(b.refs, refs...)
}

// Prepare fixes the set of nodes the recorded ways need. Call it after all ways and before AddNode.
func (b *Builder) Prepare() {
	ids := append([]int64(nil), b.refs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := ids[:0]
	for i, id := range ids {
		if i == 0 || id != ids[i-1] {
			out = append(out, id)
		}
	}
	b.ids = out
	b.lat = make([]int32, len(out))
	b.lon = make([]int32, len(out))
	b.have = make([]bool, len(out))
}

// Wanted reports whether a node is used by a recorded way.
func (b *Builder) Wanted(id int64) bool {
	_, ok := b.find(id)
	return ok
}

func (b *Builder) find(id int64) (int, bool) {
	i := sort.Search(len(b.ids), func(i int) bool { return b.ids[i] >= id })
	return i, i < len(b.ids) && b.ids[i] == id
}

// AddNode records a node's position if a way uses it.
func (b *Builder) AddNode(id int64, lat, lon float64) {
	i, ok := b.find(id)
	if !ok {
		return
	}
	b.lat[i], b.lon[i], b.have[i] = int32(math.Round(lat*degScale)), int32(math.Round(lon*degScale)), true
}

// Build makes the graph. Ways with unknown nodes (outside the extract) are cut at the gap.
func (b *Builder) Build() (*Graph, error) {
	n := len(b.ids)
	if n == 0 {
		return nil, errors.New("walk: no walkable ways")
	}
	type half struct{ a, b int32 }
	var edges []half
	var costs []float32
	for _, w := range b.ways {
		prev := int32(-1)
		for _, id := range b.refs[w.off : w.off+w.n] {
			i, _ := b.find(id)
			cur := int32(i)
			if !b.have[cur] {
				prev = -1
				continue
			}
			if prev >= 0 && prev != cur {
				d := geo.DistanceM(geo.Point{Lat: float64(b.lat[prev]) / degScale, Lon: float64(b.lon[prev]) / degScale},
					geo.Point{Lat: float64(b.lat[cur]) / degScale, Lon: float64(b.lon[cur]) / degScale})
				edges = append(edges, half{prev, cur})
				costs = append(costs, float32(d)*w.cost)
			}
			prev = cur
		}
	}
	if len(edges) == 0 {
		return nil, errors.New("walk: no usable street segments")
	}

	// Keep only nodes in components of a useful size.
	parent := make([]int32, n)
	for i := range parent {
		parent[i] = int32(i)
	}
	var find func(int32) int32
	find = func(x int32) int32 {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	for _, e := range edges {
		ra, rb := find(e.a), find(e.b)
		if ra != rb {
			parent[ra] = rb
		}
	}
	size := make([]int32, n)
	for i := 0; i < n; i++ {
		size[find(int32(i))]++
	}
	minComp := b.MinComponent
	if minComp == 0 {
		minComp = minComponent
	}
	remap := make([]int32, n)
	g := &Graph{}
	for i := 0; i < n; i++ {
		remap[i] = -1
		if b.have[i] && size[find(int32(i))] >= int32(minComp) {
			remap[i] = int32(len(g.lat))
			g.lat, g.lon = append(g.lat, b.lat[i]), append(g.lon, b.lon[i])
		}
	}
	m := len(g.lat)
	if m == 0 {
		return nil, errors.New("walk: no connected street network")
	}
	deg := make([]int32, m+1)
	kept := 0
	for _, e := range edges {
		if remap[e.a] >= 0 && remap[e.b] >= 0 {
			deg[remap[e.a]]++
			deg[remap[e.b]]++
			kept++
		}
	}
	g.start = make([]int32, m+1)
	for i := 0; i < m; i++ {
		g.start[i+1] = g.start[i] + deg[i]
	}
	g.to = make([]int32, g.start[m])
	g.cost = make([]float32, g.start[m])
	fill := append([]int32(nil), g.start[:m]...)
	for k, e := range edges {
		a, c := remap[e.a], remap[e.b]
		if a < 0 || c < 0 {
			continue
		}
		g.to[fill[a]], g.cost[fill[a]] = c, costs[k]
		fill[a]++
		g.to[fill[c]], g.cost[fill[c]] = a, costs[k]
		fill[c]++
	}
	g.finish()
	return g, nil
}

// finish computes derived data (bounds and the spatial index).
func (g *Graph) finish() {
	g.MinLat, g.MinLon, g.MaxLat, g.MaxLon = 90, 180, -90, -180
	for i := range g.lat {
		p := g.point(int32(i))
		g.MinLat, g.MaxLat = math.Min(g.MinLat, p.Lat), math.Max(g.MaxLat, p.Lat)
		g.MinLon, g.MaxLon = math.Min(g.MinLon, p.Lon), math.Max(g.MaxLon, p.Lon)
	}
	g.idx = newIndex(g.lat, g.lon)
}

// FromPBF reads an OpenStreetMap extract and builds the walking graph. It reads the file twice: ways
// first (to learn which nodes matter), then nodes.
func FromPBF(path string) (*Graph, error) {
	var b Builder
	if err := readPBF(path, osmpbf.Handler{WantWays: true, Way: func(w osmpbf.Way) error {
		b.AddWay(w.Tags, w.Refs)
		return nil
	}}); err != nil {
		return nil, err
	}
	b.Prepare()
	if err := readPBF(path, osmpbf.Handler{WantNodes: true, Node: func(n osmpbf.Node) error {
		b.AddNode(n.ID, n.Lat, n.Lon)
		return nil
	}}); err != nil {
		return nil, err
	}
	return b.Build()
}

func readPBF(path string, h osmpbf.Handler) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := osmpbf.Read(f, h); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
