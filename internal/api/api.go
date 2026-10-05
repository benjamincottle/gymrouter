// Package api is the HTTP API. All /api/* routes need the access token; /healthz is public and
// minimal. Personal data (home locations, walk times) only arrives in request bodies, which are
// never logged or stored.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/benjamincottle/gymrouter/internal/config"
	"github.com/benjamincottle/gymrouter/internal/engine"
	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/gyms"
	"github.com/benjamincottle/gymrouter/internal/lines"
	"github.com/benjamincottle/gymrouter/internal/raptor"
	"github.com/benjamincottle/gymrouter/internal/tfnsw"
	"github.com/benjamincottle/gymrouter/internal/walk"
)

// Engine is what the API needs from the data engine.
type Engine interface {
	SnapshotFor(t time.Time) (*engine.Snapshot, error)
	Vehicles(set lines.Set) []engine.Vehicle
	Health() engine.Health
	Touch()
	Now() time.Time
	RoutingOptions() raptor.Options
	Config() *config.Config
	Geocode(ctx context.Context, q string) ([]tfnsw.Place, error)
	LegGeometry(s *engine.Snapshot, tripID, fromStop, toStop string) (path, stops []geo.Point, ok bool)
	VehiclesNear(set lines.Set, rides []engine.Ride) []engine.Vehicle
	MapFile() (string, bool)
	Ensure(set lines.Set) error
	Prefetch(set lines.Set) error
	Catalog() *engine.Catalog
	Approach(net *raptor.Network, p geo.Point, maxWalkM float64, o raptor.Options) engine.Approach
	NearbyStops(c *engine.Catalog, p geo.Point, radiusM float64, o raptor.Options) ([]engine.NearStop, bool)
	Walker() *walk.Graph
	PathsFrom(net *raptor.Network, p geo.Point, maxM float64) engine.Approach
	WalkPath(a, b geo.Point) ([]geo.Point, bool)
	Suggest(ctx context.Context, from engine.SuggestPlace, targets []engine.SuggestPlace, radiusM float64,
		progress func(done, of int)) ([]*engine.SuggestResult, error)
}

// maxBody bounds request bodies.
const maxBody = 64 << 10

// Server holds the API's dependencies.
type Server struct {
	eng  Engine
	auth *Auth
	log  *slog.Logger
	web  fs.FS // static frontend; may be nil
	jobs jobStore
}

// New returns the API server.
func New(eng Engine, auth *Auth, log *slog.Logger, web fs.FS) *Server {
	return &Server{eng: eng, auth: auth, log: log, web: web}
}

// Handler returns the full HTTP handler with middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	api := http.NewServeMux()
	api.HandleFunc("GET /api/defaults", s.defaults)
	api.HandleFunc("POST /api/plan", s.plan)
	api.HandleFunc("POST /api/lines", s.loadLines)
	api.HandleFunc("POST /api/stops/near", s.stopsNear)
	api.HandleFunc("POST /api/suggest-lines", s.suggestLines)
	api.HandleFunc("GET /api/suggest-lines/{id}", s.suggestStatus)
	api.HandleFunc("GET /api/vehicles", s.vehicles)
	api.HandleFunc("GET /api/status", s.status)
	api.HandleFunc("POST /api/geocode", s.geocode)
	api.HandleFunc("GET /api/shape", s.legShape)
	api.HandleFunc("GET /api/map.pmtiles", s.mapTiles)
	api.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeError(w, http.StatusNotFound, "not found") })
	mux.Handle("/api/", s.auth.Require(s.touch(api)))
	mux.HandleFunc("GET /healthz", s.healthz)
	if s.web != nil {
		files := http.FileServerFS(noDirs{s.web})
		mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				http.NotFound(w, r) // nothing here takes anything else; look like it
				return
			}
			if strings.HasPrefix(r.URL.Path, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // content-hashed names
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			files.ServeHTTP(w, r)
		}))
	} else {
		mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = io.WriteString(w, "Gym Router API\n")
		})
	}
	return s.recover(s.logRequests(securityHeaders(mux)))
}

// noDirs hides every directory but the root, so the file server never lists one (the root serves index.html).
type noDirs struct{ fs.FS }

func (n noDirs) Open(name string) (fs.File, error) {
	f, err := n.FS.Open(name)
	if err != nil || name == "." {
		return f, err
	}
	if fi, err := f.Stat(); err != nil || fi.IsDir() {
		f.Close()
		return nil, fs.ErrNotExist
	}
	return f, nil
}

func (s *Server) touch(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/status" { // the app checks the server's state now and then: that isn't using it
			s.eng.Touch()
		}
		h.ServeHTTP(w, r)
	})
}

// securityHeaders sets a strict policy: everything same-origin, MapLibre workers from blob:, HTTPS only.
func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; worker-src 'self' blob:; "+
			"style-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(self)")
		hd.Set("Cross-Origin-Opener-Policy", "same-origin")
		hd.Set("Cross-Origin-Resource-Policy", "same-origin")
		// The app is only reached over HTTPS (Traefik terminates TLS); browsers ignore this header on plain http,
		// so local development is unaffected.
		hd.Set("Strict-Transport-Security", "max-age=31536000")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			hd.Set("Cache-Control", "no-store")
		}
		h.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the real writer (per-request deadlines, flushing).
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// logRequests logs method, path (never the query string or body), status and duration.
func (s *Server) logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(sw, r)
		s.log.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status,
			"ms", time.Since(start).Milliseconds(), "client", clientIP(r, s.eng.Config().Server.TrustProxy))
	})
}

