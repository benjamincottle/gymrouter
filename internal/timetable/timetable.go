// Package timetable assembles the routable service day for a set of lines from the TfNSW feeds.
package timetable

import (
	"time"

	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/lines"
)

// Paths locates the downloaded static feeds.
type Paths struct {
	Complete string // complete GTFS bundle (all modes)
	Trains   string // per-mode Sydney Trains bundle; better realtime trip-ID match. Optional.
}

// Load returns the trips of the given lines running on date. Trains come from the Sydney Trains
// bundle when one is given, everything else from the complete bundle. All stops are kept.
func Load(date time.Time, p Paths, set lines.Set) (*gtfs.Day, error) {
	isTrain := func(r gtfs.Route) bool { return lines.ModeOf(r.Type) == lines.Train }
	in := func(r gtfs.Route) bool { return set.Has(r.Type, r.ShortName) }
	if p.Trains == "" {
		return gtfs.LoadService(date, gtfs.Source{Path: p.Complete, Include: in})
	}
	return gtfs.LoadService(date,
		gtfs.Source{Path: p.Complete, Include: func(r gtfs.Route) bool { return in(r) && !isTrain(r) }},
		gtfs.Source{Path: p.Trains, Include: func(r gtfs.Route) bool { return in(r) && isTrain(r) }},
	)
}

// LoadAll returns every public route running on date (school buses excluded), for building the stop
// catalogue and suggesting lines. It's big (tens of thousands of trips): don't keep it around.
func LoadAll(date time.Time, p Paths) (*gtfs.Day, error) {
	isTrain := func(r gtfs.Route) bool { return lines.ModeOf(r.Type) == lines.Train }
	// Unnamed routes are the trains' "Out of Service" and "Non Revenue" movements: nobody can ride them.
	public := func(r gtfs.Route) bool { return lines.ModeOf(r.Type) != lines.SchoolBus && r.ShortName != "" }
	if p.Trains == "" {
		return gtfs.LoadService(date, gtfs.Source{Path: p.Complete, Include: public})
	}
	return gtfs.LoadService(date,
		gtfs.Source{Path: p.Complete, Include: func(r gtfs.Route) bool { return public(r) && !isTrain(r) }},
		gtfs.Source{Path: p.Trains, Include: func(r gtfs.Route) bool { return public(r) && isTrain(r) }},
	)
}

// Missing reports configured lines with no trips on the loaded day (renamed, withdrawn, or not
// running that day) so they can be flagged on refresh.
func Missing(d *gtfs.Day, set lines.Set) []lines.Key {
	seen := lines.Set{}
	for _, t := range d.Trips {
		r := d.Routes[t.Route]
		seen[lines.Of(r.Type, r.ShortName)] = true
	}
	var out []lines.Key
	for k := range set {
		if !seen[k] {
			out = append(out, k)
		}
	}
	return out
}
