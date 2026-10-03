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
	"github.com/benjamincottle/gymrouter/internal/plan"
	"github.com/benjamincottle/gymrouter/internal/raptor"
)

// Gym locations are public business addresses (9degrees.com.au/locations), geocoded.
var knownGyms = map[string]geo.Point{
	"lanecove":  {Lat: -33.807948, Lon: 151.150629},   // 1a/21 Mars Rd, Lane Cove West
	"chatswood": {Lat: -33.7856633, Lon: 151.2003558}, // 7/372 Eastern Valley Way, Chatswood
	"rydalmere": {Lat: -33.816213, Lon: 151.039465},   // Unit 11, 38-46 South St, Rydalmere
}

func lineKey(r gtfs.Route) string { return lines.Of(r.Type, r.ShortName).String() }

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

type itinStats struct {
	desc      string
	lines     []string
	durations []int32
	rides     int
	ends      map[string]int // first boarding / last alighting stop → count
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

	stats := map[string]*itinStats{}
	queries := 0
	for dep := t0; dep <= t1; dep += int32(step.Seconds()) {
		found := plan.Alternatives(net, raptor.Query{
			Depart: dep, Access: acc, Egress: egr, MaxRides: 4, MinChange: int32(minChange.Seconds()),
		}, int32(slack.Seconds()), 40)
		queries++
		for _, j := range found {
			desc, lines := describe(net, j)
			st := stats[desc]
			if st == nil {
				st = &itinStats{desc: desc, lines: lines, rides: j.Rides, ends: map[string]int{}}
				stats[desc] = st
			}
			st.durations = append(st.durations, doorToDoor(j))
			st.ends[endpoints(net, j)]++
		}
	}

	var list []*itinStats
	for _, s := range stats {
		list = append(list, s)
	}
	sort.Slice(list, func(a, b int) bool {
		ma, mb := median(list[a].durations), median(list[b].durations)
		if ma != mb {
			return ma < mb
		}
		return len(list[a].durations) > len(list[b].durations)
	})
	nDeps := int((t1-t0)/int32(step.Seconds())) + 1
	fmt.Printf("%s %s–%s, every %s (%d departure times, %d searches)\n\n",
		date.Format("Mon 2006-01-02"), *start, *end, *step, nDeps, queries)
	fmt.Printf("%-6s %-5s %-6s  %s\n", "median", "best", "seen", "itinerary")
	lineCount := map[string]int{}
	for _, s := range list {
		fmt.Printf("%4dm  %4dm  %2d/%-2d  %s\n", median(s.durations)/60, minOf(s.durations)/60,
			len(s.durations), nDeps, s.desc)
		fmt.Printf("%22s via %s\n", "", topKey(s.ends))
		for _, l := range s.lines {
			lineCount[l] += len(s.durations)
		}
	}
	var lines []string
	for l := range lineCount {
		lines = append(lines, l)
	}
	sort.Slice(lines, func(a, b int) bool { return lineCount[lines[a]] > lineCount[lines[b]] })
	fmt.Println("\nsuggested lines (by how often they appear):")
	for _, l := range lines {
		fmt.Printf("  %-22s %d\n", l, lineCount[l])
	}
	return nil
}

func stationName(net *raptor.Network, s int32) string {
	st := net.Day.Stops[s]
	if st.Parent != "" {
		if pi, ok := net.Day.StopIndex[st.Parent]; ok {
			return net.Day.Stops[pi].Name
		}
	}
	return st.Name
}

// describe groups a journey by its lines and interchange stations, ignoring which
// exact stop is used at either end (those differ only by a few minutes of walking).
func describe(net *raptor.Network, j raptor.Journey) (string, []string) {
	var rides []raptor.Leg
	for _, l := range j.Legs {
		if l.Kind == raptor.Ride {
			rides = append(rides, l)
		}
	}
	var b strings.Builder
	var lines []string
	for i, l := range rides {
		k := lineKey(net.Day.Routes[l.Route])
		lines = append(lines, k)
		if i == 0 {
			b.WriteString(k)
		} else {
			fmt.Fprintf(&b, " → %s", k)
		}
		if i < len(rides)-1 {
			fmt.Fprintf(&b, " → [%s", stationName(net, l.To))
			if next := stationName(net, rides[i+1].From); next != stationName(net, l.To) {
				fmt.Fprintf(&b, " ~ %s", next)
			}
			b.WriteString("]")
		}
	}
	return b.String(), lines
}

func endpoints(net *raptor.Network, j raptor.Journey) string {
	var first, last raptor.Leg
	n := 0
	for _, l := range j.Legs {
		if l.Kind == raptor.Ride {
			if n == 0 {
				first = l
			}
			last = l
			n++
		}
	}
	return fmt.Sprintf("board %s (walk %dm), alight %s (walk %dm)",
		net.Day.Stops[first.From].Name, (j.Legs[0].Arr-j.Legs[0].Dep)/60,
		net.Day.Stops[last.To].Name, (j.Legs[len(j.Legs)-1].Arr-j.Legs[len(j.Legs)-1].Dep)/60)
}

func topKey(m map[string]int) string {
	best, bn := "", -1
	for k, n := range m {
		if n > bn || (n == bn && k < best) {
			best, bn = k, n
		}
	}
	return best
}

// doorToDoor measures from leaving the origin just in time for the first vehicle.
func doorToDoor(j raptor.Journey) int32 {
	first := j.Legs[0]
	walk := first.Arr - first.Dep
	for _, l := range j.Legs {
		if l.Kind == raptor.Ride {
			return j.Arr - (l.Dep - walk)
		}
	}
	return j.Arr - j.Dep
}

func median(v []int32) int32 {
	c := append([]int32{}, v...)
	sort.Slice(c, func(a, b int) bool { return c[a] < c[b] })
	return c[len(c)/2]
}

func minOf(v []int32) int32 {
	m := v[0]
	for _, x := range v {
		if x < m {
			m = x
		}
	}
	return m
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
