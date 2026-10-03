// Package enginetest builds an engine over the real-data test fixture with a fake TfNSW client
// and a controllable clock. For tests only.
package enginetest

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/benjamincottle/gymrouter/internal/config"
	"github.com/benjamincottle/gymrouter/internal/engine"
	"github.com/benjamincottle/gymrouter/internal/tfnsw"
)

// Sydney is the timetable timezone.
var Sydney = func() *time.Location {
	l, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		panic(err)
	}
	return l
}()

// SnapshotTime is when the fixture's realtime snapshot was captured.
var SnapshotTime = time.Date(2026, 10, 3, 12, 37, 30, 0, Sydney)

// Config uses gym-side lines that exist in the fixture.
const Config = `
[server]
public_url = "https://gym.example.com"

[[gym]]
id = "lanecove"
name = "9 Degrees Lane Cove"
lat = -33.807948
lon = 151.150629
lines = ["train T1", "train T9", "metro M1", "bus 288", "bus 291", "bus 292", "bus 533", "bus 287"]

[[gym]]
id = "chatswood"
name = "9 Degrees Chatswood"
lat = -33.7856633
lon = 151.2003558
lines = ["train T1", "metro M1", "bus 160X", "bus 283", "bus 281", "bus 271", "bus 207", "bus 194"]
`

// Fetcher serves the fixture's realtime files and records calls.
type Fetcher struct {
	mu        sync.Mutex
	dir       string
	Calls     []string
	Downloads int
	Err       error // returned by Get when set
}

// Get returns the fixture file for a realtime feed path.
func (f *Fetcher) Get(_ context.Context, path string, _ int64) ([]byte, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, path)
	err := f.Err
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(path, "/v1/tp/stop_finder") {
		return []byte(`{"locations":[{"name":"1 Example St, Epping","type":"singlehouse","coord":[-33.77,151.08]},{"name":"No coords","type":"street"}]}`), nil
	}
	for _, feed := range tfnsw.Feeds {
		kind := ""
		switch path {
		case feed.TripUpdates:
			kind = "tu"
		case feed.VehiclePos:
			kind = "vp"
		default:
			continue
		}
		b, err := os.ReadFile(filepath.Join(f.dir, kind+"-"+feed.Name+".pb"))
		if os.IsNotExist(err) {
			return []byte{}, nil // feed not in fixture: empty message
		}
		return b, err
	}
	return nil, os.ErrNotExist
}

// Download pretends the static feeds are unchanged.
func (f *Fetcher) Download(context.Context, string, string, int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Downloads++
	return false, nil
}

// CallCount returns how many realtime requests were made.
func (f *Fetcher) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Calls)
}

// Clock is a settable time source.
type Clock struct {
	mu sync.Mutex
	t  time.Time
}

// Now returns the current fake time.
func (c *Clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }

// Advance moves the clock forward.
func (c *Clock) Advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

// Env is an initialised fixture engine.
type Env struct {
	Engine  *engine.Engine
	Fetcher *Fetcher
	Clock   *Clock
	Config  *config.Config
}

func fixtureDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "fixture")
}

// New returns an initialised engine at SnapshotTime. extraConfig is appended to Config.
func New(t testing.TB, extraConfig string, log io.Writer) *Env {
	t.Helper()
	cfg, err := config.Parse(Config + extraConfig)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.DataDir = t.TempDir()
	src, err := os.ReadFile(filepath.Join(fixtureDir(), "gtfs.zip"))
	if err != nil {
		t.Fatal(err)
	}
	// The fixture holds trains and other modes in one zip, so it can serve as both bundles.
	for _, name := range []string{"complete.zip", "sydneytrains.zip"} {
		if err := os.WriteFile(filepath.Join(cfg.Server.DataDir, name), src, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if log == nil {
		log = io.Discard
	}
	f := &Fetcher{dir: fixtureDir()}
	clk := &Clock{t: SnapshotTime}
	e := engine.New(cfg, Sydney, f, slog.New(slog.NewTextHandler(log, nil)))
	e.SetClock(clk.Now)
	if err := e.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	return &Env{Engine: e, Fetcher: f, Clock: clk, Config: cfg}
}

// FeedCalls counts calls whose path contains substr.
func (f *Fetcher) FeedCalls(substr string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.Calls {
		if strings.Contains(c, substr) {
			n++
		}
	}
	return n
}
