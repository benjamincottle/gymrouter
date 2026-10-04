// Package config loads the server configuration file: infrastructure only (listener, data directory,
// realtime polling). Gyms are built into the app, homes and their lines live on the device, and routing and
// risk defaults are compiled in (devices override them). Secrets come from the environment, never from this file.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the whole configuration file.
type Config struct {
	Server   Server   `toml:"server"`
	Realtime Realtime `toml:"realtime"`
	Data     Data     `toml:"data"`

	// Compiled-in defaults, not settable from the file. Devices override the personal ones per request.
	Routing Routing `toml:"-"`
	Risk    Risk    `toml:"-"`
}

// Server settings.
type Server struct {
	Listen    string `toml:"listen"`
	DataDir   string `toml:"data_dir"`
	PublicURL string `toml:"public_url"` // used to build setup links
	// TrustProxy makes the server log the client IP from the last X-Forwarded-For entry (added by Traefik).
	TrustProxy bool `toml:"trust_proxy"`
	// StaticRefreshH is the local hour for the daily timetable download.
	StaticRefreshH int `toml:"static_refresh_hour"`
}

// Data controls the files the server fetches for itself (street network, basemap). Timetables always download.
type Data struct {
	// AutoDownload fetches and refreshes the street network and the basemap in the background.
	AutoDownload bool `toml:"auto_download"`
	// WalkSource is an OpenStreetMap extract (.osm.pbf) covering the area, e.g. BBBike's Sydney extract.
	WalkSource string `toml:"walk_source"`
	// PMTilesBin is the pmtiles tool used to cut the basemap from the Protomaps build ("" disables the basemap download).
	PMTilesBin string `toml:"pmtiles_bin"`
}

// Routing defaults, compiled in. Devices override walking speed, change buffer, longest walk, risk and transfers per request.
type Routing struct {
	MaxTransferM  float64
	WalkSpeedMps  float64
	DetourFactor  float64
	MinChangeS    int32
	MaxRides      int
	MaxWalkM      float64 // to/from the first/last stop when not curated
	AlternativesS int32
	MaxWindowS    int32
}

// Risk holds the default connection-risk thresholds (devices override).
type Risk struct {
	SafeS  int32
	TightS int32
}

// Realtime polling settings.
type Realtime struct {
	TripUpdatesEvery Duration `toml:"trip_updates_every"`
	VehiclesEvery    Duration `toml:"vehicles_every"`
	ActiveFor        Duration `toml:"active_for"`   // keep polling this long after the last app request
	DailyBudget      int      `toml:"daily_budget"` // hard cap on upstream requests per day
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
		Server: Server{Listen: ":8080", DataDir: "/data", StaticRefreshH: 5},
		Routing: Routing{MaxTransferM: 400, WalkSpeedMps: 1.3, DetourFactor: 1.3, MinChangeS: 60, MaxRides: 4,
			MaxWalkM: 1000, AlternativesS: 600, MaxWindowS: 3 * 3600},
		Risk: Risk{SafeS: 180, TightS: 60},
		Data: Data{AutoDownload: true, WalkSource: "https://download.bbbike.org/osm/bbbike/Sydney/Sydney.osm.pbf", PMTilesBin: "pmtiles"},
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

func (c *Config) validate() error {
	var errs []error
	if c.Server.PublicURL != "" {
		u, err := url.Parse(c.Server.PublicURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			errs = append(errs, fmt.Errorf("server.public_url %q is not an http(s) URL", c.Server.PublicURL))
		}
	}
	if c.Server.StaticRefreshH < 0 || c.Server.StaticRefreshH > 23 {
		errs = append(errs, errors.New("server.static_refresh_hour must be 0..23"))
	}
	if u, err := url.Parse(c.Data.WalkSource); c.Data.WalkSource != "" && (err != nil || u.Scheme != "https" || u.Host == "") {
		errs = append(errs, fmt.Errorf("data.walk_source %q must be an https URL", c.Data.WalkSource))
	}
	if c.Realtime.TripUpdatesEvery.Duration < 10*time.Second || c.Realtime.VehiclesEvery.Duration < 10*time.Second {
		errs = append(errs, errors.New("realtime: polling intervals must be at least 10s"))
	}
	if c.Realtime.DailyBudget <= 0 || c.Realtime.DailyBudget > 60000 {
		errs = append(errs, errors.New("realtime.daily_budget must be 1..60000 (TfNSW free plan quota)"))
	}
	return errors.Join(errs...)
}
