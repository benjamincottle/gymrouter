package engine

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/raptor"
	"github.com/benjamincottle/gymrouter/internal/walk"
)

// walkLimitFactor lets a walk be this many times the "longest walk" setting along the streets. That setting is
// measured in a straight line (it picks the candidate stops); the streets can be much longer around a barrier,
// and the router should be free to say so rather than pretend the stop is out of reach.
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

// Approach is how a place connects to nearby stops: the stops with their walking times, and (when the street
// network is available) the routes themselves for drawing.
type Approach struct {
	Access  []raptor.Access
	Streets bool // times come from the street network, not a straight-line guess
	reach   *walk.Reach
	net     *raptor.Network
}

// Approach finds the stops within maxWalkM (straight line) of p and how long each takes to walk to. With the
// street network, the walk follows streets and paths and stops that can't be reached on foot are dropped.
func (e *Engine) Approach(net *raptor.Network, p geo.Point, maxWalkM float64, o raptor.Options) Approach {
	cand := net.StopsNear(p, maxWalkM, o)
	g := e.Walker()
	if g == nil {
		return Approach{Access: cand, net: net}
	}
	reach, ok := g.From(p, maxWalkM*walkLimitFactor)
	if !ok {
		return Approach{Access: cand, net: net} // not on the map (outside the extract, or on water)
	}
	out := make([]raptor.Access, 0, len(cand))
	for _, a := range cand {
		if m, ok := reach.Metres(net.Day.Stops[a.Stop].Pos); ok {
			out = append(out, raptor.Access{Stop: a.Stop, Secs: int32(m / o.WalkSpeedMps)})
		}
	}
	return Approach{Access: out, Streets: true, reach: reach, net: net}
}

// Path returns the walk between the place and a stop along the streets, if known.
func (a Approach) Path(stop int32) ([]geo.Point, bool) {
	if a.reach == nil || stop < 0 {
		return nil, false
	}
	return a.reach.Path(a.net.Day.Stops[stop].Pos)
}

// NearbyStops lists catalogue stops within radiusM (straight line) of p with their walking times, nearest first.
// The second result says whether the times follow the streets.
func (e *Engine) NearbyStops(c *Catalog, p geo.Point, radiusM float64, o raptor.Options) ([]NearStop, bool) {
	near := c.Near(p, radiusM, o)
	g := e.Walker()
	if g == nil {
		return near, false
	}
	reach, ok := g.From(p, radiusM*walkLimitFactor)
	if !ok {
		return near, false
	}
	out := near[:0]
	for _, n := range near {
		if m, ok := reach.Metres(n.Pos); ok {
			n.WalkS = int32(m / o.WalkSpeedMps)
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].WalkS < out[b].WalkS })
	return out, true
}
