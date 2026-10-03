package walk

import (
	"container/heap"

	"github.com/benjamincottle/gymrouter/internal/geo"
)

// Reach holds the walking cost from one place to everything within a limit.
type Reach struct {
	g      *Graph
	origin geo.Point
	snap   float64 // straight-line metres from the origin to its street node (already weighted)
	dist   map[int32]float32
	prev   map[int32]int32
	node   int32
}

type item struct {
	d float32
	n int32
}
type pq []item

func (h pq) Len() int           { return len(h) }
func (h pq) Less(i, j int) bool { return h[i].d < h[j].d }
func (h pq) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *pq) Push(x any)        { *h = append(*h, x.(item)) }
func (h *pq) Pop() any          { o := *h; n := len(o); x := o[n-1]; *h = o[:n-1]; return x }

// From finds how far each street node within maxM (effective metres, including the walk onto the street) is from p.
// ok is false when p is too far from any street (outside the extract, or on water).
func (g *Graph) From(p geo.Point, maxM float64) (*Reach, bool) {
	n, d, ok := g.nearest(p, SnapM)
	if !ok {
		return nil, false
	}
	r := &Reach{g: g, origin: p, snap: d * snapDetour, dist: map[int32]float32{}, prev: map[int32]int32{}, node: n}
	limit := float32(maxM - r.snap)
	if limit < 0 {
		return r, true
	}
	r.dist[n] = 0
	h := &pq{{0, n}}
	done := map[int32]bool{}
	for h.Len() > 0 {
		it := heap.Pop(h).(item)
		if done[it.n] {
			continue
		}
		done[it.n] = true
		for e := r.g.start[it.n]; e < r.g.start[it.n+1]; e++ {
			nd := it.d + g.cost[e]
			if nd > limit {
				continue
			}
			if old, seen := r.dist[g.to[e]]; !seen || nd < old {
				r.dist[g.to[e]] = nd
				r.prev[g.to[e]] = it.n
				heap.Push(h, item{nd, g.to[e]})
			}
		}
	}
	return r, true
}

// Metres returns the effective walking distance from the origin to q (street distance plus the walk onto and
// off the street at either end). ok is false if q is unreachable within the limit.
func (r *Reach) Metres(q geo.Point) (float64, bool) {
	n, d, ok := r.g.nearest(q, SnapM)
	if !ok {
		return 0, false
	}
	dd, ok := r.dist[n]
	if !ok {
		return 0, false
	}
	return r.snap + float64(dd) + d*snapDetour, true
}

// Path returns the streets walked from the origin to q as a polyline (origin and q included).
func (r *Reach) Path(q geo.Point) ([]geo.Point, bool) {
	n, _, ok := r.g.nearest(q, SnapM)
	if !ok {
		return nil, false
	}
	if _, ok := r.dist[n]; !ok {
		return nil, false
	}
	var rev []geo.Point
	for cur := n; ; {
		rev = append(rev, r.g.point(cur))
		if cur == r.node {
			break
		}
		cur = r.prev[cur]
	}
	out := make([]geo.Point, 0, len(rev)+2)
	out = append(out, r.origin)
	for i := len(rev) - 1; i >= 0; i-- {
		out = append(out, rev[i])
	}
	return append(out, q), true
}
