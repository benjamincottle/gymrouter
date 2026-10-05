// Package raptor implements round-based public transit routing (RAPTOR) over one service day.
package raptor

import (
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/gtfs"
)

const inf = int32(math.MaxInt32)

// Pattern is a set of trips of one route that call at exactly the same stops in the same order.
type Pattern struct {
	Route int32
	Stops []int32
	Trips []int32 // indexes into Day.Trips, sorted by departure from the first stop
	// fifo is true when no trip overtakes another at any stop (realtime delays can break this),
	// allowing binary search for the earliest trip.
	fifo bool
}

type patRef struct {
	pat int32
	idx int32
}

// Footpath is a walking transfer between two stops.
type Footpath struct {
	To   int32
	Secs int32
}

// Network is a routable view of a gtfs.Day.
type Network struct {
	Day          *gtfs.Day
	Patterns     []Pattern
	stopPatterns [][]patRef
	Footpaths    [][]Footpath
	tripPattern  map[int32]int32 // Day.Trips index → pattern

	// A search only needs the stops trips call at: a few thousand, where the feed has well over a hundred thousand.
	// It numbers those densely so its working arrays stay small. local maps a stop to its dense number (-1 if no
	// trip calls there) and global maps back; patStops and localFoot are Patterns' stops and Footpaths numbered so.
	local     []int32
	global    []int32
	patStops  [][]int32
	localFoot [][]Footpath
	scratch   sync.Pool // *runScratch, reused between searches
}

// Options control network construction.
type Options struct {
	MaxTransferM float64 // longest walking transfer between stops
	WalkSpeedMps float64
	DetourFactor float64 // straight-line distance multiplier for walking
	IncludeRoute func(r gtfs.Route) bool
	// PlatformAllowance is added to in-station transfers derived from GTFS pathways, which only
	// time the stairs/escalators/corridors, not walking along the platform.
	PlatformAllowance int32
	// Footpaths, if set, replaces the straight-line walking transfers between stops (the candidates within
	// MaxTransferM) with better ones, e.g. timed along real streets, dropping pairs that can't be walked.
	// It gets, for each stop, the straight-line candidates (nil for stops no trip uses) and returns the final list.
	Footpaths func(d *gtfs.Day, used []bool, straight [][]Footpath) [][]Footpath
}

// DefaultOptions are conservative generic defaults; personal values replace them later.
func DefaultOptions() Options {
	return Options{MaxTransferM: 400, WalkSpeedMps: 1.3, DetourFactor: 1.3, PlatformAllowance: 60}
}

// WalkSecs converts a straight-line distance to walking seconds.
func (o Options) WalkSecs(distM float64) int32 {
	return int32(distM * o.DetourFactor / o.WalkSpeedMps)
}

