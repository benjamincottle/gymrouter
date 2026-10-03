package gtfs

import (
	"archive/zip"
	"sort"
	"strconv"

	"github.com/benjamincottle/gymrouter/internal/geo"
)

// LoadShapes streams shapes.txt from a feed and returns the requested shapes, simplified to tolM
// metres. shapes.txt is large (~1 GB in the TfNSW bundle), so only the wanted IDs are kept.
func LoadShapes(path string, want map[string]bool, tolM float64) (map[string][]geo.Point, error) {
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
		seq int
		p   geo.Point
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
		raw[id] = append(raw[id], pt{seq, geo.Point{Lat: lat, Lon: lon}})
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string][]geo.Point, len(raw))
	for id, ps := range raw {
		sort.Slice(ps, func(a, b int) bool { return ps[a].seq < ps[b].seq })
		line := make([]geo.Point, len(ps))
		for i, p := range ps {
			line[i] = p.p
		}
		out[id] = geo.Simplify(line, tolM)
	}
	return out, nil
}
