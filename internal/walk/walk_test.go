package walk

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/osmpbf/osmpbftest"
)

func TestClassify(t *testing.T) {
	type tags = map[string]string
	for name, tc := range map[string]struct {
		tags tags
		ok   bool
		cost float32
	}{
		"residential":       {tags{"highway": "residential"}, true, costNormal},
		"footway":           {tags{"highway": "footway"}, true, costNormal},
		"steps are slower":  {tags{"highway": "steps"}, true, costSteps},
		"motorway":          {tags{"highway": "motorway"}, false, 0},
		"motorway foot=yes": {tags{"highway": "motorway", "foot": "yes"}, false, 0},
		"foot=no road":      {tags{"highway": "primary", "foot": "no"}, false, 0},
		"private service":   {tags{"highway": "service", "access": "private"}, false, 0},
		"private but foot":  {tags{"highway": "service", "access": "private", "foot": "yes"}, true, costNormal},
		"bike path":         {tags{"highway": "cycleway"}, false, 0},
		"shared path":       {tags{"highway": "cycleway", "foot": "designated"}, true, costNormal},
		"pedestrian area":   {tags{"highway": "pedestrian", "area": "yes"}, false, 0},
		"construction":      {tags{"highway": "construction"}, false, 0},
		"platform":          {tags{"railway": "platform"}, true, costNormal},
		"building":          {tags{"building": "yes"}, false, 0},
		"no tags":           {tags{}, false, 0},
	} {
		t.Run(name, func(t *testing.T) {
			cost, ok := classify(tc.tags)
			if ok != tc.ok || (ok && cost != tc.cost) {
				t.Errorf("classify = %v, %v; want %v, %v", cost, ok, tc.cost, tc.ok)
			}
		})
	}
}

// A river with one bridge: the two banks are 100 m apart as the crow flies and about 3.4 km on foot (a degree of longitude is shorter than one of latitude).
//
//	north bank  y=1 ──────────────────────────  (x from 0 to 20)
//	                          bridge at x=0..1
//	south bank  y=0 ──────────────────────────
const step = 0.0009 // degrees, about 100 m

