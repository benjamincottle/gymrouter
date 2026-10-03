package realtime

import (
	"strings"
	"time"

	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/lines"
)

// Stats summarises how a set of trip updates matched the timetable.
type Stats struct {
	Updates      int `json:"updates"`        // trip updates for routes in the loaded day
	Matched      int `json:"matched"`        // by trip_id
	MatchedByRun int `json:"matched_by_run"` // trains matched by run number + time
	Added        int `json:"added"`          // realtime-only trips added
	Cancelled    int `json:"cancelled"`
	Empty        int `json:"empty"` // unmatched updates without predictions (TfNSW sends these for other timetable versions)
	Unmatched    int `json:"unmatched"`
	// UnmatchedSample lists a few unmatched trip IDs for diagnostics.
	UnmatchedSample []string `json:"unmatched_sample,omitempty"`
}

// ServiceMidnight is the GTFS service-day reference time: noon minus 12h, local time
// (correct across daylight-saving changes).
func ServiceMidnight(date time.Time, loc *time.Location) time.Time {
	return time.Date(date.Year(), date.Month(), date.Day(), 12, 0, 0, 0, loc).Add(-12 * time.Hour)
}

// maxRunMatchDiff is how far a run-number match may be from the timetable.
const maxRunMatchDiff = 30 * 60

// Apply returns a copy of day with the predictions applied; day itself is not modified.
// Updates for routes that aren't loaded are ignored.
func Apply(day *gtfs.Day, loc *time.Location, updates []TripUpdate) (*gtfs.Day, Stats) {
	out := *day
	out.Trips = append([]gtfs.Trip(nil), day.Trips...)
	out.TripIndex = make(map[string][]int32, len(day.TripIndex))
	for k, v := range day.TripIndex {
		out.TripIndex[k] = v
	}
	midnight := ServiceMidnight(day.Date, loc).Unix()

	routeByID := make(map[string]int32, len(day.Routes))
	for i, r := range day.Routes {
		routeByID[r.ID] = int32(i)
	}
	lineOf := func(ri int32) lines.Key { r := day.Routes[ri]; return lines.Of(r.Type, r.ShortName) }
	runs := map[string][]int32{} // train run number → trips
	for i, t := range day.Trips {
		if m := lineOf(t.Route).Mode; m == lines.Train || m == lines.RegionalTrain {
			runs[runOf(t.ID)] = append(runs[runOf(t.ID)], int32(i))
		}
	}

	var st Stats
	for i := range updates {
		u := &updates[i]
		ti := pick(&out, day.TripIndex[u.TripID], u.StartDate)
		ri, routeKnown := routeByID[u.RouteID]
		if ti < 0 && !routeKnown {
			continue // not a line we route on
		}
		if ti < 0 && len(u.Stops) == 0 && !u.Cancelled {
			st.Empty++
			continue
		}
		st.Updates++
		switch {
		case ti >= 0:
			st.Matched++
		case !u.Added && runs[runOf(u.TripID)] != nil:
			if ti = matchByRun(&out, runs[runOf(u.TripID)], lineOf(ri), lineOf, u, midnight); ti >= 0 {
				st.MatchedByRun++
			}
		}
		if ti < 0 {
			if addTrip(&out, ri, u, midnight) {
				st.Added++
			} else {
				st.Unmatched++
				if len(st.UnmatchedSample) < 10 {
					st.UnmatchedSample = append(st.UnmatchedSample, u.RouteID+"/"+u.TripID)
				}
			}
			continue
		}
		t := &out.Trips[ti]
		if u.Cancelled {
			st.Cancelled++
			cancel(t)
			continue
		}
		predict(&out, t, u, midnight)
	}
	return &out, st
}

// runOf returns the run number prefix of a Sydney Trains trip ID ("14-R.1309.168…" → "14-R").
func runOf(tripID string) string {
	run, _, _ := strings.Cut(tripID, ".")
	return run
}

func serviceDate(d *gtfs.Day, t *gtfs.Trip) string {
	return d.Date.AddDate(0, 0, int(t.DayOffset)).Format("20060102")
}

func pick(d *gtfs.Day, cands []int32, startDate string) int32 {
	for _, c := range cands {
		if startDate == "" {
			if d.Trips[c].DayOffset == 0 || len(cands) == 1 {
				return c
			}
			continue
		}
		if serviceDate(d, &d.Trips[c]) == startDate {
			return c
		}
	}
	return -1
}

