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
	"github.com/benjamincottle/gymrouter/internal/lines"
	"github.com/benjamincottle/gymrouter/internal/raptor"
	"github.com/benjamincottle/gymrouter/internal/tfnsw"
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
	LegGeometry(s *engine.Snapshot, tripID, fromStop, toStop string) ([]geo.Point, bool)
	LineShapes(set lines.Set) []engine.LineShape
	MapFile() (string, bool)
}

// maxBody bounds request bodies.
const maxBody = 64 << 10

// Server holds the API's dependencies.
type Server struct {
	eng  Engine
	auth *Auth
	log  *slog.Logger
	web  fs.FS // static frontend; may be nil
}

// New returns the API server.
func New(eng Engine, auth *Auth, log *slog.Logger, web fs.FS) *Server {
	return &Server{eng: eng, auth: auth, log: log, web: web}
}

// Handler returns the full HTTP handler with middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	api := http.NewServeMux()
	api.HandleFunc("GET /api/gyms", s.gyms)
	api.HandleFunc("POST /api/plan", s.plan)
	api.HandleFunc("POST /api/stops/near", s.stopsNear)
	api.HandleFunc("GET /api/vehicles", s.vehicles)
	api.HandleFunc("GET /api/status", s.status)
	api.HandleFunc("POST /api/geocode", s.geocode)
	api.HandleFunc("GET /api/shape", s.legShape)
	api.HandleFunc("GET /api/shapes", s.lineShapes)
	api.HandleFunc("GET /api/map.pmtiles", s.mapTiles)
	api.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeError(w, http.StatusNotFound, "not found") })
	mux.Handle("/api/", s.auth.Require(s.touch(api)))
	mux.HandleFunc("GET /healthz", s.healthz)
	if s.web != nil {
		files := http.FileServerFS(s.web)
		mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
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

func (s *Server) touch(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.eng.Touch()
		h.ServeHTTP(w, r)
	})
}

// securityHeaders sets a strict policy: everything same-origin, MapLibre workers from blob:.
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

func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first, _, _ := strings.Cut(xff, ",")
			return strings.TrimSpace(first)
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

type gymResp struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Address string   `json:"address,omitempty"`
	Lat     float64  `json:"lat"`
	Lon     float64  `json:"lon"`
	Lines   []string `json:"lines"`
}

func (s *Server) gyms(w http.ResponseWriter, r *http.Request) {
	cfg := s.eng.Config()
	out := make([]gymResp, 0, len(cfg.Gyms))
	for _, g := range cfg.Gyms {
		out = append(out, gymResp{ID: g.ID, Name: g.Name, Address: g.Address, Lat: g.Lat, Lon: g.Lon, Lines: g.LineSet.Strings()})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"gyms": out,
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

func (s *Server) vehicles(w http.ResponseWriter, r *http.Request) {
	g, ok := s.eng.Config().Gym(r.URL.Query().Get("gym"))
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown gym")
		return
	}
	vs := s.eng.Vehicles(g.LineSet)
	out := make([]vehicleResp, 0, len(vs))
	for _, v := range vs {
		out = append(out, vehicleResp{ID: v.ID, Label: v.Label, Line: v.Line.String(), Color: v.Color, TripID: v.TripID,
			Lat: v.Lat, Lon: v.Lon, Bearing: v.Bearing, Status: v.Status, Timestamp: v.Timestamp})
	}
	writeJSON(w, http.StatusOK, map[string]any{"vehicles": out})
}
