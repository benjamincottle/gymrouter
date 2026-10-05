package engine

import (
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/raptor"
	"github.com/benjamincottle/gymrouter/internal/walk"
)

// walkLimitFactor bounds a walk along the streets at this many times the straight line, for searches that start from
// a straight-line distance: the streets can be much longer around a barrier, but not without end.
const walkLimitFactor = 2.5

const walkFile = "walk.graph"

// Walker returns the pedestrian street network, or nil until it has been built (first start).
func (e *Engine) Walker() *walk.Graph { return e.walker.Load() }

// SetWalker starts using a street network and rebuilds the timetable so transfers between stops use it too.
func (e *Engine) SetWalker(g *walk.Graph) {
	e.walker.Store(g)
	e.foot.reset()
	e.cacheMu.Lock()
	e.cache = map[string]*Snapshot{}
	e.generation++
	e.cacheMu.Unlock()
	if e.today.Load() != nil {
		if err := e.reloadToday(); err != nil {
			e.log.Error("rebuilding the timetable with the street network failed", "err", err)
		}
	}
}

// loadWalkCache reads a previously built street network from the data directory, if there is one.
func (e *Engine) loadWalkCache() {
	g, err := walk.Load(filepath.Join(e.cfg.Server.DataDir, walkFile))
	switch {
	case err == nil:
		e.walker.Store(g) // before the first timetable load, which then uses it for transfers
		e.log.Info("street network loaded", "nodes", g.Nodes(), "edges", g.Edges())
	case !os.IsNotExist(err):
		e.log.Warn("street network cache unusable; will rebuild", "err", err)
	}
}

const (
	// BandM widens the longest walk: stops up to this much further than the nearest are used too, so the router
	// still has a choice when the nearest stop is a poor one or beyond the longest walk.
	BandM = 500
	// BandCapM is as far as a place looks for stops (straight line). Past it a place has no stops.
	BandCapM = 3000
)

// nearby is the outcome of picking the positions a place walks to.
type nearby struct {
	idx       []int     // the candidates kept
	metres    []float64 // the walk to each: along the streets, or a straight line
	streets   bool
	reach     *walk.Reach
	stretched float64 // the walk to the nearest when even that is beyond the longest walk, else 0
}

// secs is the walking time to the kth kept candidate.
func (n nearby) secs(k int, o raptor.Options) int32 {
	if n.streets {
		return int32(n.metres[k] / o.WalkSpeedMps)
	}
	return o.WalkSecs(n.metres[k])
}

// nearby picks which of the candidate positions a place walks to: every one within maxWalkM, or within BandM of the
// nearest, whichever reaches further, and none past BandCapM in a straight line. With the street network the walks
// are measured along the streets (positions that can't be walked to are dropped); a place off the map, or no network
// yet, falls back to straight lines.
func (e *Engine) nearby(p geo.Point, cand []geo.Point, maxWalkM float64) nearby {
	metres := make([]float64, len(cand))
	var in []geo.Point
	for i, q := range cand {
		if metres[i] = geo.DistanceM(p, q); metres[i] <= BandCapM {
			in = append(in, q)
		} else {
			metres[i] = math.Inf(1)
		}
	}
	out := nearby{}
	if g := e.Walker(); g != nil && len(in) > 0 {
		if reach, ok := g.FromNearest(p, in, BandM, maxWalkM, BandCapM*walkLimitFactor); ok {
			out = nearby{streets: true, reach: reach}
			for i, q := range cand {
				if m, ok := reach.Metres(q); ok && !math.IsInf(metres[i], 1) {
					metres[i] = m
				} else {
					metres[i] = math.Inf(1)
				}
			}
		}
	}
	best := math.Inf(1)
	for _, m := range metres {
		best = min(best, m)
	}
	if math.IsInf(best, 1) {
		return out
	}
	limit := max(maxWalkM, best+BandM)
	for i, m := range metres {
		if m <= limit {
			out.idx, out.metres = append(out.idx, i), append(out.metres, m)
		}
	}
	if best > maxWalkM {
		out.stretched = best
	}
	return out
}

// Approach is how a place connects to nearby stops: the stops with their walking times, and (when the street
// network is available) the routes themselves for drawing.
type Approach struct {
	Access  []raptor.Access
	Streets bool // times come from the street network, not a straight-line guess
	// StretchedM is the walk to the nearest stop when even that is beyond the longest walk, else 0.
	StretchedM float64
	reach      *walk.Reach
	net        *raptor.Network
}

// Approach finds the stops a place walks to (see nearby) and how long each walk takes.
func (e *Engine) Approach(net *raptor.Network, p geo.Point, maxWalkM float64, o raptor.Options) Approach {
	cand := net.StopsNear(p, max(maxWalkM, BandCapM), o)
	pos := make([]geo.Point, len(cand))
	for i, a := range cand {
		pos[i] = net.Day.Stops[a.Stop].Pos
	}
	nb := e.nearby(p, pos, maxWalkM)
	out := make([]raptor.Access, len(nb.idx))
	for k, i := range nb.idx {
		out[k] = raptor.Access{Stop: cand[i].Stop, Secs: nb.secs(k, o)}
	}
	return Approach{Access: out, Streets: nb.streets, StretchedM: nb.stretched, reach: nb.reach, net: net}
}

// Path returns the walk between the place and a stop along the streets, if known.
func (a Approach) Path(stop int32) ([]geo.Point, bool) {
	if a.reach == nil || stop < 0 {
		return nil, false
	}
	return a.reach.Path(a.net.Day.Stops[stop].Pos)
}

// NearbyStops lists the catalogue stops a place walks to (see nearby; radiusM is the longest walk) with their walking
// times, nearest first. The second result says whether the times follow the streets.
func (e *Engine) NearbyStops(c *Catalog, p geo.Point, radiusM float64, o raptor.Options) ([]NearStop, bool) {
	cand := c.Near(p, max(radiusM, BandCapM), o)
	pos := make([]geo.Point, len(cand))
	for i, n := range cand {
		pos[i] = n.Pos
	}
	nb := e.nearby(p, pos, radiusM)
	out := make([]NearStop, len(nb.idx))
	for k, i := range nb.idx {
		out[k] = cand[i]
		out[k].WalkS = nb.secs(k, o)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].WalkS < out[b].WalkS })
	return out, nb.streets
}

// PathsFrom is for places whose walks are curated: the times are the person's own, but the route to each stop
// can still be drawn along the streets. The result has no stops of its own, only paths.
func (e *Engine) PathsFrom(net *raptor.Network, p geo.Point, maxM float64) Approach {
	g := e.Walker()
	if g == nil {
		return Approach{net: net}
	}
	reach, ok := g.From(p, maxM)
	if !ok {
		return Approach{net: net}
	}
	return Approach{Streets: true, reach: reach, net: net}
}

// WalkPath returns the route along the streets between two points (for drawing a walk between stops).
func (e *Engine) WalkPath(a, b geo.Point) ([]geo.Point, bool) {
	g := e.Walker()
	if g == nil {
		return nil, false
	}
	reach, ok := g.From(a, geo.DistanceM(a, b)*walkLimitFactor+150)
	if !ok {
		return nil, false
	}
	return reach.Path(b)
}