// clientIP is the address the trusted proxy saw: the last X-Forwarded-For entry, which the proxy added. Earlier
// entries are whatever the client sent.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		xff := r.Header.Values("X-Forwarded-For")
		if len(xff) > 0 {
			last := xff[len(xff)-1]
			if i := strings.LastIndex(last, ","); i >= 0 {
				last = last[i+1:]
			}
			if ip := strings.TrimSpace(last); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) recover(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.Error("panic", "path", r.URL.Path, "err", fmt.Sprint(v), "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decode reads a size-limited JSON body, rejecting unknown fields and trailing data.
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		return errors.New("Content-Type must be application/json")
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if dec.More() {
		return errors.New("invalid JSON: trailing data")
	}
	return nil
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	h := s.eng.Health()
	status := http.StatusOK
	if !h.OK {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{"ok": h.OK, "service_date": h.ServiceDate})
}

func (s *Server) geocode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Q string `json:"q"`
	}
	if err := decode(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	q := strings.TrimSpace(req.Q)
	if len(q) < 3 || len(q) > 200 {
		writeError(w, http.StatusBadRequest, "q must be 3..200 characters")
		return
	}
	res, err := s.eng.Geocode(r.Context(), q)
	switch {
	case errors.Is(err, engine.ErrBusy):
		writeError(w, http.StatusTooManyRequests, err.Error())
	case err != nil:
		s.log.Warn("geocode failed", "err", err)
		writeError(w, http.StatusBadGateway, "address lookup failed")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"results": res})
	}
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.eng.Health())
}

// defaults returns the gyms the app knows about and the routing defaults.
func (s *Server) defaults(w http.ResponseWriter, r *http.Request) {
	cfg := s.eng.Config()
	known := gyms.Known()
	writeJSON(w, http.StatusOK, map[string]any{
		"gyms": known,
		"defaults": map[string]any{
			"walk_speed_mps": cfg.Routing.WalkSpeedMps, "min_change_s": cfg.Routing.MinChangeS,
			"max_walk_m": cfg.Routing.MaxWalkM, "risk": map[string]int32{"safe_s": cfg.Risk.SafeS, "tight_s": cfg.Risk.TightS},
		},
	})
}

type vehicleResp struct {
	ID        string    `json:"id"`
	Label     string    `json:"label,omitempty"`
	Line      string    `json:"line"`
	Color     string    `json:"color,omitempty"`
	TripID    string    `json:"trip_id,omitempty"`
	Lat       float64   `json:"lat"`
	Lon       float64   `json:"lon"`
	Bearing   *float32  `json:"bearing,omitempty"`
	Status    string    `json:"status,omitempty"`
	Timestamp time.Time `json:"ts"`
}

// linesParam reads the comma-separated ?lines= list shared by the map endpoints and loads them.
func (s *Server) linesParam(w http.ResponseWriter, r *http.Request) (lines.Set, bool) {
	var ss []string
	if v := r.URL.Query().Get("lines"); v != "" {
		ss = strings.Split(v, ",")
	}
	set, err := parseLines(ss)
	if err == nil {
		err = s.ensure(set)
	}
	if err != nil {
		var br badRequest
		if errors.As(err, &br) {
			writeError(w, http.StatusBadRequest, br.msg)
		} else {
			s.log.Error("loading lines failed", "err", err)
			writeError(w, http.StatusInternalServerError, "timetable unavailable")
		}
		return nil, false
	}
	return set, true
}

// maxRides bounds the rides one vehicles request may name.
const maxRides = 8

// vehicles lists live vehicles on the lines; with rides=<trip>|<from stop>|<to stop>,… only the vehicles running
// those rides, while near the part ridden.
func (s *Server) vehicles(w http.ResponseWriter, r *http.Request) {
	set, ok := s.linesParam(w, r)
	if !ok {
		return
	}
	var vs []engine.Vehicle
	if v := r.URL.Query().Get("rides"); v != "" {
		var rides []engine.Ride
		for _, one := range strings.Split(v, ",") {
			p := strings.Split(one, "|")
			if len(p) != 3 || p[0] == "" || p[1] == "" || p[2] == "" || len(one) > 300 || len(rides) >= maxRides {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("rides must be up to %d of <trip>|<from stop>|<to stop>", maxRides))
				return
			}
			rides = append(rides, engine.Ride{TripID: p[0], From: p[1], To: p[2]})
		}
		vs = s.eng.VehiclesNear(set, rides)
	} else {
		vs = s.eng.Vehicles(set)
	}
	out := make([]vehicleResp, 0, len(vs))
	for _, v := range vs {
		out = append(out, vehicleResp{ID: v.ID, Label: v.Label, Line: v.Line.String(), Color: v.Color, TripID: v.TripID,
			Lat: v.Lat, Lon: v.Lon, Bearing: v.Bearing, Status: v.Status, Timestamp: v.Timestamp})
	}
	writeJSON(w, http.StatusOK, map[string]any{"vehicles": out})
}
