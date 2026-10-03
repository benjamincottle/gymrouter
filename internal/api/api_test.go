package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/benjamincottle/gymrouter/internal/api"
	"github.com/benjamincottle/gymrouter/internal/engine/enginetest"
)

const token = "test-token-0123456789abcdefghijklmnopqrstuvwxyz"

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

type harness struct {
	env *enginetest.Env
	h   http.Handler
	log *syncBuffer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessWeb(t, nil)
}

func newHarnessWeb(t *testing.T, web fs.FS) *harness {
	t.Helper()
	logs := &syncBuffer{}
	env := enginetest.New(t, "", logs)
	auth, err := api.NewAuth(token)
	if err != nil {
		t.Fatal(err)
	}
	srv := api.New(env.Engine, auth, slog.New(slog.NewJSONHandler(logs, nil)), web)
	return &harness{env: env, h: srv.Handler(), log: logs}
}

func (h *harness) do(t *testing.T, method, path, auth string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	if s, ok := body.(string); ok {
		rd = strings.NewReader(s)
	} else if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	return rec
}

func TestAuth(t *testing.T) {
	h := newHarness(t)
	if rec := h.do(t, "GET", "/api/gyms", "", nil); rec.Code != 401 {
		t.Errorf("no token: %d", rec.Code)
	}
	if rec := h.do(t, "GET", "/api/gyms", token+"x", nil); rec.Code != 401 {
		t.Errorf("wrong token: %d", rec.Code)
	}
	if rec := h.do(t, "GET", "/api/nonexistent", "", nil); rec.Code != 401 {
		t.Errorf("unknown API paths must also need the token: %d", rec.Code)
	}
	rec := h.do(t, "GET", "/api/gyms", token, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"lanecove"`) {
		t.Fatalf("gyms: %d %s", rec.Code, rec.Body)
	}
	if _, err := api.NewAuth("short"); err == nil {
		t.Error("short tokens must be rejected")
	}
	if tok, err := api.NewToken(); err != nil || len(tok) < api.MinTokenLen {
		t.Errorf("NewToken: %q %v", tok, err)
	}
}

func TestHealthzIsPublicAndMinimal(t *testing.T) {
	h := newHarness(t)
	rec := h.do(t, "GET", "/healthz", "", nil)
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != 200 || got["ok"] != true || len(got) != 2 {
		t.Errorf("healthz: %d %v", rec.Code, got)
	}
	if rec := h.do(t, "GET", "/api/status", token, nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "feeds") {
		t.Errorf("status: %d %s", rec.Code, rec.Body)
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := newHarness(t)
	rec := h.do(t, "GET", "/api/gyms", token, nil)
	for k, want := range map[string]string{
		"Content-Security-Policy": "default-src 'self'",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
		"Cache-Control":           "no-store",
	} {
		if !strings.Contains(rec.Header().Get(k), want) {
			t.Errorf("%s = %q", k, rec.Header().Get(k))
		}
	}
}

// Epping Station (public), 2026-10-08 16:30 Sydney (+11:00).
var eppingToLaneCove = map[string]any{
	"from":       map[string]any{"lat": -33.7727, "lon": 151.0821},
	"to":         map[string]any{"gym": "lanecove"},
	"time":       "2026-10-08T16:30:00+11:00",
	"window_min": 20,
	"prefs":      map[string]any{"max_walk_m": 400, "risk": map[string]any{"safe_s": 120, "tight_s": 30}},
}

type planResp struct {
	ServiceDate string `json:"service_date"`
	Realtime    bool   `json:"realtime"`
	Options     []struct {
		LeaveAt   string   `json:"leave_at"`
		Arrive    string   `json:"arrive"`
		DurationS int      `json:"duration_s"`
		Lines     []string `json:"lines"`
		Risk      string   `json:"risk"`
		Legs      []struct {
			Kind string `json:"kind"`
			Line *struct {
				Mode, Name, Color string
			} `json:"line"`
			From *struct{ Name, Station string } `json:"from"`
		} `json:"legs"`
	} `json:"options"`
}

func TestPlan(t *testing.T) {
	h := newHarness(t)
	rec := h.do(t, "POST", "/api/plan", token, eppingToLaneCove)
	if rec.Code != 200 {
		t.Fatalf("plan: %d %s", rec.Code, rec.Body)
	}
	var p planResp
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.ServiceDate != "2026-10-08" || p.Realtime || len(p.Options) == 0 {
		t.Fatalf("plan: %+v", p)
	}
	o := p.Options[0]
	if !strings.HasPrefix(o.LeaveAt, "2026-10-08T16:") || !strings.HasSuffix(o.LeaveAt, "+11:00") {
		t.Errorf("leave_at %q", o.LeaveAt)
	}
	if o.Legs[0].Kind != "walk" || o.Legs[0].From != nil || o.Legs[1].Line == nil || o.Legs[1].Line.Color == "" {
		t.Errorf("legs: %+v", o.Legs)
	}
	if o.Lines[0] != "metro M1" {
		t.Errorf("expected the metro first, got %v", o.Lines)
	}
}

func TestPlanWithRealtimeToday(t *testing.T) {
	h := newHarness(t)
	h.env.Engine.PollOnce(context.Background())
	req := map[string]any{"from": eppingToLaneCove["from"], "to": eppingToLaneCove["to"]}
	rec := h.do(t, "POST", "/api/plan", token, req)
	var p planResp
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != 200 || !p.Realtime || p.ServiceDate != "2026-10-03" || len(p.Options) == 0 {
		t.Fatalf("realtime plan: %d %s", rec.Code, rec.Body)
	}
}

func TestPlanCuratedAccessAndGymToHome(t *testing.T) {
	h := newHarness(t)
	// Find Epping's platforms via stops/near, then use them as curated access stops.
	rec := h.do(t, "POST", "/api/stops/near", token, map[string]any{"lat": -33.7727, "lon": 151.0821, "radius_m": 300})
	var near struct {
		Stops []struct {
			ID, Station string
			WalkS       int      `json:"walk_s"`
			Lines       []string `json:"lines"`
		} `json:"stops"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &near)
	var access []map[string]any
	for _, s := range near.Stops {
		if s.Station == "Epping Station" {
			access = append(access, map[string]any{"stop": s.ID, "walk_s": 240})
		}
	}
	if rec.Code != 200 || len(access) == 0 {
		t.Fatalf("stops near: %d %s", rec.Code, rec.Body)
	}
	req := map[string]any{
		"from": map[string]any{"gym": "lanecove"},
		"to":   map[string]any{"lat": -33.7727, "lon": 151.0821, "access": access},
		"time": "2026-10-08T19:30:00+11:00",
	}
	rec = h.do(t, "POST", "/api/plan", token, req)
	var p planResp
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != 200 || len(p.Options) == 0 {
		t.Fatalf("gym → home: %d %s", rec.Code, rec.Body)
	}
	last := p.Options[0].Legs[len(p.Options[0].Legs)-1]
	if last.Kind != "walk" {
		t.Errorf("last leg %+v", last)
	}
}

