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
