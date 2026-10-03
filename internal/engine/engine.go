// Package engine keeps the routable timetable and realtime data current: it downloads the static
// feeds daily, polls realtime feeds only while the app is in use, and serves snapshots to the API.
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/benjamincottle/gymrouter/internal/config"
	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/lines"
	"github.com/benjamincottle/gymrouter/internal/raptor"
	"github.com/benjamincottle/gymrouter/internal/realtime"
	"github.com/benjamincottle/gymrouter/internal/tfnsw"
	"github.com/benjamincottle/gymrouter/internal/timetable"
)

// Fetcher is the subset of the TfNSW client the engine uses (faked in tests).
type Fetcher interface {
	Get(ctx context.Context, path string, maxBytes int64) ([]byte, error)
	Download(ctx context.Context, path, dest string, maxBytes int64) (bool, error)
}

const (
	maxStaticBytes   = 1 << 30
	maxRealtimeBytes = 64 << 20
	limitedBackoff   = 5 * time.Minute
	errorBackoff     = 30 * time.Second
	maxRealtimeAge   = 3 * time.Minute // older predictions are ignored
	maxCachedDays    = 3
	pollTick         = 2 * time.Second
)

// Snapshot is an immutable routable view of one service day.
type Snapshot struct {
	Date     time.Time // local midnight of the service date
	Midnight time.Time // GTFS service-day reference (noon − 12 h)
	Day      *gtfs.Day
	Net      *raptor.Network
	// Realtime is set when predictions were applied.
	Realtime   bool
	RealtimeAt time.Time
	Stats      realtime.Stats
	Static     *Snapshot // the same day without predictions (for today's snapshot)
}

// Clock converts a service-day offset in seconds to wall time.
func (s *Snapshot) Clock(secs int32) time.Time {
	return s.Midnight.Add(time.Duration(secs) * time.Second)
}

// Secs converts wall time to a service-day offset.
func (s *Snapshot) Secs(t time.Time) int32 { return int32(t.Sub(s.Midnight) / time.Second) }

// Vehicle is a live vehicle on one of the configured lines.
type Vehicle struct {
	ID, Label string
	Line      lines.Key
	Color     string // route colour (hex, no '#')
	TripID    string
	Lat, Lon  float64
	Bearing   *float32
	Status    string
	Timestamp time.Time
}

type feedState struct {
	feed       tfnsw.Feed
	tripsAt    time.Time
	vehiclesAt time.Time
	trips      []realtime.TripUpdate
	vehicles   []realtime.Vehicle
	lastErr    string
	errAt      time.Time
}

// Engine is safe for concurrent use.
type Engine struct {
	cfg   *config.Config
	loc   *time.Location
	fetch Fetcher
	log   *slog.Logger
	now   func() time.Time
	paths timetable.Paths
	all   lines.Set

	today      atomic.Pointer[Snapshot]
	shapes     atomic.Pointer[map[string][]geo.Point]
	lastActive atomic.Int64 // unix nanos
	wake       chan struct{}

	loadMu     sync.Mutex // serialises timetable loads
	cacheMu    sync.Mutex
	cache      map[string]*Snapshot // other dates, static only
	generation int                  // bumped when static files change

	rtMu         sync.Mutex
	feeds        []*feedState
	budgetDay    string
	budgetUsed   int
	backoffUntil time.Time
	staticAt     time.Time
	staticErr    string
	missing      []lines.Key
	lastGeocode  time.Time
}

// New creates an engine. Call Start before serving.
func New(cfg *config.Config, loc *time.Location, f Fetcher, log *slog.Logger) *Engine {
	e := &Engine{
		cfg: cfg, loc: loc, fetch: f, log: log, now: time.Now,
		paths: timetable.Paths{
			Complete: filepath.Join(cfg.Server.DataDir, "complete.zip"),
			Trains:   filepath.Join(cfg.Server.DataDir, "sydneytrains.zip"),
		},
		all:   cfg.AllLines(),
		wake:  make(chan struct{}, 1),
		cache: map[string]*Snapshot{},
	}
	for _, f := range tfnsw.FeedsFor(e.all) {
		e.feeds = append(e.feeds, &feedState{feed: f})
	}
	return e
}

