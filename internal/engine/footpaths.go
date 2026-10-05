package engine

import (
	"sync"

	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/par"
	"github.com/benjamincottle/gymrouter/internal/raptor"
	"github.com/benjamincottle/gymrouter/internal/walk"
)

// footKey identifies a walking transfer by stop IDs, which stay valid when the timetable is rebuilt.
type footKey struct{ from, to string }

// footCache remembers street walking distances between stops, so rebuilding the timetable (which happens
// every time live predictions arrive) doesn't route thousands of walks again. A distance of -1 means the stop
// can't be reached on foot.
type footCache struct {
	mu sync.Mutex
	m  map[footKey]float32
}

func (c *footCache) get(k footKey) (float32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[k]
	return v, ok
}

func (c *footCache) put(from string, to []string, metres []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[footKey]float32{}
	}
	for i, t := range to {
		c.m[footKey{from, t}] = metres[i]
	}
}

func (c *footCache) reset() {
	c.mu.Lock()
	c.m = nil
	c.mu.Unlock()
}

// snapshotOptions are the routing options for the timetable snapshots: transfers between stops follow the
// streets when the street network is available. (Whole-network passes use routingOptions: too many stops.)
func (e *Engine) snapshotOptions() raptor.Options {
	o := e.routingOptions()
	if g := e.Walker(); g != nil {
		o.Footpaths = func(d *gtfs.Day, used []bool, straight [][]raptor.Footpath) [][]raptor.Footpath {
			return e.streetFootpaths(g, o, d, used, straight)
		}
	}
	return o
}

// streetFootpaths times each straight-line transfer candidate along the streets and drops the ones that can't
// be walked within a reasonable detour. Stops are worked on in parallel; results are cached.
func (e *Engine) streetFootpaths(g *walk.Graph, o raptor.Options, d *gtfs.Day, used []bool, straight [][]raptor.Footpath) [][]raptor.Footpath {
	out := make([][]raptor.Footpath, len(straight))
	var stops []int32
	for s := range straight {
		if used[s] && len(straight[s]) > 0 {
			stops = append(stops, int32(s))
		}
	}
	par.Do(len(stops), func(i int) {
		s := stops[i]
		out[s] = e.streetFootpathsFrom(g, o, d, s, straight[s])
	})
	return out
}

func (e *Engine) streetFootpathsFrom(g *walk.Graph, o raptor.Options, d *gtfs.Day, s int32, cands []raptor.Footpath) []raptor.Footpath {
	from := d.Stops[s]
	missing := false
	for _, c := range cands {
		if _, ok := e.foot.get(footKey{from.ID, d.Stops[c.To].ID}); !ok {
			missing = true
			break
		}
	}
	if missing {
		reach, ok := g.From(from.Pos, o.MaxTransferM*walkLimitFactor)
		if !ok {
			return cands // not on the street map: keep the straight-line estimate
		}
		ids := make([]string, len(cands))
		metres := make([]float32, len(cands))
		for i, c := range cands {
			ids[i] = d.Stops[c.To].ID
			if m, ok := reach.Metres(d.Stops[c.To].Pos); ok {
				metres[i] = float32(m)
			} else {
				metres[i] = -1
			}
		}
		e.foot.put(from.ID, ids, metres)
	}
	out := make([]raptor.Footpath, 0, len(cands))
	for _, c := range cands {
		if m, _ := e.foot.get(footKey{from.ID, d.Stops[c.To].ID}); m >= 0 {
			out = append(out, raptor.Footpath{To: c.To, Secs: int32(float64(m) / o.WalkSpeedMps)})
		}
	}
	return out
}
