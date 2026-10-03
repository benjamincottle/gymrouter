// Package lines identifies public transport lines the way people name them: mode + short name
// (e.g. "bus 288", "train T9"). GTFS route IDs are too fine-grained and short names alone are ambiguous.
package lines

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Mode is a passenger-facing transport mode.
type Mode string

const (
	Train          Mode = "train"
	RegionalTrain  Mode = "regional-train"
	Metro          Mode = "metro"
	LightRail      Mode = "light-rail"
	Ferry          Mode = "ferry"
	Bus            Mode = "bus"
	SchoolBus      Mode = "school-bus"
	ReplacementBus Mode = "replacement-bus"
	Coach          Mode = "coach"
	Other          Mode = "other"
)

// ModeOf maps a GTFS route_type (including the TfNSW extended values) to a Mode.
func ModeOf(routeType int) Mode {
	switch routeType {
	case 2:
		return Train
	case 106:
		return RegionalTrain
	case 1, 401:
		return Metro
	case 0, 900:
		return LightRail
	case 4:
		return Ferry
	case 3, 700:
		return Bus
	case 712:
		return SchoolBus
	case 714:
		return ReplacementBus
	case 204, 205:
		return Coach
	default:
		return Other
	}
}

// Key identifies a line.
type Key struct {
	Mode Mode
	Name string
}

// Of returns the line key for a GTFS route.
func Of(routeType int, shortName string) Key {
	return Key{Mode: ModeOf(routeType), Name: shortName}
}

func (k Key) String() string { return string(k.Mode) + " " + k.Name }

// Parse reads a key written as "<mode> <name>", e.g. "bus 288" or "light-rail L4".
func Parse(s string) (Key, error) {
	mode, name, ok := strings.Cut(strings.TrimSpace(s), " ")
	name = strings.TrimSpace(name)
	if !ok || name == "" {
		return Key{}, fmt.Errorf("line %q: want \"<mode> <name>\"", s)
	}
	m := Mode(strings.ToLower(mode))
	switch m {
	case Train, RegionalTrain, Metro, LightRail, Ferry, Bus, SchoolBus, ReplacementBus, Coach:
	default:
		return Key{}, fmt.Errorf("line %q: unknown mode %q", s, mode)
	}
	return Key{Mode: m, Name: name}, nil
}

// Set is a set of line keys.
type Set map[Key]bool

// ParseSet parses keys, failing on the first invalid one.
func ParseSet(ss ...string) (Set, error) {
	set := Set{}
	for _, s := range ss {
		k, err := Parse(s)
		if err != nil {
			return nil, err
		}
		set[k] = true
	}
	return set, nil
}

// MustSet is ParseSet for constant inputs (tests, examples).
func MustSet(ss ...string) Set {
	s, err := ParseSet(ss...)
	if err != nil {
		panic(err)
	}
	return s
}

// Has reports whether the route belongs to a line in the set.
func (s Set) Has(routeType int, shortName string) bool { return s[Of(routeType, shortName)] }

// Union returns a new set containing all keys of the given sets.
func Union(sets ...Set) Set {
	out := Set{}
	for _, s := range sets {
		for k := range s {
			out[k] = true
		}
	}
	return out
}

// Strings returns the keys in a stable order.
func (s Set) Strings() []string {
	var out []string
	for k := range s {
		out = append(out, k.String())
	}
	sort.Slice(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
}

// less orders "bus 52" before "bus 288" by comparing numeric parts numerically.
func less(a, b string) bool {
	am, an, _ := strings.Cut(a, " ")
	bm, bn, _ := strings.Cut(b, " ")
	if am != bm {
		return am < bm
	}
	ai, aerr := strconv.Atoi(strings.TrimRight(an, "ABCDEFGHIJKLMNOPQRSTUVWXYZ"))
	bi, berr := strconv.Atoi(strings.TrimRight(bn, "ABCDEFGHIJKLMNOPQRSTUVWXYZ"))
	if aerr == nil && berr == nil && ai != bi {
		return ai < bi
	}
	return an < bn
}
