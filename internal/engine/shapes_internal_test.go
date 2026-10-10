package engine

import (
	"io"
	"log/slog"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/lines"
	"github.com/benjamincottle/gymrouter/internal/timetable"
)

// A bus out along a street to a turning loop and back the same way: getting on and off on the way back is a short
// ride, not out to the loop and back.
func TestLegGeometryAlongAStreetBothWays(t *testing.T) {
	m := 1.0 / 111320
	k := math.Cos(-33.8 * math.Pi / 180)
	at := func(x, y float64) geo.Point { return geo.Point{Lat: -33.8 + y*m, Lon: 151 + x*m/k} }
	d := &gtfs.Day{TripIndex: map[string][]int32{"t": {0}}}
	var calls []gtfs.StopTime
	// Kerbside stops: out on the north side, back on the south.
	for i, s := range []struct {
		x, y float64
		dist float32
	}{{0, 6, 0}, {1000, 6, 1000}, {2000, 0, 2000}, {1500, -6, 2500}, {1000, -6, 3000}, {0, -6, 4000}} {
		d.Stops = append(d.Stops, gtfs.Stop{ID: string(rune('a' + i)), Pos: at(s.x, s.y)})
		calls = append(calls, gtfs.StopTime{Stop: int32(i), Dist: s.dist})
	}
	d.Trips = []gtfs.Trip{{ID: "t", Shape: "s", StopTimes: calls}}
	street := []geo.Point{at(0, 0), at(1000, 0), at(2000, 0), at(1000, 0), at(0, 0)}
	e := &Engine{}
	snap := &Snapshot{Day: d}
	for _, c := range []struct {
		name string
		dist []float32
		long bool // drawn out to the loop and back
	}{
		{"by distance", []float32{0, 1000, 2000, 3000, 4000}, false},
		{"by position (no distances in the feed)", nil, true}, // why the feed's distances are used
	} {
		e.shapes.Store(&shapeSet{byID: map[string]gtfs.Shape{"s": {Pts: street, Dist: c.dist}}})
		path, stops, ok := e.LegGeometry(snap, "t", "e", "f") // 1000 m on the way back
		if !ok {
			t.Fatalf("%s: no geometry", c.name)
		}
		l := 0.0
		for i := 1; i < len(path); i++ {
			l += geo.DistanceM(path[i-1], path[i])
		}
		if got := l > 2000; got != c.long || (!c.long && math.Abs(l-1000) > 5) {
			t.Errorf("%s: path %.0f m: %v", c.name, l, path)
		}
		if len(stops) != 0 {
			t.Errorf("%s: stops passed %v", c.name, stops)
		}
	}
	// Stops passed are placed by distance too.
	e.shapes.Store(&shapeSet{byID: map[string]gtfs.Shape{"s": {Pts: street, Dist: []float32{0, 1000, 2000, 3000, 4000}}}})
	if _, stops, _ := e.LegGeometry(snap, "t", "c", "f"); len(stops) != 2 || geo.DistanceM(stops[0], at(1500, 0)) > 1 ||
		geo.DistanceM(stops[1], at(1000, 0)) > 1 {
		t.Errorf("stops passed %v", stops)
	}
}