// Build groups trips into patterns and computes walking transfers.
func Build(d *gtfs.Day, o Options) *Network {
	n := &Network{Day: d, stopPatterns: make([][]patRef, len(d.Stops)), Footpaths: make([][]Footpath, len(d.Stops)),
		tripPattern: map[int32]int32{}}
	byKey := map[string]int32{}
	var sb strings.Builder
	for ti, t := range d.Trips {
		if len(t.StopTimes) < 2 {
			continue
		}
		if o.IncludeRoute != nil && !o.IncludeRoute(d.Routes[t.Route]) {
			continue
		}
		sb.Reset()
		sb.WriteString(strconv.Itoa(int(t.Route)))
		for _, st := range t.StopTimes {
			sb.WriteByte(',')
			sb.WriteString(strconv.Itoa(int(st.Stop)))
		}
		k := sb.String()
		pi, ok := byKey[k]
		if !ok {
			pi = int32(len(n.Patterns))
			byKey[k] = pi
			stops := make([]int32, len(t.StopTimes))
			for i, st := range t.StopTimes {
				stops[i] = st.Stop
			}
			n.Patterns = append(n.Patterns, Pattern{Route: t.Route, Stops: stops})
		}
		n.Patterns[pi].Trips = append(n.Patterns[pi].Trips, int32(ti))
		n.tripPattern[int32(ti)] = pi
	}
	used := make([]bool, len(d.Stops))
	for pi := range n.Patterns {
		p := &n.Patterns[pi]
		sort.Slice(p.Trips, func(a, b int) bool {
			return d.Trips[p.Trips[a]].StopTimes[0].Dep < d.Trips[p.Trips[b]].StopTimes[0].Dep
		})
		for i, s := range p.Stops {
			n.stopPatterns[s] = append(n.stopPatterns[s], patRef{pat: int32(pi), idx: int32(i)})
			used[s] = true
		}
		p.fifo = true
		for t := 1; t < len(p.Trips) && p.fifo; t++ {
			a, b := d.Trips[p.Trips[t-1]].StopTimes, d.Trips[p.Trips[t]].StopTimes
			for i := range a {
				if a[i].Dep > b[i].Dep || a[i].Arr > b[i].Arr {
					p.fifo = false
					break
				}
			}
		}
	}
	pts := make([]geo.Point, len(d.Stops))
	for i, s := range d.Stops {
		pts[i] = s.Pos
	}
	grid := geo.NewGrid(pts, 500)
	for s := range d.Stops {
		if !used[s] {
			continue
		}
		grid.Within(d.Stops[s].Pos, o.MaxTransferM, func(j int32, dist float64) {
			if int(j) != s && used[j] {
				n.Footpaths[s] = append(n.Footpaths[s], Footpath{To: j, Secs: o.WalkSecs(dist)})
			}
		})
	}
	if o.Footpaths != nil {
		n.Footpaths = o.Footpaths(d, used, n.Footpaths)
	}
	n.applyPathways(used, o)
	n.numberStops(used)
	return n
}

// numberStops gives the stops trips call at their dense numbers (see Network.local).
func (n *Network) numberStops(used []bool) {
	n.local = make([]int32, len(used))
	for s := range used {
		n.local[s] = -1
		if used[s] {
			n.local[s] = int32(len(n.global))
			n.global = append(n.global, int32(s))
		}
	}
	n.patStops = make([][]int32, len(n.Patterns))
	for pi, p := range n.Patterns {
		ls := make([]int32, len(p.Stops))
		for i, s := range p.Stops {
			ls[i] = n.local[s]
		}
		n.patStops[pi] = ls
	}
	n.localFoot = make([][]Footpath, len(n.global))
	for ls, s := range n.global {
		for _, fp := range n.Footpaths[s] {
			if t := n.local[fp.To]; t >= 0 {
				n.localFoot[ls] = append(n.localFoot[ls], Footpath{To: t, Secs: fp.Secs})
			}
		}
	}
}

