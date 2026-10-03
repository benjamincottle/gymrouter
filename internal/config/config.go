// Package config loads the server configuration file (public data only: gyms, their lines, tuning).
// Secrets come from the environment, never from this file.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/benjamincottle/gymrouter/internal/lines"
)

// Config is the whole configuration file.
type Config struct {
	Server   Server   `toml:"server"`
	Routing  Routing  `toml:"routing"`
	Risk     Risk     `toml:"risk"`
	Realtime Realtime `toml:"realtime"`
	Gyms     []Gym    `toml:"gym"`
}

// Server settings.
type Server struct {
	Listen    string `toml:"listen"`
	DataDir   string `toml:"data_dir"`
	PublicURL string `toml:"public_url"` // used to build setup links
	// TrustProxy makes the server log the client IP from X-Forwarded-For (set by Traefik).
	TrustProxy bool `toml:"trust_proxy"`
}

// Routing defaults (devices may override walking speed, change buffer and transfers).
type Routing struct {
	MaxTransferM   float64 `toml:"max_transfer_m"`
	WalkSpeedMps   float64 `toml:"walk_speed_mps"`
	DetourFactor   float64 `toml:"detour_factor"`
	MinChangeS     int32   `toml:"min_change_s"`
	MaxRides       int     `toml:"max_rides"`
	MaxWalkM       float64 `toml:"max_walk_m"` // to/from the first/last stop when not curated
	AlternativesS  int32   `toml:"alternatives_s"`
	MaxWindowS     int32   `toml:"max_window_s"`
	StaticRefreshH int     `toml:"static_refresh_hour"` // local hour for the daily timetable refresh
}

// Risk holds the default connection-risk thresholds (devices may override).
type Risk struct {
	SafeS  int32 `toml:"safe_s"`
	TightS int32 `toml:"tight_s"`
}

// Realtime polling settings.
type Realtime struct {
	TripUpdatesEvery Duration `toml:"trip_updates_every"`
	VehiclesEvery    Duration `toml:"vehicles_every"`
	ActiveFor        Duration `toml:"active_for"`   // keep polling this long after the last app request
	DailyBudget      int      `toml:"daily_budget"` // hard cap on upstream requests per day
}

// Gym is a destination with its set of lines.
type Gym struct {
	ID      string   `toml:"id"`
	Name    string   `toml:"name"`
	Address string   `toml:"address"`
	Lat     float64  `toml:"lat"`
	Lon     float64  `toml:"lon"`
	Lines   []string `toml:"lines"`
	Access  []Access `toml:"access"`

	LineSet lines.Set `toml:"-"`
}

// Access is a curated stop near a gym with a measured walking time and optional path for the map.
type Access struct {
	Stop  string       `toml:"stop"`
	WalkS int32        `toml:"walk_s"`
	Path  [][2]float64 `toml:"path"` // [[lat, lon], ...]
}

// Duration is a time.Duration written as a string ("30s", "10m").
type Duration struct{ time.Duration }

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	d.Duration = v
	return err
}

// Defaults returns a config with every tunable set; Load overlays the file on top.
func Defaults() Config {
	return Config{
		Server: Server{Listen: ":8080", DataDir: "/data"},
		Routing: Routing{MaxTransferM: 400, WalkSpeedMps: 1.3, DetourFactor: 1.3, MinChangeS: 60, MaxRides: 4,
			MaxWalkM: 1000, AlternativesS: 600, MaxWindowS: 3 * 3600, StaticRefreshH: 5},
		Risk: Risk{SafeS: 180, TightS: 60},
		Realtime: Realtime{TripUpdatesEvery: Duration{30 * time.Second}, VehiclesEvery: Duration{15 * time.Second},
			ActiveFor: Duration{10 * time.Minute}, DailyBudget: 40000},
	}
}

// Load reads and validates the config file at path.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(string(b))
}

