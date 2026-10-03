// Package raptor implements round-based public transit routing (RAPTOR) over one service day.
package raptor

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/gtfs"
)

const inf = int32(math.MaxInt32)

// Pattern is a set of trips of one route that call at exactly the same stops in the same order.
type Pattern struct {
	Route int32
	Stops []int32
	Trips []int32 // indexes into Day.Trips, sorted by departure from the first stop
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
}

// Options control network construction.
type Options struct {
	MaxTransferM float64 // longest walking transfer between stops
	WalkSpeedMps float64
	DetourFactor float64 // straight-line distance multiplier for walking
	IncludeRoute func(r gtfs.Route) bool
}

// DefaultOptions are conservative generic defaults; personal values replace them later.
func DefaultOptions() Options {
	return Options{MaxTransferM: 400, WalkSpeedMps: 1.3, DetourFactor: 1.3}
}

// WalkSecs converts a straight-line distance to walking seconds.
func (o Options) WalkSecs(distM float64) int32 {
	return int32(distM * o.DetourFactor / o.WalkSpeedMps)
}

// Build groups trips into patterns and computes walking transfers.
func Build(d *gtfs.Day, o Options) *Network {
	n := &Network{Day: d, stopPatterns: make([][]patRef, len(d.Stops)), Footpaths: make([][]Footpath, len(d.Stops))}
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
	return n
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
	Trip     int32
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

// earliestTrip returns the first trip of p departing stop index i at or after at, or -1.
func (n *Network) earliestTrip(p *Pattern, i int, at int32) int32 {
	lo, hi := 0, len(p.Trips)
	for lo < hi {
		m := (lo + hi) / 2
		if n.tripTimes(p, int32(m))[i].Dep < at {
			lo = m + 1
		} else {
			hi = m
		}
	}
	if lo == len(p.Trips) {
		return -1
	}
	return int32(lo)
}

// Run returns the Pareto-optimal journeys (arrival time vs. number of rides).
func (n *Network) Run(q Query) []Journey {
	ns := len(n.Day.Stops)
	if q.MaxRides <= 0 {
		q.MaxRides = 4
	}
	labels := make([][]label, q.MaxRides+1)
	labels[0] = make([]label, ns)
	for i := range labels[0] {
		labels[0][i].arr = inf
	}
	best := make([]int32, ns)
	for i := range best {
		best[i] = inf
	}
	marked := make([]bool, ns)
	var markedList []int32
	mark := func(s int32) {
		if !marked[s] {
			marked[s] = true
			markedList = append(markedList, s)
		}
	}
	for _, a := range q.Access {
		arr := q.Depart + a.Secs
		if arr < labels[0][a.Stop].arr {
			labels[0][a.Stop] = label{arr: arr, kind: access, from: a.Secs}
			best[a.Stop] = arr
			mark(a.Stop)
		}
	}
	egress := map[int32]int32{}
	for _, e := range q.Egress {
		egress[e.Stop] = e.Secs
	}
	targetArr := make([]int32, q.MaxRides+1)
	targetStop := make([]int32, q.MaxRides+1)
	bestTarget := inf
	targetArr[0] = inf

	for k := 1; k <= q.MaxRides; k++ {
		targetArr[k] = inf
		labels[k] = make([]label, ns)
		copy(labels[k], labels[k-1])
		queue := map[int32]int32{}
		for _, s := range markedList {
			for _, pr := range n.stopPatterns[s] {
				if q.BanRoute != nil && q.BanRoute(n.Patterns[pr.pat].Route) {
					continue
				}
				if cur, ok := queue[pr.pat]; !ok || pr.idx < cur {
					queue[pr.pat] = pr.idx
				}
			}
			marked[s] = false
		}
		markedList = markedList[:0]
		var rideMarked []int32
		for pi, start := range queue {
			p := &n.Patterns[pi]
			t, board := int32(-1), int32(-1)
			for i := int(start); i < len(p.Stops); i++ {
				s := p.Stops[i]
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
					ready += q.MinChange
				}
				if t < 0 || ready <= n.tripTimes(p, t)[i].Dep {
					if nt := n.earliestTrip(p, i, ready); nt >= 0 && (t < 0 || nt < t) {
						t, board = nt, int32(i)
					}
				}
			}
		}
		for _, s := range rideMarked {
			from := labels[k][s]
			for _, fp := range n.Footpaths[s] {
				arr := from.arr + fp.Secs
				if arr < best[fp.To] && arr < bestTarget {
					labels[k][fp.To] = label{arr: arr, kind: walk, round: int8(k), from: s}
					best[fp.To] = arr
					mark(fp.To)
				}
			}
		}
		for s, secs := range egress {
			if l := labels[k][s]; l.arr != inf && l.arr+secs < targetArr[k] {
				targetArr[k], targetStop[k] = l.arr+secs, s
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
	for k := 1; k <= q.MaxRides && labels[k] != nil; k++ {
		if targetArr[k] >= prevBest {
			continue
		}
		prevBest = targetArr[k]
		if j, ok := n.reconstruct(labels, k, targetStop[k], egress[targetStop[k]], q.Depart); ok {
			out = append(out, j)
		}
	}
	return out
}

func (n *Network) reconstruct(labels [][]label, k int, s int32, egressSecs int32, depart int32) (Journey, bool) {
	l := labels[k][s]
	legs := []Leg{{Kind: Walk, From: s, To: -1, Dep: l.arr, Arr: l.arr + egressSecs}}
	r := k
	for steps := 0; steps < 32; steps++ {
		l = labels[r][s]
		switch l.kind {
		case access:
			legs = append(legs, Leg{Kind: Walk, From: -1, To: s, Dep: l.arr - l.from, Arr: l.arr})
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
			b := p.Stops[l.board]
			times := n.tripTimes(p, l.trip)
			legs = append(legs, Leg{Kind: Ride, From: b, To: s, Dep: times[l.board].Dep, Arr: l.arr,
				Route: p.Route, Trip: p.Trips[l.trip]})
			s, r = b, int(l.round)-1
		case walk:
			fromArr := labels[l.round][l.from].arr
			legs = append(legs, Leg{Kind: Walk, From: l.from, To: s, Dep: fromArr, Arr: l.arr})
			s, r = l.from, int(l.round)
		default:
			return Journey{}, false
		}
	}
	return Journey{}, false
}