// SetClock replaces the time source (tests).
func (e *Engine) SetClock(now func() time.Time) { e.now = now }

// Now returns the engine's current time.
func (e *Engine) Now() time.Time { return e.now() }

// Config returns the configuration.
func (e *Engine) Config() *config.Config { return e.cfg }

// Start initialises the engine, then runs the refresh and polling loops until ctx is cancelled.
func (e *Engine) Start(ctx context.Context) error {
	if err := e.Init(ctx); err != nil {
		return err
	}
	if fi, err := os.Stat(e.paths.Complete); err == nil && e.now().Sub(fi.ModTime()) > 20*time.Hour {
		go e.refreshStatic(ctx) // stale after downtime; don't wait for the daily refresh
	}
	go e.LoadShapes()
	go e.staticLoop(ctx)
	go e.pollLoop(ctx)
	return nil
}

// Init makes sure the static feeds are present and loads today's timetable.
func (e *Engine) Init(ctx context.Context) error {
	if err := e.ensureStatic(ctx); err != nil {
		return err
	}
	return e.reloadToday()
}

// Touch records app activity; realtime polling runs while the app was used recently.
func (e *Engine) Touch() {
	prev := e.lastActive.Swap(e.now().UnixNano())
	if time.Duration(e.now().UnixNano()-prev) > e.cfg.Realtime.ActiveFor.Duration {
		select { // was idle: poll right away
		case e.wake <- struct{}{}:
		default:
		}
	}
}

func (e *Engine) active() bool {
	return time.Duration(e.now().UnixNano()-e.lastActive.Load()) <= e.cfg.Realtime.ActiveFor.Duration
}

// LocalDate returns local midnight of t's date.
func (e *Engine) LocalDate(t time.Time) time.Time {
	t = t.In(e.loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, e.loc)
}

// SnapshotFor returns the routable snapshot for the service date of t: today's includes fresh
// realtime predictions; other dates are loaded on demand from the static timetable.
func (e *Engine) SnapshotFor(t time.Time) (*Snapshot, error) {
	date := e.LocalDate(t)
	if s := e.today.Load(); s != nil && s.Date.Equal(date) {
		if s.Realtime && e.now().Sub(s.RealtimeAt) > maxRealtimeAge {
			return s.Static, nil
		}
		return s, nil
	}
	key := date.Format("2006-01-02")
	e.cacheMu.Lock()
	s, ok := e.cache[key]
	gen := e.generation
	e.cacheMu.Unlock()
	if ok {
		return s, nil
	}
	s, err := e.load(date)
	if err != nil {
		return nil, err
	}
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	if gen == e.generation {
		if len(e.cache) >= maxCachedDays {
			e.cache = map[string]*Snapshot{} // tiny cache: just start over
		}
		e.cache[key] = s
	}
	return s, nil
}

func (e *Engine) load(date time.Time) (*Snapshot, error) {
	e.loadMu.Lock()
	defer e.loadMu.Unlock()
	start := e.now()
	d, err := timetable.Load(date, e.paths, e.all)
	if err != nil {
		return nil, err
	}
	s := &Snapshot{Date: date, Midnight: realtime.ServiceMidnight(date, e.loc), Day: d, Net: raptor.Build(d, e.routingOptions())}
	e.log.Info("timetable loaded", "date", date.Format("2006-01-02"), "trips", len(d.Trips),
		"took", e.now().Sub(start).Round(time.Millisecond).String())
	return s, nil
}

func (e *Engine) routingOptions() raptor.Options {
	o := raptor.DefaultOptions()
	o.MaxTransferM = e.cfg.Routing.MaxTransferM
	o.WalkSpeedMps = e.cfg.Routing.WalkSpeedMps
	o.DetourFactor = e.cfg.Routing.DetourFactor
	return o
}

// RoutingOptions returns the configured walking model.
func (e *Engine) RoutingOptions() raptor.Options { return e.routingOptions() }