// applyPathways replaces straight-line transfers inside stations with times along the station's
// pathways (stairs, escalators, corridors). Lifts are only used when there's no other way, as
// their listed times exclude waiting.
func (n *Network) applyPathways(used []bool, o Options) {
	d := n.Day
	if len(d.Pathways) == 0 {
		return
	}
	type edge struct {
		to   string
		secs int32
	}
	build := func(withLifts bool) map[string][]edge {
		g := map[string][]edge{}
		for _, p := range d.Pathways {
			if p.Mode == gtfs.PathwayElevator && !withLifts {
				continue
			}
			g[p.From] = append(g[p.From], edge{p.To, p.Secs})
			if p.Bidirectional {
				g[p.To] = append(g[p.To], edge{p.From, p.Secs})
			}
		}
		return g
	}
	graphs := []map[string][]edge{build(false), build(true)}
	shortest := func(g map[string][]edge, from string) map[string]int32 {
		dist := map[string]int32{from: 0}
		done := map[string]bool{}
		for {
			cur, best := "", int32(-1)
			for id, dd := range dist { // graphs per station are tiny; a linear scan is fine
				if !done[id] && (best < 0 || dd < best || (dd == best && id < cur)) {
					cur, best = id, dd
				}
			}
			if best < 0 || best > 1800 {
				return dist
			}
			done[cur] = true
			for _, e := range g[cur] {
				if nd := best + e.secs; !done[e.to] {
					if old, ok := dist[e.to]; !ok || nd < old {
						dist[e.to] = nd
					}
				}
			}
		}
	}
	for s := range d.Stops {
		if !used[s] || d.Stops[s].Parent == "" {
			continue
		}
		times := map[int32]int32{}
		for _, g := range graphs {
			if _, ok := g[d.Stops[s].ID]; !ok {
				continue
			}
			for id, secs := range shortest(g, d.Stops[s].ID) {
				t, ok := d.StopIndex[id]
				if !ok || int(t) == s || !used[t] || d.Stops[t].Parent != d.Stops[s].Parent {
					continue
				}
				if _, have := times[t]; !have { // without lifts first; lifts only as a fallback
					times[t] = secs + o.PlatformAllowance
				}
			}
		}
		if len(times) == 0 {
			continue
		}
		fps := n.Footpaths[s][:0]
		for _, fp := range n.Footpaths[s] {
			if _, ok := times[fp.To]; !ok {
				fps = append(fps, fp)
			}
		}
		for t, secs := range times {
			fps = append(fps, Footpath{To: t, Secs: secs})
		}
		sort.Slice(fps, func(a, b int) bool { return fps[a].To < fps[b].To })
		n.Footpaths[s] = fps
	}
}

// StopsNear returns routable stops within radiusM of p with walking times.
func (n *Network) StopsNear(p geo.Point, radiusM float64, o Options) []Access {
	var out []Access
	for s, st := range n.Day.Stops {
		if len(n.stopPatterns[s]) == 0 {
			continue
		}
		if d := geo.DistanceM(p, st.Pos); d <= radiusM {
			out = append(out, Access{Stop: int32(s), Secs: o.WalkSecs(d)})
		}
	}
	return out
}

// Access is a walk between a place and a stop.
type Access struct {
	Stop int32
	Secs int32
}

// Query is a single earliest-arrival search.
type Query struct {
	Depart    int32
	Access    []Access
	Egress    []Access
	MaxRides  int
	MinChange int32 // buffer when changing vehicles at the same stop
	BanRoute  func(route int32) bool
	// Transfer optionally overrides walking/changing time between two stops (personal times).
	// It's consulted for footpaths and for changes at the same stop (from == to).
	Transfer func(from, to int32) (secs int32, ok bool)
}

// LegKind distinguishes walking from riding.
type LegKind uint8

const (
	Walk LegKind = iota
	Ride
)

// Leg is one part of a journey. From/To are stop indexes, or -1 for the origin/destination place.
type Leg struct {
	Kind     LegKind
	From, To int32
	Dep, Arr int32
	Route    int32
	Trip     int32 // index into Day.Trips
	// Ride legs: pattern and call indexes, e.g. to look up the next service.
	Pattern             int32
	BoardIdx, AlightIdx int32
}

// Journey is a door-to-door result.
type Journey struct {
	Legs  []Leg
	Rides int
	Dep   int32
	Arr   int32
}

type labelKind uint8

const (
	none labelKind = iota
	access
	ride
	walk
)

type label struct {
	arr   int32
	kind  labelKind
	round int8
	board int32 // ride: boarding index in pattern
	pat   int32 // ride
	trip  int32 // ride: index into Pattern.Trips
	from  int32 // walk: previous stop; access: walk seconds
}

func (n *Network) tripTimes(p *Pattern, t int32) []gtfs.StopTime {
	return n.Day.Trips[p.Trips[t]].StopTimes
}

