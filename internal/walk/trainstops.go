package walk

import (
	"math"
	"sort"
	"strings"

	"github.com/benjamincottle/gymrouter/internal/geo"
)

// TrainStop is where trains stop at a platform, as the street map marks it on the track, with the map's record of
// which carriage stops beside which way off the platform.
type TrainStop struct {
	Ref string    // the platform's stop ID in the timetable
	Pos geo.Point // on the track
	// Bearing is the way the track is drawn through the stop, in degrees clockwise from north. The lists are written
	// against it: Forward for a train running that way, Backward for one running the other.
	Bearing float64
	// Forward and Backward have an entry a carriage, the front carriage first: "no", "stairs", "lift;escalator".
	// Backward is nil unless the map gives the other direction a list of its own.
	Forward, Backward []string
}

// Carriages is the stop's list for a train heading the given way (degrees clockwise from north), front carriage
// first: its own list for that direction, or the other direction's turned round.
func (s TrainStop) Carriages(heading float64) []string {
	with := math.Cos((heading-s.Bearing)*math.Pi/180) > 0
	own, other := s.Forward, s.Backward
	if !with {
		own, other = s.Backward, s.Forward
	}
	if own != nil {
		return own
	}
	out := make([]string, len(other))
	for i, v := range other {
		out[len(other)-1-i] = v
	}
	return out
}

// TrainStopFor returns the stop the map marks for a platform, by the platform's stop ID in the timetable.
func (g *Graph) TrainStopFor(ref string) (TrainStop, bool) {
	i, ok := g.stopByRef[ref]
	if !ok {
		return TrainStop{}, false
	}
	return g.TrainStops[i], true
}

func (g *Graph) indexTrainStops() {
	g.stopByRef = make(map[string]int, len(g.TrainStops))
	for i, s := range g.TrainStops {
		if _, dup := g.stopByRef[s.Ref]; !dup {
			g.stopByRef[s.Ref] = i
		}
	}
}

// Bounds on a stop's lists: on what is kept of the map's, and on what Load will allocate for a corrupt file's.
const (
	maxTrainStops    = 1 << 14
	maxCarriages     = 32
	maxCarriageEntry = 64
)

// trainStops gathers an extract's stopping points and the tracks they're on. A stop's tags say which carriage is
// beside which way off, written along the direction its track is drawn in, so the track is needed too: which is why
// every railway track's nodes are kept until the nodes have been read.
type trainStops struct {
	tracks [][]int64 // each railway track's nodes
	ids    []int64   // every track's nodes, sorted (after prepare)
	pos    []geo.Point
	have   []bool
	found  []foundStop
}

type foundStop struct {
	id                int64
	ref               string
	forward, backward []string
}

// track keeps a way if trains run on it.
func (ts *trainStops) track(tags map[string]string, refs []int64) {
	switch tags["railway"] {
	case "rail", "subway", "light_rail", "narrow_gauge":
		if len(refs) >= 2 {
			ts.tracks = append(ts.tracks, append([]int64(nil), refs...))
		}
	}
}

func (ts *trainStops) prepare() {
	for _, t := range ts.tracks {
		ts.ids = append(ts.ids, t...)
	}
	sort.Slice(ts.ids, func(i, j int) bool { return ts.ids[i] < ts.ids[j] })
	out := ts.ids[:0]
	for i, id := range ts.ids {
		if i == 0 || id != ts.ids[i-1] {
			out = append(out, id)
		}
	}
	ts.ids = out
	ts.pos, ts.have = make([]geo.Point, len(out)), make([]bool, len(out))
}

func (ts *trainStops) at(id int64) (int, bool) {
	if len(ts.ids) == 0 || id < ts.ids[0] || id > ts.ids[len(ts.ids)-1] {
		return 0, false
	}
	i := sort.Search(len(ts.ids), func(i int) bool { return ts.ids[i] >= id })
	return i, ts.ids[i] == id
}

// node places a track's node, and notes it if it's a stopping point with a list of carriages.
func (ts *trainStops) node(id int64, lat, lon float64, tags map[string]string) {
	i, ok := ts.at(id)
	if !ok {
		return
	}
	ts.pos[i], ts.have[i] = geo.Point{Lat: float64(toFixed(lat)) / degScale, Lon: float64(toFixed(lon)) / degScale}, true
	if tags == nil || (tags["railway"] != "stop" && tags["public_transport"] != "stop_position") || tags["ref"] == "" || len(ts.found) >= maxTrainStops {
		return
	}
	fwd := carriageList(tags["exit:carriages:forward"])
	if fwd == nil {
		fwd = carriageList(tags["exit:carriages"])
	}
	back := carriageList(tags["exit:carriages:backward"])
	if fwd == nil && back == nil {
		return
	}
	ts.found = append(ts.found, foundStop{id: id, ref: tags["ref"], forward: fwd, backward: back})
}

// carriageList splits a tag such as "no|no|lift|steps" into its carriages; nil for none, or one too long to be real.
func carriageList(v string) []string {
	if v == "" {
		return nil
	}
	out := strings.Split(v, "|")
	if len(out) > maxCarriages {
		return nil
	}
	for i, e := range out {
		e = strings.TrimSpace(e)
		out[i] = e[:min(len(e), maxCarriageEntry)]
	}
	return out
}

// stops returns the stopping points whose track could be followed either side of them.
func (ts *trainStops) stops() []TrainStop {
	if len(ts.found) == 0 {
		return nil
	}
	want := make(map[int64]int, len(ts.found)) // a stop's node -> its place in found
	for i, f := range ts.found {
		want[f.id] = i
	}
	bearing := make([]float64, len(ts.found))
	placed := make([]bool, len(ts.found))
	for _, t := range ts.tracks {
		for k, id := range t {
			f, ok := want[id]
			if !ok || placed[f] {
				continue
			}
			// From the node before it to the node after, or as much of that as the track has and the area holds.
			a, b := k, k
			if p, ok := ts.at(t[max(k-1, 0)]); ok && ts.have[p] {
				a = max(k-1, 0)
			}
			if p, ok := ts.at(t[min(k+1, len(t)-1)]); ok && ts.have[p] {
				b = min(k+1, len(t)-1)
			}
			if a == b {
				continue
			}
			pa, _ := ts.at(t[a])
			pb, _ := ts.at(t[b])
			// To the precision the cache file keeps, so a stop is the same before and after a restart.
			bearing[f], placed[f] = math.Round(bearingDeg(ts.pos[pa], ts.pos[pb])*1000)/1000, true
		}
	}
	var out []TrainStop
	for i, f := range ts.found {
		if !placed[i] {
			continue
		}
		p, _ := ts.at(f.id)
		out = append(out, TrainStop{Ref: f.ref, Pos: ts.pos[p], Bearing: bearing[i], Forward: f.forward, Backward: f.backward})
	}
	return out
}

// bearingDeg is the direction from a to b, in degrees clockwise from north.
func bearingDeg(a, b geo.Point) float64 {
	x := (b.Lon - a.Lon) * math.Cos(a.Lat*math.Pi/180)
	y := b.Lat - a.Lat
	return math.Mod(math.Atan2(x, y)*180/math.Pi+360, 360)
}
