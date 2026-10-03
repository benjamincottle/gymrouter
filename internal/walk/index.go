package walk

import (
	"math"
	"sort"

	"github.com/benjamincottle/gymrouter/internal/geo"
)

// index is a flat grid over nodes: cells of 0.001 degrees (about 100 m), sorted by key.
type index struct {
	keys  []uint64 // sorted cell keys
	start []int32  // per key, offset into nodes (len = len(keys)+1)
	nodes []int32
}

const cellDeg = 0.001

func cellKey(lat, lon float64) uint64 {
	return uint64(uint32(int32(math.Floor(lat/cellDeg))))<<32 | uint64(uint32(int32(math.Floor(lon/cellDeg))))
}

func newIndex(lat, lon []int32) index {
	n := len(lat)
	order := make([]int32, n)
	keyOf := make([]uint64, n)
	for i := range order {
		order[i] = int32(i)
		keyOf[i] = cellKey(float64(lat[i])/degScale, float64(lon[i])/degScale)
	}
	sort.Slice(order, func(a, b int) bool { return keyOf[order[a]] < keyOf[order[b]] })
	var ix index
	ix.nodes = order
	for i, o := range order {
		if i == 0 || keyOf[o] != keyOf[order[i-1]] {
			ix.keys = append(ix.keys, keyOf[o])
			ix.start = append(ix.start, int32(i))
		}
	}
	ix.start = append(ix.start, int32(n))
	return ix
}

func (ix *index) cell(key uint64) []int32 {
	i := sort.Search(len(ix.keys), func(i int) bool { return ix.keys[i] >= key })
	if i < len(ix.keys) && ix.keys[i] == key {
		return ix.nodes[ix.start[i]:ix.start[i+1]]
	}
	return nil
}

// nearest returns the closest node within maxM of p.
func (g *Graph) nearest(p geo.Point, maxM float64) (node int32, distM float64, ok bool) {
	rLat := int32(math.Ceil(maxM/111000/cellDeg)) + 1
	rLon := int32(math.Ceil(maxM/(111000*math.Cos(p.Lat*math.Pi/180))/cellDeg)) + 1
	cy, cx := int32(math.Floor(p.Lat/cellDeg)), int32(math.Floor(p.Lon/cellDeg))
	best := maxM + 1
	for dy := -rLat; dy <= rLat; dy++ {
		for dx := -rLon; dx <= rLon; dx++ {
			key := uint64(uint32(cy+dy))<<32 | uint64(uint32(cx+dx))
			for _, n := range g.idx.cell(key) {
				if d := geo.DistanceM(p, g.point(n)); d < best {
					best, node, ok = d, n, true
				}
			}
		}
	}
	return node, best, ok
}
