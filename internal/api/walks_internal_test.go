package api

import (
	"testing"

	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/raptor"
)

func TestTimedWalksOverrideAndAddStops(t *testing.T) {
	d := &gtfs.Day{Stops: []gtfs.Stop{
		{ID: "bus1"}, {ID: "bus2"}, {ID: "stn", LocationType: "1"}, {ID: "stn-p1", Parent: "stn"}, {ID: "stn-p2", Parent: "stn"}, {ID: "far"},
	}}
	got := withWalks(d, []raptor.Access{{Stop: 0, Secs: 300}, {Stop: 1, Secs: 400}, {Stop: 3, Secs: 500}},
		[]accessReq{{Stop: "bus2", WalkS: 200}, {Stop: "stn", WalkS: 600}, {Stop: "far", WalkS: 900}})
	want := map[int32]int32{0: 300, 1: 200, 3: 600, 4: 600, 5: 900} // a station's walk covers all its platforms
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for _, a := range got {
		if want[a.Stop] != a.Secs {
			t.Errorf("stop %s: %d s, want %d", d.Stops[a.Stop].ID, a.Secs, want[a.Stop])
		}
	}
}

func TestWalksAreChecked(t *testing.T) {
	lat, lon := -33.8, 151.1
	many := make([]accessReq, maxWalks+1)
	for i := range many {
		many[i] = accessReq{Stop: "s", WalkS: 60}
	}
	for name, p := range map[string]placeReq{
		"too many":     {Lat: &lat, Lon: &lon, Walks: many},
		"negative":     {Lat: &lat, Lon: &lon, Walks: []accessReq{{Stop: "s", WalkS: -1}}},
		"no stop":      {Lat: &lat, Lon: &lon, Walks: []accessReq{{WalkS: 60}}},
		"on a vehicle": {OnTrip: &onTripReq{TripID: "t", FromStop: "s"}, Walks: []accessReq{{Stop: "s", WalkS: 60}}},
	} {
		if checkPlace(p) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := checkPlace(placeReq{Lat: &lat, Lon: &lon, Walks: []accessReq{{Stop: "s", WalkS: 60}}}); err != nil {
		t.Error(err)
	}
}
