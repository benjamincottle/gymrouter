package walk

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/benjamincottle/gymrouter/internal/geo"
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
	r, ok := g.FromNearest(at(20, 0), []geo.Point{across, near, band, beyond}, 500, 7500)
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

// Real data, if a Sydney extract is at $WALK_TEST_PBF: the Lane Cove gym to a nearby bus stop.
func TestRealExtract(t *testing.T) {
	path := os.Getenv("WALK_TEST_PBF")
	if path == "" {
		t.Skip("set WALK_TEST_PBF to an OSM extract of Sydney")
	}
	g, err := FromPBF(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d nodes, %d half-edges", g.Nodes(), g.Edges())
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
}
