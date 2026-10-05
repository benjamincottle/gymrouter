// Package lines identifies public transport lines the way people name them: mode + short name
// (e.g. "bus 288", "train T9"). GTFS route IDs are too fine-grained and short names alone are ambiguous.
package lines

import (
	"fmt"
	"regexp"
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

// trackworkName matches the names of trackwork buses: a number, maybe a letter, then the code of the
// line they stand in for ("23T4", "3AT4", "12CN"). Event shuttles are numbers alone ("8", "5B").
var trackworkName = regexp.MustCompile(`^\d+[A-Z]?([A-Z][A-Z0-9])$`)

// trackworkCodes maps the line codes at the end of trackwork bus names to the train lines they stand in for.
var trackworkCodes = map[string]Key{
	"CN": {Train, "CCN"}, "BM": {Train, "BMT"}, "SC": {Train, "SCO"}, "HU": {Train, "HUN"},
}

func init() {
	for i := 1; i <= 9; i++ {
		trackworkCodes[fmt.Sprintf("T%d", i)] = Key{Train, fmt.Sprintf("T%d", i)}
	}
}

// Replaces returns the line a rail replacement bus stands in for, read from the end of its name. TfNSW names
// trackwork buses afresh for each closure (20T4 and 23T4 for one T4 weekend), and the buses carry those names.
func (k Key) Replaces() (Key, bool) {
	if k.Mode != ReplacementBus {
		return Key{}, false
	}
	m := trackworkName.FindStringSubmatch(k.Name)
	if m == nil {
		return Key{}, false
	}
	l, ok := trackworkCodes[m[1]]
	return l, ok
}

// UnknownTrackwork reports a replacement bus named like a trackwork bus whose line code isn't known, so
// it can't be matched to the line it stands in for.
func (k Key) UnknownTrackwork() bool {
	_, ok := k.Replaces()
	return k.Mode == ReplacementBus && !ok && trackworkName.MatchString(k.Name)
}

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

// Covers reports whether a line in the set includes k: k itself, or the line a replacement bus stands in for.
func (s Set) Covers(k Key) bool {
	if s[k] {
		return true
	}
	l, ok := k.Replaces()
	return ok && s[l]
}

// Chosen returns the set with each trackwork bus swapped for the line it stands in for: the lines to load for it.
func (s Set) Chosen() Set {
	out := Set{}
	for k := range s {
		if l, ok := k.Replaces(); ok {
			k = l
		}
		out[k] = true
	}
	return out
}

// Has reports whether the route belongs to a line in the set (counting replacement buses, see Covers).
func (s Set) Has(routeType int, shortName string) bool { return s.Covers(Of(routeType, shortName)) }

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
