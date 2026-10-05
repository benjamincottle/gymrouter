package engine

import (
	"testing"

	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/lines"
)

func TestCatalogLeavesOutReplacementBuses(t *testing.T) {
	d := &gtfs.Day{
		Stops:     []gtfs.Stop{{ID: "platform"}, {ID: "stand"}},
		StopIndex: map[string]int32{"platform": 0, "stand": 1},
		Routes:    []gtfs.Route{{ShortName: "T4", Type: 2}, {ShortName: "23T4", Type: 714}},
		Trips: []gtfs.Trip{
			{Route: 0, StopTimes: []gtfs.StopTime{{Stop: 0}}},
			{Route: 1, StopTimes: []gtfs.StopTime{{Stop: 0}, {Stop: 1}}},
		},
	}
	c := NewCatalog(d)
	if len(c.Stops) != 1 || len(c.Stops[0].Lines) != 1 || c.Stops[0].Lines[0].String() != "train T4" {
		t.Errorf("stops = %+v, want the platform with the T4 only", c.Stops)
	}
	if !c.Lines[lines.Key{Mode: lines.Train, Name: "T4"}] || len(c.Lines) != 1 {
		t.Errorf("lines = %v", c.Lines)
	}
}