// earliestTrip returns the trip of p with the earliest departure from stop index i at or after at,
// or -1. Calls that can't be boarded (gtfs.NoTime) are never chosen.
func (n *Network) earliestTrip(p *Pattern, i int, at int32) int32 {
	if p.fifo {
		lo, hi := 0, len(p.Trips)
		for lo < hi {
			m := (lo + hi) / 2
			if n.tripTimes(p, int32(m))[i].Dep < at {
				lo = m + 1
			} else {
				hi = m
			}
		}
		for ; lo < len(p.Trips); lo++ {
			if n.tripTimes(p, int32(lo))[i].Dep != gtfs.NoTime {
				return int32(lo)
			}
		}
		return -1
	}
	best, bestDep := int32(-1), gtfs.NoTime
	for t := range p.Trips {
		if dep := n.tripTimes(p, int32(t))[i].Dep; dep >= at && dep < bestDep {
			best, bestDep = int32(t), dep
		}
	}
	return best
}

// runScratch is a search's working memory, reused between searches.
type runScratch struct {
	labels     [][]label // per round, per stop (dense numbers)
	best       []int32
	marked     []bool
	markedList []int32
	rideMarked []int32
	queue      []int32 // per pattern: the first call to scan this round, or -1 if the pattern isn't queued
	queued     []int32 // the patterns queued this round
	targetArr  []int32
	targetStop []int32
	// extra are access or egress stops no trip calls at. They take the numbers after the dense ones, so a
	// journey that only walks via such a stop is still found.
	extra []int32
}

func (n *Network) getScratch() *runScratch {
	if sc, ok := n.scratch.Get().(*runScratch); ok {
		return sc
	}
	sc := &runScratch{queue: make([]int32, len(n.Patterns))}
	for i := range sc.queue {
		sc.queue[i] = -1
	}
	return sc
}

// grow returns s with length l, reusing its memory when it is big enough.
func grow[T any](s []T, l int) []T {
	if cap(s) < l {
		return make([]T, l)
	}
	return s[:l]
}

