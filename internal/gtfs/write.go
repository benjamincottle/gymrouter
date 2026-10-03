package gtfs

import (
	"archive/zip"
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/benjamincottle/gymrouter/internal/geo"
)

// WriteZip writes a minimal GTFS feed containing the trips of the given days (DayOffset 0 only),
// with services expressed as calendar_dates, plus the given shapes. Trip, stop and route IDs are
// preserved so realtime updates still match. It's used to build small test fixtures from real data.
func WriteZip(path string, shapes map[string][]geo.Point, days ...*Day) error {
	type tripRec struct {
		t       Trip
		route   Route
		stopIDs []string // per call, resolved against the day the trip came from
		dates   map[string]bool
	}
	trips := map[string]*tripRec{}
	stops := map[string]Stop{}
	var order []string
	for _, d := range days {
		date := d.Date.Format("20060102")
		for _, t := range d.Trips {
			if t.DayOffset != 0 || t.Status == Added {
				continue
			}
			rec, ok := trips[t.ID]
			if !ok {
				rec = &tripRec{t: t, route: d.Routes[t.Route], dates: map[string]bool{}}
				if t.Sched != nil {
					rec.t.StopTimes = t.Sched
				}
				for _, st := range rec.t.StopTimes {
					rec.stopIDs = append(rec.stopIDs, d.Stops[st.Stop].ID)
				}
				trips[t.ID] = rec
				order = append(order, t.ID)
			}
			rec.dates[date] = true
			for _, st := range rec.t.StopTimes {
				s := d.Stops[st.Stop]
				stops[s.ID] = s
				if s.Parent != "" {
					if pi, ok := d.StopIndex[s.Parent]; ok {
						stops[s.Parent] = d.Stops[pi]
					}
				}
			}
		}
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	write := func(name string, header []string, rows [][]string) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		cw := csv.NewWriter(w)
		if err := cw.Write(header); err != nil {
			return err
		}
		if err := cw.WriteAll(rows); err != nil {
			return err
		}
		return cw.Error()
	}

	var stopRows [][]string
	for _, s := range stops {
		stopRows = append(stopRows, []string{s.ID, s.Name, ftoa(s.Pos.Lat), ftoa(s.Pos.Lon), s.LocationType, s.Parent})
	}
	sortRows(stopRows)
	routes := map[string]Route{}
	services := map[string][]string{}
	var tripRows, timeRows [][]string
	for _, id := range order {
		rec := trips[id]
		routes[rec.route.ID] = rec.route
		var ds []string
		for d := range rec.dates {
			ds = append(ds, d)
		}
		sort.Strings(ds)
		svc := "D" + strings.Join(ds, "_")
		services[svc] = ds
		tripRows = append(tripRows, []string{rec.route.ID, svc, id, rec.t.Headsign, rec.t.Shape})
		for i, st := range rec.t.StopTimes {
			timeRows = append(timeRows, []string{id, hms(st.Arr), hms(st.Dep), rec.stopIDs[i], strconv.Itoa(int(st.Seq))})
		}
	}
	var routeRows, calRows [][]string
	for _, r := range routes {
		routeRows = append(routeRows, []string{r.ID, r.AgencyID, r.ShortName, r.LongName, r.Desc, strconv.Itoa(r.Type), r.Color, r.TextColor})
	}
	sortRows(routeRows)
	for svc, ds := range services {
		for _, d := range ds {
			calRows = append(calRows, []string{svc, d, "1"})
		}
	}
	sortRows(calRows)

	for _, w := range []struct {
		name   string
		header []string
		rows   [][]string
	}{
		{"stops.txt", []string{"stop_id", "stop_name", "stop_lat", "stop_lon", "location_type", "parent_station"}, stopRows},
		{"routes.txt", []string{"route_id", "agency_id", "route_short_name", "route_long_name", "route_desc", "route_type", "route_color", "route_text_color"}, routeRows},
		{"trips.txt", []string{"route_id", "service_id", "trip_id", "trip_headsign", "shape_id"}, tripRows},
		{"stop_times.txt", []string{"trip_id", "arrival_time", "departure_time", "stop_id", "stop_sequence"}, timeRows},
		{"calendar_dates.txt", []string{"service_id", "date", "exception_type"}, calRows},
		{"shapes.txt", []string{"shape_id", "shape_pt_lat", "shape_pt_lon", "shape_pt_sequence"}, shapeRows(shapes)},
		{"pathways.txt", []string{"pathway_id", "from_stop_id", "to_stop_id", "pathway_mode", "is_bidirectional", "traversal_time"}, pathwayRows(days, stops)},
	} {
		if err := write(w.name, w.header, w.rows); err != nil {
			zw.Close()
			f.Close()
			return fmt.Errorf("%s: %w", w.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func ftoa(v float64) string { return strconv.FormatFloat(v, 'f', 7, 64) }

func hms(t int32) string {
	return fmt.Sprintf("%02d:%02d:%02d", t/3600, t/60%60, t%60)
}

func sortRows(rows [][]string) {
	sort.Slice(rows, func(a, b int) bool { return strings.Join(rows[a], ",") < strings.Join(rows[b], ",") })
}

// pathwayRows keeps the pathways of stations that appear in the fixture.
func pathwayRows(days []*Day, stops map[string]Stop) [][]string {
	stations := map[string]bool{}
	for _, s := range stops {
		if s.Parent != "" {
			stations[s.Parent] = true
		}
	}
	// Pathway IDs and internal nodes are prefixed with the stop/station ID in TfNSW data; keep a
	// pathway if either end belongs to a fixture station (by parent) or is named after one.
	inStation := func(id string) bool {
		if s, ok := stops[id]; ok && stations[s.Parent] {
			return true
		}
		prefix, _, _ := strings.Cut(id, "_")
		return stations[prefix]
	}
	seen := map[string]bool{}
	var rows [][]string
	for _, d := range days {
		for i, p := range d.Pathways {
			if !inStation(p.From) && !inStation(p.To) {
				continue
			}
			key := p.From + ">" + p.To + ">" + strconv.Itoa(p.Mode)
			if seen[key] {
				continue
			}
			seen[key] = true
			bi := "0"
			if p.Bidirectional {
				bi = "1"
			}
			rows = append(rows, []string{"pw" + strconv.Itoa(i), p.From, p.To, strconv.Itoa(p.Mode), bi, strconv.Itoa(int(p.Secs))})
		}
	}
	sortRows(rows)
	return rows
}

func shapeRows(shapes map[string][]geo.Point) [][]string {
	ids := make([]string, 0, len(shapes))
	for id := range shapes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var rows [][]string
	for _, id := range ids {
		for i, p := range shapes[id] {
			rows = append(rows, []string{id, ftoa(p.Lat), ftoa(p.Lon), strconv.Itoa(i + 1)})
		}
	}
	return rows
}
