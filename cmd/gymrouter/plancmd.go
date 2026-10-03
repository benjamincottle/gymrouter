package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/benjamincottle/gymrouter/internal/plan"
	"github.com/benjamincottle/gymrouter/internal/raptor"
	"github.com/benjamincottle/gymrouter/internal/realtime"
	"github.com/benjamincottle/gymrouter/internal/tfnsw"
	"github.com/benjamincottle/gymrouter/internal/timetable"

	"github.com/benjamincottle/gymrouter/internal/lines"
)

// planCmd plans a trip from the command line, optionally with live data. Handy for checking the
// router against what actually happens; the origin is only used in memory.
func planCmd(args []string) error {
	fs := flag.NewFlagSet("plan", flag.ExitOnError)
	complete := fs.String("gtfs", "data/complete_gtfs.zip", "complete GTFS bundle")
	trains := fs.String("trains", "data/st_sched.zip", "Sydney Trains GTFS bundle")
	from := fs.String("from", "", "origin: gym name or \"lat,lon\"")
	to := fs.String("to", "", "destination: gym name or \"lat,lon\"")
	lineList := fs.String("lines", "", "comma-separated lines to route on")
	dateS := fs.String("date", "", "service date YYYY-MM-DD (default today)")
	at := fs.String("at", "", "leave at HH:MM (default now)")
	window := fs.Duration("window", 30*time.Minute, "also consider leaving up to this much later")
	radius := fs.Float64("radius", 1000, "max walk to/from first/last stop, metres")
	alt := fs.Duration("alternatives", 10*time.Minute, "show other routes arriving within this of the best")
	live := fs.Bool("realtime", false, "apply live predictions (needs TFNSW_API_KEY)")
	_ = fs.Parse(args)

	set, err := lines.ParseSet(splitList(*lineList)...)
	if err != nil {
		return err
	}
	if len(set) == 0 || *from == "" || *to == "" {
		return errors.New("--from, --to and --lines are required")
	}
	src, err := parsePlace(*from)
	if err != nil {
		return err
	}
	dst, err := parsePlace(*to)
	if err != nil {
		return err
	}
	now := time.Now().In(sydney)
	date := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, sydney)
	if *dateS != "" {
		if date, err = time.ParseInLocation("2006-01-02", *dateS, sydney); err != nil {
			return fmt.Errorf("--date: %w", err)
		}
	}
	midnight := realtime.ServiceMidnight(date, sydney)
	depart := int32(now.Sub(midnight).Seconds())
	if *at != "" {
		c, err := parseClock(*at)
		if err != nil {
			return err
		}
		depart = c
	}

	day, err := timetable.Load(date, timetable.Paths{Complete: *complete, Trains: *trains}, set)
	if err != nil {
		return err
	}
	if miss := timetable.Missing(day, set); len(miss) > 0 {
		fmt.Fprintf(os.Stderr, "note: no trips on %s for %v\n", date.Format("2006-01-02"), miss)
	}
	if *live {
		key := os.Getenv("TFNSW_API_KEY")
		if key == "" {
			return errors.New("TFNSW_API_KEY is not set")
		}
		c := tfnsw.NewClient(key)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		for _, f := range tfnsw.FeedsFor(set) {
			b, err := c.Get(ctx, f.TripUpdates, 64<<20)
			if err != nil {
				return err
			}
			feed, err := realtime.Decode(b)
			if err != nil {
				return err
			}
			var st realtime.Stats
			day, st = realtime.Apply(day, sydney, feed.Trips)
			fmt.Fprintf(os.Stderr, "realtime %s: %+v\n", f.Name, st)
		}
	}

	opts := raptor.DefaultOptions()
	n := raptor.Build(day, opts)
	options := plan.Plan(n, plan.Request{
		Access: n.StopsNear(src, *radius, opts), Egress: n.StopsNear(dst, *radius, opts),
		Depart: depart, Window: int32(window.Seconds()), MinChange: 60, AltSlack: int32(alt.Seconds()),
	})
	if len(options) == 0 {
		return errors.New("no options found")
	}
	for _, o := range options {
		fmt.Print(plan.Describe(day, o))
	}
	return nil
}
