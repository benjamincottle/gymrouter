// Package gyms holds the gyms the app knows about: public data shipped in the binary and offered to every
// device. A device adds the lines near its own home to what is listed here.
package gyms

import (
	_ "embed"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sync"

	"github.com/BurntSushi/toml"

	"github.com/benjamincottle/gymrouter/internal/lines"
)

//go:embed gyms.toml
var data string

// Gym is a destination and the lines that serve its end.
type Gym struct {
	ID      string   `toml:"id" json:"id"`
	Name    string   `toml:"name" json:"name"`
	Brand   string   `toml:"brand" json:"brand,omitempty"` // whose logo to show, one of Brands
	Address string   `toml:"address" json:"address,omitempty"`
	Lat     float64  `toml:"lat" json:"lat"`
	Lon     float64  `toml:"lon" json:"lon"`
	Lines   []string `toml:"lines" json:"lines"`
	Access  []Access `toml:"access" json:"access,omitempty"`

	LineSet lines.Set `toml:"-" json:"-"`
}

// Access is a stop near a gym with a measured walk from its door.
type Access struct {
	Stop  string `toml:"stop" json:"stop"`
	Name  string `toml:"name" json:"name"`
	WalkS int32  `toml:"walk_s" json:"walk_s"`
}

var (
	once sync.Once
	list []Gym
	err  error
)

// Known returns the built-in gyms. It panics if the embedded file is invalid (a test guards that).
func Known() []Gym {
	once.Do(func() { list, err = Parse(data) })
	if err != nil {
		panic(err)
	}
	return list
}

// Brands are the gym brands the app has a logo for.
var Brands = map[string]bool{
	"9degrees": true, "climbfit": true, "nomad": true, "blochaus": true, "betaone": true, "1up": true, "sandbox": true,
	"sicg": true, "skywood": true,
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// Parse reads and validates gym definitions.
func Parse(text string) ([]Gym, error) {
	var f struct {
		Gym []Gym `toml:"gym"`
	}
	md, err := toml.Decode(text, &f)
	if err != nil {
		return nil, fmt.Errorf("gyms: %w", err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		return nil, fmt.Errorf("gyms: unknown keys: %v", und)
	}
	var errs []error
	seen := map[string]bool{}
	for i := range f.Gym {
		g := &f.Gym[i]
		if !idPattern.MatchString(g.ID) || seen[g.ID] {
			errs = append(errs, fmt.Errorf("gym %d: id %q must be unique and match %s", i, g.ID, idPattern))
		}
		seen[g.ID] = true
		if g.Name == "" {
			errs = append(errs, fmt.Errorf("gym %q: name required", g.ID))
		}
		if g.Brand != "" && !Brands[g.Brand] {
			errs = append(errs, fmt.Errorf("gym %q: unknown brand %q", g.ID, g.Brand))
		}
		if math.Abs(g.Lat) > 90 || math.Abs(g.Lon) > 180 || (g.Lat == 0 && g.Lon == 0) {
			errs = append(errs, fmt.Errorf("gym %q: lat/lon required", g.ID))
		}
		set, err := lines.ParseSet(g.Lines...)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("gym %q: %w", g.ID, err))
		case len(set) == 0:
			errs = append(errs, fmt.Errorf("gym %q: lines required", g.ID))
		}
		g.LineSet = set
		for _, a := range g.Access {
			if a.Stop == "" || a.Name == "" || a.WalkS < 0 || a.WalkS > 3600 {
				errs = append(errs, fmt.Errorf("gym %q: access entries need a stop, a name and 0 <= walk_s <= 3600", g.ID))
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return f.Gym, nil
}

// AllLines is the union of every known gym's lines: what the server loads at startup, so the first
// request after a restart is fast. Devices ask for more at run time.
func AllLines() lines.Set {
	sets := make([]lines.Set, 0, len(Known()))
	for _, g := range Known() {
		sets = append(sets, g.LineSet)
	}
	return lines.Union(sets...)
}
