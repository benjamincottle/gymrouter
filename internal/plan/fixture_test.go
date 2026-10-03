package plan_test

import (
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/lines"
	"github.com/benjamincottle/gymrouter/internal/plan"
	"github.com/benjamincottle/gymrouter/internal/raptor"
	"github.com/benjamincottle/gymrouter/internal/realtime"
	"github.com/benjamincottle/gymrouter/internal/timetable"
)

// Golden tests over a trimmed real TfNSW timetable (testdata/fixture, see its README).
// Regenerate expected output with: go test ./internal/plan -run Fixture -update

var update = flag.Bool("update", false, "rewrite golden files")

const fixtureDir = "../../testdata/fixture"

var fixtureLines = lines.MustSet("train T1", "train T9", "metro M1", "bus 288", "bus 291", "bus 292",
	"bus 533", "bus 287", "bus 52", "bus 521", "bus 523", "bus 524", "bus 281", "bus 283", "bus 160X",
	"bus 207", "bus 271", "bus 194")

// Gyms: public addresses from 9degrees.com.au/locations, geocoded.
var gyms = map[string]geo.Point{
	"lanecove":  {Lat: -33.807948, Lon: 151.150629},
	"chatswood": {Lat: -33.7856633, Lon: 151.2003558},
	"rydalmere": {Lat: -33.816213, Lon: 151.039465},
}

var sydney = func() *time.Location {
	l, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		panic(err)
	}
	return l
}()

func loadFixture(t *testing.T, date string) *gtfs.Day {
	t.Helper()
	dt, err := time.ParseInLocation("2006-01-02", date, sydney)
	if err != nil {
		t.Fatal(err)
	}
	d, err := timetable.Load(dt, timetable.Paths{Complete: filepath.Join(fixtureDir, "gtfs.zip")}, fixtureLines)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// stationAccess returns the platforms of the named station with a fixed 3-minute walk to each.
func stationAccess(t *testing.T, n *raptor.Network, name string) []raptor.Access {
	t.Helper()
	var out []raptor.Access
	for i, s := range n.Day.Stops {
		if s.Parent == "" {
			continue
		}
		if p, ok := n.Day.StopIndex[s.Parent]; ok && n.Day.Stops[p].Name == name {
			out = append(out, raptor.Access{Stop: int32(i), Secs: 180})
		}
	}
	if len(out) == 0 {
		t.Fatalf("station %q not in fixture", name)
	}
	return out
}

func gymAccess(n *raptor.Network, gym string) []raptor.Access {
	return n.StopsNear(gyms[gym], 800, raptor.DefaultOptions())
}

func clock(hhmm string) int32 {
	t, _ := time.Parse("15:04", hhmm)
	return int32(t.Hour()*3600 + t.Minute()*60)
}

type fixtureCase struct {
	name, date, from, to, at string
	window, altSlack         int32
}

// Real usage windows: Sunday morning (arrive around opening) and Thursday from ~16:00.
var fixtureCases = []fixtureCase{
	{"epping-lanecove-thu", "2026-10-08", "Epping Station", "lanecove", "16:30", 1200, 0},
	{"chatswood-gym-sun", "2026-10-04", "Chatswood Station", "chatswood", "08:30", 1200, 0},
	{"eastwood-rydalmere-thu", "2026-10-08", "Eastwood Station", "rydalmere", "16:30", 1200, 0},
	// T1 to St Leonards then a bus is a known alternative to going via the metro.
	{"pymble-lanecove-alternatives-thu", "2026-10-08", "Pymble Station", "lanecove", "16:00", 0, 1500},
}

func TestFixturePlans(t *testing.T) {
	for _, c := range fixtureCases {
		t.Run(c.name, func(t *testing.T) {
			n := raptor.Build(loadFixture(t, c.date), raptor.DefaultOptions())
			opts := plan.Plan(n, plan.Request{
				Access: stationAccess(t, n, c.from), Egress: gymAccess(n, c.to),
				Depart: clock(c.at), Window: c.window, MinChange: 60, AltSlack: c.altSlack,
			})
			if len(opts) == 0 {
				t.Fatal("no options")
			}
			var b strings.Builder
			for _, o := range opts {
				b.WriteString(plan.Describe(n.Day, o))
			}
			golden(t, c.name, b.String())
		})
	}
}

func TestFixtureStLeonardsAlternative(t *testing.T) {
	n := raptor.Build(loadFixture(t, "2026-10-08"), raptor.DefaultOptions())
	opts := plan.Plan(n, plan.Request{
		Access: stationAccess(t, n, "Pymble Station"), Egress: gymAccess(n, "lanecove"),
		Depart: clock("16:00"), MinChange: 60, AltSlack: 1500,
	})
	for _, o := range opts {
		for _, l := range o.Legs {
			if l.Kind == raptor.Ride && strings.HasPrefix(n.Day.Stops[l.To].Name, "St Leonards Station") {
				return
			}
		}
	}
	t.Error("no option via St Leonards")
}

// TestFixtureRealtime applies the recorded realtime snapshot (Sat 3 Oct 2026, 12:37) and checks
// that it matches the timetable and changes the plan's times.
func TestFixtureRealtime(t *testing.T) {
	d := loadFixture(t, "2026-10-03")
	var updates []realtime.TripUpdate
	var snapshot int64
	files, _ := filepath.Glob(filepath.Join(fixtureDir, "tu-*.pb"))
	sort.Strings(files)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		feed, err := realtime.Decode(b)
		if err != nil {
			t.Fatal(err)
		}
		updates = append(updates, feed.Trips...)
		snapshot = max(snapshot, feed.Timestamp)
	}
	rd, st := realtime.Apply(d, sydney, updates)
	t.Logf("realtime: %+v", st)
	matched := st.Matched + st.MatchedByRun
	if st.Updates == 0 || float64(matched+st.Added)/float64(st.Updates) < 0.9 {
		t.Errorf("too few realtime updates matched: %+v", st)
	}
	predicted := 0
	for _, tr := range rd.Trips {
		if tr.Status == gtfs.Predicted {
			predicted++
		}
	}
	if predicted == 0 {
		t.Fatal("no trips have predictions")
	}

	now := int32(snapshot - realtime.ServiceMidnight(d.Date, sydney).Unix())
	n := raptor.Build(rd, raptor.DefaultOptions())
	opts := plan.Plan(n, plan.Request{
		Access: stationAccess(t, n, "Epping Station"), Egress: gymAccess(n, "lanecove"),
		Depart: now, Window: 1200, MinChange: 60,
	})
	if len(opts) == 0 {
		t.Fatal("no options with realtime")
	}
	var b strings.Builder
	b.WriteString("snapshot " + plan.Clock(now) + "\n")
	for _, o := range opts {
		b.WriteString(plan.Describe(n.Day, o))
	}
	golden(t, "realtime-epping-lanecove-sat", b.String())
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (run with -update to create)", err)
	}
	if string(want) != got {
		t.Errorf("%s mismatch\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}
