package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/benjamincottle/gymrouter/internal/api"
	"github.com/benjamincottle/gymrouter/internal/engine"
	"github.com/benjamincottle/gymrouter/internal/engine/enginetest"
	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/walk/walktest"
)

// laneCoveQuery is laneCoveLines as a ?lines= value.
var laneCoveQuery = url.QueryEscape(strings.Join(laneCoveLines, ","))

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
	return newHarnessWith(t, web, func(logs *syncBuffer) *enginetest.Env { return enginetest.New(t, "", logs) })
}

// newHarnessEmpty preloads nothing, so the server starts without any lines loaded.
func newHarnessEmpty(t *testing.T) *harness {
	t.Helper()
	return newHarnessWith(t, nil, func(logs *syncBuffer) *enginetest.Env {
		return enginetest.NewWith(t, "[server]\npublic_url = \"https://gym.example.com\"\n", logs)
	})
}

func newHarnessWith(t *testing.T, web fs.FS, mk func(*syncBuffer) *enginetest.Env) *harness {
	t.Helper()
	logs := &syncBuffer{}
	env := mk(logs)
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
	// Without the token there's nothing here: the same plain 404 as Go's (and Traefik's) for a missing page.
	if rec := h.do(t, "GET", "/api/defaults", "", nil); rec.Code != 404 || rec.Body.String() != "404 page not found\n" ||
		!strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") || rec.Header().Get("WWW-Authenticate") != "" {
		t.Errorf("no token: %d %q %q", rec.Code, rec.Body, rec.Header())
	}
	if rec := h.do(t, "GET", "/api/defaults", token+"x", nil); rec.Code != 404 {
		t.Errorf("wrong token: %d", rec.Code)
	}
	if rec := h.do(t, "GET", "/api/nonexistent", "", nil); rec.Code != 404 {
		t.Errorf("unknown API paths must also need the token: %d", rec.Code)
	}
	rec := h.do(t, "GET", "/api/defaults", token, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"9 Degrees Lane Cove"`) || !strings.Contains(rec.Body.String(), `"walk_speed_mps"`) ||
		!strings.Contains(rec.Body.String(), `"gyms"`) {
		t.Fatalf("defaults: %d %s", rec.Code, rec.Body)
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
	rec := h.do(t, "GET", "/api/defaults", token, nil)
	for k, want := range map[string]string{
		"Content-Security-Policy":      "default-src 'self'",
		"X-Content-Type-Options":       "nosniff",
		"Referrer-Policy":              "no-referrer",
		"Cache-Control":                "no-store",
		"Strict-Transport-Security":    "max-age=",
		"Cross-Origin-Resource-Policy": "same-origin",
	} {
		if !strings.Contains(rec.Header().Get(k), want) {
			t.Errorf("%s = %q", k, rec.Header().Get(k))
		}
	}
}

// Lane Cove gym and its lines, as a device would send them.
var (
	laneCove      = map[string]any{"lat": -33.807948, "lon": 151.150629}
	laneCoveLines = []string{"train T1", "train T9", "metro M1", "bus 288", "bus 291", "bus 292", "bus 533", "bus 287"}
)

// Epping Station (public), 2026-10-08 16:30 Sydney (+11:00).
var eppingToLaneCove = map[string]any{
	"from":       map[string]any{"lat": -33.7727, "lon": 151.0821},
	"to":         laneCove,
	"lines":      laneCoveLines,
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
	req := map[string]any{"from": eppingToLaneCove["from"], "to": laneCove, "lines": laneCoveLines}
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
		"from":  laneCove,
		"to":    map[string]any{"lat": -33.7727, "lon": 151.0821, "access": access},
		"lines": laneCoveLines,
		"time":  "2026-10-08T19:30:00+11:00",
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

// A home with no stop within the longest walk plans from the nearest ones, and the response says the walk is longer.
func TestPlanStretchesTheWalkWhenNoStopIsInReach(t *testing.T) {
	h := newHarness(t)
	home := map[string]any{"lat": -33.7637, "lon": 151.0821} // 1 km north of Epping
	req := map[string]any{"from": home, "to": laneCove, "lines": laneCoveLines, "time": "2026-10-08T16:30:00+11:00",
		"prefs": map[string]any{"max_walk_m": 400}}
	var p struct {
		Options       []json.RawMessage
		StretchedWalk *struct {
			FromM int `json:"from_m"`
			ToM   int `json:"to_m"`
		} `json:"stretched_walk"`
	}
	rec := h.do(t, "POST", "/api/plan", token, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != 200 || len(p.Options) == 0 || p.StretchedWalk == nil || p.StretchedWalk.FromM <= 400 || p.StretchedWalk.ToM != 0 {
		t.Fatalf("far home: %d %s", rec.Code, rec.Body)
	}

	// Once the walk has been timed, its length is no news.
	var near struct {
		Stops []struct {
			Station   string
			StationID string `json:"station_id"`
		}
	}
	_ = json.Unmarshal(h.do(t, "POST", "/api/stops/near", token, map[string]any{"lat": -33.7727, "lon": 151.0821, "radius_m": 300}).Body.Bytes(), &near)
	for _, s := range near.Stops {
		if s.Station == "Epping Station" {
			home["walks"] = []map[string]any{{"stop": s.StationID, "walk_s": 900}}
		}
	}
	if home["walks"] == nil {
		t.Fatal("no Epping Station nearby")
	}
	p.StretchedWalk = nil
	rec = h.do(t, "POST", "/api/plan", token, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != 200 || p.StretchedWalk != nil {
		t.Errorf("far home with a timed walk: %d %s", rec.Code, rec.Body)
	}

	req["from"] = map[string]any{"lat": -34.2, "lon": 150.5}
	if rec := h.do(t, "POST", "/api/plan", token, req); rec.Code != 400 || !strings.Contains(rec.Body.String(), "within 3 km") {
		t.Errorf("nowhere near the lines: %d %s", rec.Code, rec.Body)
	}
}

func TestPlanValidation(t *testing.T) {
	h := newHarness(t)
	place := map[string]any{"lat": -33.77, "lon": 151.08}
	ok := map[string]any{"from": place, "to": laneCove, "lines": laneCoveLines}
	with := func(k string, v any) map[string]any {
		m := map[string]any{}
		for kk, vv := range ok {
			m[kk] = vv
		}
		m[k] = v
		return m
	}
	many := make([]string, 61)
	for i := range many {
		many[i] = "bus " + strings.Repeat("9", 1+i%3)
	}
	for name, body := range map[string]any{
		"no lines":        map[string]any{"from": place, "to": laneCove},
		"empty lines":     with("lines", []string{}),
		"bad line":        with("lines", []string{"tram 1"}),
		"unknown line":    with("lines", []string{"bus 288", "bus nosuch"}),
		"too many lines":  with("lines", many),
		"long line":       with("lines", []string{"bus " + strings.Repeat("x", 60)}),
		"unknown field":   with("extra", 1),
		"gym id":          with("to", map[string]any{"gym": "lanecove"}),
		"bad lat":         with("from", map[string]any{"lat": 200, "lon": 1}),
		"missing coords":  with("from", map[string]any{}),
		"window":          with("window_min", 100000),
		"far future":      with("time", "2027-01-01T10:00:00+11:00"),
		"walk speed":      with("prefs", map[string]any{"walk_speed_mps": 50}),
		"risk order":      with("prefs", map[string]any{"risk": map[string]any{"safe_s": 10, "tight_s": 60}}),
		"bad access stop": with("from", map[string]any{"lat": -33.77, "lon": 151.08, "access": []any{map[string]any{"stop": "nope", "walk_s": 60}}}),
		"not json":        "{not json",
		"trailing":        `{"from":{"lat":1,"lon":1},"to":{"lat":1,"lon":1}} {}`,
		"too big":         `{"from":{"lat":1,"lon":1,"x":"` + strings.Repeat("a", 70<<10) + `"}}`,
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

func TestStatus(t *testing.T) {
	h := newHarness(t)
	rec := h.do(t, "GET", "/api/status", token, nil)
	var st struct{ State string }
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || rec.Code != 200 || st.State != "ok" {
		t.Errorf("status: %d %s", rec.Code, rec.Body)
	}
	if h.env.Engine.Health().PollingActive {
		t.Error("checking the server's state shouldn't start polling TfNSW")
	}
}

func TestVehiclesAndActivity(t *testing.T) {
	h := newHarness(t)
	if h.env.Engine.Health().PollingActive {
		t.Fatal("should be idle before any request")
	}
	if rec := h.do(t, "GET", "/api/vehicles?lines=tram%201", token, nil); rec.Code != 400 {
		t.Errorf("bad line: %d", rec.Code)
	}
	if rec := h.do(t, "GET", "/api/vehicles", token, nil); rec.Code != 400 {
		t.Errorf("no lines: %d", rec.Code)
	}
	if !h.env.Engine.Health().PollingActive {
		t.Fatal("an API request should activate polling")
	}
	h.env.Engine.PollOnce(context.Background())
	rec := h.do(t, "GET", "/api/vehicles?lines="+laneCoveQuery, token, nil)
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
	// Only the vehicles running your rides, near the part you ride.
	if rec := h.do(t, "GET", "/api/vehicles?lines="+laneCoveQuery+"&rides=nope", token, nil); rec.Code != 400 {
		t.Errorf("malformed rides: %d", rec.Code)
	}
	rec = h.do(t, "GET", "/api/vehicles?lines="+laneCoveQuery+"&rides="+url.QueryEscape("no-such-trip|a|b"), token, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"vehicles":[]`) {
		t.Errorf("rides filter: %d %s", rec.Code, rec.Body)
	}
	// Unauthenticated requests must not keep polling alive.
	h.env.Clock.Advance(11 * 60e9)
	h.do(t, "GET", "/api/defaults", "", nil)
	if h.env.Engine.Health().PollingActive {
		t.Error("unauthenticated request activated polling")
	}
}

