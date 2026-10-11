package osmpbf_test

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/benjamincottle/gymrouter/internal/osmpbf"
	"github.com/benjamincottle/gymrouter/internal/osmpbf/osmpbftest"
)

func TestReadsNodesAndWays(t *testing.T) {
	data := osmpbftest.Encode(
		[]osmpbftest.Node{{ID: 10, Lat: -33.8079, Lon: 151.1506}, {ID: 11, Lat: -33.8, Lon: 151.2}, {ID: 500, Lat: 10, Lon: -20.5}},
		[]osmpbftest.Way{
			{ID: 7, Refs: []int64{10, 11, 500}, Tags: map[string]string{"highway": "footway", "name": "Mars Rd"}},
			{ID: 8, Refs: []int64{11, 10}, Tags: map[string]string{"highway": "steps"}},
		})
	var nodes []osmpbf.Node
	var ways []osmpbf.Way
	err := osmpbf.Read(bytes.NewReader(data), osmpbf.Handler{
		WantNodes: true, WantWays: true,
		Node: func(n osmpbf.Node) error { nodes = append(nodes, n); return nil },
		Way: func(w osmpbf.Way) error {
			w.Refs = append([]int64(nil), w.Refs...) // valid only during the call
			ways = append(ways, w)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 || nodes[0].ID != 10 || nodes[2].ID != 500 {
		t.Fatalf("nodes: %+v", nodes)
	}
	if math.Abs(nodes[0].Lat+33.8079) > 1e-6 || math.Abs(nodes[0].Lon-151.1506) > 1e-6 || math.Abs(nodes[2].Lon+20.5) > 1e-6 {
		t.Errorf("coordinates: %+v", nodes)
	}
	if len(ways) != 2 || ways[0].ID != 7 || len(ways[0].Refs) != 3 || ways[0].Refs[2] != 500 || ways[1].Refs[0] != 11 {
		t.Fatalf("ways: %+v", ways)
	}
	if ways[0].Tags["highway"] != "footway" || ways[0].Tags["name"] != "Mars Rd" || ways[1].Tags["highway"] != "steps" {
		t.Errorf("tags: %+v", ways)
	}
}

func TestSkipsWhatIsNotWanted(t *testing.T) {
	data := osmpbftest.Encode([]osmpbftest.Node{{ID: 1, Lat: 1, Lon: 1}}, []osmpbftest.Way{{ID: 2, Refs: []int64{1}, Tags: map[string]string{"a": "b"}}})
	nodes, ways := 0, 0
	err := osmpbf.Read(bytes.NewReader(data), osmpbf.Handler{WantWays: true,
		Node: func(osmpbf.Node) error { nodes++; return nil }, Way: func(osmpbf.Way) error { ways++; return nil }})
	if err != nil || nodes != 0 || ways != 1 {
		t.Errorf("nodes %d ways %d err %v", nodes, ways, err)
	}
}

func TestRejectsDamagedFiles(t *testing.T) {
	good := osmpbftest.Encode([]osmpbftest.Node{{ID: 1, Lat: 1, Lon: 1}}, []osmpbftest.Way{{ID: 2, Refs: []int64{1, 1}, Tags: map[string]string{"highway": "path"}}})
	h := osmpbf.Handler{WantNodes: true, WantWays: true,
		Node: func(osmpbf.Node) error { return nil }, Way: func(osmpbf.Way) error { return nil }}
	for name, data := range map[string][]byte{
		"truncated":        good[:len(good)/2],
		"garbage":          []byte(strings.Repeat("\xff", 64)),
		"huge header":      {0x7f, 0xff, 0xff, 0xff, 1, 2, 3},
		"zero header size": {0, 0, 0, 0},
	} {
		if err := osmpbf.Read(bytes.NewReader(data), h); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if err := osmpbf.Read(bytes.NewReader(nil), h); err != nil {
		t.Errorf("an empty file is just empty: %v", err)
	}
}

func TestHandlerErrorStopsReading(t *testing.T) {
	data := osmpbftest.Encode([]osmpbftest.Node{{ID: 1, Lat: 1, Lon: 1}, {ID: 2, Lat: 1, Lon: 1}}, nil)
	n := 0
	stop := bytes.ErrTooLarge
	err := osmpbf.Read(bytes.NewReader(data), osmpbf.Handler{WantNodes: true, Node: func(osmpbf.Node) error { n++; return stop }})
	if err != stop || n != 1 {
		t.Errorf("err %v after %d nodes", err, n)
	}
}

// sortedFile is laid out as an extract made by osmium is: five nodes over three blocks, three ways over two, then a
// relation, with the header saying so.
func sortedFile(sorted bool) []byte {
	var nodes []osmpbftest.Node
	for id := int64(1); id <= 5; id++ {
		nodes = append(nodes, osmpbftest.Node{ID: id, Lat: -33.8, Lon: 151 + float64(id)/100})
	}
	path := map[string]string{"highway": "path"}
	ways := []osmpbftest.Way{{ID: 1, Refs: []int64{1, 2}, Tags: path}, {ID: 2, Refs: []int64{2, 3}, Tags: path}, {ID: 3, Refs: []int64{4, 5}, Tags: path}}
	return osmpbftest.EncodeWith(nodes, ways, osmpbftest.Options{Sorted: sorted, PerBlock: 2, Relation: true})
}

// counting counts the bytes read through it.
type counting struct {
	r *bytes.Reader
	n int
}

func (c *counting) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

func TestASortedFileIsReadOnlyAsFarAsNeeded(t *testing.T) {
	data := sortedFile(true)
	// A pass for the nodes ends at the first way, and says where that is.
	var lay osmpbf.Layout
	nodes := 0
	in := &counting{r: bytes.NewReader(data)}
	err := osmpbf.Read(in, osmpbf.Handler{WantNodes: true, Until: osmpbf.UntilWays, Layout: &lay,
		Node: func(osmpbf.Node) error { nodes++; return nil }})
	if err != nil || nodes != 5 {
		t.Fatalf("%d nodes, err %v", nodes, err)
	}
	if !lay.Sorted || lay.WaysAt <= 0 || int(lay.WaysAt) >= len(data) {
		t.Fatalf("layout: %+v of %d bytes", lay, len(data))
	}
	if in.n >= len(data) {
		t.Errorf("read all %d bytes: it should have ended at the first way", in.n)
	}

	// A pass for the ways starts there: no node comes after it, and it ends at the relation.
	nodes, ways := 0, 0
	var from osmpbf.Layout
	err = osmpbf.Read(bytes.NewReader(data[lay.WaysAt:]), osmpbf.Handler{WantNodes: true, WantWays: true, Sorted: true, Until: osmpbf.UntilRelations, Layout: &from,
		Node: func(osmpbf.Node) error { nodes++; return nil }, Way: func(osmpbf.Way) error { ways++; return nil }})
	if err != nil || nodes != 0 || ways != 3 {
		t.Errorf("from where the ways begin: %d nodes, %d ways, err %v", nodes, ways, err)
	}
	if !from.Sorted || from.WaysAt != 0 {
		t.Errorf("layout read from the ways: %+v", from)
	}

	// Without being told the file is sorted, a read that begins past the header can't end early: it reads on.
	in = &counting{r: bytes.NewReader(data[lay.WaysAt:])}
	if err := osmpbf.Read(in, osmpbf.Handler{WantWays: true, Until: osmpbf.UntilWays, Way: func(osmpbf.Way) error { return nil }}); err != nil || in.n != len(data)-int(lay.WaysAt) {
		t.Errorf("read %d of %d bytes, err %v", in.n, len(data)-int(lay.WaysAt), err)
	}
}

func TestAnUnsortedFileIsReadInFull(t *testing.T) {
	data := sortedFile(false) // the same blocks, but the header doesn't promise their order
	var lay osmpbf.Layout
	nodes, ways := 0, 0
	in := &counting{r: bytes.NewReader(data)}
	err := osmpbf.Read(in, osmpbf.Handler{WantNodes: true, WantWays: true, Until: osmpbf.UntilWays, Layout: &lay,
		Node: func(osmpbf.Node) error { nodes++; return nil }, Way: func(osmpbf.Way) error { ways++; return nil }})
	if err != nil || nodes != 5 || ways != 3 || in.n != len(data) {
		t.Errorf("%d nodes, %d ways, %d of %d bytes, err %v", nodes, ways, in.n, len(data), err)
	}
	if lay.Sorted || lay.WaysAt <= 0 {
		t.Errorf("layout: %+v", lay)
	}
	// A file with no ways says so.
	if err := osmpbf.Read(bytes.NewReader(osmpbftest.Encode([]osmpbftest.Node{{ID: 1, Lat: 1, Lon: 1}}, nil)), osmpbf.Handler{Layout: &lay}); err != nil || lay.WaysAt != -1 {
		t.Errorf("no ways: %+v, err %v", lay, err)
	}
}
