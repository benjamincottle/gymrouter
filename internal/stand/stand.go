// Package stand works out where to board a train so that you step off beside your way on: which carriage stops
// beside the stairs or escalators the trip takes at the far end, and where on the platform you board from that
// carriage stops. It never aims for a lift.
//
// It draws on three things. The timetable's map of each station (stairs, escalators and walkways between its
// platforms, entrances and the points between) says which way off the trip's way on takes. The street map says
// where trains stop: for most platforms it lists which carriage is beside which way off, and it has each platform's
// outline to measure along where there's no list. And the metro's trains report where they stop.
package stand

import (
	"container/heap"
	_ "embed"
	"encoding/json"
	"math"
	"sort"
	"strings"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/walk"
)

const (
	// A carriage's length: an 8-carriage train is 163 m, a 6-carriage metro 132 m.
	trainCarM = 20.4
	metroCarM = 22.0
	// MetroCars is how long every metro train is; its trip IDs don't say.
	MetroCars = 6
	// The street map's carriage for a way off is believed when it's within this many carriages of where measuring
	// puts that way off: the two differ by one often enough (the timetable marks the top of a staircase, the map
	// where it lands), and by more than two only when the list is for something else.
	listReachCars = 2.5
	// Out of a station, the way on to a stop or place is costed as the crow flies, at this detour and pace.
	detour, paceMps = 1.3, 1.3
)

// Source says what a carriage was worked out from.
type Source string

const (
	ByList     Source = "list"     // the street map's list for the platform
	ByStop     Source = "stop"     // measured from where the metro's trains report stopping
	ByMeasured Source = "measured" // measured along the platform's outline, the train filling it
	ByMiddle   Source = "middle"   // measured, the train taken to stop in the middle of a platform longer than it
)

// Ride is one ride on a train, and how the trip carries on from it.
type Ride struct {
	Board, Alight int32 // the platforms, as indexes into the day's stops
	Metro         bool
	Cars          int     // how many carriages the train has
	HeadingBoard  float64 // which way the train runs at each platform, degrees clockwise from north
	HeadingAlight float64
	// Then is the stop the trip goes on to on foot (another platform, a bus stop), or -1 when it walks to the place.
	// Goal is where that is.
	Then int32
	Goal geo.Point
	// Stay: the next ride leaves from the platform this one arrives at.
	Stay bool
}

// Advice is where to board.
type Advice struct {
	Carriage, Of int // the carriage, counted from the front, and how many the train has
	By           Source
	// Spot is where on the platform you board from that carriage stops, when the platform's outline is known.
	Spot    geo.Point
	HasSpot bool
}

