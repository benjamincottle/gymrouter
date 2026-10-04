// Provisioning: the street network and the basemap fetch themselves.
package engine_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/benjamincottle/gymrouter/internal/engine/enginetest"
	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/lines"
	"github.com/benjamincottle/gymrouter/internal/osmpbf/osmpbftest"
	"github.com/benjamincottle/gymrouter/internal/walk"
)

// streetsPBF is an OSM extract with an 11 x 11 grid of residential streets around Epping.
func streetsPBF() []byte {
	var nodes []osmpbftest.Node
	var ways []osmpbftest.Way
	id := func(i, j int) int64 { return int64(1 + i*100 + j) }
	for i := 0; i < 11; i++ {
		var row, col []int64
		for j := 0; j < 11; j++ {
			nodes = append(nodes, osmpbftest.Node{ID: id(i, j), Lat: -33.775 + float64(i)*0.0005, Lon: 151.080 + float64(j)*0.0005})
			row, col = append(row, id(i, j)), append(col, id(j, i))
		}
		ways = append(ways, osmpbftest.Way{ID: int64(i + 1), Refs: row, Tags: map[string]string{"highway": "residential"}},
			osmpbftest.Way{ID: int64(100 + i), Refs: col, Tags: map[string]string{"highway": "residential"}})
	}
	return osmpbftest.Encode(nodes, ways)
}

func TestStreetNetworkIsDownloadedBuiltAndCached(t *testing.T) {
	env := enginetest.NewWith(t, noPresets, nil)
	e := env.Engine
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(streetsPBF()) }))
	defer srv.Close()
	e.SetTransport(srv.Client().Transport)
	env.Config.Data.WalkSource = srv.URL + "/Sydney.osm.pbf"

	if e.Walker() != nil || e.Health().Data.WalkReady {
		t.Fatal("no street network should exist yet")
	}
	if err := e.RefreshWalk(ctx); err != nil {
		t.Fatal(err)
	}
	g := e.Walker()
	if g == nil || g.Nodes() != 121 {
		t.Fatalf("walker after refresh: %v", g)
	}
	if d := e.Health().Data; !d.WalkReady || d.WalkAgeS == nil {
		t.Errorf("status: %+v", d)
	}
	cached, err := walk.Load(filepath.Join(env.Config.Server.DataDir, "walk.graph"))
	if err != nil || cached.Nodes() != 121 {
		t.Fatalf("cache file: %v", err)
	}
	if entries, _ := os.ReadDir(env.Config.Server.DataDir); true {
		for _, en := range entries {
			if strings.Contains(en.Name(), ".osm.pbf") || strings.HasPrefix(en.Name(), ".walk-") {
				t.Errorf("temporary file left behind: %s", en.Name())
			}
		}
	}
	r, ok := g.From(geo.Point{Lat: -33.7725, Lon: 151.0825}, 500)
	if !ok {
		t.Fatal("not on the network")
	}
	if _, ok := r.Metres(geo.Point{Lat: -33.7735, Lon: 151.0835}); !ok {
		t.Error("route on the downloaded streets failed")
	}
}

func TestStreetNetworkFailuresLeaveTheAppWorking(t *testing.T) {
	env := enginetest.NewWith(t, noPresets, nil)
	e := env.Engine
	status := 500
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != 200 {
			http.Error(w, "nope", status)
			return
		}
		w.Write([]byte("this is not an osm file"))
	}))
	defer srv.Close()
	env.Config.Data.WalkSource = srv.URL
	for _, st := range []int{500, 200} {
		status = st
		if err := e.RefreshWalk(ctx); err == nil {
			t.Errorf("status %d: expected an error", st)
		}
		if e.Walker() != nil {
			t.Errorf("status %d: a failed refresh must not install a network", st)
		}
	}
	if _, err := os.Stat(filepath.Join(env.Config.Server.DataDir, "walk.graph")); err == nil {
		t.Error("a failed refresh wrote a cache file")
	}
	// The planner still works, on estimates.
	if err := e.Ensure(lines.MustSet("metro M1", "train T9")); err != nil {
		t.Fatal(err)
	}
	snap, _ := e.SnapshotFor(env.Clock.Now())
	if ap := e.Approach(snap.Net, geo.Point{Lat: -33.7727, Lon: 151.0821}, 400, e.RoutingOptions()); ap.Streets || len(ap.Access) == 0 {
		t.Errorf("fallback approach: %+v", ap)
	}
}

func TestOversizedDownloadsAreRefused(t *testing.T) {
	env := enginetest.NewWith(t, noPresets, nil)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "999999999999")
	}))
	defer srv.Close()
	env.Engine.SetTransport(srv.Client().Transport)
	env.Config.Data.WalkSource = srv.URL
	if err := env.Engine.RefreshWalk(ctx); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("want a size-limit error, got %v", err)
	}
}

func TestBasemapIsCutWithThePMTilesTool(t *testing.T) {
	env := enginetest.NewWith(t, noPresets, nil)
	e := env.Engine
	dir := t.TempDir()
	tool := filepath.Join(dir, "pmtiles")
	// A stand-in for the real tool: writes 2 MB to the output path (its third argument) and records its arguments.
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + filepath.Join(dir, "args") + "\nhead -c 2000000 /dev/zero > \"$3\"\n"
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env.Config.Data.PMTilesBin = tool
	if err := e.RefreshMap(ctx); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(env.Config.Server.DataDir, "map.pmtiles")); err != nil || fi.Size() != 2000000 {
		t.Fatalf("map file: %v", err)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	for _, want := range []string{"extract", "https://build.protomaps.com/", "--bbox=150.55,-34.15,151.35,-33.45"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("tool arguments missing %q:\n%s", want, args)
		}
	}
	if !e.Health().Data.MapReady {
		t.Error("status should show the map as ready")
	}

	// A tool that fails, and one that isn't there, leave no map and report why.
	os.Remove(filepath.Join(env.Config.Server.DataDir, "map.pmtiles"))
	if err := os.WriteFile(tool, []byte("#!/bin/sh\necho boom >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := e.RefreshMap(ctx); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("failing tool: %v", err)
	}
	env.Config.Data.PMTilesBin = filepath.Join(dir, "missing")
	if err := e.RefreshMap(ctx); err == nil {
		t.Error("missing tool: expected an error")
	}
	if _, err := os.Stat(filepath.Join(env.Config.Server.DataDir, "map.pmtiles")); err == nil {
		t.Error("a failed run left a map file")
	}
}