func TestPlanValidation(t *testing.T) {
	h := newHarness(t)
	place := map[string]any{"lat": -33.77, "lon": 151.08}
	gym := map[string]any{"gym": "lanecove"}
	for name, body := range map[string]any{
		"no gym":          map[string]any{"from": place, "to": place},
		"unknown gym":     map[string]any{"from": place, "to": map[string]any{"gym": "nope"}},
		"unknown field":   map[string]any{"from": place, "to": gym, "extra": 1},
		"bad lat":         map[string]any{"from": map[string]any{"lat": 200, "lon": 1}, "to": gym},
		"missing coords":  map[string]any{"from": map[string]any{}, "to": gym},
		"window":          map[string]any{"from": place, "to": gym, "window_min": 100000},
		"far future":      map[string]any{"from": place, "to": gym, "time": "2027-01-01T10:00:00+11:00"},
		"walk speed":      map[string]any{"from": place, "to": gym, "prefs": map[string]any{"walk_speed_mps": 50}},
		"risk order":      map[string]any{"from": place, "to": gym, "prefs": map[string]any{"risk": map[string]any{"safe_s": 10, "tight_s": 60}}},
		"bad access stop": map[string]any{"from": map[string]any{"lat": -33.77, "lon": 151.08, "access": []any{map[string]any{"stop": "nope", "walk_s": 60}}}, "to": gym},
		"not json":        "{not json",
		"trailing":        `{"from":{"gym":"lanecove"},"to":{"lat":1,"lon":1}} {}`,
		"too big":         `{"from":{"gym":"lanecove","x":"` + strings.Repeat("a", 70<<10) + `"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if rec := h.do(t, "POST", "/api/plan", token, body); rec.Code != 400 {
				t.Errorf("want 400, got %d %s", rec.Code, rec.Body)
			}
		})
	}
	req := httptest.NewRequest("POST", "/api/plan", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Errorf("missing content type: %d", rec.Code)
	}
}

func TestVehiclesAndActivity(t *testing.T) {
	h := newHarness(t)
	if h.env.Engine.Health().PollingActive {
		t.Fatal("should be idle before any request")
	}
	if rec := h.do(t, "GET", "/api/vehicles?gym=nope", token, nil); rec.Code != 400 {
		t.Errorf("unknown gym: %d", rec.Code)
	}
	if !h.env.Engine.Health().PollingActive {
		t.Fatal("an API request should activate polling")
	}
	h.env.Engine.PollOnce(context.Background())
	rec := h.do(t, "GET", "/api/vehicles?gym=lanecove", token, nil)
	var v struct {
		Vehicles []struct {
			Line     string
			Lat, Lon float64
		}
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	if rec.Code != 200 || len(v.Vehicles) == 0 || v.Vehicles[0].Lat > -33 {
		t.Fatalf("vehicles: %d %s", rec.Code, rec.Body)
	}
	// Unauthenticated requests must not keep polling alive.
	h.env.Clock.Advance(11 * 60e9)
	h.do(t, "GET", "/api/gyms", "", nil)
	if h.env.Engine.Health().PollingActive {
		t.Error("unauthenticated request activated polling")
	}
}

func TestPrivateDataNeverLogged(t *testing.T) {
	h := newHarness(t)
	req := map[string]any{
		"from": map[string]any{"lat": -33.7731234, "lon": 151.0824321},
		"to":   map[string]any{"gym": "lanecove"},
		"time": "2026-10-08T16:30:00+11:00",
	}
	if rec := h.do(t, "POST", "/api/plan", token, req); rec.Code != 200 {
		t.Fatalf("plan: %d %s", rec.Code, rec.Body)
	}
	h.do(t, "GET", "/api/vehicles?gym=lanecove&secret=-33.7731234", token, nil)
	logs := h.log.String()
	if !strings.Contains(logs, `"path":"/api/plan"`) {
		t.Fatalf("expected request logs, got %s", logs)
	}
	for _, s := range []string{"7731234", "0824321", token} {
		if strings.Contains(logs, s) {
			t.Errorf("logs contain %q", s)
		}
	}
}

func TestGeocode(t *testing.T) {
	h := newHarness(t)
	rec := h.do(t, "POST", "/api/geocode", token, map[string]any{"q": "1 Example St, Epping"})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"lat":-33.77`) || strings.Contains(rec.Body.String(), "No coords") {
		t.Fatalf("geocode: %d %s", rec.Code, rec.Body)
	}
	if rec := h.do(t, "POST", "/api/geocode", token, map[string]any{"q": "1 Example St, Epping"}); rec.Code != 429 {
		t.Errorf("second request within a second: %d", rec.Code)
	}
	if rec := h.do(t, "POST", "/api/geocode", token, map[string]any{"q": "ab"}); rec.Code != 400 {
		t.Errorf("short query: %d", rec.Code)
	}
	if strings.Contains(h.log.String(), "Example St") {
		t.Error("address leaked into logs")
	}
	if !strings.Contains(h.env.Fetcher.Calls[len(h.env.Fetcher.Calls)-1], "name_sf=1+Example+St") {
		t.Errorf("query not passed upstream: %v", h.env.Fetcher.Calls)
	}
}

