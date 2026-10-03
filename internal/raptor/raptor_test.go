package raptor

import (
	"testing"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/gtfs"
)

// Synthetic network along a line of longitude (~111 m per 0.001°):
//
//	A ──R1── B ──R1── C
//	         B'──R2── D      (B' is 100 m from B: a walking transfer)
//	A ─────────R3─────────── D   (slow direct route)
func testDay() *gtfs.Day {
	stops := []gtfs.Stop{
		{ID: "A", Pos: geo.Point{Lat: -33.800, Lon: 151.0}},
		{ID: "B", Pos: geo.Point{Lat: -33.810, Lon: 151.0}},
		{ID: "C", Pos: geo.Point{Lat: -33.820, Lon: 151.0}},
		{ID: "B2", Pos: geo.Point{Lat: -33.8109, Lon: 151.0}},
		{ID: "D", Pos: geo.Point{Lat: -33.830, Lon: 151.0}},
	}
	idx := map[string]int32{}
	for i, s := range stops {
		idx[s.ID] = int32(i)
	}
	st := func(stop string, t int32) gtfs.StopTime { return gtfs.StopTime{Stop: idx[stop], Arr: t, Dep: t} }
	return &gtfs.Day{
		Stops:     stops,
		StopIndex: idx,
		Routes:    []gtfs.Route{{ID: "R1"}, {ID: "R2"}, {ID: "R3"}},
		Trips: []gtfs.Trip{
			{ID: "r1a", Route: 0, StopTimes: []gtfs.StopTime{st("A", 1000), st("B", 1300), st("C", 1600)}},
			{ID: "r1b", Route: 0, StopTimes: []gtfs.StopTime{st("A", 2000), st("B", 2300), st("C", 2600)}},
			{ID: "r2a", Route: 1, StopTimes: []gtfs.StopTime{st("B2", 1500), st("D", 1800)}},
			{ID: "r2b", Route: 1, StopTimes: []gtfs.StopTime{st("B2", 2500), st("D", 2800)}},
			{ID: "r3", Route: 2, StopTimes: []gtfs.StopTime{st("A", 1100), st("D", 2400)}},
		},
	}
}

func query(n *Network, depart int32) Query {
	a, d := n.Day.StopIndex["A"], n.Day.StopIndex["D"]
	return Query{Depart: depart, Access: []Access{{Stop: a, Secs: 60}}, Egress: []Access{{Stop: d, Secs: 30}}, MaxRides: 3}
}

func TestParetoFrontier(t *testing.T) {
	n := Build(testDay(), DefaultOptions())
	js := n.Run(query(n, 900))
	if len(js) != 2 {
		t.Fatalf("want 2 Pareto journeys (direct slow, 1 transfer fast), got %d: %+v", len(js), js)
	}
	if js[0].Rides != 1 || js[0].Arr != 2400+30 {
		t.Errorf("direct: got rides=%d arr=%d", js[0].Rides, js[0].Arr)
	}
	if js[1].Rides != 2 || js[1].Arr != 1800+30 {
		t.Errorf("transfer: got rides=%d arr=%d", js[1].Rides, js[1].Arr)
	}
	// legs: walk, ride R1 A→B, walk B→B2, ride R2 B2→D, walk
	kinds := []LegKind{Walk, Ride, Walk, Ride, Walk}
	if len(js[1].Legs) != len(kinds) {
		t.Fatalf("transfer legs: got %d, want %d", len(js[1].Legs), len(kinds))
	}
	for i, k := range kinds {
		if js[1].Legs[i].Kind != k {
			t.Errorf("leg %d kind %d, want %d", i, js[1].Legs[i].Kind, k)
		}
	}
}

func TestMissedConnectionTakesNextTrip(t *testing.T) {
	n := Build(testDay(), DefaultOptions())
	// Departing after r1a and r3 leave: only r1b → r2b remains.
	js := n.Run(query(n, 1500))
	if len(js) != 1 || js[0].Arr != 2800+30 {
		t.Fatalf("want one journey arriving 2830, got %+v", js)
	}
}

func TestBanRoute(t *testing.T) {
	n := Build(testDay(), DefaultOptions())
	q := query(n, 900)
	q.BanRoute = func(r int32) bool { return r == 1 } // no R2
	js := n.Run(q)
	if len(js) != 1 || js[0].Rides != 1 || js[0].Arr != 2430 {
		t.Fatalf("want only the direct R3 journey, got %+v", js)
	}
}

func TestNoTransferWhenWalkTooLong(t *testing.T) {
	o := DefaultOptions()
	o.MaxTransferM = 50 // B→B2 is ~100 m
	n := Build(testDay(), o)
	js := n.Run(query(n, 900))
	if len(js) != 1 || js[0].Rides != 1 {
		t.Fatalf("want only the direct journey, got %+v", js)
	}
}