// Parse reads and validates config text.
func Parse(text string) (*Config, error) {
	c := Defaults()
	md, err := toml.Decode(text, &c)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		keys := make([]string, len(und))
		for i, k := range und {
			keys[i] = k.String()
		}
		return nil, fmt.Errorf("config: unknown keys: %s", strings.Join(keys, ", "))
	}
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return &c, nil
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

func (c *Config) validate() error {
	var errs []error
	if c.Server.PublicURL != "" {
		u, err := url.Parse(c.Server.PublicURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			errs = append(errs, fmt.Errorf("server.public_url %q is not an http(s) URL", c.Server.PublicURL))
		}
	}
	if c.Routing.WalkSpeedMps <= 0 || c.Routing.DetourFactor < 1 || c.Routing.MaxRides < 1 || c.Routing.MaxRides > 6 {
		errs = append(errs, errors.New("routing: walk_speed_mps > 0, detour_factor >= 1 and 1 <= max_rides <= 6 required"))
	}
	if c.Routing.StaticRefreshH < 0 || c.Routing.StaticRefreshH > 23 {
		errs = append(errs, errors.New("routing.static_refresh_hour must be 0..23"))
	}
	if c.Risk.TightS < 0 || c.Risk.SafeS < c.Risk.TightS {
		errs = append(errs, errors.New("risk: need 0 <= tight_s <= safe_s"))
	}
	if c.Realtime.TripUpdatesEvery.Duration < 10*time.Second || c.Realtime.VehiclesEvery.Duration < 10*time.Second {
		errs = append(errs, errors.New("realtime: polling intervals must be at least 10s"))
	}
	if c.Realtime.DailyBudget <= 0 || c.Realtime.DailyBudget > 60000 {
		errs = append(errs, errors.New("realtime.daily_budget must be 1..60000 (TfNSW free plan quota)"))
	}
	if len(c.Gyms) == 0 {
		errs = append(errs, errors.New("at least one [[gym]] is required"))
	}
	seen := map[string]bool{}
	for i := range c.Gyms {
		g := &c.Gyms[i]
		if !idPattern.MatchString(g.ID) {
			errs = append(errs, fmt.Errorf("gym %d: id %q must match %s", i, g.ID, idPattern))
		}
		if seen[g.ID] {
			errs = append(errs, fmt.Errorf("gym %q: duplicate id", g.ID))
		}
		seen[g.ID] = true
		if g.Name == "" {
			errs = append(errs, fmt.Errorf("gym %q: name required", g.ID))
		}
		if g.Lat < -90 || g.Lat > 90 || g.Lon < -180 || g.Lon > 180 || (g.Lat == 0 && g.Lon == 0) {
			errs = append(errs, fmt.Errorf("gym %q: lat/lon required", g.ID))
		}
		set, err := lines.ParseSet(g.Lines...)
		if err != nil {
			errs = append(errs, fmt.Errorf("gym %q: %w", g.ID, err))
		} else if len(set) == 0 {
			errs = append(errs, fmt.Errorf("gym %q: lines required", g.ID))
		}
		g.LineSet = set
		for _, a := range g.Access {
			if a.Stop == "" || a.WalkS < 0 || a.WalkS > 3600 {
				errs = append(errs, fmt.Errorf("gym %q: access entries need a stop and 0 <= walk_s <= 3600", g.ID))
			}
		}
	}
	return errors.Join(errs...)
}

// Gym returns the gym with the given id.
func (c *Config) Gym(id string) (*Gym, bool) {
	for i := range c.Gyms {
		if c.Gyms[i].ID == id {
			return &c.Gyms[i], true
		}
	}
	return nil, false
}

// AllLines is the union of every gym's lines: what the server loads and polls for.
func (c *Config) AllLines() lines.Set {
	sets := make([]lines.Set, len(c.Gyms))
	for i, g := range c.Gyms {
		sets[i] = g.LineSet
	}
	return lines.Union(sets...)
}
