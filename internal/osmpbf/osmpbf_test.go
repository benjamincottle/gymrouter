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