func TestServesFrontendAlongsideAPI(t *testing.T) {
	web := fstest.MapFS{
		"index.html":        {Data: []byte("<!doctype html><title>app</title>")},
		"assets/app-abc.js": {Data: []byte("console.log(1)")},
	}
	h := newHarnessWeb(t, web)
	rec := h.do(t, "GET", "/", "", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<title>app") || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("index: %d %q %q", rec.Code, rec.Header().Get("Cache-Control"), rec.Body)
	}
	rec = h.do(t, "GET", "/assets/app-abc.js", "", nil)
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("asset: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	if rec := h.do(t, "POST", "/", "", "{}"); rec.Code != 405 {
		t.Errorf("POST /: %d", rec.Code)
	}
	if rec := h.do(t, "GET", "/api/gyms", "", nil); rec.Code != 401 {
		t.Errorf("API still needs the token next to the frontend: %d", rec.Code)
	}
	if rec := h.do(t, "GET", "/api/gyms", token, nil); rec.Code != 200 {
		t.Errorf("API with token: %d", rec.Code)
	}
}

func TestLegShapeLineShapesAndMap(t *testing.T) {
	h := newHarness(t)
	rec := h.do(t, "POST", "/api/plan", token, eppingToLaneCove)
	var p struct {
		ServiceDate string `json:"service_date"`
		Options     []struct {
			Legs []struct {
				Kind   string
				TripID string `json:"trip_id"`
				From   struct{ ID string }
				To     struct{ ID string }
			}
		}
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	var q string
	for _, l := range p.Options[0].Legs {
		if l.Kind == "ride" {
			q = "/api/shape?date=" + p.ServiceDate + "&trip=" + l.TripID + "&from=" + l.From.ID + "&to=" + l.To.ID
			break
		}
	}
	shape := func() int {
		rec := h.do(t, "GET", q, token, nil)
		var s struct{ Coordinates [][2]float64 }
		_ = json.Unmarshal(rec.Body.Bytes(), &s)
		if rec.Code != 200 || len(s.Coordinates) < 2 || s.Coordinates[0][0] < 150 {
			t.Fatalf("shape %s: %d %s", q, rec.Code, rec.Body)
		}
		return len(s.Coordinates)
	}
	before := shape() // straight lines between stops until shapes load
	if rec := h.do(t, "GET", "/api/shapes?gym=lanecove", token, nil); !strings.Contains(rec.Body.String(), `"features":[]`) {
		t.Errorf("line shapes before loading: %s", rec.Body)
	}
	h.env.Engine.LoadShapes()
	if after := shape(); after <= before {
		t.Errorf("route shape should have more detail than stops: %d vs %d points", after, before)
	}
	rec = h.do(t, "GET", "/api/shapes?gym=lanecove", token, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"line":"metro M1"`) || !strings.Contains(rec.Body.String(), "MultiLineString") {
		t.Errorf("line shapes: %d %.300s", rec.Code, rec.Body)
	}
	if rec := h.do(t, "GET", "/api/shape?date=2026-10-08&trip=nope&from=a&to=b", token, nil); rec.Code != 404 {
		t.Errorf("unknown trip: %d", rec.Code)
	}
	if rec := h.do(t, "GET", "/api/shape?date=bad&trip=a&from=a&to=b", token, nil); rec.Code != 400 {
		t.Errorf("bad date: %d", rec.Code)
	}

	if rec := h.do(t, "GET", "/api/map.pmtiles", token, nil); rec.Code != 404 {
		t.Errorf("missing map: %d", rec.Code)
	}
	if err := os.WriteFile(filepath.Join(h.env.Config.Server.DataDir, "map.pmtiles"), []byte("PMTiles-test-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/map.pmtiles", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Range", "bytes=0-6")
	rr := httptest.NewRecorder()
	h.h.ServeHTTP(rr, req)
	if rr.Code != 206 || rr.Body.String() != "PMTiles" || !strings.Contains(rr.Header().Get("Cache-Control"), "private") {
		t.Errorf("range request: %d %q %q", rr.Code, rr.Body, rr.Header().Get("Cache-Control"))
	}
	if rec := h.do(t, "GET", "/api/map.pmtiles", "", nil); rec.Code != 401 {
		t.Errorf("map without token: %d", rec.Code)
	}
}

type rideLeg struct {
	Kind   string
	TripID string `json:"trip_id"`
	Dep    string
	Arr    string
	From   struct{ ID string }
	To     struct{ ID string }
}

type simplePlan struct {
	Options []struct {
		LeaveAt string `json:"leave_at"`
		Arrive  string
		Rides   int
		Legs    []rideLeg
	}
}

func TestPlanFromOnboard(t *testing.T) {
	h := newHarness(t)
	req := map[string]any{"from": eppingToLaneCove["from"], "to": eppingToLaneCove["to"]}
	var before simplePlan
	_ = json.Unmarshal(h.do(t, "POST", "/api/plan", token, req).Body.Bytes(), &before)
	if len(before.Options) == 0 {
		t.Fatal("no options to board")
	}
	o := before.Options[0]
	var ride rideLeg
	for _, l := range o.Legs {
		if l.Kind == "ride" {
			ride = l
			break
		}
	}
	dep, _ := time.Parse(time.RFC3339, ride.Dep)
	h.env.Clock.Advance(dep.Sub(h.env.Clock.Now()) + 2*time.Minute) // two minutes after boarding

	onboard := map[string]any{
		"from": map[string]any{"on_trip": map[string]any{"trip_id": ride.TripID, "from_stop": ride.From.ID}},
		"to":   map[string]any{"gym": "lanecove"},
	}
	rec := h.do(t, "POST", "/api/plan", token, onboard)
	var after simplePlan
	_ = json.Unmarshal(rec.Body.Bytes(), &after)
	if rec.Code != 200 || len(after.Options) == 0 {
		t.Fatalf("onboard plan: %d %s", rec.Code, rec.Body)
	}
	first := after.Options[0].Legs[0]
	if first.Kind != "ride" || first.TripID != ride.TripID || first.From.ID != ride.From.ID {
		t.Errorf("first leg should be the trip we're on: %+v", first)
	}
	if after.Options[0].Arrive > o.Arrive {
		t.Errorf("staying on the planned trip should arrive no later: %s vs %s", after.Options[0].Arrive, o.Arrive)
	}
	bad := map[string]any{"from": map[string]any{"on_trip": map[string]any{"trip_id": "nope", "from_stop": "x"}}, "to": map[string]any{"gym": "lanecove"}}
	if rec := h.do(t, "POST", "/api/plan", token, bad); rec.Code != 400 {
		t.Errorf("unknown trip: %d", rec.Code)
	}
	withTime := map[string]any{"from": onboard["from"], "to": onboard["to"], "time": "2026-10-03T15:00:00+10:00"}
	if rec := h.do(t, "POST", "/api/plan", token, withTime); rec.Code != 400 {
		t.Errorf("on_trip with a time: %d", rec.Code)
	}
}

func TestPlanArriveBy(t *testing.T) {
	h := newHarness(t)
	req := map[string]any{
		"from": eppingToLaneCove["from"], "to": eppingToLaneCove["to"],
		"time": "2026-10-08T17:30:00+11:00", "arrive_by": true,
		"prefs": eppingToLaneCove["prefs"],
	}
	rec := h.do(t, "POST", "/api/plan", token, req)
	var p simplePlan
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != 200 || len(p.Options) == 0 {
		t.Fatalf("arrive by: %d %s", rec.Code, rec.Body)
	}
	for i, o := range p.Options {
		if o.Arrive > "2026-10-08T17:30:00+11:00" {
			t.Errorf("option %d arrives %s, after the deadline", i, o.Arrive)
		}
		if i > 0 && o.LeaveAt > p.Options[i-1].LeaveAt {
			t.Errorf("options not latest-departure first: %s after %s", o.LeaveAt, p.Options[i-1].LeaveAt)
		}
	}
	if p.Options[0].LeaveAt < "2026-10-08T16:45" {
		t.Errorf("latest departure %s is suspiciously early for a 17:30 deadline", p.Options[0].LeaveAt)
	}
	noTime := map[string]any{"from": req["from"], "to": req["to"], "arrive_by": true}
	if rec := h.do(t, "POST", "/api/plan", token, noTime); rec.Code != 400 {
		t.Errorf("arrive_by without time: %d", rec.Code)
	}
}
