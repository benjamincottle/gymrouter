package engine

import (
	"testing"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/gtfs"
)

// A line of 12 stops, 1 km apart; the ride is from stop 5 to stop 7.
func lineDay() *gtfs.Day {
	d := &gtfs.Day{TripIndex: map[string][]int32{"t": {0}}}
	var calls []gtfs.StopTime
	for i := range 12 {
		d.Stops = append(d.Stops, gtfs.Stop{ID: string(rune('a' + i)), Pos: geo.Point{Lat: -33.8 + float64(i)*0.009, Lon: 151.1}})
		calls = append(calls, gtfs.StopTime{Stop: int32(i)})
	}
	d.Trips = []gtfs.Trip{{ID: "t", StopTimes: calls}}
	return d
}

func TestNearRide(t *testing.T) {
	d := lineDay()
	ride := Ride{TripID: "t", From: "f", To: "h"} // calls 5 to 7
	for _, c := range []struct {
		stop string
		want bool
	}{
		{"a", false}, {"b", false}, {"c", true}, {"f", true}, {"h", true}, {"k", true}, {"l", false},
	} {
		if got := nearRide(d, Vehicle{TripID: "t", StopID: c.stop}, ride); got != c.want {
			t.Errorf("vehicle heading for %s: near %v, want %v", c.stop, got, c.want)
		}
	}
	// Without a stop in the feed, the nearest call by position decides.
	at := func(i int) Vehicle { p := d.Stops[i].Pos; return Vehicle{TripID: "t", Lat: p.Lat + 0.0005, Lon: p.Lon} }
	if !nearRide(d, at(4), ride) || nearRide(d, at(0), ride) || nearRide(d, at(11), ride) {
		t.Error("position fallback")
	}
	if nearRide(d, Vehicle{TripID: "t", StopID: "f"}, Ride{TripID: "t", From: "h", To: "f"}) {
		t.Error("a ride the trip doesn't make (wrong direction) matched")
	}
}
