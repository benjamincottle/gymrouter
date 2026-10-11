package walk

import (
	"math"
	"sort"
	"strings"

	"github.com/benjamincottle/gymrouter/internal/geo"
)

// Platform is a railway platform's outline, as OpenStreetMap draws it: where you stand to wait for a train.
type Platform struct {
	// Ref is the platform numbers it carries, as tagged: "3", or "2;3" for an island with a face each side.
	Ref string
	// Outline is its edge; the last point repeats the first when it's drawn as an area (nearly all are).
	Outline []geo.Point
}

// How far a platform's position in the timetable may be from an outline and still be that platform: PlatformSnapM,
// or PlatformNamedSnapM when the outline carries the platform's number (the timetable's point isn't always on the
// platform: Circular Quay's are 15 to 25 m off theirs).
const (
	PlatformSnapM      = 15
	PlatformNamedSnapM = 30
)

// PlatformAt returns the outline of the platform at a position (the timetable's point for it): the nearest in reach,
// preferring one that carries the platform number `code` when it's known, since an island's two faces and the
// platform across the track are all close.
func (g *Graph) PlatformAt(pos geo.Point, code string) ([]geo.Point, bool) {
	best, bestD, bestNamed := -1, math.Inf(1), false
	for i, p := range g.Platforms {
		d := outlineDistM(p.Outline, pos)
		named := code != "" && carries(p.Ref, code)
		if d > PlatformSnapM && !(named && d <= PlatformNamedSnapM) {
			continue
		}
		if (named && !bestNamed) || (named == bestNamed && d < bestD) {
			best, bestD, bestNamed = i, d, named
		}
	}
	if best < 0 {
		return nil, false
	}
	return g.Platforms[best].Outline, true
}

// carries reports whether a platform's ref ("2;3", "1, 2") lists a number.
func carries(ref, code string) bool {
	for _, r := range strings.FieldsFunc(ref, func(c rune) bool { return c == ';' || c == ',' || c == '/' }) {
		if strings.TrimSpace(r) == code {
			return true
		}
	}
	return false
}

// outlineDistM is how far a position is from an outline, in metres: zero inside a closed one.
func outlineDistM(out []geo.Point, p geo.Point) float64 {
	if len(out) == 0 {
		return math.Inf(1)
	}
	// Metres east and north of p: a platform is small enough for a flat map.
	k := math.Cos(p.Lat*math.Pi/180) * 111_320
	xy := func(q geo.Point) (float64, float64) { return (q.Lon - p.Lon) * k, (q.Lat - p.Lat) * 110_540 }
	best, inside := math.Inf(1), false
	closed := len(out) > 2 && out[0] == out[len(out)-1]
	for i := 0; i+1 < len(out); i++ {
		ax, ay := xy(out[i])
		bx, by := xy(out[i+1])
		dx, dy := bx-ax, by-ay
		t := 0.0
		if l := dx*dx + dy*dy; l > 0 {
			t = math.Max(0, math.Min(1, -(ax*dx+ay*dy)/l))
		}
		best = math.Min(best, math.Hypot(ax+t*dx, ay+t*dy))
		// A ray east from p: an odd number of edges crossed means p is inside.
		if (ay > 0) != (by > 0) && ax+(0-ay)/(by-ay)*dx > 0 {
			inside = !inside
		}
	}
	if len(out) == 1 {
		best = math.Hypot(xy(out[0]))
	}
	if closed && inside {
		return 0
	}
	return best
}

// platformWays gathers the platforms among an extract's ways, then the positions of their nodes: they aren't all
// walkable ways (an area isn't), so the graph's builder doesn't see them all.
type platformWays struct {
	ways []platformWay
	ids  []int64 // their nodes, sorted (after prepare)
	pos  []geo.Point
	have []bool
}

type platformWay struct {
	ref  string
	refs []int64
}

// add notes a way if it's a platform. Its refs are copied: they're only lent.
func (pw *platformWays) add(tags map[string]string, refs []int64) {
	if tags["railway"] == "platform" && len(refs) >= 2 {
		pw.ways = append(pw.ways, platformWay{ref: tags["ref"], refs: append([]int64(nil), refs...)})
	}
}

// prepare fixes the nodes wanted. Call it after the ways and before the nodes.
func (pw *platformWays) prepare() {
	for _, w := range pw.ways {
		pw.ids = append(pw.ids, w.refs...)
	}
	sort.Slice(pw.ids, func(i, j int) bool { return pw.ids[i] < pw.ids[j] })
	out := pw.ids[:0]
	for i, id := range pw.ids {
		if i == 0 || id != pw.ids[i-1] {
			out = append(out, id)
		}
	}
	pw.ids = out
	pw.pos, pw.have = make([]geo.Point, len(out)), make([]bool, len(out))
}

// node records a node's position if a platform uses it.
func (pw *platformWays) node(id int64, lat, lon float64) {
	if len(pw.ids) == 0 || id < pw.ids[0] || id > pw.ids[len(pw.ids)-1] {
		return
	}
	if i := sort.Search(len(pw.ids), func(i int) bool { return pw.ids[i] >= id }); pw.ids[i] == id {
		// To the precision the cache file keeps, so a platform is the same before and after a restart.
		pw.pos[i], pw.have[i] = geo.Point{Lat: float64(toFixed(lat)) / degScale, Lon: float64(toFixed(lon)) / degScale}, true
	}
}

// platforms returns the outlines, each from the nodes that were given a position (those outside a clipped area
// weren't): a platform that crosses the area's edge is cut there.
func (pw *platformWays) platforms() []Platform {
	var out []Platform
	for _, w := range pw.ways {
		var pts []geo.Point
		for _, id := range w.refs {
			if i := sort.Search(len(pw.ids), func(i int) bool { return pw.ids[i] >= id }); i < len(pw.ids) && pw.ids[i] == id && pw.have[i] {
				pts = append(pts, pw.pos[i])
			}
		}
		if len(pts) >= 2 {
			out = append(out, Platform{Ref: w.ref, Outline: pts})
		}
	}
	return out
}
