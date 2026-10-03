package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/lines"
	"github.com/benjamincottle/gymrouter/internal/raptor"
	"github.com/benjamincottle/gymrouter/internal/suggest"
)

// Gym locations are public business addresses (9degrees.com.au/locations), geocoded.
var knownGyms = map[string]geo.Point{
	"lanecove":  {Lat: -33.807948, Lon: 151.150629},   // 1a/21 Mars Rd, Lane Cove West
	"chatswood": {Lat: -33.7856633, Lon: 151.2003558}, // 7/372 Eastern Valley Way, Chatswood
	"rydalmere": {Lat: -33.816213, Lon: 151.039465},   // Unit 11, 38-46 South St, Rydalmere
}

func parsePlace(s string) (geo.Point, error) {
	if p, ok := knownGyms[strings.ToLower(s)]; ok {
		return p, nil
	}
	parts := strings.Split(s, ",")
	if len(parts) == 2 {
		lat, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		lon, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err1 == nil && err2 == nil {
			return geo.Point{Lat: lat, Lon: lon}, nil
		}
	}
	return geo.Point{}, fmt.Errorf("place %q: want a gym name (%s) or \"lat,lon\"", s, gymNames())
}

func gymNames() string {
	var n []string
	for k := range knownGyms {
		n = append(n, k)
	}
	sort.Strings(n)
	return strings.Join(n, ", ")
}

func parseClock(s string) (int32, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, fmt.Errorf("time %q: want HH:MM", s)
	}
	return int32(t.Hour()*3600 + t.Minute()*60), nil
}

func suggestLines(args []string) error {
	fs := flag.NewFlagSet("suggest-lines", flag.ExitOnError)
	feed := fs.String("gtfs", "data/complete_gtfs.zip", "complete GTFS bundle")
	trains := fs.String("trains", "data/st_sched.zip", "Sydney Trains GTFS bundle (\"\" to use the complete bundle)")
	from := fs.String("from", "", "origin: gym name or \"lat,lon\" (not saved anywhere)")
	to := fs.String("to", "", "destination: gym name or \"lat,lon\"")
	dateS := fs.String("date", "", "service date YYYY-MM-DD (default: next Tuesday)")
	start := fs.String("start", "17:00", "first departure HH:MM")
	end := fs.String("end", "19:00", "last departure HH:MM")
	step := fs.Duration("step", 10*time.Minute, "departure time step")
	radius := fs.Float64("radius", 1200, "max walk to/from first/last stop, metres (straight line)")
	slack := fs.Duration("slack", 15*time.Minute, "keep alternatives arriving within this of the best")
	minChange := fs.Duration("min-change", time.Minute, "buffer when changing vehicles at the same stop")
	_ = fs.Parse(args)
	if *from == "" || *to == "" {
		return fmt.Errorf("--from and --to are required")
	}
	src, err := parsePlace(*from)
	if err != nil {
		return err
	}
	dst, err := parsePlace(*to)
	if err != nil {
		return err
	}
	date := nextWeekday(time.Now().In(sydney), time.Tuesday)
	if *dateS != "" {
		if date, err = time.ParseInLocation("2006-01-02", *dateS, sydney); err != nil {
			return fmt.Errorf("--date: %w", err)
		}
	}
	t0, err := parseClock(*start)
	if err != nil {
		return err
	}
	t1, err := parseClock(*end)
	if err != nil {
		return err
	}

	began := time.Now()
	public := func(r gtfs.Route) bool { return lines.ModeOf(r.Type) != lines.SchoolBus }
	srcs := []gtfs.Source{{Path: *feed, Include: public}}
	if *trains != "" {
		isTrain := func(r gtfs.Route) bool { return lines.ModeOf(r.Type) == lines.Train }
		srcs = []gtfs.Source{
			{Path: *feed, Include: func(r gtfs.Route) bool { return public(r) && !isTrain(r) }},
			{Path: *trains, Include: isTrain},
		}
	}
	day, err := gtfs.LoadService(date, srcs...)
	if err != nil {
		return err
	}
	opts := raptor.DefaultOptions()
	net := raptor.Build(day, opts)
	fmt.Fprintf(os.Stderr, "loaded %s: %d trips, %d patterns in %s\n",
		date.Format("Mon 2006-01-02"), len(day.Trips), len(net.Patterns), time.Since(began).Round(time.Millisecond))

	acc := net.StopsNear(src, *radius, opts)
	egr := net.StopsNear(dst, *radius, opts)
	if len(acc) == 0 || len(egr) == 0 {
		return fmt.Errorf("no stops within %.0fm of origin (%d) or destination (%d)", *radius, len(acc), len(egr))
	}

	res := suggest.Run(net, acc, egr, suggest.Window{
		Label: date.Format("Mon 2006-01-02"), Start: t0, End: t1, Step: int32(step.Seconds()),
	}, suggest.Params{MaxRides: 4, MinChange: int32(minChange.Seconds()), Slack: int32(slack.Seconds()), MaxAlts: 40})
	fmt.Printf("%s %s–%s, every %s (%d departure times)\n\n", date.Format("Mon 2006-01-02"), *start, *end, *step, res.Departures)
	fmt.Printf("%-6s %-5s %-6s  %s\n", "median", "best", "seen", "itinerary")
	for _, it := range res.Itineraries {
		fmt.Printf("%4dm  %4dm  %2d/%-2d  %s\n", it.MedianS/60, it.BestS/60, it.Seen, it.Of, it.Desc)
		fmt.Printf("%22s via %s\n", "", it.Via)
	}
	fmt.Println("\nsuggested lines (share of departure times at which each appears):")
	for _, l := range suggest.Scores([]suggest.WindowResult{res}) {
		mark := " "
		if l.Recommended {
			mark = "*"
		}
		fmt.Printf("  %s %-22s %3.0f%%\n", mark, l.Line, l.Share*100)
	}
	return nil
}

func nextWeekday(now time.Time, wd time.Weekday) time.Time {
	d := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, sydney)
	for {
		d = d.AddDate(0, 0, 1)
		if d.Weekday() == wd {
			return d
		}
	}
}
