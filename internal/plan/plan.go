// Package plan turns router results into ranked trip options: it searches a window of departure
// times, adds alternative routes, and rates each connection's risk.
package plan

import (
	"sort"
	"strconv"
	"strings"

	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/lines"
	"github.com/benjamincottle/gymrouter/internal/raptor"
)

// Thresholds classify connection slack (seconds to spare after walking/changing).
type Thresholds struct {
	Safe  int32 // slack >= Safe is safe
	Tight int32 // Tight <= slack < Safe is tight; 0 <= slack < Tight is at risk; < 0 missed
}

// DefaultThresholds: safe ≥ 3 min, tight 1–3 min, at risk < 1 min.
var DefaultThresholds = Thresholds{Safe: 180, Tight: 60}

// Risk is a connection's risk level.
type Risk string

const (
	Safe   Risk = "safe"
	Tight  Risk = "tight"
	AtRisk Risk = "at-risk"
	Missed Risk = "missed"
)

// Classify rates a connection with the given slack.
func (t Thresholds) Classify(slack int32) Risk {
	switch {
	case slack < 0:
		return Missed
	case slack < t.Tight:
		return AtRisk
	case slack < t.Safe:
		return Tight
	default:
		return Safe
	}
}

// TransferOverride is a personal walking/changing time between two stations or stops
// (GTFS stop IDs; a parent station ID covers all its platforms).
type TransferOverride struct {
	From, To string
	Secs     int32
}

// Request describes one planning query.
type Request struct {
	Access, Egress []raptor.Access
	Depart         int32 // earliest time to leave the origin (seconds after service-day midnight)
	Window         int32 // also consider leaving up to this much later
	MinChange      int32 // default buffer when changing vehicles at the same stop
	Overrides      []TransferOverride
	Thresholds     Thresholds
	MaxRides       int
	MaxOptions     int
	AltSlack       int32 // also return different routes arriving within this of the best (0: none)
}

// Transfer describes the connection between ride legs Legs[FromLeg] and Legs[ToLeg].
type Transfer struct {
	FromLeg, ToLeg int
	Walk           int32 // walking/changing time needed
	Slack          int32
	Risk           Risk
	// The next service of the onward line from the same stop, if this connection is missed.
	FallbackTrip int32 // Day.Trips index, -1 if none
	FallbackDep  int32
}

// Option is one way to make the trip.
type Option struct {
	Legs        []raptor.Leg
	LeaveAt     int32 // leave the origin just in time for the first vehicle
	Arrive      int32
	Rides       int
	Lines       []lines.Key
	Transfers   []Transfer
	Alternative bool // found by excluding lines of a better option
}

// Duration is door-to-door time from LeaveAt.
func (o Option) Duration() int32 { return o.Arrive - o.LeaveAt }

// WorstRisk is the riskiest connection of the option (Safe if it has none).
func (o Option) WorstRisk() Risk {
	order := map[Risk]int{Safe: 0, Tight: 1, AtRisk: 2, Missed: 3}
	w := Safe
	for _, t := range o.Transfers {
		if order[t.Risk] > order[w] {
			w = t.Risk
		}
	}
	return w
}

// maxDepartures caps how many distinct leave times are searched in one window.
const maxDepartures = 60

// Plan returns options leaving within the window, best first: earliest arrival, then fewer
// rides, then later departure. Options dominated by another (leaves no earlier, arrives no
// later, no more rides) are dropped unless they're alternatives.
func Plan(n *raptor.Network, req Request) []Option {
	if req.Thresholds == (Thresholds{}) {
		req.Thresholds = DefaultThresholds
	}
	if req.MaxRides == 0 {
		req.MaxRides = 4
	}
	if req.MaxOptions == 0 {
		req.MaxOptions = 8
	}
	transfer := overrideFunc(n.Day, req.Overrides)
	base := raptor.Query{Access: req.Access, Egress: req.Egress, MaxRides: req.MaxRides,
		MinChange: req.MinChange, Transfer: transfer}

	var all []Option
	seen := map[string]bool{}
	add := func(j raptor.Journey, alt bool) {
		o := build(n, j, req, transfer)
		if k := signature(o); !seen[k] {
			seen[k] = true
			o.Alternative = alt
			all = append(all, o)
		}
	}
	for _, leave := range leaveTimes(n, req) {
		q := base
		q.Depart = leave
		for _, j := range n.Run(q) {
			add(j, false)
		}
	}
	options := pareto(all)
	if req.AltSlack > 0 && len(options) > 0 {
		for _, j := range Alternatives(n, withDepart(base, options[0].LeaveAt), req.AltSlack, 24) {
			add(j, true)
		}
		options = pareto(all)
		best := options[0].Arrive
		have := map[string]bool{}
		for _, o := range options {
			have[lineSeq(o)] = true
		}
		for _, o := range all {
			// An alternative is only useful if it takes different lines.
			if o.Alternative && o.Arrive <= best+req.AltSlack && !have[lineSeq(o)] {
				have[lineSeq(o)] = true
				options = append(options, o)
			}
		}
	}
	options = safestVariants(options)
	sortOptions(options)
	if len(options) > req.MaxOptions {
		options = options[:req.MaxOptions]
	}
	return options
}