func TestPrivateDataNeverLogged(t *testing.T) {
	h := newHarness(t)
	req := map[string]any{
		"from":  map[string]any{"lat": -33.7731234, "lon": 151.0824321},
		"to":    laneCove,
		"lines": laneCoveLines,
		"time":  "2026-10-08T16:30:00+11:00",
	}
	if rec := h.do(t, "POST", "/api/plan", token, req); rec.Code != 200 {
		t.Fatalf("plan: %d %s", rec.Code, rec.Body)
	}
	h.do(t, "GET", "/api/vehicles?lines="+laneCoveQuery+"&secret=-33.7731234", token, nil)
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
	for _, dir := range []string{"/assets/", "/assets"} {
		if rec := h.do(t, "GET", dir, "", nil); rec.Code != 404 || rec.Body.String() != "404 page not found\n" {
			t.Errorf("directory %s is listed: %d %q", dir, rec.Code, rec.Body)
		}
	}
	if rec := h.do(t, "POST", "/", "", "{}"); rec.Code != 404 || rec.Body.String() != "404 page not found\n" {
		t.Errorf("POST /: %d", rec.Code)
	}
	if rec := h.do(t, "GET", "/api/defaults", "", nil); rec.Code != 404 {
		t.Errorf("API still needs the token next to the frontend: %d", rec.Code)
	}
	if rec := h.do(t, "GET", "/api/defaults", token, nil); rec.Code != 200 {
		t.Errorf("API with token: %d", rec.Code)
	}
}