// Run returns the Pareto-optimal journeys (arrival time vs. number of rides). It is safe to call concurrently.
func (n *Network) Run(q Query) []Journey {
	if q.MaxRides <= 0 {
		q.MaxRides = 4
	}
	sc := n.getScratch()
	nd := int32(len(n.global))
	sc.extra = sc.extra[:0]
	dense := func(s int32) int32 {
		if l := n.local[s]; l >= 0 {
			return l
		}
		for i, e := range sc.extra {
			if e == s {
				return nd + int32(i)
			}
		}
		sc.extra = append(sc.extra, s)
		return nd + int32(len(sc.extra)-1)
	}
	for _, a := range q.Access {
		dense(a.Stop)
	}
	for _, e := range q.Egress {
		dense(e.Stop)
	}
	glob := func(s int32) int32 {
		if s < nd {
			return n.global[s]
		}
		return sc.extra[s-nd]
	}
	ns := int(nd) + len(sc.extra)

	sc.labels = grow(sc.labels, q.MaxRides+1)
	labels := sc.labels
	labels[0] = grow(labels[0], ns)
	for i := range labels[0] {
		labels[0][i] = label{arr: inf}
	}
	best := grow(sc.best, ns)
	for i := range best {
		best[i] = inf
	}
	marked := grow(sc.marked, ns)
	clear(marked)
	markedList := sc.markedList[:0]
	mark := func(s int32) {
		if !marked[s] {
			marked[s] = true
			markedList = append(markedList, s)
		}
	}
	for _, a := range q.Access {
		s := dense(a.Stop)
		arr := q.Depart + a.Secs
		if arr < labels[0][s].arr {
			labels[0][s] = label{arr: arr, kind: access, from: a.Secs}
			best[s] = arr
			mark(s)
		}
	}
	egress := map[int32]int32{}
	for _, e := range q.Egress {
		s := dense(e.Stop)
		if cur, ok := egress[s]; !ok || e.Secs < cur {
			egress[s] = e.Secs
		}
	}
	targetArr := grow(sc.targetArr, q.MaxRides+1)
	targetStop := grow(sc.targetStop, q.MaxRides+1)
	bestTarget := inf
	targetArr[0] = inf
	queue, queued, rideMarked := sc.queue, sc.queued[:0], sc.rideMarked[:0]

	rounds := 0
	for k := 1; k <= q.MaxRides; k++ {
		rounds = k
		targetArr[k] = inf
		labels[k] = grow(labels[k], ns)
		copy(labels[k], labels[k-1])
		queued = queued[:0]
		for _, s := range markedList {
			for _, pr := range n.stopPatterns[glob(s)] {
				if q.BanRoute != nil && q.BanRoute(n.Patterns[pr.pat].Route) {
					continue
				}
				if cur := queue[pr.pat]; cur < 0 {
					queue[pr.pat] = pr.idx
					queued = append(queued, pr.pat)
				} else if pr.idx < cur {
					queue[pr.pat] = pr.idx
				}
			}
			marked[s] = false
		}
		markedList = markedList[:0]
		rideMarked = rideMarked[:0]
		// Scan patterns in a fixed order so ties between equal arrivals resolve deterministically.
		slices.Sort(queued)
		for _, pi := range queued {
			start := queue[pi]
			queue[pi] = -1
			p := &n.Patterns[pi]
			stops := n.patStops[pi]
			t, board := int32(-1), int32(-1)
			for i := int(start); i < len(stops); i++ {
				s := stops[i]
				if t >= 0 {
					arr := n.tripTimes(p, t)[i].Arr
					if arr < best[s] && arr < bestTarget {
						labels[k][s] = label{arr: arr, kind: ride, round: int8(k), board: board, pat: pi, trip: t}
						best[s] = arr
						if !marked[s] {
							rideMarked = append(rideMarked, s)
						}
						mark(s)
					}
				}
				prev := labels[k-1][s]
				if prev.arr == inf {
					continue
				}
				ready := prev.arr
				if prev.kind == ride {
					change := q.MinChange
					if q.Transfer != nil {
						if v, ok := q.Transfer(p.Stops[i], p.Stops[i]); ok {
							change = v
						}
					}
					ready += change
				}
				if t < 0 || ready <= n.tripTimes(p, t)[i].Dep {
					if nt := n.earliestTrip(p, i, ready); nt >= 0 &&
						(t < 0 || n.tripTimes(p, nt)[i].Dep < n.tripTimes(p, t)[i].Dep) {
						t, board = nt, int32(i)
					}
				}
			}
		}
		for _, s := range rideMarked {
			from := labels[k][s]
			for _, fp := range n.localFoot[s] {
				secs := fp.Secs
				if q.Transfer != nil {
					if v, ok := q.Transfer(glob(s), glob(fp.To)); ok {
						secs = v
					}
				}
				arr := from.arr + secs
				if arr < best[fp.To] && arr < bestTarget {
					labels[k][fp.To] = label{arr: arr, kind: walk, round: int8(k), from: s}
					best[fp.To] = arr
					mark(fp.To)
				}
			}
		}
		for _, e := range q.Egress {
			s := dense(e.Stop)
			if l := labels[k][s]; l.arr != inf && l.arr+e.Secs < targetArr[k] {
				targetArr[k], targetStop[k] = l.arr+e.Secs, s
			}
		}
		if targetArr[k] < bestTarget {
			bestTarget = targetArr[k]
		}
		if len(markedList) == 0 {
			break
		}
	}

	var out []Journey
	prevBest := inf
	for k := 1; k <= rounds; k++ {
		if targetArr[k] >= prevBest {
			continue
		}
		prevBest = targetArr[k]
		if j, ok := n.reconstruct(labels, k, targetStop[k], egress[targetStop[k]], glob); ok {
			out = append(out, j)
		}
	}
	sc.best, sc.marked, sc.markedList, sc.rideMarked, sc.queued = best, marked, markedList, rideMarked, queued
	sc.targetArr, sc.targetStop = targetArr, targetStop
	n.scratch.Put(sc)
	return out
}

