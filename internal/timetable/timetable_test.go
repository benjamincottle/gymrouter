package timetable

import (
	"testing"

	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/lines"
)

func TestMissing(t *testing.T) {
	d := &gtfs.Day{
		Routes: []gtfs.Route{{ShortName: "288", Type: 700}},
		Trips:  []gtfs.Trip{{Route: 0}},
	}
	got := Missing(d, lines.MustSet("bus 288", "bus 287"))
	if len(got) != 1 || got[0] != (lines.Key{Mode: lines.Bus, Name: "287"}) {
		t.Errorf("Missing = %v", got)
	}
}

func TestMissingCountsReplacementBuses(t *testing.T) {
	d := &gtfs.Day{
		Routes: []gtfs.Route{{ShortName: "23T4", Type: 714}},
		Trips:  []gtfs.Trip{{Route: 0}},
	}
	if got := Missing(d, lines.MustSet("train T4")); len(got) != 0 {
		t.Errorf("a line replaced by buses all day reported missing: %v", got)
	}
}

func TestUnknownTrackwork(t *testing.T) {
	// Stations A and B; the T4 calls at A's platform, the buses at stands belonging to the stations.
	d := &gtfs.Day{
		Stops: []gtfs.Stop{{ID: "A"}, {ID: "A1", Parent: "A"}, {ID: "A-stand", Parent: "A"}, {ID: "B-stand", Parent: "B"}},
		Routes: []gtfs.Route{
			{ShortName: "T4", Type: 2},
			{ShortName: "12X9", Type: 714}, // an unknown code, at our station
			{ShortName: "13X9", Type: 714}, // an unknown code, elsewhere
			{ShortName: "5B", Type: 714},   // an event shuttle
			{ShortName: "23T4", Type: 714}, // known
			{ShortName: "10M", Type: 700},  // named like a metro replacement bus, typed as an ordinary bus
		},
		Trips: []gtfs.Trip{
			{Route: 0, StopTimes: []gtfs.StopTime{{Stop: 1}}},
			{Route: 1, StopTimes: []gtfs.StopTime{{Stop: 3}, {Stop: 2}}},
			{Route: 2, StopTimes: []gtfs.StopTime{{Stop: 3}}},
			{Route: 3, StopTimes: []gtfs.StopTime{{Stop: 2}}},
			{Route: 4, StopTimes: []gtfs.StopTime{{Stop: 2}}},
			{Route: 5, StopTimes: []gtfs.StopTime{{Stop: 2}}},
		},
	}
	got := UnknownTrackwork(d, lines.MustSet("train T4"))
	if len(got) != 2 || got[0].String() != "bus 10M" || got[1].String() != "replacement-bus 12X9" {
		t.Errorf("UnknownTrackwork = %v, want [bus 10M replacement-bus 12X9]", got)
	}
}
