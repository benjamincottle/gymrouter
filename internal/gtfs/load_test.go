package gtfs

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseTime(t *testing.T) {
	for in, want := range map[string]int32{"00:00:00": 0, "08:05:09": 8*3600 + 5*60 + 9, "25:10:00": 25*3600 + 600, "": -1} {
		got, err := parseTime(in)
		if err != nil || got != want {
			t.Errorf("parseTime(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	if _, err := parseTime("8:00"); err == nil {
		t.Error("want error for malformed time")
	}
}

func writeFeed(t *testing.T, files map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "feed.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadDayFiltersServicesAndOrdersStops(t *testing.T) {
	p := writeFeed(t, map[string]string{
		"stops.txt": "\ufeffstop_id,stop_name,stop_lat,stop_lon,parent_station\n" +
			`"S1","One","-33.8","151.0",""` + "\n" + `"S2","Two","-33.81","151.0",""` + "\n",
		"routes.txt": "route_id,agency_id,route_short_name,route_long_name,route_desc,route_type\nR,A,288,L,D,700\n",
		"calendar.txt": "service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\n" +
			"WK,1,1,1,1,1,0,0,20260101,20261231\nSAT,0,0,0,0,0,1,0,20260101,20261231\n",
		"calendar_dates.txt": "service_id,date,exception_type\nWK,20261007,2\nSAT,20261007,1\n",
		"trips.txt":          "route_id,service_id,trip_id\nR,WK,t-wk\nR,SAT,t-sat\n",
		// out of sequence order on purpose
		"stop_times.txt": "trip_id,arrival_time,departure_time,stop_id,stop_sequence\n" +
			"t-wk,08:10:00,08:10:00,S2,2\nt-wk,08:00:00,08:00:00,S1,1\n" +
			"t-sat,09:10:00,09:10:00,S2,2\nt-sat,09:00:00,09:00:00,S1,1\n",
	})
	tue := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	d, err := LoadDay(p, tue)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Trips) != 1 || d.Trips[0].ID != "t-wk" {
		t.Fatalf("Tuesday: want only t-wk, got %+v", d.Trips)
	}
	st := d.Trips[0].StopTimes
	if len(st) != 2 || d.Stops[st[0].Stop].ID != "S1" || st[0].Dep != 8*3600 {
		t.Fatalf("stop times not ordered by sequence: %+v", st)
	}
	// Wednesday 7th: weekday service removed, Saturday service added by calendar_dates.
	d, err = LoadDay(p, tue.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Trips) != 1 || d.Trips[0].ID != "t-sat" {
		t.Fatalf("Wednesday exceptions: want only t-sat, got %+v", d.Trips)
	}
}

func TestLoadServiceOvernightAndSources(t *testing.T) {
	base := map[string]string{
		"stops.txt":  "stop_id,stop_name,stop_lat,stop_lon\nS1,One,-33.8,151.0\nS2,Two,-33.81,151.0\n",
		"routes.txt": "route_id,route_short_name,route_type\nBUS,288,700\nTRN,T9,2\n",
		"calendar.txt": "service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\n" +
			"MON,1,0,0,0,0,0,0,20260101,20261231\nTUE,0,1,0,0,0,0,0,20260101,20261231\n",
		"trips.txt": "route_id,service_id,trip_id\nBUS,MON,late-mon\nBUS,MON,early-mon\nBUS,TUE,tue\nTRN,TUE,train\n",
		"stop_times.txt": "trip_id,arrival_time,departure_time,stop_id,stop_sequence\n" +
			"late-mon,23:50:00,23:50:00,S1,1\nlate-mon,24:20:00,24:20:00,S2,2\n" +
			"early-mon,10:00:00,10:00:00,S1,1\nearly-mon,10:20:00,10:20:00,S2,2\n" +
			"tue,08:00:00,08:00:00,S1,1\ntue,08:20:00,08:20:00,S2,2\n" +
			"train,08:05:00,08:05:00,S1,1\ntrain,08:15:00,08:15:00,S2,2\n",
	}
	other := map[string]string{
		"stops.txt":  "stop_id,stop_name,stop_lat,stop_lon\nS2,Two (dup),-33.81,151.0\nS3,Three,-33.82,151.0\n",
		"routes.txt": "route_id,route_short_name,route_type\nT9X,T9,2\n",
		"calendar.txt": "service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\n" +
			"TUE,0,1,0,0,0,0,0,20260101,20261231\n",
		"trips.txt":      "route_id,service_id,trip_id\nT9X,TUE,train2\n",
		"stop_times.txt": "trip_id,arrival_time,departure_time,stop_id,stop_sequence\ntrain2,09:00:00,09:00:00,S2,1\ntrain2,09:10:00,09:10:00,S3,2\n",
	}
	notTrain := func(r Route) bool { return r.Type != 2 }
	tue := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	d, err := LoadService(tue, Source{Path: writeFeed(t, base), Include: notTrain}, Source{Path: writeFeed(t, other)})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Trip{}
	for _, tr := range d.Trips {
		got[tr.ID] = tr
	}
	if _, ok := got["early-mon"]; ok {
		t.Error("Monday trip finished before midnight should be dropped")
	}
	if _, ok := got["train"]; ok {
		t.Error("train route from first source should be excluded")
	}
	late, ok := got["late-mon"]
	if !ok || late.DayOffset != -1 || late.StopTimes[1].Arr != 20*60 || late.StopTimes[0].Dep != -10*60 {
		t.Errorf("late Monday trip not shifted to Tuesday times: %+v", late)
	}
	if _, ok := got["tue"]; !ok {
		t.Error("Tuesday bus missing")
	}
	if tr, ok := got["train2"]; !ok || len(tr.StopTimes) != 2 {
		t.Errorf("train from second source missing or incomplete: %+v", tr)
	}
	if len(d.Stops) != 3 || d.Stops[d.StopIndex["S2"]].Name != "Two" {
		t.Errorf("stops not merged by id (first source wins): %+v", d.Stops)
	}
	if ids := d.TripIndex["late-mon"]; len(ids) != 1 || d.Trips[ids[0]].ID != "late-mon" {
		t.Errorf("TripIndex wrong: %v", ids)
	}
}
