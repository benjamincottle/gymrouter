// Package gtfs loads GTFS static feeds, keeping only the trips that run on one service day
// (plus the previous day's trips still running after midnight).
package gtfs

import (
	"archive/zip"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/benjamincottle/gymrouter/internal/geo"
)

const secsPerDay = 24 * 3600

// Stop is a GTFS stop, platform or station.
type Stop struct {
	ID, Name, Parent, LocationType string
	Pos                            geo.Point
}

// Route is a GTFS route.
type Route struct {
	ID, AgencyID, ShortName, LongName, Desc string
	Type                                    int
	Color, TextColor                        string // hex without '#', may be empty
}

// StopTime is one call of a trip at a stop. Times are seconds after midnight of the loaded day
// (negative for calls of a previous-day trip that happened before midnight).
type StopTime struct {
	Stop     int32
	Seq      int32 // GTFS stop_sequence
	Arr, Dep int32
}

// Trip is a trip running on the loaded day, with its calls in order.
type Trip struct {
	ID        string
	Route     int32
	Headsign  string
	Shape     string
	DayOffset int8 // 0: runs on the loaded date; -1: started the previous day
	StopTimes []StopTime
	// Realtime state, set by the realtime package on a copy of the day.
	Status TripStatus
	Sched  []StopTime // scheduled times when StopTimes holds predictions
}

// TripStatus is a trip's realtime state.
type TripStatus uint8

const (
	Scheduled TripStatus = iota // no realtime information
	Predicted                   // StopTimes hold realtime predictions
	Cancelled
	Added // realtime-only trip, not in the static timetable
)

// NoTime marks a call where boarding or alighting is impossible (skipped stop, cancelled trip).
const NoTime = int32(1<<31 - 1)

// Day is the subset of one or more feeds that runs on a single service date.
type Day struct {
	Date      time.Time
	Stops     []Stop
	StopIndex map[string]int32
	Routes    []Route
	Trips     []Trip
	// TripIndex maps trip_id to trips of the loaded day (one per day offset).
	TripIndex map[string][]int32
}

// Source is one GTFS zip and the routes to take from it.
type Source struct {
	Path    string
	Include func(Route) bool // nil includes every route
}

// LoadDay loads every route of a single feed for date. It is shorthand for LoadService.
func LoadDay(path string, date time.Time) (*Day, error) {
	return LoadService(date, Source{Path: path})
}

// LoadService loads the trips of the included routes running on date from each source, plus
// trips from the previous day still running after midnight (times shifted by -24h).
// Stops are merged by stop_id across sources (the first source's definition wins).
func LoadService(date time.Time, sources ...Source) (*Day, error) {
	d := &Day{Date: date, StopIndex: map[string]int32{}, TripIndex: map[string][]int32{}}
	for _, src := range sources {
		if err := d.load(src); err != nil {
			return nil, fmt.Errorf("%s: %w", src.Path, err)
		}
	}
	return d, nil
}

