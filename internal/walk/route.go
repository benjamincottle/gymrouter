package walk

import (
	"math"

	"github.com/benjamincottle/gymrouter/internal/geo"
)

// Reach holds the walking cost from one place to everything within a limit.
type Reach struct {
	g      *Graph
	origin geo.Point
	snap   float64 // straight-line metres from the origin to its street node (already weighted)
	nodes  map[int32]reached
	node   int32
}

// reached is a street node's walking cost from the origin and the node before it on the way.
type reached struct {
	d    float32
	prev int32
}

type item struct {
	d float32
	n int32
}

// pq is a binary min-heap on d (container/heap's algorithm, typed so a push doesn't allocate).
type pq []item

func (h *pq) push(x item) {
	*h = append(*h, x)
	q := *h
	for j := len(q) - 1; j > 0; {
		i := (j - 1) / 2
		if i == j || !(q[j].d < q[i].d) {
			break
		}
		q[i], q[j] = q[j], q[i]
		j = i
	}
}

func (h *pq) pop() item {
	q := *h
	n := len(q) - 1
	q[0], q[n] = q[n], q[0]
	for i := 0; ; {
		j := 2*i + 1
		if j >= n || j < 0 {
			break
		}
		if j2 := j + 1; j2 < n && q[j2].d < q[j].d {
			j = j2
		}
		if !(q[j].d < q[i].d) {
			break
		}
		q[i], q[j] = q[j], q[i]
		i = j
	}
	*h = q[:n]
	return q[n]
}

// From finds how far each street node within maxM (effective metres, including the walk onto the street) is from p.
// ok is false when p is too far from any street (outside the extract, or on water).
func (g *Graph) From(p geo.Point, maxM float64) (*Reach, bool) {
	return g.search(p, maxM, nil, 0)
}

// FromNearest walks out from p only as far as it takes to find the nearest of targets along the streets and every
// target up to slackM further, and never beyond maxM. Distances beyond that aren't final, so Metres is only to be
// trusted for targets within the nearest plus slackM (any further target reads as further than that, or unreachable).
// ok is false when p is too far from any street.
func (g *Graph) FromNearest(p geo.Point, targets []geo.Point, slackM, maxM float64) (*Reach, bool) {
	off := map[int32]float32{} // a target's street node, and the walk from it to the target
	for _, q := range targets {
		if n, d, ok := g.nearest(q, SnapM); ok {
			if o, seen := off[n]; !seen || float32(d*snapDetour) < o {
				off[n] = float32(d * snapDetour)
			}
		}
	}
	return g.search(p, maxM, off, float32(slackM))
}

// search is a Dijkstra out from p, bounded by maxM. With targets (street node → the walk on from it), it stops once
// every node left is further than the nearest target found plus slack.
func (g *Graph) search(p geo.Point, maxM float64, targets map[int32]float32, slack float32) (*Reach, bool) {
	n, d, ok := g.nearest(p, SnapM)
	if !ok {
		return nil, false
	}
	r := &Reach{g: g, origin: p, snap: d * snapDetour, nodes: map[int32]reached{}, node: n}
	limit := float32(maxM - r.snap)
	if limit < 0 {
		return r, true
	}
	best := float32(math.Inf(1)) // street distance (from the origin's node) to the nearest target, walk off included
	r.nodes[n] = reached{0, -1}
	h := pq{{0, n}}
	for len(h) > 0 {
		it := h.pop()
		if it.d > r.nodes[it.n].d {
			continue // a stale entry: the node was reached more cheaply since
		}
		if targets != nil {
			if it.d > best+slack {
				break // every target not yet reached is further than the nearest plus slack
			}
			if o, ok := targets[it.n]; ok {
				best = min(best, it.d+o)
			}
		}
		for e := g.start[it.n]; e < g.start[it.n+1]; e++ {
			nd := it.d + g.cost[e]
			if nd > limit {
				continue
			}
			if old, seen := r.nodes[g.to[e]]; !seen || nd < old.d {
				r.nodes[g.to[e]] = reached{nd, it.n}
				h.push(item{nd, g.to[e]})
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
	rn, ok := r.nodes[n]
	if !ok {
		return 0, false
	}
	return r.snap + float64(rn.d) + d*snapDetour, true
}

// Path returns the streets walked from the origin to q as a polyline (origin and q included).
func (r *Reach) Path(q geo.Point) ([]geo.Point, bool) {
	n, _, ok := r.g.nearest(q, SnapM)
	if !ok {
		return nil, false
	}
	if _, ok := r.nodes[n]; !ok {
		return nil, false
	}
	var rev []geo.Point
	for cur := n; ; {
		rev = append(rev, r.g.point(cur))
		if cur == r.node {
			break
		}
		cur = r.nodes[cur].prev
	}
	out := make([]geo.Point, 0, len(rev)+2)
	out = append(out, r.origin)
	for i := len(rev) - 1; i >= 0; i-- {
		out = append(out, rev[i])
	}
	return append(out, q), true
}