func (e *Engine) reloadToday() error {
	date := e.LocalDate(e.now())
	s, err := e.load(date)
	if err != nil {
		return err
	}
	s.Static = s
	missing := timetable.Missing(s.Day, e.all)
	sort.Slice(missing, func(a, b int) bool { return missing[a].String() < missing[b].String() })
	e.rtMu.Lock()
	e.missing = missing
	e.rtMu.Unlock()
	if len(missing) > 0 {
		e.log.Warn("configured lines have no trips today", "lines", fmt.Sprint(missing))
	}
	e.today.Store(s)
	e.applyRealtime() // re-apply current predictions to the new day
	return nil
}

// ensureStatic downloads the static feeds if they're missing.
func (e *Engine) ensureStatic(ctx context.Context) error {
	if err := os.MkdirAll(e.cfg.Server.DataDir, 0o755); err != nil {
		return err
	}
	for path, dest := range map[string]string{tfnsw.CompleteGTFS: e.paths.Complete, tfnsw.TrainsGTFS: e.paths.Trains} {
		if _, err := os.Stat(dest); err == nil {
			continue
		}
		e.log.Info("downloading timetable", "feed", path)
		if _, err := e.fetch.Download(ctx, path, dest, maxStaticBytes); err != nil {
			return fmt.Errorf("download %s: %w", path, err)
		}
		e.countRequest()
	}
	e.rtMu.Lock()
	e.staticAt = e.now()
	if fi, err := os.Stat(e.paths.Complete); err == nil {
		e.staticAt = fi.ModTime()
	}
	e.rtMu.Unlock()
	return nil
}

// refreshStatic re-downloads the static feeds (conditional GET) and reloads when they changed.
func (e *Engine) refreshStatic(ctx context.Context) {
	changed := false
	var errs []error
	for path, dest := range map[string]string{tfnsw.CompleteGTFS: e.paths.Complete, tfnsw.TrainsGTFS: e.paths.Trains} {
		c, err := e.fetch.Download(ctx, path, dest, maxStaticBytes)
		e.countRequest()
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			continue
		}
		changed = changed || c
	}
	e.rtMu.Lock()
	if err := errors.Join(errs...); err != nil {
		e.staticErr = err.Error()
		e.log.Error("timetable refresh failed; keeping the last good copy", "err", err)
	} else {
		e.staticErr = ""
		if fi, err := os.Stat(e.paths.Complete); err == nil {
			e.staticAt = fi.ModTime()
		}
	}
	e.rtMu.Unlock()
	if changed {
		e.cacheMu.Lock()
		e.cache = map[string]*Snapshot{}
		e.generation++
		e.cacheMu.Unlock()
		if err := e.reloadToday(); err != nil {
			e.log.Error("reload after refresh failed", "err", err)
		}
		e.LoadShapes()
	}
}

// staticLoop refreshes the timetable daily and rolls over to the new service day after midnight.
func (e *Engine) staticLoop(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	lastRefresh := e.LocalDate(e.now())
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := e.now().In(e.loc)
		if s := e.today.Load(); s != nil && !s.Date.Equal(e.LocalDate(now)) {
			if err := e.reloadToday(); err != nil {
				e.log.Error("day rollover failed", "err", err)
			} else {
				go e.LoadShapes()
			}
		}
		if today := e.LocalDate(now); !today.Equal(lastRefresh) && now.Hour() >= e.cfg.Routing.StaticRefreshH {
			lastRefresh = today
			e.refreshStatic(ctx)
		}
	}
}

// pollLoop fetches realtime feeds on their intervals while the app is active.
func (e *Engine) pollLoop(ctx context.Context) {
	t := time.NewTicker(pollTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-e.wake:
		}
		if e.active() {
			e.PollOnce(ctx)
		}
	}
}