// A train is drawn along its own track, to the platform it uses, when the complete bundle has one for the trip:
// its shape in the trains bundle is one line for the whole route, here 40 m east of the platforms.
func TestLegGeometryFollowsATrainsOwnTrack(t *testing.T) {
	m := 1.0 / 111320
	k := math.Cos(-33.8 * math.Pi / 180)
	at := func(x, y float64) geo.Point { return geo.Point{Lat: -33.8 + y*m, Lon: 151 + x*m/k} }
	d := &gtfs.Day{TripIndex: map[string][]int32{"t": {0}}}
	nan := float32(math.NaN())
	for i, y := range []float64{0, 1000, 2000} { // platforms, 2 m beside the track
		d.Stops = append(d.Stops, gtfs.Stop{ID: string(rune('a' + i)), Pos: at(2, y)})
	}
	d.Trips = []gtfs.Trip{{ID: "t", Shape: "route", StopTimes: []gtfs.StopTime{{Stop: 0, Dist: nan}, {Stop: 1, Dist: nan}, {Stop: 2, Dist: nan}}}}
	route := gtfs.Shape{Pts: []geo.Point{at(40, -500), at(40, 2500)}}
	e := &Engine{}
	snap := &Snapshot{Day: d}
	for _, c := range []struct {
		name  string
		track []geo.Point
		x     float64 // where the ride is drawn, metres east
	}{
		{"its track", []geo.Point{at(0, -500), at(0, 2500)}, 0},
		{"no track for the trip", nil, 40},
		// The complete bundle's trip was cut short: its track doesn't reach where this ride gets off.
		{"a track that stops short", []geo.Point{at(0, -500), at(0, 1500)}, 40},
		// It has the trip the other way round: the platforms are beside it, in the wrong order.
		{"a track the other way", []geo.Point{at(0, 2500), at(0, -500)}, 40},
	} {
		set := shapeSet{byID: map[string]gtfs.Shape{"route": route}}
		if c.track != nil {
			set.byID["track"], set.track = gtfs.Shape{Pts: c.track}, map[string]string{"t": "track"}
		}
		e.shapes.Store(&set)
		path, stops, ok := e.LegGeometry(snap, "t", "a", "c")
		if !ok || len(path) != 2 || len(stops) != 1 {
			t.Fatalf("%s: path %v, stops %v, ok %v", c.name, path, stops, ok)
		}
		for i, want := range []geo.Point{at(c.x, 0), at(c.x, 2000)} {
			if geo.DistanceM(path[i], want) > 0.5 {
				t.Errorf("%s: end %d of the path is %.1f m from the line it should follow", c.name, i, geo.DistanceM(path[i], want))
			}
		}
		if geo.DistanceM(stops[0], at(c.x, 1000)) > 0.5 { // the stop passed is on the same line
			t.Errorf("%s: stop passed at %v", c.name, stops[0])
		}
	}
}

// The trains bundle has the trips and one line per route; the complete bundle has the same trips with their tracks.
func TestLoadShapesFindsTrainTracksInTheCompleteBundle(t *testing.T) {
	date := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	p := func(lat float64) geo.Point { return geo.Point{Lat: lat, Lon: 151} }
	day := func(tripShapes map[string]string) *gtfs.Day {
		d := &gtfs.Day{Date: date, Routes: []gtfs.Route{{ID: "r", ShortName: "T1", Type: 2}},
			Stops: []gtfs.Stop{{ID: "a", Pos: p(-33.8)}, {ID: "b", Pos: p(-33.81)}}}
		for _, id := range []string{"t", "u"} { // u is only in the trains bundle
			if shape, ok := tripShapes[id]; ok {
				d.Trips = append(d.Trips, gtfs.Trip{ID: id, Shape: shape,
					StopTimes: []gtfs.StopTime{{Stop: 0, Seq: 1, Arr: 8 * 3600, Dep: 8 * 3600}, {Stop: 1, Seq: 2, Arr: 8*3600 + 120, Dep: 8*3600 + 120}}})
			}
		}
		return d
	}
	line := gtfs.Shape{Pts: []geo.Point{p(-33.8), p(-33.81)}}
	dir := t.TempDir()
	paths := timetable.Paths{Complete: filepath.Join(dir, "complete.zip"), Trains: filepath.Join(dir, "trains.zip")}
	if err := gtfs.WriteZip(paths.Complete, map[string]gtfs.Shape{"track": line}, day(map[string]string{"t": "track"})); err != nil {
		t.Fatal(err)
	}
	if err := gtfs.WriteZip(paths.Trains, map[string]gtfs.Shape{"route": line}, day(map[string]string{"t": "route", "u": "route"})); err != nil {
		t.Fatal(err)
	}
	d, err := timetable.Load(date, paths, lines.MustSet("train T1"))
	if err != nil {
		t.Fatal(err)
	}
	snap := &Snapshot{Day: d}
	snap.Static = snap
	e := &Engine{log: slog.New(slog.NewTextHandler(io.Discard, nil)), now: time.Now, paths: paths}
	e.today.Store(snap)
	e.LoadShapes()
	set := e.shapes.Load()
	if len(d.Trips) != 2 || len(set.track) != 1 || set.track["t"] != "track" {
		t.Fatalf("trips %d, tracks %v", len(d.Trips), set.track)
	}
	for _, id := range []string{"track", "route"} { // the route's line too: u has no track, and t's might not fit a ride
		if len(set.byID[id].Pts) != 2 {
			t.Errorf("shape %q not loaded: %v", id, set.byID)
		}
	}
}