// matchByRun finds the timetabled train with the same run number and line whose scheduled time
// at an updated stop is closest to the predicted one. Without absolute times it only accepts a
// unique candidate.
func matchByRun(d *gtfs.Day, cands []int32, key lines.Key, lineOf func(int32) lines.Key, u *TripUpdate, midnight int64) int32 {
	var same []int32
	for _, c := range cands {
		if lineOf(d.Trips[c].Route) == key && d.Trips[c].Status != gtfs.Added {
			same = append(same, c)
		}
	}
	best, bestDiff := int32(-1), int64(maxRunMatchDiff+1)
	hadTime := false
	for _, su := range u.Stops {
		abs := su.DepTime
		if abs == nil {
			abs = su.ArrTime
		}
		si, ok := d.StopIndex[su.StopID]
		if abs == nil || !ok {
			continue
		}
		hadTime = true
		rel := *abs - midnight
		for _, c := range same {
			for _, call := range d.Trips[c].StopTimes {
				if call.Stop != si {
					continue
				}
				if diff := absInt(rel - int64(call.Dep)); diff < bestDiff {
					best, bestDiff = c, diff
				}
			}
		}
	}
	if !hadTime && len(same) == 1 {
		return same[0]
	}
	return best
}

func absInt(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func cancel(t *gtfs.Trip) {
	if t.Sched == nil {
		t.Sched = t.StopTimes
	}
	st := make([]gtfs.StopTime, len(t.Sched))
	copy(st, t.Sched)
	for i := range st {
		st[i].Arr, st[i].Dep = gtfs.NoTime, gtfs.NoTime
	}
	t.StopTimes, t.Status = st, gtfs.Cancelled
}

// addTrip adds a realtime-only trip built from absolute predicted times, if there are at least two.
func addTrip(d *gtfs.Day, route int32, u *TripUpdate, midnight int64) bool {
	if u.Cancelled {
		return false
	}
	if int(route) >= len(d.Routes) {
		return false
	}
	var calls []gtfs.StopTime
	for _, su := range u.Stops {
		si, ok := d.StopIndex[su.StopID]
		arr, dep := su.ArrTime, su.DepTime
		if arr == nil {
			arr = dep
		}
		if dep == nil {
			dep = arr
		}
		if !ok || arr == nil || su.Skipped {
			continue
		}
		c := gtfs.StopTime{Stop: si, Arr: int32(*arr - midnight), Dep: int32(*dep - midnight)}
		if su.Seq != nil {
			c.Seq = *su.Seq
		}
		if n := len(calls); n > 0 && c.Arr < calls[n-1].Dep {
			continue // out of order; ignore rather than invent times
		}
		calls = append(calls, c)
	}
	if len(calls) < 2 {
		return false
	}
	d.TripIndex[u.TripID] = append(d.TripIndex[u.TripID], int32(len(d.Trips)))
	d.Trips = append(d.Trips, gtfs.Trip{ID: u.TripID, Route: route, StopTimes: calls, Status: gtfs.Added})
	return true
}

// predict applies stop-level predictions to t, propagating the last known delay downstream
// (GTFS-realtime semantics). Calls before the first prediction keep their scheduled times
// unless the update has a trip-level delay.
func predict(d *gtfs.Day, t *gtfs.Trip, u *TripUpdate, midnight int64) {
	sched := t.StopTimes
	if t.Sched != nil {
		sched = t.Sched
	}
	upd := make([]*StopUpdate, len(sched))
	next := 0
	for k := range u.Stops {
		su := &u.Stops[k]
		si, hasStop := d.StopIndex[su.StopID]
		for i := next; i < len(sched); i++ {
			if (su.Seq != nil && sched[i].Seq == *su.Seq) || (su.Seq == nil && hasStop && sched[i].Stop == si) {
				upd[i] = su
				next = i + 1
				break
			}
		}
	}

	st := make([]gtfs.StopTime, len(sched))
	copy(st, sched)
	var delay int64
	have := false
	if u.Delay != nil {
		delay, have = int64(*u.Delay), true
	}
	prevDep := int64(-1 << 40)
	for i := range st {
		s := sched[i]
		arr, dep := int64(s.Arr), int64(s.Dep)
		su := upd[i]
		switch {
		case su != nil && su.NoData:
			have = false
		case su != nil && su.Skipped:
			st[i].Arr, st[i].Dep = gtfs.NoTime, gtfs.NoTime
			continue
		case su != nil:
			switch {
			case su.ArrTime != nil:
				arr = *su.ArrTime - midnight
				delay, have = arr-int64(s.Arr), true
			case su.ArrDelay != nil:
				delay, have = int64(*su.ArrDelay), true
				arr = int64(s.Arr) + delay
			case have:
				arr += delay
			}
			switch {
			case su.DepTime != nil:
				dep = *su.DepTime - midnight
				delay, have = dep-int64(s.Dep), true
			case su.DepDelay != nil:
				delay, have = int64(*su.DepDelay), true
				dep = int64(s.Dep) + delay
			case have:
				dep = int64(s.Dep) + delay
			}
		case have:
			arr += delay
			dep += delay
		}
		if dep < arr {
			dep = arr
		}
		if arr < prevDep {
			arr = prevDep
			if dep < arr {
				dep = arr
			}
		}
		st[i].Arr, st[i].Dep = int32(arr), int32(dep)
		prevDep = dep
	}
	if t.Sched == nil {
		t.Sched = sched
	}
	t.StopTimes, t.Status = st, gtfs.Predicted
}
