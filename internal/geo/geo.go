// Package geo provides small geographic helpers.
package geo

import (
	"math"
	"sort"
)

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

// Simplify reduces a polyline with the Douglas–Peucker algorithm, keeping points that deviate more
// than tolM metres from the simplified line. Endpoints are always kept. It returns the indexes of the kept points.
func Simplify(pts []Point, tolM float64) []int {
	if len(pts) < 3 {
		out := make([]int, len(pts))
		for i := range out {
			out[i] = i
		}
		return out
	}
	keep := make([]bool, len(pts))
	keep[0], keep[len(pts)-1] = true, true
	type span struct{ a, b int }
	stack := []span{{0, len(pts) - 1}}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		maxD, idx := 0.0, -1
		for i := s.a + 1; i < s.b; i++ {
			if d := crossTrackM(pts[i], pts[s.a], pts[s.b]); d > maxD {
				maxD, idx = d, i
			}
		}
		if idx >= 0 && maxD > tolM {
			keep[idx] = true
			stack = append(stack, span{s.a, idx}, span{idx, s.b})
		}
	}
	out := make([]int, 0, len(pts)/4+2)
	for i, k := range keep {
		if k {
			out = append(out, i)
		}
	}
	return out
}

// crossTrackM is the distance from p to segment ab in metres (equirectangular; fine at city scale).
func crossTrackM(p, a, b Point) float64 {
	k := math.Cos(a.Lat * math.Pi / 180)
	ax, ay := a.Lon*k, a.Lat
	bx, by := b.Lon*k, b.Lat
	px, py := p.Lon*k, p.Lat
	dx, dy := bx-ax, by-ay
	t := 0.0
	if l := dx*dx + dy*dy; l > 0 {
		t = math.Max(0, math.Min(1, ((px-ax)*dx+(py-ay)*dy)/l))
	}
	ex, ey := ax+t*dx-px, ay+t*dy-py
	return math.Sqrt(ex*ex+ey*ey) * 111320
}

// Along is a place on a line: segment Seg (pts[Seg] to pts[Seg+1]), T of the way along it (0..1), at Pt.
type Along struct {
	Seg int
	T   float64
	Pt  Point
}

// Before reports whether a comes before b on the line.
func (a Along) Before(b Along) bool { return a.Seg < b.Seg || (a.Seg == b.Seg && a.T < b.T) }

// Project finds the point on the line pts nearest to p, looking only at or after `from` (equirectangular; fine at
// city scale). A line of fewer than two points has no segments: it returns the first point.
func Project(pts []Point, p Point, from Along) Along {
	best, bestD := from, math.Inf(1)
	if len(pts) < 2 {
		if len(pts) == 1 {
			best.Pt = pts[0]
		}
		return best
	}
	for i := max(0, from.Seg); i < len(pts)-1; i++ {
		a, b := pts[i], pts[i+1]
		k := math.Cos(a.Lat * math.Pi / 180)
		dx, dy := (b.Lon-a.Lon)*k, b.Lat-a.Lat
		t := 0.0
		if l := dx*dx + dy*dy; l > 0 {
			t = math.Max(0, math.Min(1, ((p.Lon-a.Lon)*k*dx+(p.Lat-a.Lat)*dy)/l))
		}
		if i == from.Seg && t < from.T {
			t = from.T
		}
		q := Point{Lat: a.Lat + t*(b.Lat-a.Lat), Lon: a.Lon + t*(b.Lon-a.Lon)}
		if d := DistanceM(p, q); d < bestD {
			best, bestD = Along{Seg: i, T: t, Pt: q}, d
		}
	}
	return best
}

// PlaceLeg finds where a ride gets on (from) and off (to) along its route line. The line can pass the boarding stop
// more than once (a train out and back round a loop starts and ends in the same place), so every pass that comes
// within passM of the nearest is tried, and the one from which the alighting stop is closest, further along, wins.
// ok is false when no pass puts the alighting stop after the boarding one.
func PlaceLeg(pts []Point, from, to Point) (a, b Along, ok bool) {
	const passM = 50
	best := math.Inf(1)
	for _, c := range passes(pts, from, passM) {
		e := Project(pts, to, c)
		if !c.Before(e) {
			continue
		}
		if d := DistanceM(from, c.Pt) + DistanceM(to, e.Pt); d < best {
			a, b, ok, best = c, e, true, d
		}
	}
	return a, b, ok
}

// passes returns the closest point of each pass of the line near p: each run of segments that gets nearer and then
// further away, whose closest point is within slackM of the nearest overall.
func passes(pts []Point, p Point, slackM float64) []Along {
	if len(pts) < 2 {
		return []Along{Project(pts, p, Along{})}
	}
	n := len(pts) - 1
	at := make([]Along, n)
	d := make([]float64, n)
	bestD := math.Inf(1)
	for i := range n {
		at[i] = Project(pts[:i+2], p, Along{Seg: i}) // segment i only
		d[i] = DistanceM(p, at[i].Pt)
		bestD = math.Min(bestD, d[i])
	}
	var out []Along
	for i := range n {
		if d[i] <= bestD+slackM && (i == 0 || d[i] < d[i-1]) && (i == n-1 || d[i] <= d[i+1]) {
			out = append(out, at[i])
		}
	}
	return out
}

// AtDist finds the place x along the line, given how far along it each of its points is (dist, never decreasing, in
// the same unit as x). Before the first point or past the last, it's that end.
func AtDist(pts []Point, dist []float32, x float32) Along {
	if len(pts) < 2 {
		return Project(pts, Point{}, Along{})
	}
	i := min(max(sort.Search(len(dist), func(i int) bool { return dist[i] > x })-1, 0), len(pts)-2)
	t := 0.0
	if l := dist[i+1] - dist[i]; l > 0 {
		t = math.Max(0, math.Min(1, float64(x-dist[i])/float64(l)))
	}
	a, b := pts[i], pts[i+1]
	return Along{Seg: i, T: t, Pt: Point{Lat: a.Lat + t*(b.Lat-a.Lat), Lon: a.Lon + t*(b.Lon-a.Lon)}}
}

// Cut returns the part of the line from a to b (a before b).
func Cut(pts []Point, a, b Along) []Point {
	out := []Point{a.Pt}
	for i := a.Seg + 1; i <= b.Seg; i++ {
		out = append(out, pts[i])
	}
	return append(out, b.Pt)
}