// Cars returns how many carriages a trip's train has: the metro's are all the same, and a Sydney Trains trip ID
// carries its set and length ("839P.1414.159.2.A.8.91434157" is an 8-carriage A set). 0 when it can't be told.
func Cars(tripID string, metro bool) int {
	if metro {
		return MetroCars
	}
	parts := strings.Split(tripID, ".")
	if len(parts) < 7 {
		return 0
	}
	n := 0
	for _, c := range parts[5] {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	if parts[5] == "" || n < 2 || n > 16 {
		return 0
	}
	return n
}

//go:embed metrostops.json
var metroStopsJSON []byte

// metroFront is where the front of a metro train stops at each platform, by the platform's stop ID: the position its
// trains report while stopped, collected from the live feed on 2026-10-12. It's the same train after train, to the
// last digit.
var metroFront = func() map[string]geo.Point {
	var raw map[string][2]float64
	if err := json.Unmarshal(metroStopsJSON, &raw); err != nil {
		panic("stand: metrostops.json: " + err.Error())
	}
	out := make(map[string]geo.Point, len(raw))
	for id, p := range raw {
		out[id] = geo.Point{Lat: p[0], Lon: p[1]}
	}
	return out
}()

// Stations is a day's stations as the timetable maps them, ready to be asked for ways off a platform.
type Stations struct {
	day       *gtfs.Day
	out       map[string][]edge  // the ways from each stop, lifts left out
	lifts     map[string][]int32 // the lifts off each platform that have a position, to line a list up by
	entrances map[string][]int32 // each station's entrances
}

type edge struct {
	to   string
	secs float64
	mode int
}

// NewStations indexes a day's pathways.
func NewStations(d *gtfs.Day) *Stations {
	st := &Stations{day: d, out: map[string][]edge{}, lifts: map[string][]int32{}, entrances: map[string][]int32{}}
	for _, p := range d.Pathways {
		if p.Mode == gtfs.PathwayElevator || st.liftOnly(p.From) || st.liftOnly(p.To) {
			// Never a way on. A lift under another name counts too: Meadowbank's "LFT Railway Rd" is a "walkway".
			for _, pair := range [][2]string{{p.From, p.To}, {p.To, p.From}} {
				if a, ok := d.StopIndex[pair[0]]; ok && platform(d.Stops[a]) {
					if b, ok := d.StopIndex[pair[1]]; ok && !platform(d.Stops[b]) {
						st.lifts[pair[0]] = append(st.lifts[pair[0]], b)
					}
				}
			}
			continue
		}
		st.out[p.From] = append(st.out[p.From], edge{to: p.To, secs: float64(p.Secs), mode: p.Mode})
		if p.Bidirectional {
			st.out[p.To] = append(st.out[p.To], edge{to: p.From, secs: float64(p.Secs), mode: p.Mode})
		}
	}
	for i, s := range d.Stops {
		if s.LocationType == "2" && s.Parent != "" {
			st.entrances[s.Parent] = append(st.entrances[s.Parent], int32(i))
		}
	}
	return st
}

func platform(s gtfs.Stop) bool { return s.LocationType == "" || s.LocationType == "0" }

// isPlatform reports whether a pathway's end is a platform. An end with no position isn't in the day's stops: it's a
// point inside the station.
func (st *Stations) isPlatform(id string) bool {
	i, ok := st.day.StopIndex[id]
	return ok && platform(st.day.Stops[i])
}

// liftOnly reports whether a point in a station is a lift and nothing else, by its name ("LFT To P1", not "STR/LFT
// To P1/2").
func (st *Stations) liftOnly(id string) bool {
	i, ok := st.day.StopIndex[id]
	if !ok {
		return false
	}
	first, _, _ := strings.Cut(strings.ToUpper(st.day.Stops[i].Name), " ")
	return strings.HasPrefix(first, "LFT") && !strings.Contains(first, "/")
}

// wayOff is one way off a platform that isn't a lift.
type wayOff struct {
	stop int32   // the point it leads to
	mode int     // stairs, escalator, walkway…
	cost float64 // seconds to carry on by it
}

// waysOn lists the ways off a platform with what each costs to carry on by, cheapest first; and the platform the
// trip changes to, when it changes inside the station. across reports that that platform is the one across this one
// (or the same one): no stairs to aim for.
func (st *Stations) waysOn(r Ride) (ways []wayOff, across bool) {
	d := st.day
	from := d.Stops[r.Alight]
	target := ""
	if r.Then >= 0 && r.Then != r.Alight && platform(d.Stops[r.Then]) && d.Stops[r.Then].Parent != "" && d.Stops[r.Then].Parent == from.Parent {
		target = d.Stops[r.Then].ID
		for _, e := range st.out[from.ID] {
			if e.to == target {
				return nil, true
			}
		}
	}
	search := func(target string) []wayOff {
		var found []wayOff
		for _, first := range st.out[from.ID] {
			fi, ok := d.StopIndex[first.to]
			if !ok || platform(d.Stops[fi]) {
				continue // a point with no position can't be placed along the platform; another platform isn't a way off
			}
			best := st.from(first.to, first.secs, target)
			cost := math.Inf(1)
			if target != "" {
				if c, ok := best[target]; ok {
					cost = c
				}
			} else {
				for _, e := range st.entrances[from.Parent] {
					if c, ok := best[d.Stops[e].ID]; ok {
						cost = math.Min(cost, c+geo.DistanceM(d.Stops[e].Pos, r.Goal)*detour/paceMps)
					}
				}
				if math.IsInf(cost, 1) { // the station's pathways don't lead out from here: as the crow flies
					cost = first.secs + geo.DistanceM(d.Stops[fi].Pos, r.Goal)*detour/paceMps
				}
			}
			if !math.IsInf(cost, 1) {
				found = append(found, wayOff{stop: fi, mode: first.mode, cost: cost})
			}
		}
		return found
	}
	ways = search(target)
	if target != "" && len(ways) == 0 {
		// A stop that belongs to the station but isn't on its pathways (a bus stand): head for it on foot.
		ways = search("")
	}
	sort.SliceStable(ways, func(i, j int) bool { return ways[i].cost < ways[j].cost })
	return ways, false
}

// from is the quickest time to every point of a station from one of them, not passing through a platform other than
// the one aimed for.
func (st *Stations) from(start string, secs float64, target string) map[string]float64 {
	best := map[string]float64{start: secs}
	h := &queue{{start, secs}}
	for h.Len() > 0 {
		cur := heap.Pop(h).(item)
		if cur.d > best[cur.id] {
			continue
		}
		if cur.id == target {
			break
		}
		for _, e := range st.out[cur.id] {
			if e.to != target && st.isPlatform(e.to) {
				continue
			}
			if nd := cur.d + e.secs; nd < get(best, e.to) {
				best[e.to] = nd
				heap.Push(h, item{e.to, nd})
			}
		}
	}
	return best
}

func get(m map[string]float64, k string) float64 {
	if v, ok := m[k]; ok {
		return v
	}
	return math.Inf(1)
}

type item struct {
	id string
	d  float64
}
type queue []item

func (q queue) Len() int           { return len(q) }
func (q queue) Less(i, j int) bool { return q[i].d < q[j].d }
func (q queue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *queue) Push(x any)        { *q = append(*q, x.(item)) }
func (q *queue) Pop() any {
	old := *q
	x := old[len(old)-1]
	*q = old[:len(old)-1]
	return x
}

// frame lays a platform's outline along its own length, with the end the train leaves by as the front.
type frame struct {
	origin         geo.Point
	k              float64 // metres per degree of longitude there
	mx, my, ux, uy float64
	hi, length     float64
}

const mPerDegLat = 110540.0

func newFrame(outline []geo.Point, heading float64) frame {
	f := frame{origin: outline[0]}
	f.k = math.Cos(f.origin.Lat*math.Pi/180) * 111320
	n := float64(len(outline))
	xs, ys := make([]float64, len(outline)), make([]float64, len(outline))
	for i, p := range outline {
		xs[i], ys[i] = f.xy(p)
		f.mx += xs[i] / n
		f.my += ys[i] / n
	}
	var sxx, syy, sxy float64
	for i := range xs {
		sxx += (xs[i] - f.mx) * (xs[i] - f.mx)
		syy += (ys[i] - f.my) * (ys[i] - f.my)
		sxy += (xs[i] - f.mx) * (ys[i] - f.my)
	}
	th := 0.5 * math.Atan2(2*sxy, sxx-syy) // the outline's long axis
	f.ux, f.uy = math.Cos(th), math.Sin(th)
	if h := heading * math.Pi / 180; f.ux*math.Sin(h)+f.uy*math.Cos(h) < 0 {
		f.ux, f.uy = -f.ux, -f.uy // pointing the way the train runs
	}
	lo := math.Inf(1)
	f.hi = math.Inf(-1)
	for i := range xs {
		a := (xs[i]-f.mx)*f.ux + (ys[i]-f.my)*f.uy
		lo, f.hi = math.Min(lo, a), math.Max(f.hi, a)
	}
	f.length = f.hi - lo
	return f
}

func (f frame) xy(p geo.Point) (x, y float64) {
	return (p.Lon - f.origin.Lon) * f.k, (p.Lat - f.origin.Lat) * mPerDegLat
}

// fromFront is how far back from the platform's front end a point is, along the platform.
func (f frame) fromFront(p geo.Point) float64 {
	x, y := f.xy(p)
	return f.hi - ((x-f.mx)*f.ux + (y-f.my)*f.uy)
}

// at is the point on the platform's long axis that far back from its front end.
func (f frame) at(fromFront float64) geo.Point {
	a := f.hi - fromFront
	x, y := f.mx+a*f.ux, f.my+a*f.uy
	return geo.Point{Lat: f.origin.Lat + y/mPerDegLat, Lon: f.origin.Lon + x/f.k}
}

// kinds are the words the street map's lists use for each kind of way off.
func kinds(mode int) []string {
	switch mode {
	case gtfs.PathwayStairs:
		return []string{"stairs", "steps"}
	case gtfs.PathwayEscalator:
		return []string{"escalator"}
	case gtfs.PathwayElevator:
		return []string{"lift"}
	default: // a way off on the level: a ramp, the end of a terminal platform
		return []string{"flat", "ramp", "yes", "exit"}
	}
}

// inList finds the carriage a list gives for a kind of way off, nearest to where measuring puts it (est, as a
// carriage number: 1.0 is the middle of the 1st, 1.5 the join with the 2nd), and how many carriages away that is.
func inList(list []string, mode int, est float64) (carriage int, away float64, ok bool) {
	best, bestD := 0, math.Inf(1)
	for i, entry := range list {
		for _, word := range strings.Split(entry, ";") {
			for _, k := range kinds(mode) {
				if strings.TrimSpace(word) == k {
					if d := math.Abs(float64(i+1) - est); d < bestD {
						best, bestD = i+1, d
					}
				}
			}
		}
	}
	return best, bestD, bestD <= listReachCars
}

// trainAt says how far back from a platform's front end the front of a train stops there, and what that rests on:
// where the metro's trains report stopping, else the middle of the room the train has.
func trainAt(stopID string, f frame, r Ride, carM float64) (float64, Source) {
	if r.Metro {
		if p, ok := metroFront[stopID]; ok {
			return f.fromFront(p), ByStop
		}
	}
	slack := f.length - float64(r.Cars)*carM
	if slack > carM/2 {
		return slack / 2, ByMiddle
	}
	return slack / 2, ByMeasured
}

// Advise works out where to board for a ride. ok is false when there's nothing to say (the next train leaves from
// the same platform or the one across it) or nothing known (no outline for the platform, no way off that isn't a
// lift, a train of unknown length).
func (st *Stations) Advise(g *walk.Graph, r Ride) (Advice, bool) {
	d := st.day
	if g == nil || r.Stay || r.Cars < 2 || r.Board < 0 || r.Alight < 0 {
		return Advice{}, false
	}
	carM := trainCarM
	if r.Metro {
		carM = metroCarM
	}
	to := d.Stops[r.Alight]
	outline, ok := g.PlatformAt(to.Pos, to.Platform)
	if !ok || len(outline) < 2 {
		return Advice{}, false
	}
	ways, across := st.waysOn(r)
	if across || len(ways) == 0 {
		return Advice{}, false
	}
	f := newFrame(outline, r.HeadingAlight)
	way := ways[0]
	x := math.Max(0, math.Min(f.length, f.fromFront(d.Stops[way.stop].Pos)))
	front, by := trainAt(to.ID, f, r, carM)
	clampCar := func(c int) int { return max(1, min(r.Cars, c)) }
	adv := Advice{Carriage: clampCar(int(math.Floor((x-front)/carM)) + 1), Of: r.Cars, By: by}
	if ts, ok := g.TrainStopFor(to.ID); ok {
		if list := ts.Carriages(r.HeadingAlight); len(list) == r.Cars {
			// The timetable can give one point two ways up that cost the same (stairs beside an escalator), which the
			// list puts beside different carriages: of those, the one it puts nearest the point.
			nearest := math.Inf(1)
			for _, w := range ways {
				if w.stop != way.stop || w.cost > way.cost {
					continue
				}
				if c, away, ok := inList(list, w.mode, (x-front)/carM+0.5); ok && away < nearest {
					adv.Carriage, adv.By, nearest = c, ByList, away
				}
			}
		}
	}

	// Where that carriage stops on the platform you board from.
	fromStop := d.Stops[r.Board]
	bo, ok := g.PlatformAt(fromStop.Pos, fromStop.Platform)
	if !ok || len(bo) < 2 {
		return adv, true
	}
	bf := newFrame(bo, r.HeadingBoard)
	bfront, bby := trainAt(fromStop.ID, bf, r, carM)
	if bby != ByStop {
		if ts, ok := g.TrainStopFor(fromStop.ID); ok {
			if list := ts.Carriages(r.HeadingBoard); len(list) == r.Cars {
				// Each way off the list places says where the train stands: average them.
				var sum float64
				n := 0
				place := func(stop int32, mode int) {
					bx := bf.fromFront(d.Stops[stop].Pos)
					if c, _, ok := inList(list, mode, (bx-bfront)/carM+0.5); ok {
						sum += bx - (float64(c)-0.5)*carM
						n++
					}
				}
				for _, e := range st.out[fromStop.ID] {
					if i, ok := d.StopIndex[e.to]; ok && !platform(d.Stops[i]) {
						place(i, e.mode)
					}
				}
				for _, i := range st.lifts[fromStop.ID] {
					place(i, gtfs.PathwayElevator)
				}
				if n > 0 {
					bfront = sum / float64(n)
				}
			}
		}
	}
	along := math.Max(0, math.Min(bf.length, bfront+(float64(adv.Carriage)-0.5)*carM))
	adv.Spot, adv.HasSpot = bf.at(along), true
	return adv, true
}
