// Package suggest finds the lines worth considering between two places: it searches the whole network
// at many departure times and reports which lines show up in the best options, and how often.
// The result is a starting point for a person to review, not a final answer.
package suggest

import (
	"fmt"
	"sort"
	"strings"

	"github.com/benjamincottle/gymrouter/internal/lines"
	"github.com/benjamincottle/gymrouter/internal/plan"
	"github.com/benjamincottle/gymrouter/internal/raptor"
)

// Window is a run of departure times on one service day.
type Window struct {
	Label            string
	Start, End, Step int32 // seconds after midnight
}

// Departures is how many departure times the window covers.
func (w Window) Departures() int {
	if w.Step <= 0 || w.End < w.Start {
		return 0
	}
	return int((w.End-w.Start)/w.Step) + 1
}

// Params tunes the search.
type Params struct {
	MaxRides  int
	MinChange int32 // buffer when changing vehicles at the same stop
	Slack     int32 // keep alternatives arriving within this of the best
	MaxAlts   int
}

// DefaultParams are the values used by the app and the command line.
func DefaultParams() Params {
	return Params{MaxRides: 4, MinChange: 60, Slack: 15 * 60, MaxAlts: 40}
}

// Itinerary is a distinct way of getting there: the same lines through the same interchange stations.
type Itinerary struct {
	Desc    string   `json:"desc"`
	Lines   []string `json:"lines"`
	MedianS int32    `json:"median_s"` // door to door, leaving just in time
	BestS   int32    `json:"best_s"`
	Seen    int      `json:"seen"` // departure times at which it was among the options
	Of      int      `json:"of"`   // departure times searched
	Window  string   `json:"window"`
	Via     string   `json:"via,omitempty"` // the most common first stop and last stop
}

// WindowResult is everything found in one window.
type WindowResult struct {
	Window      Window
	Departures  int
	Itineraries []Itinerary
	// LineSeen counts, per line, the departure times at which it appeared in some option.
	LineSeen  map[lines.Key]int
	LineColor map[lines.Key]string
}

// Run searches one window over a network.
func Run(net *raptor.Network, access, egress []raptor.Access, w Window, p Params) WindowResult {
	type acc struct {
		it   Itinerary
		durs []int32
		ends map[string]int
	}
	byDesc := map[string]*acc{}
	res := WindowResult{Window: w, LineSeen: map[lines.Key]int{}, LineColor: map[lines.Key]string{}}
	for dep := w.Start; dep <= w.End; dep += w.Step {
		res.Departures++
		found := plan.Alternatives(net, raptor.Query{
			Depart: dep, Access: access, Egress: egress, MaxRides: p.MaxRides, MinChange: p.MinChange,
		}, p.Slack, p.MaxAlts)
		atThisTime := map[lines.Key]bool{}
		for _, j := range found {
			desc, ks := describe(net, j)
			a := byDesc[desc]
			if a == nil {
				a = &acc{it: Itinerary{Desc: desc, Window: w.Label}, ends: map[string]int{}}
				for _, k := range ks {
					a.it.Lines = append(a.it.Lines, k.String())
				}
				byDesc[desc] = a
			}
			a.durs = append(a.durs, doorToDoor(j))
			a.ends[endpoints(net, j)]++
			for _, k := range ks {
				atThisTime[k] = true
			}
			for _, l := range j.Legs {
				if l.Kind == raptor.Ride {
					r := net.Day.Routes[l.Route]
					if k := lines.Of(r.Type, r.ShortName); k.Mode != lines.ReplacementBus {
						res.LineColor[k] = r.Color
					}
				}
			}
		}
		for k := range atThisTime {
			res.LineSeen[k]++
		}
		if w.Step <= 0 {
			break
		}
	}
	for _, a := range byDesc {
		a.it.MedianS, a.it.BestS = median(a.durs), minOf(a.durs)
		a.it.Seen, a.it.Of = len(a.durs), res.Departures
		a.it.Via = topKey(a.ends)
		res.Itineraries = append(res.Itineraries, a.it)
	}
	sort.Slice(res.Itineraries, func(a, b int) bool {
		x, y := res.Itineraries[a], res.Itineraries[b]
		if x.MedianS != y.MedianS {
			return x.MedianS < y.MedianS
		}
		if x.Seen != y.Seen {
			return x.Seen > y.Seen
		}
		return x.Desc < y.Desc
	})
	return res
}

// LineScore says how much a line matters.
type LineScore struct {
	Line  lines.Key
	Color string
	// Share is the largest fraction, over the windows, of departure times at which the line appeared in an option.
	Share       float64
	Recommended bool
}

// RecommendShare is the share a line needs to be pre-selected: it features in the best options for a good
// part of the day, not just now and then. Lines below it are still listed, unticked.
const RecommendShare = 0.3

// Scores combines windows into one ranked list of lines, most useful first.
func Scores(ws []WindowResult) []LineScore {
	best := map[lines.Key]float64{}
	color := map[lines.Key]string{}
	for _, w := range ws {
		if w.Departures == 0 {
			continue
		}
		for k, n := range w.LineSeen {
			if s := float64(n) / float64(w.Departures); s > best[k] {
				best[k] = s
			}
			if c := w.LineColor[k]; c != "" {
				color[k] = c
			}
		}
	}
	out := make([]LineScore, 0, len(best))
	for k, s := range best {
		out = append(out, LineScore{Line: k, Color: color[k], Share: s, Recommended: s >= RecommendShare})
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Share != out[b].Share {
			return out[a].Share > out[b].Share
		}
		return out[a].Line.String() < out[b].Line.String()
	})
	return out
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

// describe groups a journey by its lines and interchange stations, ignoring which exact stop is
// used at either end (those differ only by a few minutes of walking). The keys are the lines to suggest.
func describe(net *raptor.Network, j raptor.Journey) (string, []lines.Key) {
	var rides []raptor.Leg
	for _, l := range j.Legs {
		if l.Kind == raptor.Ride {
			rides = append(rides, l)
		}
	}
	var b strings.Builder
	var ks []lines.Key
	for i, l := range rides {
		r := net.Day.Routes[l.Route]
		k := lines.Of(r.Type, r.ShortName)
		if k.Mode == lines.ReplacementBus {
			// Suggestions are lines to keep: a trackwork bus counts for the line it stands in for, and other
			// replacement buses (event shuttles) for nothing.
			if l, ok := k.Replaces(); ok {
				ks = append(ks, l)
			}
		} else {
			ks = append(ks, k)
		}
		if i == 0 {
			b.WriteString(k.String())
		} else {
			fmt.Fprintf(&b, ", %s", k)
		}
		if i < len(rides)-1 {
			// In words: "metro M1, change at North Ryde Station (walk to Epping Rd At Rivett Rd), bus 533"
			fmt.Fprintf(&b, ", change at %s", stationName(net, l.To))
			if next := stationName(net, rides[i+1].From); next != stationName(net, l.To) {
				fmt.Fprintf(&b, " (walk to %s)", next)
			}
		}
	}
	return b.String(), ks
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
