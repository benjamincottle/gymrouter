package gtfs

import (
	"archive/zip"
	"sort"
	"strconv"

	"github.com/benjamincottle/gymrouter/internal/geo"
)

// Shape is a route's line on the ground.
type Shape struct {
	Pts []geo.Point
	// Dist is how far along the shape each point is (shape_dist_traveled, the unit of the feed's stop_times), or nil
	// when the feed doesn't say for every point.
	Dist []float32
}

// LoadShapes streams shapes.txt from a feed and returns the requested shapes, simplified to tolM
// metres. shapes.txt is large (~1 GB in the TfNSW bundle), so only the wanted IDs are kept.
func LoadShapes(path string, want map[string]bool, tolM float64) (map[string]Shape, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	type pt struct {
		seq  int
		p    geo.Point
		dist float32 // NaN when not given
	}
	raw := map[string][]pt{}
	err = readCSV(files, "shapes.txt", false, func(r row) error {
		id := r.get("shape_id")
		if !want[id] {
			return nil
		}
		lat, err1 := strconv.ParseFloat(r.get("shape_pt_lat"), 64)
		lon, err2 := strconv.ParseFloat(r.get("shape_pt_lon"), 64)
		seq, err3 := strconv.Atoi(r.get("shape_pt_sequence"))
		if err1 != nil || err2 != nil || err3 != nil {
			return nil
		}
		raw[id] = append(raw[id], pt{seq, geo.Point{Lat: lat, Lon: lon}, parseDist(r.get("shape_dist_traveled"))})
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]Shape, len(raw))
	for id, ps := range raw {
		sort.Slice(ps, func(a, b int) bool { return ps[a].seq < ps[b].seq })
		line := make([]geo.Point, len(ps))
		for i, p := range ps {
			line[i] = p.p
		}
		keep := geo.Simplify(line, tolM)
		sh := Shape{Pts: make([]geo.Point, len(keep)), Dist: make([]float32, len(keep))}
		for i, k := range keep {
			sh.Pts[i], sh.Dist[i] = ps[k].p, ps[k].dist
		}
		for i, d := range sh.Dist {
			if !(d >= 0 && (i == 0 || d >= sh.Dist[i-1])) { // a missing distance is NaN, which fails too
				sh.Dist = nil
				break
			}
		}
		out[id] = sh
	}
	return out, nil
}

// TripShapes returns the shape of each of the wanted trips that a feed has, whatever days they run: trip_id to
// shape_id. A trip the feed doesn't have, or has without a shape, is left out.
func TripShapes(path string, want map[string]bool) (map[string]string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	out := map[string]string{}
	err = readCSV(files, "trips.txt", true, func(r row) error {
		if id, shape := r.get("trip_id"), r.get("shape_id"); want[id] && shape != "" {
			out[id] = shape
		}
		return nil
	})
	return out, err
}
