package plan

import (
	"testing"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/raptor"
)

// Network: home ─walk─ A ──bus 1── X (station platform X1) ; X2 (other platform) ──train T── G ─walk─ gym
// Also a slow direct bus 9 from A to G.
func testNet(t *testing.T) *raptor.Network {
	t.Helper()
	stops := []gtfs.Stop{
		{ID: "A", Pos: geo.Point{Lat: -33.800, Lon: 151.0}},
		{ID: "X1", Parent: "X", Pos: geo.Point{Lat: -33.810, Lon: 151.0}},
		{ID: "X2", Parent: "X", Pos: geo.Point{Lat: -33.8105, Lon: 151.0}}, // ~55 m from X1
		{ID: "G", Pos: geo.Point{Lat: -33.840, Lon: 151.0}},
		{ID: "X", Pos: geo.Point{Lat: -33.8102, Lon: 151.0}},
	}
	idx := map[string]int32{}
	for i, s := range stops {
		idx[s.ID] = int32(i)
	}
	st := func(s string, tm int32) gtfs.StopTime { return gtfs.StopTime{Stop: idx[s], Arr: tm, Dep: tm} }
	trips := []gtfs.Trip{
		{ID: "bus1-a", Route: 0, StopTimes: []gtfs.StopTime{st("A", 1000), st("X1", 1300)}},
		{ID: "bus1-b", Route: 0, StopTimes: []gtfs.StopTime{st("A", 1600), st("X1", 1900)}},
		{ID: "train-a", Route: 1, StopTimes: []gtfs.StopTime{st("X2", 1400), st("G", 1700)}},
		{ID: "train-b", Route: 1, StopTimes: []gtfs.StopTime{st("X2", 2000), st("G", 2300)}},
		{ID: "train-c", Route: 1, StopTimes: []gtfs.StopTime{st("X2", 2600), st("G", 2900)}},
		{ID: "bus9", Route: 2, StopTimes: []gtfs.StopTime{st("A", 1050), st("G", 2500)}},
	}
	d := &gtfs.Day{Stops: stops, StopIndex: idx, Trips: trips, TripIndex: map[string][]int32{},
		Routes: []gtfs.Route{{ID: "b1", ShortName: "1", Type: 700}, {ID: "t", ShortName: "T", Type: 2}, {ID: "b9", ShortName: "9", Type: 700}}}
	for i, tr := range trips {
		d.TripIndex[tr.ID] = []int32{int32(i)}
	}
	return raptor.Build(d, raptor.DefaultOptions())
}

func req(n *raptor.Network, depart, window int32) Request {
	return Request{
		Access: []raptor.Access{{Stop: n.Day.StopIndex["A"], Secs: 120}},
		Egress: []raptor.Access{{Stop: n.Day.StopIndex["G"], Secs: 60}},
		Depart: depart, Window: window, MinChange: 60,
	}
}

func TestPlanWindowReturnsOptionsPerDeparture(t *testing.T) {
	n := testNet(t)
	opts := Plan(n, req(n, 800, 900))
	// bus1-a+train-a (arr 1760), bus1-b+train-b (arr 2360); bus9 (arr 2560, 1 ride) leaves after
	// bus1-a but arrives later than bus1-b+train-b... it has fewer rides, so it survives.
	if len(opts) != 3 {
		for _, o := range opts {
			t.Logf("leave %d arrive %d rides %d", o.LeaveAt, o.Arrive, o.Rides)
		}
		t.Fatalf("want 3 options, got %d", len(opts))
	}
	first := opts[0]
	if first.LeaveAt != 1000-120 || first.Arrive != 1700+60 || first.Rides != 2 {
		t.Errorf("first option: leave %d arrive %d rides %d", first.LeaveAt, first.Arrive, first.Rides)
	}
	if first.Duration() != 880 {
		t.Errorf("duration %d", first.Duration())
	}
	if opts[1].Arrive != 2360 || opts[2].Rides != 1 {
		t.Errorf("unexpected order: %+v", opts)
	}
}

func TestTransferRiskAndFallback(t *testing.T) {
	n := testNet(t)
	opts := Plan(n, req(n, 800, 0))
	o := opts[0]
	if len(o.Transfers) != 1 {
		t.Fatalf("want 1 transfer, got %d", len(o.Transfers))
	}
	tr := o.Transfers[0]
	// arrive X1 1300, walk X1→X2 (~55 m → ~55 s), train departs 1400: slack ~45 s → at risk.
	if tr.Risk != AtRisk || tr.Slack <= 0 || tr.Slack >= 60 {
		t.Errorf("risk %s slack %d", tr.Risk, tr.Slack)
	}
	if tr.FallbackDep != 2000 || n.Day.Trips[tr.FallbackTrip].ID != "train-b" {
		t.Errorf("fallback %d %d", tr.FallbackTrip, tr.FallbackDep)
	}
	if o.WorstRisk() != AtRisk {
		t.Errorf("worst risk %s", o.WorstRisk())
	}
	custom := Thresholds{Safe: 30, Tight: 10}
	if custom.Classify(tr.Slack) != Safe {
		t.Errorf("custom thresholds should rate %ds safe", tr.Slack)
	}
}

func TestPersonalTransferOverride(t *testing.T) {
	n := testNet(t)
	r := req(n, 800, 0)
	r.Overrides = []TransferOverride{{From: "X", To: "X", Secs: 150}} // station-level: X1→X2 takes 2.5 min
	opts := Plan(n, r)
	// 1300 + 150 > 1400: train-a can't be made; best two-ride option now uses train-b (arr 2360),
	// which beats nothing on arrival vs bus9 (2560) → both kept.
	if opts[0].Arrive != 2360 {
		t.Fatalf("want first arrival 2360 with override, got %d", opts[0].Arrive)
	}
	if got := opts[0].Transfers[0]; got.Walk != 150 || got.Slack != 2000-1300-150 {
		t.Errorf("transfer walk %d slack %d", got.Walk, got.Slack)
	}
}

func TestAlternativesSurfaceDifferentRoute(t *testing.T) {
	n := testNet(t)
	r := req(n, 800, 0)
	r.AltSlack = 900 // bus9 arrives 800 s after the best
	opts := Plan(n, r)
	var sawBus9 bool
	for _, o := range opts {
		if o.Rides == 1 {
			sawBus9 = true
		}
	}
	if !sawBus9 {
		t.Fatalf("bus 9 alternative missing: %+v", opts)
	}
}

func TestRealtimeCancellationIsAvoided(t *testing.T) {
	n := testNet(t)
	d := n.Day
	ti := d.TripIndex["train-a"][0]
	for i := range d.Trips[ti].StopTimes {
		d.Trips[ti].StopTimes[i].Arr, d.Trips[ti].StopTimes[i].Dep = gtfs.NoTime, gtfs.NoTime
	}
	d.Trips[ti].Status = gtfs.Cancelled
	n = raptor.Build(d, raptor.DefaultOptions())
	opts := Plan(n, req(n, 800, 0))
	for _, o := range opts {
		for _, l := range o.Legs {
			if l.Kind == raptor.Ride && d.Trips[l.Trip].ID == "train-a" {
				t.Fatal("cancelled trip used")
			}
		}
	}
}
