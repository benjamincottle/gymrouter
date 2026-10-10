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
	// A station ID stands for its rail platforms only: it doesn't reach the bus stop X1 that belongs to the station.
	r.Overrides = []TransferOverride{{From: "X", To: "X", Secs: 150}}
	if opts := Plan(n, r); opts[0].Arrive == 2360 {
		t.Fatal("a station-level change time applied to a bus stop of the station")
	}
	// The bus stop by its own ID, the train by its station: X1→X2 takes 2.5 min.
	r.Overrides = []TransferOverride{{From: "X1", To: "X", Secs: 150}}
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

func TestAllowRestrictsRoutes(t *testing.T) {
	n := testNet(t)
	r := req(n, 800, 0)
	r.AltSlack = 900
	r.Allow = func(route int32) bool { return route != 2 } // no bus 9, even as an alternative
	for _, o := range Plan(n, r) {
		if o.Rides == 1 {
			t.Fatal("bus 9 used despite Allow")
		}
	}
}

func TestOnboardReplansFromTheVehicle(t *testing.T) {
	n := testNet(t)
	d := n.Day
	busA := d.TripIndex["bus1-a"][0]
	ob := Onboard{Trip: busA, BoardCall: 0}
	r := Request{
		Access: OnboardAccess(n, &ob, 1100, nil), Egress: []raptor.Access{{Stop: d.StopIndex["G"], Secs: 60}},
		Depart: 1100, MinChange: 60, Onboard: &ob,
	}
	opts := Plan(n, r)
	if len(opts) == 0 {
		t.Fatal("no options from onboard")
	}
	o := opts[0]
	if o.Legs[0].Kind != raptor.Ride || d.Trips[o.Legs[0].Trip].ID != "bus1-a" || o.Legs[0].Arr != 1300 {
		t.Fatalf("first leg should be the bus we're on: %+v", o.Legs[0])
	}
	if o.Rides != 2 || o.Arrive != 1760 || len(o.Transfers) != 1 || o.LeaveAt != 1100 {
		t.Errorf("option: rides %d arrive %d transfers %d leave %d", o.Rides, o.Arrive, len(o.Transfers), o.LeaveAt)
	}
	// Get off at X1, walk to platform X2, train: the walk is its own leg and the change is rated.
	if o.Legs[1].Kind != raptor.Walk || o.Legs[1].From != d.StopIndex["X1"] || o.Legs[1].To != d.StopIndex["X2"] {
		t.Errorf("walk after getting off: %+v", o.Legs[1])
	}
	if tr := o.Transfers[0]; tr.Risk != AtRisk || tr.FromLeg != 0 {
		t.Errorf("change risk from the vehicle we're on: %+v", tr)
	}

	// On the direct bus 9: staying on is the answer.
	bus9 := d.TripIndex["bus9"][0]
	ob9 := Onboard{Trip: bus9, BoardCall: 0}
	r.Access, r.Onboard, r.Depart = OnboardAccess(n, &ob9, 1500, nil), &ob9, 1500
	opts = Plan(n, r)
	if len(opts) == 0 || opts[0].Rides != 1 || opts[0].Arrive != 2560 || opts[0].Legs[0].Kind != raptor.Ride {
		t.Fatalf("stay on bus 9: %+v", opts)
	}
	if got := OnboardAccess(n, &ob9, 3000, nil); len(got) != 0 {
		t.Errorf("finished trip should have no remaining stops: %v", got)
	}
}

func TestArriveBy(t *testing.T) {
	n := testNet(t)
	opts := ArriveBy(n, req(n, 0, 0), 2400, 3600)
	if len(opts) < 2 {
		t.Fatalf("want at least 2 options, got %+v", opts)
	}
	if opts[0].LeaveAt != 1600-120 || opts[0].Arrive != 2360 {
		t.Errorf("latest departure first: leave %d arrive %d", opts[0].LeaveAt, opts[0].Arrive)
	}
	for _, o := range opts {
		if o.Arrive > 2400 {
			t.Errorf("option arrives after the deadline: %d", o.Arrive)
		}
	}
	if len(ArriveBy(n, req(n, 0, 0), 1000, 3600)) != 0 {
		t.Error("nothing arrives by 1000")
	}
}

func TestMergedWalksKeepTheStopBetween(t *testing.T) {
	got := mergeWalks([]raptor.Leg{
		{Kind: raptor.Ride, From: 0, To: 1, Dep: 0, Arr: 100},
		{Kind: raptor.Walk, From: 1, To: 2, Dep: 100, Arr: 160},
		{Kind: raptor.Walk, From: 2, To: -1, Dep: 160, Arr: 400},
	})
	if len(got) != 2 {
		t.Fatalf("legs: %+v", got)
	}
	if w := got[1]; w.From != 1 || w.To != -1 || w.Arr != 400 || len(w.Via) != 1 || w.Via[0] != 2 {
		t.Errorf("joined walk: %+v", w)
	}
}