func withDepart(q raptor.Query, t int32) raptor.Query { q.Depart = t; return q }

// leaveTimes lists the distinct times to leave the origin that just catch a departure from an
// access stop within the window, plus the window start itself.
func leaveTimes(n *raptor.Network, req Request) []int32 {
	set := map[int32]bool{req.Depart: true}
	for _, a := range req.Access {
		n.DeparturesFrom(a.Stop, req.Depart+a.Secs, req.Depart+req.Window+a.Secs, nil, func(dep int32) {
			set[dep-a.Secs] = true
		})
	}
	out := make([]int32, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Slice(out, func(a, b int) bool { return out[a] < out[b] })
	if len(out) > maxDepartures {
		step := float64(len(out)) / maxDepartures
		thin := make([]int32, 0, maxDepartures)
		for i := 0; i < maxDepartures; i++ {
			thin = append(thin, out[int(float64(i)*step)])
		}
		out = thin
	}
	return out
}

// Alternatives re-runs q with the lines of each found journey excluded (up to two lines at a
// time) and returns the distinct journeys arriving within slack of the best. maxRuns bounds the work.
func Alternatives(n *raptor.Network, q raptor.Query, slack int32, maxRuns int) []raptor.Journey {
	keyOf := func(r int32) lines.Key { rt := n.Day.Routes[r]; return lines.Of(rt.Type, rt.ShortName) }
	var out []raptor.Journey
	seen := map[string]bool{}
	tried := map[string]bool{}
	best := gtfs.NoTime
	queue := [][]lines.Key{nil}
	for runs := 0; len(queue) > 0 && runs < maxRuns; {
		ban := queue[0]
		queue = queue[1:]
		names := make([]string, len(ban))
		for i, k := range ban {
			names[i] = k.String()
		}
		sort.Strings(names)
		key := strings.Join(names, "|")
		if tried[key] {
			continue
		}
		tried[key] = true
		runs++
		banned := map[lines.Key]bool{}
		for _, k := range ban {
			banned[k] = true
		}
		q.BanRoute = func(r int32) bool { return banned[keyOf(r)] }
		for _, j := range n.Run(q) {
			if j.Arr < best {
				best = j.Arr
			}
			if j.Arr > best+slack {
				continue
			}
			var ls []lines.Key
			var sig strings.Builder
			for _, l := range j.Legs {
				if l.Kind == raptor.Ride {
					ls = append(ls, keyOf(l.Route))
					sig.WriteString(keyOf(l.Route).String() + ">" + strconv.Itoa(int(l.From)) + ">" + strconv.Itoa(int(l.To)) + ";")
				}
			}
			if seen[sig.String()] {
				continue
			}
			seen[sig.String()] = true
			out = append(out, j)
			if len(ban) < 2 {
				for _, l := range ls {
					queue = append(queue, append(append([]lines.Key{}, ban...), l))
				}
			}
		}
	}
	kept := out[:0]
	for _, j := range out {
		if j.Arr <= best+slack {
			kept = append(kept, j)
		}
	}
	return kept
}

func build(n *raptor.Network, j raptor.Journey, req Request, transfer func(a, b int32) (int32, bool)) Option {
	j.Legs = mergeWalks(j.Legs)
	o := Option{Legs: j.Legs, Arrive: j.Arr, Rides: j.Rides, LeaveAt: j.Dep}
	var rides []int
	for i, l := range j.Legs {
		if l.Kind == raptor.Ride {
			rides = append(rides, i)
			r := n.Day.Routes[l.Route]
			o.Lines = append(o.Lines, lines.Of(r.Type, r.ShortName))
		}
	}
	if len(rides) > 0 && j.Legs[0].Kind == raptor.Walk {
		walk := j.Legs[0].Arr - j.Legs[0].Dep
		o.LeaveAt = j.Legs[rides[0]].Dep - walk
		o.Legs = append([]raptor.Leg(nil), j.Legs...)
		o.Legs[0].Dep, o.Legs[0].Arr = o.LeaveAt, o.LeaveAt+walk
	}
	for k := 1; k < len(rides); k++ {
		in, out := j.Legs[rides[k-1]], j.Legs[rides[k]]
		need := req.MinChange
		if in.To != out.From {
			need = 0
			for i := rides[k-1] + 1; i < rides[k]; i++ {
				need += j.Legs[i].Arr - j.Legs[i].Dep
			}
		} else if v, ok := transfer(in.To, in.To); ok {
			need = v
		}
		slack := out.Dep - (in.Arr + need)
		t := Transfer{FromLeg: rides[k-1], ToLeg: rides[k], Walk: need, Slack: slack,
			Risk: req.Thresholds.Classify(slack), FallbackTrip: -1}
		if ft, fd, ok := n.NextDeparture(out.Pattern, out.BoardIdx, out.Dep, out.Trip); ok {
			t.FallbackTrip, t.FallbackDep = ft, fd
		}
		o.Transfers = append(o.Transfers, t)
	}
	return o
}

// mergeWalks joins consecutive walking legs (a transfer footpath followed by the final walk).
func mergeWalks(legs []raptor.Leg) []raptor.Leg {
	out := make([]raptor.Leg, 0, len(legs))
	for _, l := range legs {
		if n := len(out); n > 0 && l.Kind == raptor.Walk && out[n-1].Kind == raptor.Walk {
			out[n-1].To, out[n-1].Arr = l.To, out[n-1].Arr+(l.Arr-l.Dep)
			continue
		}
		out = append(out, l)
	}
	return out
}

// overrideFunc resolves personal transfer times to stop pairs; a station ID matches its platforms.
func overrideFunc(d *gtfs.Day, ovs []TransferOverride) func(a, b int32) (int32, bool) {
	if len(ovs) == 0 {
		return func(int32, int32) (int32, bool) { return 0, false }
	}
	type pair struct{ a, b string }
	m := map[pair]int32{}
	for _, o := range ovs {
		m[pair{o.From, o.To}] = o.Secs
	}
	ids := func(s int32) []string {
		st := d.Stops[s]
		if st.Parent != "" {
			return []string{st.ID, st.Parent}
		}
		return []string{st.ID}
	}
	return func(a, b int32) (int32, bool) {
		for _, x := range ids(a) {
			for _, y := range ids(b) {
				if v, ok := m[pair{x, y}]; ok {
					return v, true
				}
			}
		}
		return 0, false
	}
}

func signature(o Option) string {
	var b strings.Builder
	for _, l := range o.Legs {
		if l.Kind == raptor.Ride {
			b.WriteString(strconv.Itoa(int(l.Trip)) + ":" + strconv.Itoa(int(l.From)) + ">" + strconv.Itoa(int(l.To)) + ";")
		}
	}
	return b.String()
}

func dominates(a, b Option) bool {
	return a.LeaveAt >= b.LeaveAt && a.Arrive <= b.Arrive && a.Rides <= b.Rides &&
		(a.LeaveAt > b.LeaveAt || a.Arrive < b.Arrive || a.Rides < b.Rides)
}

func pareto(all []Option) []Option {
	var out []Option
	for i, o := range all {
		if o.Alternative {
			continue
		}
		dominated := false
		for k, p := range all {
			if k != i && !p.Alternative && dominates(p, o) {
				dominated = true
				break
			}
		}
		if !dominated {
			out = append(out, o)
		}
	}
	sortOptions(out)
	return out
}

func lineSeq(o Option) string {
	parts := make([]string, len(o.Lines))
	for i, k := range o.Lines {
		parts[i] = k.String()
	}
	return strings.Join(parts, ">")
}

// safestVariants keeps one option per (lines, leave, arrive): the one with the least risky
// connections. Variants differ only in which nearby stop is used for a change.
func safestVariants(opts []Option) []Option {
	rank := map[Risk]int{Safe: 0, Tight: 1, AtRisk: 2, Missed: 3}
	minSlack := func(o Option) int32 {
		m := int32(1 << 30)
		for _, t := range o.Transfers {
			m = min(m, t.Slack)
		}
		return m
	}
	type key struct {
		lines         string
		leave, arrive int32
	}
	best := map[key]int{}
	var out []Option
	for _, o := range opts {
		k := key{lineSeq(o), o.LeaveAt, o.Arrive}
		i, ok := best[k]
		if !ok {
			best[k] = len(out)
			out = append(out, o)
			continue
		}
		cur := out[i]
		if r, c := rank[o.WorstRisk()], rank[cur.WorstRisk()]; r < c || (r == c && minSlack(o) > minSlack(cur)) {
			out[i] = o
		}
	}
	return out
}

func sortOptions(o []Option) {
	sort.SliceStable(o, func(a, b int) bool {
		if o[a].Arrive != o[b].Arrive {
			return o[a].Arrive < o[b].Arrive
		}
		if o[a].Rides != o[b].Rides {
			return o[a].Rides < o[b].Rides
		}
		return o[a].LeaveAt > o[b].LeaveAt
	})
}