func riverGraph(t *testing.T) *Graph {
	t.Helper()
	b := &Builder{MinComponent: 1}
	id := func(x, y int) int64 { return int64(1 + y*100 + x) }
	var south, north, bridge []int64
	for x := 0; x <= 20; x++ {
		south = append(south, id(x, 0))
		north = append(north, id(x, 1))
	}
	bridge = []int64{id(0, 0), id(0, 1)}
	b.AddWay(map[string]string{"highway": "residential"}, south)
	b.AddWay(map[string]string{"highway": "residential"}, north)
	b.AddWay(map[string]string{"highway": "footway"}, bridge)
	b.AddWay(map[string]string{"highway": "service", "access": "private"}, []int64{id(20, 0), id(20, 1)}) // a gate we can't use
	b.Prepare()
	for y := 0; y <= 1; y++ {
		for x := 0; x <= 20; x++ {
			b.AddNode(id(x, y), -33.9+float64(y)*step, 151.2+float64(x)*step)
		}
	}
	g, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func at(x, y float64) geo.Point { return geo.Point{Lat: -33.9 + y*step, Lon: 151.2 + x*step} }

func TestRoutingFollowsTheStreetsNotTheCrow(t *testing.T) {
	g := riverGraph(t)
	r, ok := g.From(at(20, 0), 5000)
	if !ok {
		t.Fatal("origin not on the network")
	}
	crow := geo.DistanceM(at(20, 0), at(20, 1))
	got, ok := r.Metres(at(20, 1))
	if !ok {
		t.Fatal("unreachable")
	}
	// Across the river is ~100 m straight, but you must walk to the bridge and back: ~3.4 km.
	if got < 3300 || got > 3600 {
		t.Errorf("walk across the river = %.0f m (crow %.0f m); want about 3.4 km", got, crow)
	}
	// The private gate must not be a shortcut; the same bank is a straight walk.
	same, _ := r.Metres(at(10, 0))
	if want := geo.DistanceM(at(20, 0), at(10, 0)); math.Abs(same-want) > want*0.05 {
		t.Errorf("along the bank = %.0f m, want about %.0f", same, want)
	}
}

func TestLimitAndSnap(t *testing.T) {
	g := riverGraph(t)
	r, _ := g.From(at(20, 0), 500)
	if _, ok := r.Metres(at(0, 1)); ok {
		t.Error("a destination beyond the limit must be unreachable")
	}
	if _, ok := g.From(geo.Point{Lat: -34.5, Lon: 150}, 1000); ok {
		t.Error("a point far from any street must not snap")
	}
	// A point 50 m off the street is placed on it, with the extra walk counted.
	off := geo.Point{Lat: at(10, 0).Lat - 0.00045, Lon: at(10, 0).Lon}
	r2, ok := g.From(off, 2000)
	if !ok {
		t.Fatal("off-street origin")
	}
	m, _ := r2.Metres(at(12, 0))
	if along := geo.DistanceM(at(10, 0), at(12, 0)); m < along+50 || m > along+80 {
		t.Errorf("%.0f m along the bank from 50 m off the street = %.0f", along, m)
	}
}

func TestFromNearestStopsPastTheBand(t *testing.T) {
	g := riverGraph(t)
	across, near, band, beyond := at(20, 1), at(14, 0), at(10, 0), at(5, 0) // ~3.4 km, 600 m, 1 km, 1.5 km along the streets
	r, ok := g.FromNearest(at(20, 0), []geo.Point{across, near, band, beyond}, 500, 0, 7500)
	if !ok {
		t.Fatal("origin not on the network")
	}
	m := func(q geo.Point) float64 {
		if v, ok := r.Metres(q); ok {
			return v
		}
		return math.Inf(1)
	}
	all, _ := g.From(at(20, 0), 7500)
	for _, q := range []geo.Point{near, band} {
		if want, _ := all.Metres(q); math.Abs(m(q)-want) > 1 {
			t.Errorf("within the band: %.0f m, want %.0f", m(q), want)
		}
	}
	// Across the river is nearest in a straight line, not on foot; it and the stop past the band read as further.
	for _, q := range []geo.Point{across, beyond} {
		if m(q) <= m(near)+500 {
			t.Errorf("%.0f m is past the band (nearest %.0f m)", m(q), m(near))
		}
	}
	if len(r.nodes) >= len(all.nodes) {
		t.Errorf("searched %d nodes; the whole limit is %d", len(r.nodes), len(all.nodes))
	}
	// A floor past the band: everything within it is final too.
	r, _ = g.FromNearest(at(20, 0), []geo.Point{across, near, band, beyond}, 500, 1600, 7500)
	if want, _ := all.Metres(beyond); math.Abs(m(beyond)-want) > 1 {
		t.Errorf("within the floor: %.0f m, want %.0f", m(beyond), want)
	}
}

func TestPath(t *testing.T) {
	g := riverGraph(t)
	r, _ := g.From(at(20, 0), 5000)
	p, ok := r.Path(at(20, 1))
	if !ok || len(p) < 10 {
		t.Fatalf("path: %v %d", ok, len(p))
	}
	if p[0] != at(20, 0) || p[len(p)-1] != at(20, 1) {
		t.Errorf("path ends: %v .. %v", p[0], p[len(p)-1])
	}
	var length float64
	for i := 1; i < len(p); i++ {
		length += geo.DistanceM(p[i-1], p[i])
	}
	if length < 3300 {
		t.Errorf("path length %.0f m is shorter than the walk", length)
	}
}

func TestStepsAreSlower(t *testing.T) {
	b := &Builder{MinComponent: 1}
	b.AddWay(map[string]string{"highway": "footway"}, []int64{1, 2})
	b.AddWay(map[string]string{"highway": "steps"}, []int64{2, 3})
	b.Prepare()
	b.AddNode(1, -33.9, 151.2)
	b.AddNode(2, -33.9, 151.201)
	b.AddNode(3, -33.9, 151.202)
	g, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	r, _ := g.From(geo.Point{Lat: -33.9, Lon: 151.2}, 1000)
	flat, _ := r.Metres(geo.Point{Lat: -33.9, Lon: 151.201})
	stairs, _ := r.Metres(geo.Point{Lat: -33.9, Lon: 151.202})
	if stairs-flat < (flat * 1.4) { // the second segment, same length, costs 1.6x
		t.Errorf("flat %.0f, with steps %.0f", flat, stairs)
	}
}

func TestSmallIslandsAreDropped(t *testing.T) {
	b := &Builder{MinComponent: 3}
	b.AddWay(map[string]string{"highway": "footway"}, []int64{1, 2, 3, 4})
	b.AddWay(map[string]string{"highway": "footway"}, []int64{10, 11}) // an island of two
	b.Prepare()
	for i, id := range []int64{1, 2, 3, 4, 10, 11} {
		b.AddNode(id, -33.9, 151.2+float64(i)*0.001)
	}
	g, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	if g.Nodes() != 4 {
		t.Errorf("nodes = %d, want 4", g.Nodes())
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	g := riverGraph(t)
	path := filepath.Join(t.TempDir(), "walk.graph")
	if err := g.Save(path); err != nil {
		t.Fatal(err)
	}
	h, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if h.Nodes() != g.Nodes() || h.Edges() != g.Edges() {
		t.Fatalf("sizes differ: %d/%d vs %d/%d", h.Nodes(), h.Edges(), g.Nodes(), g.Edges())
	}
	r1, _ := g.From(at(20, 0), 5000)
	r2, _ := h.From(at(20, 0), 5000)
	a, _ := r1.Metres(at(20, 1))
	b, _ := r2.Metres(at(20, 1))
	if a != b {
		t.Errorf("routes differ after reload: %v vs %v", a, b)
	}
	// Corrupt and truncated files are rejected, not trusted.
	raw, _ := os.ReadFile(path)
	for name, bad := range map[string][]byte{"truncated": raw[:len(raw)/2], "garbage": []byte("not a graph"), "empty": nil} {
		p := filepath.Join(t.TempDir(), name)
		_ = os.WriteFile(p, bad, 0o644)
		if _, err := Load(p); err == nil {
			t.Errorf("%s file loaded", name)
		}
	}
}

// streets is an extract of three streets running east, twelve nodes each about 90 m apart, joined by a street running
// north at every node. The box takes in the six western nodes of each: the streets running east cross its edge.
func streets() (nodes []osmpbftest.Node, ways []osmpbftest.Way, box geo.Box) {
	id := func(x, y int) int64 { return int64(1 + y*100 + x) }
	road := map[string]string{"highway": "residential"}
	for y := 0; y < 3; y++ {
		var east []int64
		for x := 0; x < 12; x++ {
			nodes = append(nodes, osmpbftest.Node{ID: id(x, y), Lat: -33.9 + float64(y)*0.001, Lon: 151 + float64(x)*0.001})
			east = append(east, id(x, y))
		}
		ways = append(ways, osmpbftest.Way{ID: int64(1 + y), Refs: east, Tags: road})
	}
	for x := 0; x < 12; x++ {
		ways = append(ways, osmpbftest.Way{ID: int64(10 + x), Refs: []int64{id(x, 0), id(x, 1), id(x, 2)}, Tags: road})
	}
	// A path that leaves the box and comes back: both of its halves cross the edge, so neither is kept.
	ways = append(ways, osmpbftest.Way{ID: 30, Refs: []int64{id(5, 0), id(6, 1), id(5, 2)}, Tags: map[string]string{"highway": "path"}})
	// A building: not for walking, wherever it is.
	ways = append(ways, osmpbftest.Way{ID: 31, Refs: []int64{id(0, 0), id(1, 0), id(1, 1)}, Tags: map[string]string{"building": "yes"}})
	return nodes, ways, geo.Box{MinLon: 150.9995, MinLat: -33.95, MaxLon: 151.0055, MaxLat: -33.85}
}

func writeFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "extract.osm.pbf")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// same reports whether two graphs are the same network.
func same(a, b *Graph) bool {
	return reflect.DeepEqual(a.lat, b.lat) && reflect.DeepEqual(a.lon, b.lon) && reflect.DeepEqual(a.start, b.start) &&
		reflect.DeepEqual(a.to, b.to) && reflect.DeepEqual(a.cost, b.cost) && a.Area == b.Area
}

func TestAnExtractIsClippedToTheArea(t *testing.T) {
	nodes, ways, box := streets()
	path := writeFile(t, osmpbftest.Encode(nodes, ways))
	small := func() *Builder { return &Builder{MinComponent: 1} }

	whole, err := fromPBF(path, nil, small(), true)
	if err != nil {
		t.Fatal(err)
	}
	// 36 nodes; 3 streets of 11 segments, 12 of 2, and the path's 2; two half-edges a segment.
	if whole.Nodes() != 36 || whole.Edges() != 2*(33+24+2) || whole.Area != (geo.Box{}) {
		t.Fatalf("the whole extract: %d nodes, %d half-edges, area %+v", whole.Nodes(), whole.Edges(), whole.Area)
	}

	g, err := fromPBF(path, &box, small(), true)
	if err != nil {
		t.Fatal(err)
	}
	// The 18 nodes inside; each street running east is cut at its sixth node (5 segments), the 6 running north
	// inside are whole, the 6 outside are gone, and the path that leaves and comes back adds nothing.
	if g.Nodes() != 18 || g.Edges() != 2*(15+12) {
		t.Errorf("clipped: %d nodes, %d half-edges; want 18 and 54", g.Nodes(), g.Edges())
	}
	if g.Area != box || g.MaxLon > box.MaxLon || g.MinLon < box.MinLon {
		t.Errorf("clipped to %+v, reaching %v to %v", g.Area, g.MinLon, g.MaxLon)
	}
	// Along a street to its last node inside is the street's own length: nothing was lost short of the edge.
	r, ok := g.From(geo.Point{Lat: -33.9, Lon: 151}, 2000)
	if !ok {
		t.Fatal("the western end isn't on the network")
	}
	end := geo.Point{Lat: -33.9, Lon: 151.005}
	if m, ok := r.Metres(end); !ok || math.Abs(m-geo.DistanceM(geo.Point{Lat: -33.9, Lon: 151}, end)) > 1 {
		t.Errorf("to the edge: %v m, %v", m, ok)
	}

	// The area is kept with the graph.
	saved := filepath.Join(t.TempDir(), "walk.graph")
	if err := g.Save(saved); err != nil {
		t.Fatal(err)
	}
	if h, err := Load(saved); err != nil || !same(g, h) {
		t.Errorf("after save and load: %v, area %+v", err, h.Area)
	}

	// An area with no streets in it is an error, not an empty network.
	if _, err := fromPBF(path, &geo.Box{MinLon: 10, MinLat: 10, MaxLon: 11, MaxLat: 11}, small(), true); err == nil {
		t.Error("an area with nothing in it built a network")
	}
}

func TestTheSameNetworkHoweverTheFileIsLaidOut(t *testing.T) {
	nodes, ways, box := streets()
	small := func() *Builder { return &Builder{MinComponent: 1} }
	var first *Graph
	for name, data := range map[string][]byte{
		"sorted, in several blocks": osmpbftest.EncodeWith(nodes, ways, osmpbftest.Options{Sorted: true, PerBlock: 5, Relation: true}),
		"sorted, one block each":    osmpbftest.EncodeWith(nodes, ways, osmpbftest.Options{Sorted: true}),
		"not said to be sorted":     osmpbftest.EncodeWith(nodes, ways, osmpbftest.Options{PerBlock: 5, Relation: true}),
	} {
		path := writeFile(t, data)
		for _, shortCuts := range []bool{true, false} {
			for _, clip := range []*geo.Box{&box, nil} {
				g, err := fromPBF(path, clip, small(), shortCuts)
				if err != nil {
					t.Fatalf("%s (short cuts %v): %v", name, shortCuts, err)
				}
				if clip == nil {
					if g.Nodes() != 36 {
						t.Errorf("%s (short cuts %v), unclipped: %d nodes", name, shortCuts, g.Nodes())
					}
					continue
				}
				if first == nil {
					first = g
				}
				if g.Nodes() != 18 || !same(first, g) {
					t.Errorf("%s (short cuts %v): a different network, %d nodes", name, shortCuts, g.Nodes())
				}
			}
		}
	}
}

func TestClippingNeedsNodesInOrder(t *testing.T) {
	nodes, ways, box := streets()
	nodes[3], nodes[4] = nodes[4], nodes[3] // two nodes inside the box, the wrong way round
	path := writeFile(t, osmpbftest.Encode(nodes, ways))
	if _, err := fromPBF(path, &box, &Builder{MinComponent: 1}, true); !errors.Is(err, errUnsorted) {
		t.Errorf("clipping a file out of order: %v", err)
	}
	// The whole extract needs no order.
	if g, err := fromPBF(path, nil, &Builder{MinComponent: 1}, true); err != nil || g.Nodes() != 36 {
		t.Errorf("unclipped: %v", err)
	}
}

// A station beside the streets: a platform drawn as an area (so not a way to walk along) with a face each side, one
// drawn as a line along its edge, one that runs out of the box, and one wholly outside it.
func TestPlatformsAreKeptWithTheNetwork(t *testing.T) {
	nodes, ways, box := streets()
	next := int64(1000)
	shape := func(tags map[string]string, pts ...geo.Point) {
		var refs []int64
		for i, p := range pts {
			if i == len(pts)-1 && len(pts) > 2 && p == pts[0] {
				refs = append(refs, refs[0]) // closed: back to the first node
				break
			}
			nodes = append(nodes, osmpbftest.Node{ID: next, Lat: p.Lat, Lon: p.Lon})
			refs = append(refs, next)
			next++
		}
		ways = append(ways, osmpbftest.Way{ID: next, Refs: refs, Tags: tags})
	}
	rect := func(lat, lon0, lon1 float64) []geo.Point {
		return []geo.Point{{Lat: lat, Lon: lon0}, {Lat: lat, Lon: lon1}, {Lat: lat + 0.00008, Lon: lon1}, {Lat: lat + 0.00008, Lon: lon0}, {Lat: lat, Lon: lon0}}
	}
	shape(map[string]string{"railway": "platform", "ref": "1;2", "area": "yes"}, rect(-33.91, 151.001, 151.003)...)
	shape(map[string]string{"railway": "platform", "ref": "3"}, geo.Point{Lat: -33.9105, Lon: 151.001}, geo.Point{Lat: -33.9105, Lon: 151.003})
	shape(map[string]string{"railway": "platform", "ref": "4", "area": "yes"}, rect(-33.911, 151.004, 151.008)...) // across the edge at 151.0055
	shape(map[string]string{"railway": "platform", "ref": "5", "area": "yes"}, rect(-33.912, 151.007, 151.009)...) // outside
	path := writeFile(t, osmpbftest.EncodeWith(nodes, ways, osmpbftest.Options{Sorted: true, PerBlock: 7, Relation: true}))

	g, err := fromPBF(path, &box, &Builder{MinComponent: 1}, true)
	if err != nil {
		t.Fatal(err)
	}
	refs := map[string]int{}
	for _, p := range g.Platforms {
		refs[p.Ref] = len(p.Outline)
	}
	// Whole ones keep every point (an area's last is its first); the one across the edge keeps only what's inside:
	// its two western corners, the first of them again to close it.
	if len(g.Platforms) != 3 || refs["1;2"] != 5 || refs["3"] != 2 || refs["4"] != 3 {
		t.Fatalf("platforms kept: %v", refs)
	}
	// The area isn't walked along, the line is: 18 street nodes and the line's 2 (an island, kept at this size).
	if g.Nodes() != 20 {
		t.Errorf("%d nodes: an area platform's corners shouldn't be street nodes", g.Nodes())
	}
	whole, err := fromPBF(path, nil, &Builder{MinComponent: 1}, true)
	if err != nil || len(whole.Platforms) != 4 {
		t.Errorf("unclipped: %d platforms, %v", len(whole.Platforms), err)
	}

	// The timetable's point for a platform finds its outline: by its number when several are close, else the nearest.
	on12 := geo.Point{Lat: -33.90996, Lon: 151.002} // on the island
	for name, tc := range map[string]struct {
		at   geo.Point
		code string
		want string // the ref of the platform found; "" for none
	}{
		"25 m off the island, as platform 2":         {geo.Point{Lat: -33.90969, Lon: 151.002}, "2", "1;2"}, // its number reaches further
		"25 m off the island, no number":             {geo.Point{Lat: -33.90969, Lon: 151.002}, "", ""},
		"25 m off the island, as platform 3":         {geo.Point{Lat: -33.90969, Lon: 151.002}, "3", ""},
		"on the island, as platform 2":               {on12, "2", "1;2"},
		"on the island, no number":                   {on12, "", "1;2"},
		"on the island, as platform 3":               {on12, "3", "1;2"}, // 3 is 55 m away: too far to be it
		"between the two, as platform 3":             {geo.Point{Lat: -33.91041, Lon: 151.002}, "3", "3"},
		"between the two, as platform 1":             {geo.Point{Lat: -33.91041, Lon: 151.002}, "1", "3"}, // the island is 46 m off
		"beside the line, within 15 m":               {geo.Point{Lat: -33.91060, Lon: 151.002}, "", "3"},
		"22 m from the line, no number":              {geo.Point{Lat: -33.91070, Lon: 151.002}, "", ""},
		"22 m from the line, as platform 3":          {geo.Point{Lat: -33.91070, Lon: 151.002}, "3", "3"},
		"45 m from the line, as platform 3":          {geo.Point{Lat: -33.91091, Lon: 151.002}, "3", ""},
		"past the end of the island by 10 m":         {geo.Point{Lat: -33.90996, Lon: 151.00311}, "1", "1;2"},
		"past the end of the island by 30 m":         {geo.Point{Lat: -33.90996, Lon: 151.00333}, "1", ""},
		"by what is left of the one cut at the edge": {geo.Point{Lat: -33.91096, Lon: 151.00405}, "4", "4"},
		"where the rest of it was":                   {geo.Point{Lat: -33.91096, Lon: 151.0045}, "4", ""},
		"at the one outside it (not kept)":           {geo.Point{Lat: -33.912, Lon: 151.008}, "5", ""},
	} {
		out, ok := g.PlatformAt(tc.at, tc.code)
		got := ""
		for _, p := range g.Platforms {
			if ok && len(p.Outline) == len(out) && p.Outline[0] == out[0] {
				got = p.Ref
			}
		}
		if got != tc.want {
			t.Errorf("%s: found %q, want %q", name, got, tc.want)
		}
	}

	// They're kept with the cache.
	saved := filepath.Join(t.TempDir(), "walk.graph")
	if err := g.Save(saved); err != nil {
		t.Fatal(err)
	}
	h, err := Load(saved)
	if err != nil || !reflect.DeepEqual(h.Platforms, g.Platforms) {
		t.Errorf("after save and load: %v, %d platforms", err, len(h.Platforms))
	}
}

// A train's stopping point is a tagged node on its track. It's kept with which carriage is beside which way off, and
// with the direction the track is drawn in, which says which end of that list is the front.
func TestTrainStopsAreKeptWithTheNetwork(t *testing.T) {
	nodes, ways, box := streets()
	at := func(id int64, lat, lon float64, tags map[string]string) int64 {
		nodes = append(nodes, osmpbftest.Node{ID: id, Lat: lat, Lon: lon, Tags: tags})
		return id
	}
	stop := func(ref string, more map[string]string) map[string]string {
		tags := map[string]string{"railway": "stop", "public_transport": "stop_position", "ref": ref}
		for k, v := range more {
			tags[k] = v
		}
		return tags
	}
	rail := map[string]string{"railway": "rail"}
	// A track drawn eastwards, with a stop on it; one drawn westwards whose stop only has a list for trains running
	// against it; a stop past the edge of the box (151.0055); and a tagged stop that's on no track.
	ways = append(ways,
		osmpbftest.Way{ID: 601, Tags: rail, Refs: []int64{
			at(3001, -33.9102, 151.001, nil), at(3002, -33.9102, 151.002, stop("P1", map[string]string{"exit:carriages:forward": "stairs|no|no|lift;escalator"})), at(3003, -33.9102, 151.003, nil)}},
		osmpbftest.Way{ID: 602, Tags: rail, Refs: []int64{
			at(3013, -33.9103, 151.003, nil), at(3012, -33.9103, 151.002, stop("P2", map[string]string{"exit:carriages:backward": "no|ramp|no"})), at(3011, -33.9103, 151.001, nil)}},
		osmpbftest.Way{ID: 603, Tags: rail, Refs: []int64{
			at(3021, -33.9104, 151.007, nil), at(3022, -33.9104, 151.008, stop("P3", map[string]string{"exit:carriages:forward": "no|stairs"})), at(3023, -33.9104, 151.009, nil)}},
		osmpbftest.Way{ID: 604, Tags: map[string]string{"highway": "footway"}, Refs: []int64{
			at(3031, -33.9105, 151.001, nil), at(3032, -33.9105, 151.002, stop("P4", map[string]string{"exit:carriages:forward": "no|stairs"}))}},
		osmpbftest.Way{ID: 605, Tags: rail, Refs: []int64{ // a stop with no list isn't worth keeping
			at(3041, -33.9106, 151.001, nil), at(3042, -33.9106, 151.002, stop("P5", nil)), at(3043, -33.9106, 151.003, nil)}},
	)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	path := writeFile(t, osmpbftest.EncodeWith(nodes, ways, osmpbftest.Options{Sorted: true, PerBlock: 7, Relation: true}))
	g, err := fromPBF(path, &box, &Builder{MinComponent: 1}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.TrainStops) != 2 {
		t.Fatalf("stops kept: %+v", g.TrainStops)
	}
	p1, ok := g.TrainStopFor("P1")
	if !ok || math.Abs(p1.Bearing-90) > 0.5 || p1.Pos != (geo.Point{Lat: -33.9102, Lon: 151.002}) {
		t.Fatalf("P1: %+v, %v", p1, ok)
	}
	east, west := []string{"stairs", "no", "no", "lift;escalator"}, []string{"lift;escalator", "no", "no", "stairs"}
	if got := p1.Carriages(85); !reflect.DeepEqual(got, east) {
		t.Errorf("P1, a train running with the track: %v", got)
	}
	if got := p1.Carriages(265); !reflect.DeepEqual(got, west) {
		t.Errorf("P1, a train running against the track: %v", got)
	}
	// P2's track is drawn westwards and its list is for trains running against that: eastbound ones.
	p2, ok := g.TrainStopFor("P2")
	if !ok || math.Abs(p2.Bearing-270) > 0.5 {
		t.Fatalf("P2: %+v, %v", p2, ok)
	}
	if got := p2.Carriages(90); !reflect.DeepEqual(got, []string{"no", "ramp", "no"}) {
		t.Errorf("P2 eastbound: %v", got)
	}
	if got := p2.Carriages(270); !reflect.DeepEqual(got, []string{"no", "ramp", "no"}) { // its mirror: the same, as it happens
		t.Errorf("P2 westbound: %v", got)
	}
	if _, ok := g.TrainStopFor("P3"); ok {
		t.Error("a stop outside the area was kept")
	}
	if _, ok := g.TrainStopFor("P4"); ok {
		t.Error("a stop that's on no track was kept")
	}
	// The 18 street nodes and the footway's 2: tracks aren't streets.
	if g.Nodes() != 20 {
		t.Errorf("%d nodes: tracks aren't streets", g.Nodes())
	}
	// They come back from the cache file as they went in.
	file := filepath.Join(t.TempDir(), "walk.graph")
	if err := g.Save(file); err != nil {
		t.Fatal(err)
	}
	back, err := Load(file)
	if err != nil || !reflect.DeepEqual(back.TrainStops, g.TrainStops) {
		t.Fatalf("after a restart: %+v, %v", back.TrainStops, err)
	}
	if s, ok := back.TrainStopFor("P1"); !ok || !reflect.DeepEqual(s.Carriages(90), east) {
		t.Errorf("P1 after a restart: %+v", s)
	}
	// The whole extract, unclipped, keeps the one past the edge too.
	whole, err := fromPBF(path, nil, &Builder{MinComponent: 1}, true)
	if err != nil || len(whole.TrainStops) != 3 {
		t.Errorf("unclipped: %d stops, %v", len(whole.TrainStops), err)
	}
}

// A platform with a stairwell cut out of it is drawn as a relation: its outer edge in two pieces that carry no tags of
// their own (one of them drawn the other way round), and the hole.
func TestPlatformsDrawnInPieces(t *testing.T) {
	nodes, ways, box := streets()
	at := func(id int64, lat, lon float64) int64 {
		nodes = append(nodes, osmpbftest.Node{ID: id, Lat: lat, Lon: lon})
		return id
	}
	// The edge: south-west, south-east, north-east, north-west corners, 185 m by 9 m.
	sw, se, ne, nw := at(2001, -33.91, 151.001), at(2002, -33.91, 151.003), at(2003, -33.90992, 151.003), at(2004, -33.90992, 151.001)
	h1, h2, h3 := at(2005, -33.90997, 151.0019), at(2006, -33.90997, 151.0021), at(2007, -33.90995, 151.002)
	far1, far2 := at(2008, -33.92, 151.02), at(2009, -33.92, 151.021) // a platform outside the box
	ways = append(ways,
		osmpbftest.Way{ID: 501, Refs: []int64{sw, se, ne}},
		osmpbftest.Way{ID: 502, Refs: []int64{sw, nw, ne}}, // the other half, drawn from the same corner
		osmpbftest.Way{ID: 503, Refs: []int64{h1, h2, h3, h1}},
		osmpbftest.Way{ID: 504, Refs: []int64{far1, far2}},
	)
	way := func(id int64, role string) osmpbftest.Member { return osmpbftest.Member{Type: 1, Ref: id, Role: role} }
	rels := []osmpbftest.Relation{
		{ID: 900, Tags: map[string]string{"type": "multipolygon", "railway": "platform", "ref": "16;17"},
			Members: []osmpbftest.Member{way(501, "outer"), way(503, "inner"), way(502, "outer")}},
		{ID: 901, Tags: map[string]string{"type": "multipolygon", "railway": "platform", "ref": "9"}, Members: []osmpbftest.Member{way(504, "outer")}},
		{ID: 902, Tags: map[string]string{"type": "multipolygon", "railway": "platform", "ref": "8"}, Members: []osmpbftest.Member{way(777, "outer")}}, // a way the extract doesn't have
		{ID: 903, Tags: map[string]string{"type": "multipolygon", "landuse": "grass"}, Members: []osmpbftest.Member{way(1, "outer")}},
	}
	for name, o := range map[string]osmpbftest.Options{
		"sorted":     {Sorted: true, PerBlock: 9, Relations: rels, Relation: true},
		"not sorted": {Relations: rels},
	} {
		g, err := fromPBF(writeFile(t, osmpbftest.EncodeWith(nodes, ways, o)), &box, &Builder{MinComponent: 1}, true)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(g.Platforms) != 1 || g.Platforms[0].Ref != "16;17" {
			t.Fatalf("%s: platforms %+v", name, g.Platforms)
		}
		// One closed outline of the four corners, however the pieces were drawn.
		out := g.Platforms[0].Outline
		if len(out) != 5 || out[0] != out[4] {
			t.Errorf("%s: outline %v", name, out)
		}
		for where, p := range map[string]geo.Point{"its middle": {Lat: -33.90996, Lon: 151.0015}, "the stairwell": {Lat: -33.90996, Lon: 151.002}} {
			if found, ok := g.PlatformAt(p, "17"); !ok || len(found) != 5 {
				t.Errorf("%s: %s isn't on the platform", name, where)
			}
		}
		if g.Nodes() != 18 {
			t.Errorf("%s: %d nodes: a platform's pieces aren't streets", name, g.Nodes())
		}
	}
}

func TestJoinEnds(t *testing.T) {
	for name, tc := range map[string]struct {
		parts [][]int64
		want  [][]int64
	}{
		"one closed way":                 {[][]int64{{1, 2, 3, 1}}, [][]int64{{1, 2, 3, 1}}},
		"two halves, head to tail":       {[][]int64{{1, 2, 3}, {3, 4, 1}}, [][]int64{{1, 2, 3, 4, 1}}},
		"the second drawn backwards":     {[][]int64{{1, 2, 3}, {1, 4, 3}}, [][]int64{{1, 2, 3, 4, 1}}},
		"three pieces, out of order":     {[][]int64{{1, 2}, {3, 1}, {2, 3}}, [][]int64{{3, 1, 2, 3}}}, // the same ring, from another corner
		"joined at the front":            {[][]int64{{2, 3}, {1, 2}, {3, 1}}, [][]int64{{1, 2, 3, 1}}},
		"two rings":                      {[][]int64{{1, 2, 1}, {5, 6}, {6, 7, 5}}, [][]int64{{1, 2, 1}, {5, 6, 7, 5}}},
		"a piece missing: what there is": {[][]int64{{1, 2, 3}, nil, {3, 4}}, [][]int64{{1, 2, 3, 4}}},
		"nothing":                        {nil, nil},
	} {
		if got := joinEnds(tc.parts); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}

// Real data, if an extract is at $WALK_TEST_PBF (Geofabrik's New South Wales, or any that covers Sydney): clipped to
// the app's area, with a walk at each end of it. WALK_TEST_COMPARE=1 also builds it without the short cuts a sorted
// file allows, to see that the network is the same and what they save.
func TestRealExtract(t *testing.T) {
	path := os.Getenv("WALK_TEST_PBF")
	if path == "" {
		t.Skip("set WALK_TEST_PBF to an OSM extract that covers Sydney")
	}
	area := geo.Box{MinLon: 150.55, MinLat: -34.15, MaxLon: 151.35, MaxLat: -33.45} // the engine's
	start := time.Now()
	g, err := FromPBF(path, &area)
	if err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)
	t.Logf("%d nodes, %d half-edges, reaching %.3f to %.3f E and %.3f to %.3f; built in %v", g.Nodes(), g.Edges(), g.MinLon, g.MaxLon, g.MinLat, g.MaxLat, took.Round(100*time.Millisecond))
	// Platforms, as the timetable places them: one drawn as a single shape, and three drawn in pieces.
	t.Logf("%d platforms", len(g.Platforms))
	for name, p := range map[string]struct {
		at   geo.Point
		code string
	}{
		"Hornsby 1":   {geo.Point{Lat: -33.70323338, Lon: 151.09857464}, "1"},
		"Central 18":  {geo.Point{Lat: -33.88405165, Lon: 151.20698224}, "18"},
		"Chatswood 1": {geo.Point{Lat: -33.79746616, Lon: 151.18107432}, "1"},
		"Auburn 1":    {geo.Point{Lat: -33.84922459, Lon: 151.03296397}, "1"},
	} {
		if out, ok := g.PlatformAt(p.at, p.code); !ok {
			t.Errorf("%s has no platform outline", name)
		} else {
			t.Logf("%s: an outline of %d points", name, len(out))
		}
	}
	if g.MinLon < area.MinLon || g.MaxLon > area.MaxLon || g.MinLat < area.MinLat || g.MaxLat > area.MaxLat {
		t.Error("the network reaches outside the area")
	}
	gym := geo.Point{Lat: -33.807948, Lon: 151.150629}
	stop := geo.Point{Lat: -33.80435, Lon: 151.1561} // roughly Epping Rd, north-east of the gym
	r, ok := g.From(gym, 3000)
	if !ok {
		t.Fatal("gym not on the network")
	}
	m, ok := r.Metres(stop)
	crow := geo.DistanceM(gym, stop)
	t.Logf("crow %.0f m, street %.0f m", crow, m)
	if !ok || m < crow*0.95 || m > crow*3 {
		t.Errorf("street distance %.0f vs crow %.0f", m, crow)
	}
	// The western edge: Jamisontown to Penrith Station, which BBBike's Sydney extract stopped short of.
	west, station := geo.Point{Lat: -33.767272, Lon: 150.687465}, geo.Point{Lat: -33.7503, Lon: 150.6963}
	if r, ok := g.From(west, 5000); !ok {
		t.Error("Jamisontown isn't on the network")
	} else if m, ok := r.Metres(station); !ok || m < geo.DistanceM(west, station)*0.95 {
		t.Errorf("Jamisontown to Penrith Station: %.0f m, %v", m, ok)
	} else {
		t.Logf("Jamisontown to Penrith Station: crow %.0f m, street %.0f m", geo.DistanceM(west, station), m)
	}
	if os.Getenv("WALK_TEST_COMPARE") == "" {
		return
	}
	start = time.Now()
	h, err := fromPBF(path, &area, &Builder{}, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("reading every pass in full: built in %v (against %v)", time.Since(start).Round(100*time.Millisecond), took.Round(100*time.Millisecond))
	if !same(g, h) {
		t.Errorf("a different network without the short cuts: %d nodes, %d half-edges", h.Nodes(), h.Edges())
	}
}
