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

const (
	// BandM widens the longest walk when nothing is within it: the nearest stop is used, and any up to this much
	// further (along the streets when they're known), so the router still has a choice.
	BandM = 500
	// BandCapM is as far as the band reaches (straight line). Past it a place has no stops.
	BandCapM = 3000
)

// nearby is the outcome of picking the positions a place walks to.
type nearby struct {
	idx       []int     // the candidates kept
	metres    []float64 // the walk to each: along the streets, or a straight line
	streets   bool
	reach     *walk.Reach
	stretched float64 // metres to the nearest when nothing was within the longest walk (the band was used), else 0
}

// secs is the walking time to the kth kept candidate.
func (n nearby) secs(k int, o raptor.Options) int32 {
	if n.streets {
		return int32(n.metres[k] / o.WalkSpeedMps)
	}
	return o.WalkSecs(n.metres[k])
}

// nearby picks which of the candidate positions a place walks to: those within maxWalkM (straight line) that can
// be walked to, or if there are none, the band (see BandM). With the street network, walks follow the streets and
// positions that can't be walked to are dropped; a place off the map falls back to straight lines.
func (e *Engine) nearby(p geo.Point, cand []geo.Point, maxWalkM float64) nearby {
	crow := make([]float64, len(cand))
	for i, q := range cand {
		crow[i] = geo.DistanceM(p, q)
	}
	if g := e.Walker(); g != nil {
		if reach, ok := g.From(p, maxWalkM*walkLimitFactor); ok {
			out := nearby{streets: true, reach: reach}
			for i, q := range cand {
				if crow[i] <= maxWalkM {
					if m, ok := reach.Metres(q); ok {
						out.idx, out.metres = append(out.idx, i), append(out.metres, m)
					}
				}
			}
			if len(out.idx) > 0 {
				return out
			}
			var far []geo.Point
			for i, q := range cand {
				if crow[i] <= BandCapM {
					far = append(far, q)
				}
			}
			if len(far) == 0 {
				return out
			}
			reach, _ = g.FromNearest(p, far, BandM, BandCapM*walkLimitFactor) // p is on the map: From found it
			metres := make([]float64, len(cand))
			best := math.Inf(1)
			for i, q := range cand {
				metres[i] = math.Inf(1)
				if m, ok := reach.Metres(q); ok && crow[i] <= BandCapM {
					metres[i], best = m, min(best, m)
				}
			}
			return band(metres, best, nearby{streets: true, reach: reach})
		}
	}
	out := nearby{}
	best := math.Inf(1)
	for i, d := range crow {
		if d <= maxWalkM {
			out.idx, out.metres = append(out.idx, i), append(out.metres, d)
		}
		if d <= BandCapM {
			best = min(best, d)
		}
	}
	if len(out.idx) > 0 {
		return out
	}
	return band(crow, best, out)
}

// band keeps the candidates within BandM of the nearest (best metres away).
func band(metres []float64, best float64, out nearby) nearby {
	if math.IsInf(best, 1) {
		return out
	}
	for i, m := range metres {
		if m <= best+BandM {
			out.idx, out.metres = append(out.idx, i), append(out.metres, m)
		}
	}
	out.stretched = best
	return out
}

// Approach is how a place connects to nearby stops: the stops with their walking times, and (when the street
// network is available) the routes themselves for drawing.
type Approach struct {
	Access  []raptor.Access
	Streets bool // times come from the street network, not a straight-line guess
	// StretchedM is the walk to the nearest stop when no stop was within the longest walk and the band was used.
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
