package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/gtfsrt/pb"
	"github.com/benjamincottle/gymrouter/internal/lines"
	"github.com/benjamincottle/gymrouter/internal/tfnsw"
	"github.com/benjamincottle/gymrouter/internal/timetable"
)

// makeFixture writes a small real-data test fixture: a trimmed GTFS zip for some lines and dates,
// and optionally a realtime snapshot filtered to those lines.
func makeFixture(args []string) error {
	fs := flag.NewFlagSet("make-fixture", flag.ExitOnError)
	complete := fs.String("gtfs", "data/complete_gtfs.zip", "complete GTFS bundle")
	trains := fs.String("trains", "data/st_sched.zip", "Sydney Trains GTFS bundle (\"\" to use the complete bundle)")
	lineList := fs.String("lines", "", "comma-separated lines, e.g. \"train T9,bus 288\"")
	dateList := fs.String("dates", "", "comma-separated service dates YYYY-MM-DD")
	out := fs.String("out", "testdata/fixture", "output directory")
	rt := fs.Bool("realtime", false, "also save a realtime snapshot (needs TFNSW_API_KEY)")
	_ = fs.Parse(args)

	set, err := lines.ParseSet(splitList(*lineList)...)
	if err != nil {
		return err
	}
	if len(set) == 0 || *dateList == "" {
		return fmt.Errorf("--lines and --dates are required")
	}
	var days []*gtfs.Day
	for _, ds := range splitList(*dateList) {
		date, err := time.ParseInLocation("2006-01-02", ds, sydney)
		if err != nil {
			return fmt.Errorf("--dates: %w", err)
		}
		d, err := timetable.Load(date, timetable.Paths{Complete: *complete, Trains: *trains}, set)
		if err != nil {
			return err
		}
		if miss := timetable.Missing(d, set); len(miss) > 0 {
			fmt.Fprintf(os.Stderr, "warning: %s: no trips for %v\n", ds, miss)
		}
		days = append(days, d)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	if err := gtfs.WriteZip(filepath.Join(*out, "gtfs.zip"), days...); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", filepath.Join(*out, "gtfs.zip"))
	if !*rt {
		return nil
	}

	key := os.Getenv("TFNSW_API_KEY")
	if key == "" {
		return fmt.Errorf("TFNSW_API_KEY is not set")
	}
	trips, routes := map[string]bool{}, map[string]bool{}
	for _, d := range days {
		for id := range d.TripIndex {
			trips[id] = true
		}
		for _, r := range d.Routes {
			routes[r.ID] = true
		}
	}
	c := tfnsw.NewClient(key)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, f := range tfnsw.FeedsFor(set) {
		for kind, path := range map[string]string{"tu": f.TripUpdates, "vp": f.VehiclePos} {
			b, err := c.Get(ctx, path, 64<<20)
			if err != nil {
				return err
			}
			var m pb.FeedMessage
			if err := proto.Unmarshal(b, &m); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			kept := m.Entity[:0]
			for _, e := range m.Entity {
				td := e.GetTripUpdate().GetTrip()
				if td == nil {
					td = e.GetVehicle().GetTrip()
				}
				if trips[td.GetTripId()] || routes[td.GetRouteId()] {
					kept = append(kept, e)
				}
			}
			m.Entity = kept
			b, err = proto.Marshal(&m)
			if err != nil {
				return err
			}
			name := filepath.Join(*out, fmt.Sprintf("%s-%s.pb", kind, f.Name))
			if err := os.WriteFile(name, b, 0o644); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s (%d entities)\n", name, len(kept))
		}
	}
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
