package engine_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/benjamincottle/gymrouter/internal/engine"
	"github.com/benjamincottle/gymrouter/internal/engine/enginetest"
	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/lines"
	"github.com/benjamincottle/gymrouter/internal/tfnsw"
)

var ctx = context.Background()

func TestInitLoadsTodayWithoutRealtime(t *testing.T) {
	env := enginetest.New(t, "", nil)
	s, err := env.Engine.SnapshotFor(env.Clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if s.Realtime || s.Date.Format("2006-01-02") != "2026-10-03" || len(s.Day.Trips) == 0 {
		t.Fatalf("snapshot: realtime=%v date=%v trips=%d", s.Realtime, s.Date, len(s.Day.Trips))
	}
	if env.Fetcher.Downloads != 0 {
		t.Error("static files exist; nothing should be downloaded")
	}
	if h := env.Engine.Health(); !h.OK || h.ServiceDate != "2026-10-03" {
		t.Errorf("health: %+v", h)
	}
}

func TestPollingOnlyWhileActive(t *testing.T) {
	env := enginetest.New(t, "", nil)
	h := env.Engine.Health()
	if h.PollingActive {
		t.Fatal("should start idle")
	}
	env.Engine.Touch()
	if !env.Engine.Health().PollingActive {
		t.Fatal("Touch should activate polling")
	}
	env.Clock.Advance(11 * time.Minute)
	if env.Engine.Health().PollingActive {
		t.Fatal("polling should stop 10 min after the last request")
	}
}

func TestPollSchedulesAndAppliesRealtime(t *testing.T) {
	env := enginetest.New(t, "", nil)
	feeds := len(tfnsw.FeedsFor(enginetest.Preload)) // trains, metro, buses
	env.Engine.PollOnce(ctx)
	if got := env.Fetcher.CallCount(); got != 2*feeds {
		t.Fatalf("first poll: %d calls, want %d", got, 2*feeds)
	}
	s, err := env.Engine.SnapshotFor(env.Clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !s.Realtime || s.Stats.Matched == 0 {
		t.Fatalf("realtime not applied: %+v", s.Stats)
	}
	if s.Static == nil || s.Static.Realtime {
		t.Error("realtime snapshot should keep its static base")
	}

	env.Engine.PollOnce(ctx) // nothing due yet
	if got := env.Fetcher.CallCount(); got != 2*feeds {
		t.Fatalf("immediate re-poll made requests: %d", got)
	}
	env.Clock.Advance(16 * time.Second) // vehicles due (15 s), trip updates not (30 s)
	env.Engine.PollOnce(ctx)
	if tu, vp := env.Fetcher.FeedCalls("/realtime/"), env.Fetcher.FeedCalls("/vehiclepos/"); tu != feeds || vp != 2*feeds {
		t.Fatalf("after 16 s: tu=%d vp=%d", tu, vp)
	}
	env.Clock.Advance(15 * time.Second)
	env.Engine.PollOnce(ctx)
	if tu := env.Fetcher.FeedCalls("/realtime/"); tu != 2*feeds {
		t.Fatalf("after 31 s: tu=%d", tu)
	}
	if v := env.Engine.Vehicles(lines.MustSet("metro M1")); len(v) == 0 {
		t.Error("no metro vehicles")
	}
	if h := env.Engine.Health(); h.RequestsToday != 5*feeds || h.RealtimeStats == nil {
		t.Errorf("health: %+v", h)
	}
}

func TestStaleRealtimeFallsBackToTimetable(t *testing.T) {
	env := enginetest.New(t, "", nil)
	env.Engine.PollOnce(ctx)
	env.Clock.Advance(4 * time.Minute)
	s, err := env.Engine.SnapshotFor(env.Clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if s.Realtime {
		t.Error("4-minute-old predictions should not be used")
	}
	if len(env.Engine.Vehicles(lines.MustSet("metro M1"))) != 0 {
		t.Error("stale vehicles should not be returned")
	}
}

func TestRateLimitBacksOff(t *testing.T) {
	env := enginetest.New(t, "", nil)
	env.Fetcher.Err = tfnsw.ErrLimited
	env.Engine.PollOnce(ctx)
	first := env.Fetcher.CallCount()
	if first != 1 {
		t.Fatalf("should stop after the first rejected request, made %d", first)
	}
	if h := env.Engine.Health(); h.Feeds[0].Error == "" {
		t.Error("health should report the feed error")
	}
	env.Fetcher.Err = nil
	env.Clock.Advance(4 * time.Minute)
	env.Engine.PollOnce(ctx)
	if env.Fetcher.CallCount() != first {
		t.Fatal("polled during backoff")
	}
	env.Clock.Advance(2 * time.Minute)
	env.Engine.PollOnce(ctx)
	if env.Fetcher.CallCount() == first {
		t.Fatal("did not resume after backoff")
	}
}

func TestDailyBudgetIsAHardCap(t *testing.T) {
	env := enginetest.New(t, "[realtime]\ndaily_budget = 4\n", nil)
	for i := 0; i < 5; i++ {
		env.Engine.PollOnce(ctx)
		env.Clock.Advance(time.Minute)
	}
	if got := env.Fetcher.CallCount(); got != 4 {
		t.Fatalf("made %d requests with a budget of 4", got)
	}
	env.Clock.Advance(24 * time.Hour) // new day, new budget
	env.Engine.PollOnce(ctx)
	if env.Fetcher.CallCount() == 4 {
		t.Fatal("budget did not reset the next day")
	}
}

func TestOtherDatesLoadOnDemand(t *testing.T) {
	env := enginetest.New(t, "", nil)
	thu := time.Date(2026, 10, 8, 16, 0, 0, 0, enginetest.Sydney)
	s1, err := env.Engine.SnapshotFor(thu)
	if err != nil {
		t.Fatal(err)
	}
	s2, _ := env.Engine.SnapshotFor(thu)
	if s1 != s2 || s1.Realtime || s1.Date.Format("2006-01-02") != "2026-10-08" {
		t.Fatalf("other-date snapshot not cached or wrong: %v %v", s1.Date, s1 == s2)
	}
	if got := s1.Secs(thu); got != 16*3600 {
		t.Errorf("Secs = %d", got)
	}
	if !s1.Clock(16 * 3600).Equal(thu) {
		t.Errorf("Clock = %v", s1.Clock(16*3600))
	}
}

const noPresets = `
[server]
public_url = "https://gym.example.com"
`

func TestStartsEmptyAndLoadsLinesOnDemand(t *testing.T) {
	env := enginetest.NewWith(t, noPresets, nil)
	e := env.Engine
	if len(e.Lines()) != 0 {
		t.Fatalf("lines before any request: %v", e.Lines())
	}
	if s, _ := e.SnapshotFor(env.Clock.Now()); len(s.Day.Trips) != 0 || len(e.Health().Feeds) != 0 {
		t.Fatalf("nothing should be loaded or polled yet: %d trips, %d feeds", len(s.Day.Trips), len(e.Health().Feeds))
	}

	metro := lines.MustSet("metro M1")
	if err := e.Ensure(metro); err != nil {
		t.Fatal(err)
	}
	s1, _ := e.SnapshotFor(env.Clock.Now())
	if len(s1.Day.Trips) == 0 || len(e.Health().Feeds) != 1 {
		t.Fatalf("after M1: %d trips, %d feeds", len(s1.Day.Trips), len(e.Health().Feeds))
	}
	// Already covered: no reload.
	if err := e.Ensure(metro); err != nil {
		t.Fatal(err)
	}
	if s2, _ := e.SnapshotFor(env.Clock.Now()); s2 != s1 {
		t.Error("a covered request must not reload the timetable")
	}

	if err := e.Ensure(lines.MustSet("train T9", "bus 288")); err != nil {
		t.Fatal(err)
	}
	s3, _ := e.SnapshotFor(env.Clock.Now())
	if len(s3.Day.Trips) <= len(s1.Day.Trips) || len(e.Lines()) != 3 || len(e.Health().Feeds) != 3 {
		t.Errorf("after growing: %d trips, lines %v, %d feeds", len(s3.Day.Trips), e.Lines(), len(e.Health().Feeds))
	}
	// Other dates reflect the grown set too.
	thu := time.Date(2026, 10, 8, 16, 0, 0, 0, enginetest.Sydney)
	if s, _ := e.SnapshotFor(thu); len(s.Day.TripIndex) == 0 {
		t.Error("other dates should load the grown set")
	}
}

func TestLoadedLinesAreCapped(t *testing.T) {
	env := enginetest.NewWith(t, noPresets, nil)
	set := lines.Set{}
	for i := 0; i <= engine.MaxLoadedLines; i++ {
		set[lines.Key{Mode: lines.Bus, Name: fmt.Sprint(i)}] = true
	}
	if err := env.Engine.Ensure(set); !errors.Is(err, engine.ErrTooManyLines) {
		t.Errorf("want ErrTooManyLines, got %v", err)
	}
	if len(env.Engine.Lines()) != 0 {
		t.Error("a rejected request must not change the loaded lines")
	}
}

func TestCatalogListsEveryLineAtAStop(t *testing.T) {
	env := enginetest.NewWith(t, noPresets, nil) // no lines loaded: the catalogue still knows the network
	cat := env.Engine.Catalog()
	if cat == nil {
		t.Fatal("catalogue not built")
	}
	near := cat.Near(geo.Point{Lat: -33.7727, Lon: 151.0821}, 300, env.Engine.RoutingOptions())
	if len(near) == 0 {
		t.Fatal("no stops near Epping")
	}
	seen := map[string]bool{}
	for _, n := range near {
		for _, k := range n.Lines {
			seen[k.String()] = true
		}
	}
	if !seen["metro M1"] || !seen["train T9"] {
		t.Errorf("lines near Epping: %v", seen)
	}
	for i := 1; i < len(near); i++ {
		if near[i].WalkS < near[i-1].WalkS {
			t.Fatal("not nearest first")
		}
	}
}

func TestSuggestFindsTheLinesBetweenTwoPlaces(t *testing.T) {
	env := enginetest.NewWith(t, noPresets, nil)
	results, err := env.Engine.Suggest(ctx,
		engine.SuggestPlace{Pos: geo.Point{Lat: -33.7727, Lon: 151.0821}},         // Epping
		[]engine.SuggestPlace{{Pos: geo.Point{Lat: -33.807948, Lon: 151.150629}}}, // Lane Cove
		1200)
	if err != nil || len(results) != 1 {
		t.Fatal(err, results)
	}
	res := results[0]
	if len(res.Windows) == 0 || len(res.Lines) == 0 || len(res.Itineraries) == 0 {
		t.Fatalf("suggest: %+v", res)
	}
	have := map[string]suggestLine{}
	for _, l := range res.Lines {
		have[l.Line.String()] = suggestLine{l.Share, l.Recommended}
	}
	if m := have["metro M1"]; !m.rec {
		t.Errorf("metro M1 should be recommended Epping → Lane Cove: %v", have)
	}
	if len(env.Engine.Lines()) != 0 {
		t.Error("suggesting must not load lines into the routable timetable")
	}
}

type suggestLine struct {
	share float64
	rec   bool
}

func TestSuggestRunsOneAtATimeAndNeedsStops(t *testing.T) {
	env := enginetest.NewWith(t, noPresets, nil)
	_, err := env.Engine.Suggest(ctx, engine.SuggestPlace{Pos: geo.Point{Lat: -20, Lon: 130}},
		[]engine.SuggestPlace{{Pos: geo.Point{Lat: -33.8, Lon: 151.15}}}, 1200)
	if !errors.Is(err, engine.ErrNoStops) {
		t.Errorf("want ErrNoStops, got %v", err)
	}
}