func (d *Day) load(src Source) error {
	zr, err := zip.OpenReader(src.Path)
	if err != nil {
		return err
	}
	defer zr.Close()
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}

	err = readCSV(files, "stops.txt", true, func(r row) error {
		id := r.get("stop_id")
		if _, dup := d.StopIndex[id]; dup {
			return nil
		}
		lat, err1 := strconv.ParseFloat(r.get("stop_lat"), 64)
		lon, err2 := strconv.ParseFloat(r.get("stop_lon"), 64)
		if err1 != nil || err2 != nil {
			return nil // stops without coordinates (e.g. generic nodes) are not routable
		}
		d.StopIndex[id] = int32(len(d.Stops))
		d.Stops = append(d.Stops, Stop{
			ID: id, Name: r.get("stop_name"), Parent: r.get("parent_station"),
			LocationType: r.get("location_type"), Pos: geo.Point{Lat: lat, Lon: lon},
		})
		return nil
	})
	if err != nil {
		return err
	}

	routeIndex := map[string]int32{}
	err = readCSV(files, "routes.txt", true, func(r row) error {
		t, _ := strconv.Atoi(r.get("route_type"))
		rt := Route{
			ID: r.get("route_id"), AgencyID: r.get("agency_id"), ShortName: r.get("route_short_name"),
			LongName: r.get("route_long_name"), Desc: r.get("route_desc"), Type: t,
			Color: r.get("route_color"), TextColor: r.get("route_text_color"),
		}
		if src.Include != nil && !src.Include(rt) {
			return nil
		}
		routeIndex[rt.ID] = int32(len(d.Routes))
		d.Routes = append(d.Routes, rt)
		return nil
	})
	if err != nil {
		return err
	}

	today, err := activeServices(files, d.Date)
	if err != nil {
		return err
	}
	yesterday, err := activeServices(files, d.Date.AddDate(0, 0, -1))
	if err != nil {
		return err
	}

	// Trip slots for this source: trip_id → indexes into d.Trips (today and/or yesterday copies).
	slots := map[string][]int32{}
	first := int32(len(d.Trips))
	err = readCSV(files, "trips.txt", true, func(r row) error {
		ri, ok := routeIndex[r.get("route_id")]
		if !ok {
			return nil
		}
		svc := r.get("service_id")
		for _, off := range []int8{0, -1} {
			if (off == 0 && !today[svc]) || (off == -1 && !yesterday[svc]) {
				continue
			}
			id := r.get("trip_id")
			slots[id] = append(slots[id], int32(len(d.Trips)))
			d.Trips = append(d.Trips, Trip{ID: id, Route: ri, Headsign: r.get("trip_headsign"),
				Shape: r.get("shape_id"), DayOffset: off})
		}
		return nil
	})
	if err != nil {
		return err
	}

	err = readCSV(files, "stop_times.txt", true, func(r row) error {
		ts, ok := slots[r.get("trip_id")]
		if !ok {
			return nil
		}
		si, ok := d.StopIndex[r.get("stop_id")]
		if !ok {
			return nil
		}
		seq, err := strconv.Atoi(r.get("stop_sequence"))
		if err != nil {
			return fmt.Errorf("stop_sequence: %w", err)
		}
		arr, _ := parseTime(r.get("arrival_time"))
		dep, _ := parseTime(r.get("departure_time"))
		if arr < 0 {
			arr = dep
		}
		if dep < 0 {
			dep = arr
		}
		for _, ti := range ts {
			d.Trips[ti].StopTimes = append(d.Trips[ti].StopTimes, StopTime{Stop: si, Seq: int32(seq), Arr: arr, Dep: dep})
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Finalise this source's trips: order calls, shift yesterday's trips, drop those over before midnight.
	kept := d.Trips[:first]
	for i := first; i < int32(len(d.Trips)); i++ {
		t := d.Trips[i]
		sort.Slice(t.StopTimes, func(a, b int) bool { return t.StopTimes[a].Seq < t.StopTimes[b].Seq })
		fillMissingTimes(t.StopTimes)
		if len(t.StopTimes) == 0 {
			continue
		}
		if t.DayOffset == -1 {
			if t.StopTimes[len(t.StopTimes)-1].Arr < secsPerDay {
				continue
			}
			for j := range t.StopTimes {
				t.StopTimes[j].Arr -= secsPerDay
				t.StopTimes[j].Dep -= secsPerDay
			}
		}
		d.TripIndex[t.ID] = append(d.TripIndex[t.ID], int32(len(kept)))
		kept = append(kept, t)
	}
	d.Trips = kept
	return nil
}

// fillMissingTimes carries the previous time forward for calls without times (non-timepoints).
func fillMissingTimes(st []StopTime) {
	for i := range st {
		if st[i].Arr < 0 && i > 0 {
			st[i].Arr, st[i].Dep = st[i-1].Dep, st[i-1].Dep
		}
	}
}

func activeServices(files map[string]*zip.File, date time.Time) (map[string]bool, error) {
	ymd := date.Format("20060102")
	dow := strings.ToLower(date.Weekday().String())
	active := map[string]bool{}
	err := readCSV(files, "calendar.txt", false, func(r row) error {
		if r.get(dow) == "1" && r.get("start_date") <= ymd && ymd <= r.get("end_date") {
			active[r.get("service_id")] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = readCSV(files, "calendar_dates.txt", false, func(r row) error {
		if r.get("date") != ymd {
			return nil
		}
		switch r.get("exception_type") {
		case "1":
			active[r.get("service_id")] = true
		case "2":
			delete(active, r.get("service_id"))
		}
		return nil
	})
	return active, err
}

// parseTime parses HH:MM:SS (hours may exceed 23). Returns -1 for an empty value.
func parseTime(s string) (int32, error) {
	if s == "" {
		return -1, nil
	}
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return -1, fmt.Errorf("bad time %q", s)
	}
	var v [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return -1, fmt.Errorf("bad time %q", s)
		}
		v[i] = n
	}
	return int32(v[0]*3600 + v[1]*60 + v[2]), nil
}

type row struct {
	cols map[string]int
	rec  []string
}

func (r row) get(col string) string {
	if i, ok := r.cols[col]; ok && i < len(r.rec) {
		return r.rec[i]
	}
	return ""
}

func readCSV(files map[string]*zip.File, name string, required bool, fn func(row) error) error {
	f, ok := files[name]
	if !ok {
		if required {
			return fmt.Errorf("%s missing from feed", name)
		}
		return nil
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	cr := csv.NewReader(rc)
	cr.ReuseRecord = true
	cr.FieldsPerRecord = -1
	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	cols := map[string]int{}
	for i, h := range header {
		cols[strings.TrimPrefix(strings.TrimSpace(h), "\ufeff")] = i
	}
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := fn(row{cols: cols, rec: rec}); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
}