// reconstruct follows the labels back from stop s (a dense number) at round k; glob turns dense numbers back into stops.
func (n *Network) reconstruct(labels [][]label, k int, s int32, egressSecs int32, glob func(int32) int32) (Journey, bool) {
	l := labels[k][s]
	legs := []Leg{{Kind: Walk, From: glob(s), To: -1, Dep: l.arr, Arr: l.arr + egressSecs}}
	r := k
	for steps := 0; steps < 32; steps++ {
		l = labels[r][s]
		switch l.kind {
		case access:
			legs = append(legs, Leg{Kind: Walk, From: -1, To: glob(s), Dep: l.arr - l.from, Arr: l.arr})
			// reverse
			for i, j := 0, len(legs)-1; i < j; i, j = i+1, j-1 {
				legs[i], legs[j] = legs[j], legs[i]
			}
			rides := 0
			for _, lg := range legs {
				if lg.Kind == Ride {
					rides++
				}
			}
			return Journey{Legs: legs, Rides: rides, Dep: legs[0].Dep, Arr: legs[len(legs)-1].Arr}, true
		case ride:
			p := &n.Patterns[l.pat]
			stops := n.patStops[l.pat]
			times := n.tripTimes(p, l.trip)
			alight := l.board
			for i := l.board + 1; i < int32(len(stops)); i++ {
				if stops[i] == s && times[i].Arr == l.arr {
					alight = i
					break
				}
			}
			legs = append(legs, Leg{Kind: Ride, From: p.Stops[l.board], To: glob(s), Dep: times[l.board].Dep, Arr: l.arr,
				Route: p.Route, Trip: p.Trips[l.trip], Pattern: l.pat, BoardIdx: l.board, AlightIdx: alight})
			s, r = stops[l.board], int(l.round)-1
		case walk:
			fromArr := labels[l.round][l.from].arr
			legs = append(legs, Leg{Kind: Walk, From: glob(l.from), To: glob(s), Dep: fromArr, Arr: l.arr})
			s, r = l.from, int(l.round)
		default:
			return Journey{}, false
		}
	}
	return Journey{}, false
}

// NextDeparture returns the first trip of pattern pat (other than skipTrip, a Day.Trips index) that
// departs call index idx strictly after `after`, as a Day.Trips index and departure time.
func (n *Network) NextDeparture(pat, idx, after, skipTrip int32) (trip, dep int32, ok bool) {
	p := &n.Patterns[pat]
	best, bestDep := int32(-1), gtfs.NoTime
	for t, ti := range p.Trips {
		if ti == skipTrip {
			continue
		}
		if d := n.tripTimes(p, int32(t))[idx].Dep; d > after && d < bestDep {
			best, bestDep = ti, d
		}
	}
	return best, bestDep, best >= 0
}

// DeparturesFrom calls fn with every boardable departure time from stop s within [from, to].
func (n *Network) DeparturesFrom(s, from, to int32, ban func(route int32) bool, fn func(dep int32)) {
	for _, pr := range n.stopPatterns[s] {
		p := &n.Patterns[pr.pat]
		if ban != nil && ban(p.Route) || pr.idx == int32(len(p.Stops)-1) {
			continue
		}
		for t := range p.Trips {
			if d := n.tripTimes(p, int32(t))[pr.idx].Dep; d >= from && d <= to {
				fn(d)
			}
		}
	}
}

// RoutesAt returns the routes (Day.Routes indexes) with a departure or arrival at stop s.
func (n *Network) RoutesAt(s int32) []int32 {
	seen := map[int32]bool{}
	var out []int32
	for _, pr := range n.stopPatterns[s] {
		if r := n.Patterns[pr.pat].Route; !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

// PatternOf returns the pattern a trip (Day.Trips index) belongs to; its call indexes match the trip's.
func (n *Network) PatternOf(trip int32) (int32, bool) {
	p, ok := n.tripPattern[trip]
	return p, ok
}