func TestCheckDescribesTheTripKeptToWhateverASearchWouldReturn(t *testing.T) {
	n := testNet(t)
	d := n.Day
	ride := func(trip string, board, alight int32) Kept {
		return Kept{Trip: d.TripIndex[trip][0], Board: board, Alight: alight}
	}
	r := req(n, 800, 0)

	// The planned trip, before setting off: 120 s to the stop for the 1000 bus, leaving at 800.
	o, catch, ok := Check(n, r, []Kept{ride("bus1-a", 0, 1), ride("train-a", 0, 1)})
	if !ok || catch != 80 || o.Arrive != 1760 || o.Rides != 2 || o.LeaveAt != 880 || len(o.Legs) != 5 {
		t.Fatalf("planned trip: ok %v catch %d arrive %d rides %d leave %d legs %d", ok, catch, o.Arrive, o.Rides, o.LeaveAt, len(o.Legs))
	}
	if tr := o.Transfers[0]; tr.Risk != AtRisk || tr.FromLeg != 1 || tr.ToLeg != 3 || tr.FallbackDep != 2000 {
		t.Errorf("its change: %+v", tr)
	}
	// Too late to reach the stop: still described, with how short you are.
	r.Depart = 950
	if _, catch, ok := Check(n, r, []Kept{ride("bus1-a", 0, 1), ride("train-a", 0, 1)}); !ok || catch != -70 {
		t.Errorf("leaving at 950: ok %v catch %d", ok, catch)
	}
	r.Depart = 800

	// A trip the search drops (the later train from the same bus: bus 9 gets there no later with fewer rides... and
	// the earlier train beats it) is still a trip you can be on.
	for _, p := range Plan(n, r) {
		if len(p.Legs) > 3 && d.Trips[p.Legs[3].Trip].ID == "train-b" && d.Trips[p.Legs[1].Trip].ID == "bus1-a" {
			t.Fatal("the search was expected to drop bus1-a with train-b")
		}
	}
	o, _, ok = Check(n, r, []Kept{ride("bus1-a", 0, 1), ride("train-b", 0, 1)})
	if !ok || o.Arrive != 2360 || o.Transfers[0].Risk != Safe {
		t.Errorf("the dropped trip: ok %v arrive %d transfers %+v", ok, o.Arrive, o.Transfers)
	}

	// Your own time for the change counts, and a change there's no time for is Missed, not gone.
	r.Overrides = []TransferOverride{{From: "X1", To: "X", Secs: 150}}
	o, _, ok = Check(n, r, []Kept{ride("bus1-a", 0, 1), ride("train-a", 0, 1)})
	if !ok || o.Transfers[0].Walk != 150 || o.Transfers[0].Slack != -50 || o.Transfers[0].Risk != Missed {
		t.Errorf("timed change: ok %v %+v", ok, o.Transfers)
	}
	r.Overrides = nil

	// On the bus, just past its time at your stop: you're still on that trip, and the search can still start there.
	ob := Onboard{Trip: d.TripIndex["bus1-a"][0], BoardCall: 0, AlightCall: 1}
	r.Onboard, r.Depart = &ob, 1320
	r.Access = OnboardAccess(n, &ob, 1320, nil)
	if len(r.Access) == 0 {
		t.Fatal("the stop you're getting off at should stay reachable")
	}
	o, catch, ok = Check(n, r, []Kept{ride("bus1-a", 0, 1), ride("train-a", 0, 1)})
	if !ok || catch != 0 || o.Legs[0].Kind != raptor.Ride || o.Arrive != 1760 || o.Transfers[0].Risk != AtRisk || o.LeaveAt != 1320 {
		t.Errorf("aboard: ok %v catch %d arrive %d %+v", ok, catch, o.Arrive, o.Transfers)
	}
	if opts := Plan(n, r); len(opts) == 0 || opts[0].Arrive != 1760 {
		t.Errorf("the search from the stop you're getting off at: %+v", opts)
	}
	// Still aboard as the train's time nears: the walk (about 55 s) has to fit in what's left, and once it can't,
	// the change is missed, not on track for ever.
	r.Depart = 1390
	if o, _, _ := Check(n, r, []Kept{ride("bus1-a", 0, 1), ride("train-a", 0, 1)}); o.Transfers[0].Risk != Missed || o.Transfers[0].Slack >= 0 {
		t.Errorf("aboard with 10 s to the train: %+v", o.Transfers)
	}
	r.Depart = 1320

	// A stop already passed that isn't yours is gone, as before.
	other := Onboard{Trip: ob.Trip, BoardCall: 0}
	if got := OnboardAccess(n, &other, 1320, nil); len(got) != 0 {
		t.Errorf("passed stops: %v", got)
	}
	// Your own time for the walk from the stop you get off at counts from the vehicle too.
	acc := OnboardAccess(n, &Onboard{Trip: ob.Trip, BoardCall: 0}, 1100, []TransferOverride{{From: "X1", To: "X", Secs: 150}})
	for _, a := range acc {
		if a.Stop == d.StopIndex["X2"] && a.Secs != 200+150 {
			t.Errorf("X2 from the bus with a timed change: %d s", a.Secs)
		}
	}

	// A ride that no longer runs, or a place the trip doesn't reach: it can't be described.
	r = req(n, 800, 0)
	d.Trips[d.TripIndex["train-a"][0]].Status = gtfs.Cancelled
	if _, _, ok := Check(n, r, []Kept{ride("bus1-a", 0, 1), ride("train-a", 0, 1)}); ok {
		t.Error("a cancelled ride should not check out")
	}
	r.Egress = []raptor.Access{{Stop: d.StopIndex["A"], Secs: 60}}
	if _, _, ok := Check(n, r, []Kept{ride("bus9", 0, 1)}); ok {
		t.Error("a trip that doesn't end near the place should not check out")
	}
}
