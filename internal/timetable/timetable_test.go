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
