// Package geo provides small geographic helpers.
package geo

import "math"

const earthRadiusM = 6371000.0

// Point is a WGS84 coordinate.
type Point struct {
	Lat, Lon float64
}

// DistanceM returns the great-circle distance between a and b in metres.
func DistanceM(a, b Point) float64 {
	la1, la2 := a.Lat*math.Pi/180, b.Lat*math.Pi/180
	dLat := la2 - la1
	dLon := (b.Lon - a.Lon) * math.Pi / 180
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(la1)*math.Cos(la2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusM * math.Asin(math.Min(1, math.Sqrt(h)))
}

// Grid is a coarse spatial index for nearby-point lookups.
type Grid struct {
	cell  float64
	cells map[[2]int32][]int32
	pts   []Point
}

// NewGrid indexes pts using square cells of roughly cellM metres.
func NewGrid(pts []Point, cellM float64) *Grid {
	g := &Grid{cell: cellM / 111000.0, cells: make(map[[2]int32][]int32), pts: pts}
	for i, p := range pts {
		k := g.key(p)
		g.cells[k] = append(g.cells[k], int32(i))
	}
	return g
}

func (g *Grid) key(p Point) [2]int32 {
	return [2]int32{int32(math.Floor(p.Lat / g.cell)), int32(math.Floor(p.Lon / g.cell))}
}

// Within calls fn for every indexed point within radiusM of p.
func (g *Grid) Within(p Point, radiusM float64, fn func(i int32, distM float64)) {
	k := g.key(p)
	ry := int32(math.Ceil(radiusM/(g.cell*111000.0))) + 1
	rx := int32(math.Ceil(radiusM/(g.cell*111000.0*math.Cos(p.Lat*math.Pi/180)))) + 1
	for dy := -ry; dy <= ry; dy++ {
		for dx := -rx; dx <= rx; dx++ {
			for _, i := range g.cells[[2]int32{k[0] + dy, k[1] + dx}] {
				if d := DistanceM(p, g.pts[i]); d <= radiusM {
					fn(i, d)
				}
			}
		}
	}
}