// PollOnce fetches every feed that's due (respecting backoff and the daily budget) and applies
// new trip updates. It's exported for tests.
func (e *Engine) PollOnce(ctx context.Context) {
	now := e.now()
	tripsChanged := false
	for _, fs := range e.feeds {
		if now.Sub(fs.tripsAt) >= e.cfg.Realtime.TripUpdatesEvery.Duration && e.allowRequest() {
			if b, err := e.get(ctx, fs, fs.feed.TripUpdates); err == nil {
				if f, err := realtime.Decode(b); err != nil {
					e.feedError(fs, err)
				} else {
					e.rtMu.Lock()
					fs.trips, fs.tripsAt = f.Trips, now
					e.rtMu.Unlock()
					tripsChanged = true
				}
			}
		}
		if now.Sub(fs.vehiclesAt) >= e.cfg.Realtime.VehiclesEvery.Duration && e.allowRequest() {
			if b, err := e.get(ctx, fs, fs.feed.VehiclePos); err == nil {
				if f, err := realtime.Decode(b); err != nil {
					e.feedError(fs, err)
				} else {
					e.rtMu.Lock()
					fs.vehicles, fs.vehiclesAt = f.Vehicles, now
					e.rtMu.Unlock()
				}
			}
		}
	}
	if tripsChanged {
		e.applyRealtime()
	}
}

func (e *Engine) get(ctx context.Context, fs *feedState, path string) ([]byte, error) {
	e.countRequest()
	b, err := e.fetch.Get(ctx, path, maxRealtimeBytes)
	if err != nil {
		e.feedError(fs, err)
		e.rtMu.Lock()
		backoff := errorBackoff
		if errors.Is(err, tfnsw.ErrLimited) {
			backoff = limitedBackoff
		}
		e.backoffUntil = e.now().Add(backoff)
		e.rtMu.Unlock()
		return nil, err
	}
	e.rtMu.Lock()
	fs.lastErr = ""
	e.rtMu.Unlock()
	return b, nil
}

func (e *Engine) feedError(fs *feedState, err error) {
	e.log.Warn("realtime feed error", "feed", fs.feed.Name, "err", err)
	e.rtMu.Lock()
	fs.lastErr, fs.errAt = err.Error(), e.now()
	e.rtMu.Unlock()
}

// allowRequest enforces backoff and the daily upstream budget.
func (e *Engine) allowRequest() bool {
	e.rtMu.Lock()
	defer e.rtMu.Unlock()
	now := e.now()
	if now.Before(e.backoffUntil) {
		return false
	}
	if day := e.LocalDate(now).Format("2006-01-02"); day != e.budgetDay {
		e.budgetDay, e.budgetUsed = day, 0
	}
	return e.budgetUsed < e.cfg.Realtime.DailyBudget
}

func (e *Engine) countRequest() {
	e.rtMu.Lock()
	defer e.rtMu.Unlock()
	if day := e.LocalDate(e.now()).Format("2006-01-02"); day != e.budgetDay {
		e.budgetDay, e.budgetUsed = day, 0
	}
	e.budgetUsed++
}

// applyRealtime rebuilds today's snapshot from the static day plus the latest trip updates.
func (e *Engine) applyRealtime() {
	cur := e.today.Load()
	if cur == nil {
		return
	}
	e.rtMu.Lock()
	var updates []realtime.TripUpdate
	var oldest time.Time
	for _, fs := range e.feeds {
		if fs.tripsAt.IsZero() {
			continue
		}
		updates = append(updates, fs.trips...)
		if oldest.IsZero() || fs.tripsAt.Before(oldest) {
			oldest = fs.tripsAt
		}
	}
	e.rtMu.Unlock()
	if len(updates) == 0 {
		return
	}
	static := cur.Static
	day, st := realtime.Apply(static.Day, e.loc, updates)
	s := &Snapshot{Date: static.Date, Midnight: static.Midnight, Day: day, Net: raptor.Build(day, e.routingOptions()),
		Realtime: true, RealtimeAt: oldest, Stats: st, Static: static}
	// Only publish if today hasn't been replaced meanwhile.
	e.today.CompareAndSwap(cur, s)
}

// ErrBusy is returned when geocoding is requested too often.
var ErrBusy = errors.New("too many requests; try again shortly")

// Geocode looks up an address or place via the TfNSW Trip Planner. Rate-limited to one request
// per second and counted against the daily budget. The query is never logged.
func (e *Engine) Geocode(ctx context.Context, q string) ([]tfnsw.Place, error) {
	e.rtMu.Lock()
	now := e.now()
	if now.Sub(e.lastGeocode) < time.Second {
		e.rtMu.Unlock()
		return nil, ErrBusy
	}
	e.lastGeocode = now
	e.rtMu.Unlock()
	if !e.allowRequest() {
		return nil, ErrBusy
	}
	e.countRequest()
	b, err := e.fetch.Get(ctx, tfnsw.GeocodePath(q), 1<<20)
	if err != nil {
		return nil, err
	}
	return tfnsw.ParseGeocode(b, 6)
}

