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

func TestOvertakingAndSkippedStops(t *testing.T) {
	d := testDay()
	// r1a is now running 20 minutes late, so r1b (scheduled after it) overtakes it.
	for i := range d.Trips[0].StopTimes {
		d.Trips[0].StopTimes[i].Arr += 1200
		d.Trips[0].StopTimes[i].Dep += 1200
	}
	// r1b skips stop A entirely: it can't be boarded there.
	d.Trips[1].StopTimes[0].Arr, d.Trips[1].StopTimes[0].Dep = gtfs.NoTime, gtfs.NoTime
	n := Build(d, DefaultOptions())
	if n.Patterns[0].fifo {
		t.Fatal("pattern with overtaking/skips should not be FIFO")
	}
	js := n.Run(query(n, 900))
	// Delayed r1a reaches B at 2500, too late to walk to B2 for r2b (2500), so direct r3 is best.
	for _, j := range js {
		for _, l := range j.Legs {
			if l.Kind == Ride && n.Day.Trips[l.Trip].ID == "r1b" {
				t.Fatal("boarded r1b at a skipped stop")
			}
		}
	}
	if len(js) == 0 || js[0].Arr != 2430 {
		t.Fatalf("want direct r3 arriving 2430 first, got %+v", js)
	}
}

func TestPathwaysSetInStationTransfers(t *testing.T) {
	d := testDay()
	// Make B and B2 platforms of one station, linked by stairs via an internal node (no coordinates)
	// and by a faster lift.
	d.Stops = append(d.Stops, gtfs.Stop{ID: "ST", Pos: d.Stops[1].Pos})
	d.StopIndex["ST"] = int32(len(d.Stops) - 1)
	d.Stops[1].Parent, d.Stops[3].Parent = "ST", "ST"
	d.Pathways = []gtfs.Pathway{
		{From: "B", To: "ST_node", Mode: gtfs.PathwayStairs, Secs: 100, Bidirectional: true},
		{From: "ST_node", To: "B2", Mode: gtfs.PathwayEscalator, Secs: 80, Bidirectional: true},
		{From: "B", To: "B2", Mode: gtfs.PathwayElevator, Secs: 30, Bidirectional: true},
	}
	n := Build(d, DefaultOptions())
	var got int32 = -1
	for _, fp := range n.Footpaths[d.StopIndex["B"]] {
		if fp.To == d.StopIndex["B2"] {
			got = fp.Secs
		}
	}
	if got != 100+80+60 {
		t.Fatalf("B→B2 transfer = %d, want stairs+escalator+allowance = 240 (lift ignored)", got)
	}
	// r1a reaches B at 1300; r2a leaves B2 at 1500: 240 s no longer makes it, and the next
	// connection (r2b, arriving 2830) is worse than the direct r3 (2430), so only r3 remains.
	js := n.Run(query(n, 900))
	if len(js) != 1 || js[0].Rides != 1 || js[0].Arr != 2430 {
		t.Errorf("want only the direct journey, got %+v", js)
	}

	// Without the stairs, the lift is the only way.
	d.Pathways = d.Pathways[2:]
	n = Build(d, DefaultOptions())
	for _, fp := range n.Footpaths[d.StopIndex["B"]] {
		if fp.To == d.StopIndex["B2"] && fp.Secs != 30+60 {
			t.Errorf("lift fallback: %d", fp.Secs)
		}
	}
}
