package plan_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/benjamincottle/gymrouter/internal/lines"
	"github.com/benjamincottle/gymrouter/internal/plan"
	"github.com/benjamincottle/gymrouter/internal/raptor"
	"github.com/benjamincottle/gymrouter/internal/timetable"
)

// On Sat 10 Oct 2026 buses replace T4 trains between Bondi Junction and Central (testdata/trackwork, see its README).
func TestTrackworkBusesStandInForTheirLine(t *testing.T) {
	date := time.Date(2026, 10, 10, 0, 0, 0, 0, sydney)
	load := func(set lines.Set) *raptor.Network {
		d, err := timetable.Load(date, timetable.Paths{Complete: filepath.Join("../../testdata/trackwork", "gtfs.zip")}, set)
		if err != nil {
			t.Fatal(err)
		}
		return raptor.Build(d, raptor.DefaultOptions())
	}
	others := []string{"train T2", "metro M1", "bus 392", "bus 320", "bus 304", "bus 343"}

	n := load(lines.MustSet(append(others, "train T4")...))
	if miss := timetable.Missing(n.Day, lines.MustSet("train T4")); len(miss) > 0 {
		t.Errorf("T4 reported missing: %v", miss)
	}
	opts := plan.Plan(n, plan.Request{
		Access: stationAccess(t, n, "Bondi Junction Station"), Egress: gymAccess(n, "waterloo"),
		Depart: clock("10:00"), Window: 1800, MinChange: 60,
	})
	if len(opts) == 0 {
		t.Fatal("no options from Bondi Junction during trackwork")
	}
	if first := opts[0].Lines[0]; first.Mode != lines.ReplacementBus {
		t.Errorf("first ride is %s, want a replacement bus", first)
	} else if l, _ := first.Replaces(); l.String() != "train T4" {
		t.Errorf("%s replaces %v, want train T4", first, l)
	}

	for _, r := range load(lines.MustSet(others...)).Day.Routes {
		if lines.ModeOf(r.Type) == lines.ReplacementBus {
			t.Errorf("T4's buses loaded without T4: %s", r.ShortName)
		}
	}
}
