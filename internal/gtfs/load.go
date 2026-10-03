// Package gtfs loads a GTFS static feed, keeping only the trips that run on one service day.
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

// Stop is a GTFS stop, platform or station.
type Stop struct {
	ID, Name, Parent, LocationType string
	Pos                            geo.Point
}

// Route is a GTFS route.
type Route struct {
	ID, AgencyID, ShortName, LongName, Desc string
	Type                                    int
}

// StopTime is one call of a trip at a stop. Times are seconds after midnight of the service day.
type StopTime struct {
	Stop     int32
	Arr, Dep int32
}

// Trip is a trip running on the loaded service day, with its calls in order.
type Trip struct {
	ID        string
	Route     int32
	Headsign  string
	StopTimes []StopTime
}

// Day is the subset of a feed that runs on a single service date.
type Day struct {
	Date      time.Time
	Stops     []Stop
	StopIndex map[string]int32
	Routes    []Route
	Trips     []Trip
}

// LoadDay reads the zipped feed at path and returns the trips active on date.
func LoadDay(path string, date time.Time) (*Day, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	d := &Day{Date: date, StopIndex: map[string]int32{}}

	err = readCSV(files, "stops.txt", true, func(r row) error {
		lat, err1 := strconv.ParseFloat(r.get("stop_lat"), 64)
		lon, err2 := strconv.ParseFloat(r.get("stop_lon"), 64)
		if err1 != nil || err2 != nil {
			return nil // stops without coordinates (e.g. generic nodes) are not routable
		}
		d.StopIndex[r.get("stop_id")] = int32(len(d.Stops))
		d.Stops = append(d.Stops, Stop{
			ID: r.get("stop_id"), Name: r.get("stop_name"), Parent: r.get("parent_station"),
			LocationType: r.get("location_type"), Pos: geo.Point{Lat: lat, Lon: lon},
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	routeIndex := map[string]int32{}
	err = readCSV(files, "routes.txt", true, func(r row) error {
		t, _ := strconv.Atoi(r.get("route_type"))
		routeIndex[r.get("route_id")] = int32(len(d.Routes))
		d.Routes = append(d.Routes, Route{
			ID: r.get("route_id"), AgencyID: r.get("agency_id"), ShortName: r.get("route_short_name"),
			LongName: r.get("route_long_name"), Desc: r.get("route_desc"), Type: t,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	active, err := activeServices(files, date)
	if err != nil {
		return nil, err
	}

	tripIndex := map[string]int32{}
	err = readCSV(files, "trips.txt", true, func(r row) error {
		if !active[r.get("service_id")] {
			return nil
		}
		ri, ok := routeIndex[r.get("route_id")]
		if !ok {
			return nil
		}
		tripIndex[r.get("trip_id")] = int32(len(d.Trips))
		d.Trips = append(d.Trips, Trip{ID: r.get("trip_id"), Route: ri, Headsign: r.get("trip_headsign")})
		return nil
	})
	if err != nil {
		return nil, err
	}

	seqs := make([][]int32, len(d.Trips))
	err = readCSV(files, "stop_times.txt", true, func(r row) error {
		ti, ok := tripIndex[r.get("trip_id")]
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
		d.Trips[ti].StopTimes = append(d.Trips[ti].StopTimes, StopTime{Stop: si, Arr: arr, Dep: dep})
		seqs[ti] = append(seqs[ti], int32(seq))
		return nil
	})
	if err != nil {
		return nil, err
	}
	for i := range d.Trips {
		sortBySeq(d.Trips[i].StopTimes, seqs[i])
		fillMissingTimes(d.Trips[i].StopTimes)
	}
	return d, nil
}

func sortBySeq(st []StopTime, seq []int32) {
	idx := make([]int, len(st))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return seq[idx[a]] < seq[idx[b]] })
	out := make([]StopTime, len(st))
	for i, j := range idx {
		out[i] = st[j]
	}
	copy(st, out)
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
