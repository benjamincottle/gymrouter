package walk

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
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
