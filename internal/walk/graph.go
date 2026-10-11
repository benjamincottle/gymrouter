// Package walk routes pedestrians over a street network built from OpenStreetMap. It exists because
// "straight line times 1.3" is a poor guide to how long it takes to walk to a stop, in both directions.
//
// The graph keeps every node of every walkable way (no compression), as flat arrays: a few million
// nodes cost tens of megabytes.
package walk

import (
	"bufio"
	"errors"
	"fmt"
	"io"
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
	// Area is the box the network was clipped to when it was built from a larger extract; zero if it wasn't.
	Area geo.Box
	// Platforms are the railway platforms in the extract (see PlatformAt).
	Platforms []Platform
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
	if cost, ok := classify(tags); ok {
		b.addWay(cost, refs)
	}
}

// addWay records a way pedestrians can use, at its cost.
func (b *Builder) addWay(cost float32, refs []int64) {
	if len(refs) < 2 {
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

// errUnsorted: clipping needs the nodes in order of ID, which extracts made by osmium (Geofabrik's, BBBike's) are.
var errUnsorted = errors.New("walk: the extract's nodes aren't in order of ID, which clipping it to an area needs")

// FromPBF reads an OpenStreetMap extract and builds the walking graph: of the whole extract, or with clip, of the
// part of it inside that box, however much larger the extract is. A way that leaves the box is cut at its last node
// inside.
//
// Unclipped, the file is read for its ways first (to learn which nodes matter), then its nodes. Clipped, there is
// a pass before those, over the nodes, to learn which are inside the box, so that only the ways that touch it are
// kept: the memory the build needs then goes with the size of the box, not of the extract. Before the ways there is
// also a pass for the relations, which say which ways make up a platform drawn in pieces. In a sorted file (all the
// nodes, then all the ways, then relations) each pass skips the part before its own.
func FromPBF(path string, clip *geo.Box) (*Graph, error) {
	return fromPBF(path, clip, &Builder{}, true)
}

// fromPBF is FromPBF building with b, and with or without the short cuts a sorted file allows (the graph is the same
// either way; tests compare them).
func fromPBF(path string, clip *geo.Box, b *Builder, shortCuts bool) (*Graph, error) {
	until := func(u osmpbf.Until) osmpbf.Until {
		if shortCuts {
			return u
		}
		return osmpbf.UntilEnd
	}
	var lay osmpbf.Layout
	var inside idSet
	var plats platformWays
	if clip != nil {
		if err := readPBF(path, 0, osmpbf.Handler{WantNodes: true, Until: until(osmpbf.UntilWays), Layout: &lay, Node: func(n osmpbf.Node) error {
			if clip.Contains(n.Lat, n.Lon) && !inside.add(n.ID) {
				return errUnsorted
			}
			return nil
		}}); err != nil {
			return nil, err
		}
	}
	var waysAt int64 // where the ways begin, when the pass before found out
	if shortCuts && lay.Sorted && lay.WaysAt > 0 {
		waysAt = lay.WaysAt
	}
	// The platforms drawn in several pieces, before the ways: a relation says which ways are its edge, and those
	// ways don't say so themselves.
	if err := readPBF(path, waysAt, osmpbf.Handler{WantRelations: true, Sorted: lay.Sorted, Relation: func(r osmpbf.Relation) error {
		plats.relation(r.Tags, r.Members)
		return nil
	}}); err != nil {
		return nil, err
	}
	if err := readPBF(path, waysAt, osmpbf.Handler{WantWays: true, Until: until(osmpbf.UntilRelations), Sorted: lay.Sorted, Way: func(w osmpbf.Way) error {
		plats.member(w.ID, w.Refs)
		if clip == nil {
			b.AddWay(w.Tags, w.Refs)
			plats.add(w.Tags, w.Refs)
		} else if cost, ok := classify(w.Tags); ok && inside.hasAny(w.Refs) {
			b.addWay(cost, w.Refs)
			plats.add(w.Tags, w.Refs)
		} else if !ok && w.Tags["railway"] == "platform" && inside.hasAny(w.Refs) {
			plats.add(w.Tags, w.Refs) // a platform drawn as an area isn't a way to walk along, but it's still a platform
		}
		return nil
	}}); err != nil {
		return nil, err
	}
	inside = idSet{} // done with: the build needs the room
	b.Prepare()
	plats.prepare()
	if err := readPBF(path, 0, osmpbf.Handler{WantNodes: true, Until: until(osmpbf.UntilWays), Node: func(n osmpbf.Node) error {
		// A kept way's nodes outside the box get no position, which is where Build cuts it.
		if clip == nil || clip.Contains(n.Lat, n.Lon) {
			b.AddNode(n.ID, n.Lat, n.Lon)
			plats.node(n.ID, n.Lat, n.Lon)
		}
		return nil
	}}); err != nil {
		return nil, err
	}
	g, err := b.Build()
	if err != nil {
		return nil, err
	}
	if clip != nil {
		g.Area = *clip
	}
	g.Platforms = plats.platforms()
	return g, nil
}

// readPBF passes the file to h, from a block that begins `from` bytes in.
func readPBF(path string, from int64, h osmpbf.Handler) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if from > 0 {
		if _, err := f.Seek(from, io.SeekStart); err != nil {
			return err
		}
	}
	if err := osmpbf.Read(bufio.NewReaderSize(f, 1<<20), h); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
