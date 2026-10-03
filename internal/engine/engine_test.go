package engine_test

import (
	"context"
	"testing"
	"time"

	"github.com/benjamincottle/gymrouter/internal/engine/enginetest"
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
	feeds := len(tfnsw.FeedsFor(env.Config.AllLines())) // trains, metro, buses
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