func TestLegShapeAndMap(t *testing.T) {
	h := newHarness(t)
	rec := h.do(t, "POST", "/api/plan", token, eppingToLaneCove)
	var p struct {
		ServiceDate string `json:"service_date"`
		Options     []struct {
			Legs []struct {
				Kind   string
				TripID string `json:"trip_id"`
				Stops  int
				From   struct{ ID string }
				To     struct{ ID string }
			}
		}
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	var q string
	var travelled int
	for _, l := range p.Options[0].Legs {
		if l.Kind == "ride" {
			q = "/api/shape?date=" + p.ServiceDate + "&trip=" + l.TripID + "&from=" + l.From.ID + "&to=" + l.To.ID
			travelled = l.Stops
			break
		}
	}
	if travelled < 1 {
		t.Errorf("a ride should count the stops travelled: %d", travelled)
	}
	shape := func() (int, float64) {
		rec := h.do(t, "GET", q, token, nil)
		var s struct{ Coordinates, Stops [][2]float64 }
		_ = json.Unmarshal(rec.Body.Bytes(), &s)
		if rec.Code != 200 || len(s.Coordinates) < 2 || s.Coordinates[0][0] < 150 {
			t.Fatalf("shape %s: %d %s", q, rec.Code, rec.Body)
		}
		if len(s.Stops) != travelled-1 { // the stops passed, not where you get on or off
			t.Errorf("shape stops: %d, want %d", len(s.Stops), travelled-1)
		}
		path := make([]geo.Point, len(s.Coordinates))
		for i, c := range s.Coordinates {
			path[i] = geo.Point{Lon: c[0], Lat: c[1]}
		}
		off := 0.0 // how far the furthest stop is from the line
		for _, c := range s.Stops {
			p := geo.Point{Lon: c[0], Lat: c[1]}
			off = max(off, geo.DistanceM(p, geo.Project(path, p, geo.Along{}).Pt))
		}
		return len(s.Coordinates), off
	}
	before, _ := shape() // straight lines between stops until shapes load
	h.env.Engine.LoadShapes()
	after, off := shape()
	if after <= before {
		t.Errorf("route shape should have more detail than stops: %d vs %d points", after, before)
	}
	if off > 1.5 { // coordinates are rounded to about a metre
		t.Errorf("stops should be drawn on the route shape: one is %.1f m off it", off)
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
	if rec := h.do(t, "GET", "/api/map.pmtiles", "", nil); rec.Code != 404 {
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
	req := map[string]any{"from": eppingToLaneCove["from"], "to": laneCove, "lines": laneCoveLines}
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
		"from":  map[string]any{"on_trip": map[string]any{"trip_id": ride.TripID, "from_stop": ride.From.ID}},
		"to":    laneCove,
		"lines": laneCoveLines,
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
	bad := map[string]any{"from": map[string]any{"on_trip": map[string]any{"trip_id": "nope", "from_stop": "x"}}, "to": laneCove, "lines": laneCoveLines}
	if rec := h.do(t, "POST", "/api/plan", token, bad); rec.Code != 400 {
		t.Errorf("unknown trip: %d", rec.Code)
	}
	withTime := map[string]any{"from": onboard["from"], "to": onboard["to"], "lines": laneCoveLines, "time": "2026-10-03T15:00:00+10:00"}
	if rec := h.do(t, "POST", "/api/plan", token, withTime); rec.Code != 400 {
		t.Errorf("on_trip with a time: %d", rec.Code)
	}
}

func TestPlanArriveBy(t *testing.T) {
	h := newHarness(t)
	req := map[string]any{
		"from": eppingToLaneCove["from"], "to": laneCove, "lines": laneCoveLines,
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
	noTime := map[string]any{"from": req["from"], "to": req["to"], "lines": laneCoveLines, "arrive_by": true}
	if rec := h.do(t, "POST", "/api/plan", token, noTime); rec.Code != 400 {
		t.Errorf("arrive_by without time: %d", rec.Code)
	}
}

func TestPlanLoadsLinesTheServerHasNotSeen(t *testing.T) {
	h := newHarnessEmpty(t)
	rec := h.do(t, "POST", "/api/plan", token, eppingToLaneCove)
	var p planResp
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != 200 || len(p.Options) == 0 {
		t.Fatalf("first plan with unseen lines: %d %s", rec.Code, rec.Body)
	}
	if got := len(h.env.Engine.Lines()); got != len(laneCoveLines) {
		t.Errorf("server should now cover %d lines, has %d", len(laneCoveLines), got)
	}
	// The map endpoints load lines the same way.
	rec = h.do(t, "GET", "/api/vehicles?lines=bus%20271", token, nil)
	if rec.Code != 200 || len(h.env.Engine.Lines()) != len(laneCoveLines)+1 {
		t.Errorf("vehicles for a new line: %d, lines %d", rec.Code, len(h.env.Engine.Lines()))
	}
}

func TestLinesLoadAheadOfTheFirstSearch(t *testing.T) {
	h := newHarnessEmpty(t)
	rec := h.do(t, "POST", "/api/lines", token, map[string]any{"lines": laneCoveLines})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("load lines: %d %s", rec.Code, rec.Body)
	}
	for deadline := time.Now().Add(30 * time.Second); len(h.env.Engine.Lines()) < len(laneCoveLines); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("lines after asking: %v", h.env.Engine.Lines())
		}
	}
	tooMany := make([]string, engine.MaxLoadedLines+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("bus %d", i)
	}
	for name, body := range map[string]any{
		"not a line": map[string]any{"lines": []string{"nonsense"}},
		"too many":   map[string]any{"lines": tooMany},
		"too long":   map[string]any{"lines": []string{"bus " + strings.Repeat("9", 50)}},
	} {
		if rec := h.do(t, "POST", "/api/lines", token, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
}

func TestSuggestLines(t *testing.T) {
	h := newHarnessEmpty(t)
	chatswood := map[string]any{"lat": -33.7856633, "lon": 151.2003558}
	body := map[string]any{
		"from": map[string]any{"lat": -33.7727, "lon": 151.0821},
		"to":   []any{laneCove, chatswood},
	}
	type result struct {
		Windows []struct {
			Label      string
			Departures int
		}
		Lines []struct {
			Line        string
			Share       float64
			Recommended bool
		}
		Itineraries []struct {
			Desc  string
			Lines []string
			Seen  int
		}
	}
	var r struct {
		State    string
		Done, Of int
		Results  []result
	}
	_ = json.Unmarshal(runSuggest(t, h, body), &r)
	if r.State != "done" || len(r.Results) != 2 || r.Done != r.Of || r.Of != 6 {
		t.Fatalf("suggest: %+v", r)
	}
	for i, res := range r.Results {
		if len(res.Windows) == 0 || len(res.Lines) == 0 || len(res.Itineraries) == 0 {
			t.Fatalf("result %d is empty: %+v", i, res)
		}
	}
	has := func(res result, line string) bool {
		for _, l := range res.Lines {
			if l.Line == line && l.Recommended && l.Share > 0 {
				return true
			}
		}
		return false
	}
	if !has(r.Results[0], "metro M1") || !has(r.Results[0], "bus 288") {
		t.Errorf("Lane Cove should recommend M1 and the 288: %+v", r.Results[0].Lines)
	}
	if has(r.Results[1], "bus 288") {
		t.Errorf("the 288 doesn't go to Chatswood: %+v", r.Results[1].Lines)
	}
	if n := len(h.env.Engine.Lines()); n != 0 {
		t.Errorf("suggesting loaded %d lines into the timetable", n)
	}

	// Bad input is rejected.
	nowhere := map[string]any{"from": map[string]any{"lat": -20, "lon": 130}, "to": []any{laneCove}}
	if got := string(runSuggest(t, h, nowhere)); !strings.Contains(got, `"state":"failed"`) || !strings.Contains(got, "no stops") {
		t.Errorf("no stops nearby: %s", got)
	}
	if rec := h.do(t, "GET", "/api/suggest-lines/nope", token, nil); rec.Code != 404 {
		t.Errorf("unknown job: %d", rec.Code)
	}
	for name, b := range map[string]map[string]any{
		"no destination":  {"from": laneCove},
		"empty list":      {"from": laneCove, "to": []any{}},
		"too many":        {"from": laneCove, "to": slices.Repeat([]any{laneCove}, 13)},
		"not a list":      {"from": laneCove, "to": laneCove},
		"huge radius":     {"from": laneCove, "to": []any{laneCove}, "radius_m": 99999},
		"destination bad": {"from": laneCove, "to": []any{map[string]any{"lat": 200, "lon": 1}}},
	} {
		if rec := h.do(t, "POST", "/api/suggest-lines", token, b); rec.Code != 400 {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	if rec := h.do(t, "POST", "/api/suggest-lines", "", body); rec.Code != 404 {
		t.Errorf("suggest without a token: %d", rec.Code)
	}
}

// runSuggest starts a line search and polls it until it finishes, returning the final status body.
func runSuggest(t *testing.T, h *harness, body any) []byte {
	t.Helper()
	rec := h.do(t, "POST", "/api/suggest-lines", token, body)
	var started struct{ Job string }
	if _ = json.Unmarshal(rec.Body.Bytes(), &started); rec.Code != http.StatusAccepted || started.Job == "" {
		t.Fatalf("starting a search: %d %s", rec.Code, rec.Body)
	}
	for deadline := time.Now().Add(2 * time.Minute); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		rec = h.do(t, "GET", "/api/suggest-lines/"+started.Job, token, nil)
		if rec.Code != 200 {
			t.Fatalf("job status: %d %s", rec.Code, rec.Body)
		}
		if !strings.Contains(rec.Body.String(), `"state":"running"`) {
			return rec.Body.Bytes()
		}
	}
	t.Fatal("the search didn't finish")
	return nil
}

func TestStopsNearWorksBeforeAnyLinesAreChosen(t *testing.T) {
	h := newHarnessEmpty(t)
	rec := h.do(t, "POST", "/api/stops/near", token, map[string]any{"lat": -33.7727, "lon": 151.0821, "radius_m": 400})
	var near struct {
		Stops []struct {
			Name  string
			WalkS int      `json:"walk_s"`
			Lines []string `json:"lines"`
		}
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &near)
	if rec.Code != 200 || len(near.Stops) == 0 {
		t.Fatalf("stops near: %d %.300s", rec.Code, rec.Body)
	}
	all := strings.Join(near.Stops[0].Lines, ",")
	for _, s := range near.Stops {
		all += "," + strings.Join(s.Lines, ",")
	}
	if !strings.Contains(all, "metro M1") || !strings.Contains(all, "train T9") {
		t.Errorf("lines near Epping: %s", all)
	}
	if n := len(h.env.Engine.Lines()); n != 0 {
		t.Errorf("stops/near loaded %d lines", n)
	}
}

func TestPlanReportsHowWalksWereTimed(t *testing.T) {
	h := newHarness(t)
	var p struct {
		Walking string
		Options []struct {
			Legs []struct {
				Kind string
				Path [][2]float64
			}
		}
	}
	_ = json.Unmarshal(h.do(t, "POST", "/api/plan", token, eppingToLaneCove).Body.Bytes(), &p)
	if p.Walking != "estimate" || len(p.Options) == 0 {
		t.Fatalf("without a street network: %+v", p.Walking)
	}
	for _, l := range p.Options[0].Legs {
		if l.Path != nil {
			t.Error("estimated walks have no street path")
		}
	}

	// With a street network around both ends, walks follow streets and the legs carry their paths.
	h.env.Engine.SetWalker(walktest.Grid(t, geo.Point{Lat: -33.7727, Lon: 151.0821}, geo.Point{Lat: -33.807948, Lon: 151.150629}))
	p.Options = nil
	_ = json.Unmarshal(h.do(t, "POST", "/api/plan", token, eppingToLaneCove).Body.Bytes(), &p)
	if p.Walking != "streets" || len(p.Options) == 0 {
		t.Fatalf("with a street network: walking=%q options=%d", p.Walking, len(p.Options))
	}
	legs := p.Options[0].Legs
	first, last := legs[0], legs[len(legs)-1]
	if first.Kind != "walk" || len(first.Path) < 2 || last.Kind != "walk" || len(last.Path) < 2 {
		t.Errorf("first and last walks should carry street paths: %d and %d points", len(first.Path), len(last.Path))
	}
	// The last walk's path must end at the gym (it was computed from the gym outwards and reversed).
	if end := last.Path[len(last.Path)-1]; end[0] < 151.15 || end[0] > 151.151 {
		t.Errorf("last path ends at %v, want the gym", end)
	}
}

// Walks you timed yourself, and the changes between stops, are drawn along the streets too, not as straight lines.
func TestCuratedWalksAndChangesAreDrawnAlongStreets(t *testing.T) {
	h := newHarness(t)
	type leg struct {
		Kind string
		From *struct{ ID string }
		To   *struct{ ID string }
		Path [][2]float64
	}
	type plan struct {
		Options []struct{ Legs []leg }
	}
	// Curated access stops for the start (as the app sends them for a home with timed walks).
	rec := h.do(t, "POST", "/api/stops/near", token, map[string]any{"lat": -33.7727, "lon": 151.0821, "radius_m": 300})
	var near struct {
		Stops []struct {
			ID, Station string
			Lat, Lon    float64
		}
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &near)
	var access []map[string]any
	for _, s := range near.Stops {
		if s.Station == "Epping Station" {
			access = append(access, map[string]any{"stop": s.ID, "walk_s": 240})
		}
	}
	req := map[string]any{"from": map[string]any{"lat": -33.7727, "lon": 151.0821, "access": access}, "to": laneCove,
		"lines": laneCoveLines, "time": "2026-10-08T16:30:00+11:00", "window_min": 20}

	// Find where the plan's changes happen, then lay streets over those places.
	var p plan
	_ = json.Unmarshal(h.do(t, "POST", "/api/plan", token, req).Body.Bytes(), &p)
	if len(p.Options) == 0 {
		t.Fatal("no options")
	}
	centres := []geo.Point{{Lat: -33.7727, Lon: 151.0821}, {Lat: -33.807948, Lon: 151.150629}}
	stopPos := func(id string) geo.Point {
		snap, _ := h.env.Engine.SnapshotFor(h.env.Clock.Now().Add(7 * 24 * time.Hour))
		return snap.Day.Stops[snap.Day.StopIndex[id]].Pos
	}
	snapDay, _ := h.env.Engine.SnapshotFor(h.env.Clock.Now().Add(7 * 24 * time.Hour))
	// Platforms of one station are timed by the station's own pathways, so only changes between stations count.
	betweenStops := func(l leg) bool {
		if l.Kind != "walk" || l.From == nil || l.To == nil {
			return false
		}
		a, b := snapDay.Day.Stops[snapDay.Day.StopIndex[l.From.ID]], snapDay.Day.Stops[snapDay.Day.StopIndex[l.To.ID]]
		return a.Parent == "" || a.Parent != b.Parent
	}
	// Which changes the planner picks depends on the streets, so lay streets over the changes found, and repeat
	// until every change it picks is covered.
	for round := 0; round < 4; round++ {
		grew := false
		for _, o := range p.Options {
			for _, l := range o.Legs {
				if betweenStops(l) && len(l.Path) < 2 {
					centres = append(centres, stopPos(l.From.ID), stopPos(l.To.ID))
					grew = true
				}
			}
		}
		if round > 0 && !grew {
			break
		}
		h.env.Engine.SetWalker(walktest.Grid(t, centres...))
		p = plan{}
		_ = json.Unmarshal(h.do(t, "POST", "/api/plan", token, req).Body.Bytes(), &p)
	}

	changes, withPath := 0, 0
	for _, o := range p.Options {
		first := o.Legs[0]
		if first.Kind != "walk" || len(first.Path) < 2 {
			t.Errorf("a curated start walk should still be drawn along the streets: %d points", len(first.Path))
		}
		for _, l := range o.Legs[1:] {
			if betweenStops(l) {
				changes++
				if len(l.Path) >= 2 {
					withPath++
				}
			}
		}
	}
	if changes == 0 {
		t.Skip("no change between stops in these options")
	}
	if withPath == 0 {
		t.Errorf("none of the %d changes between stops carries a street path", changes)
	}
}
