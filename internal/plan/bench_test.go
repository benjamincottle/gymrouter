package plan_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/benjamincottle/gymrouter/internal/lines"
	"github.com/benjamincottle/gymrouter/internal/plan"
	"github.com/benjamincottle/gymrouter/internal/raptor"
	"github.com/benjamincottle/gymrouter/internal/realtime"
	"github.com/benjamincottle/gymrouter/internal/timetable"
)

// The planning benchmark runs over the real timetable, which isn't in the repo: point GYMROUTER_BENCH_DATA at a
// server data directory (complete.zip and sydneytrains.zip), e.g.
//
//	GYMROUTER_BENCH_DATA=/path/to/data go test ./internal/plan -run '^$' -bench Plan -benchmem

var bench struct {
	once     sync.Once
	net      *raptor.Network
	midnight time.Time
	err      error
}

func benchNetwork(b *testing.B) (*raptor.Network, time.Time) {
	b.Helper()
	dir := os.Getenv("GYMROUTER_BENCH_DATA")
	if dir == "" {
		b.Skip("GYMROUTER_BENCH_DATA not set")
	}
	bench.once.Do(func() {
		now := time.Now().In(sydney)
		date := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, sydney)
		set := lines.MustSet("train T1", "train T9", "metro M1", "bus 288", "bus 291", "bus 292", "bus 533", "bus 287",
			"bus 160X", "bus 283", "bus 281", "bus 280", "bus 271", "bus 207", "bus 194")
		d, err := timetable.Load(date, timetable.Paths{Complete: filepath.Join(dir, "complete.zip"),
			Trains: filepath.Join(dir, "sydneytrains.zip")}, set)
		if err != nil {
			bench.err = err
			return
		}
		bench.net = raptor.Build(d, raptor.DefaultOptions())
		bench.midnight = realtime.ServiceMidnight(date, sydney)
	})
	if bench.err != nil {
		b.Fatal(bench.err)
	}
	return bench.net, bench.midnight
}

// BenchmarkPlan plans Lane Cove gym → Chatswood gym in the afternoon the way the app asks: a 45-minute window,
// alternatives within 10 minutes.
func BenchmarkPlan(b *testing.B) {
	n, _ := benchNetwork(b)
	o := raptor.DefaultOptions()
	req := plan.Request{
		Access: n.StopsNear(gyms["lanecove"], 1000, o), Egress: n.StopsNear(gyms["chatswood"], 1000, o),
		Depart: 16 * 3600, Window: 45 * 60, MinChange: 60, MaxRides: 4, AltSlack: 600,
	}
	if len(plan.Plan(n, req)) == 0 {
		b.Fatal("no options")
	}
	b.ResetTimer()
	for b.Loop() {
		plan.Plan(n, req)
	}
}