// Vehicles returns live vehicles on the given lines (fresh data only).
func (e *Engine) Vehicles(set lines.Set) []Vehicle {
	s := e.today.Load()
	if s == nil {
		return nil
	}
	type lineInfo struct {
		key   lines.Key
		color string
	}
	routes := map[string]lineInfo{}
	for _, r := range s.Day.Routes {
		if k := lines.Of(r.Type, r.ShortName); set[k] {
			routes[r.ID] = lineInfo{k, r.Color}
		}
	}
	tripLine := func(id string) (lineInfo, bool) {
		for _, ti := range s.Day.TripIndex[id] {
			r := s.Day.Routes[s.Day.Trips[ti].Route]
			if k := lines.Of(r.Type, r.ShortName); set[k] {
				return lineInfo{k, r.Color}, true
			}
		}
		return lineInfo{}, false
	}
	e.rtMu.Lock()
	defer e.rtMu.Unlock()
	var out []Vehicle
	for _, fs := range e.feeds {
		if e.now().Sub(fs.vehiclesAt) > maxRealtimeAge {
			continue
		}
		for _, v := range fs.vehicles {
			k, ok := routes[v.RouteID]
			if !ok {
				if k, ok = tripLine(v.TripID); !ok {
					continue
				}
			}
			out = append(out, Vehicle{ID: v.ID, Label: v.Label, Line: k.key, Color: k.color, TripID: v.TripID, Lat: v.Lat, Lon: v.Lon,
				Bearing: v.Bearing, Status: v.Status, Timestamp: time.Unix(v.Timestamp, 0)})
		}
	}
	return out
}

// Health summarises data freshness for /healthz.
type Health struct {
	OK            bool            `json:"ok"`
	ServiceDate   string          `json:"service_date"`
	StaticAgeS    int64           `json:"static_age_s"`
	StaticError   string          `json:"static_error,omitempty"`
	PollingActive bool            `json:"polling_active"`
	RequestsToday int             `json:"upstream_requests_today"`
	Feeds         []FeedHealth    `json:"feeds"`
	MissingLines  []string        `json:"missing_lines,omitempty"`
	RealtimeStats *realtime.Stats `json:"realtime,omitempty"`
}

// FeedHealth is one realtime feed's state.
type FeedHealth struct {
	Name         string `json:"name"`
	TripsAgeS    *int64 `json:"trip_updates_age_s,omitempty"`
	VehiclesAgeS *int64 `json:"vehicles_age_s,omitempty"`
	Error        string `json:"error,omitempty"`
}

// Health reports the engine's state.
func (e *Engine) Health() Health {
	now := e.now()
	h := Health{PollingActive: e.active()}
	s := e.today.Load()
	if s != nil {
		h.ServiceDate = s.Date.Format("2006-01-02")
		if s.Realtime {
			st := s.Stats
			st.UnmatchedSample = nil
			h.RealtimeStats = &st
		}
	}
	e.rtMu.Lock()
	defer e.rtMu.Unlock()
	h.StaticAgeS = int64(now.Sub(e.staticAt).Seconds())
	h.StaticError = e.staticErr
	h.RequestsToday = e.budgetUsed
	for _, k := range e.missing {
		h.MissingLines = append(h.MissingLines, k.String())
	}
	age := func(t time.Time) *int64 {
		if t.IsZero() {
			return nil
		}
		v := int64(now.Sub(t).Seconds())
		return &v
	}
	for _, fs := range e.feeds {
		h.Feeds = append(h.Feeds, FeedHealth{Name: fs.feed.Name, TripsAgeS: age(fs.tripsAt), VehiclesAgeS: age(fs.vehiclesAt), Error: fs.lastErr})
	}
	h.OK = s != nil && h.StaticAgeS < 3*24*3600
	return h
}
