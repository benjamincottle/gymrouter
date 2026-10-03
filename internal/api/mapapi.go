package api

import (
	"math"
	"net/http"
	"os"
	"time"

	"github.com/benjamincottle/gymrouter/internal/geo"
)

// coords converts points to GeoJSON order ([lon, lat]) rounded to ~1 m.
func coords(pts []geo.Point) [][2]float64 {
	out := make([][2]float64, len(pts))
	for i, p := range pts {
		out[i] = [2]float64{math.Round(p.Lon*1e5) / 1e5, math.Round(p.Lat*1e5) / 1e5}
	}
	return out
}

// legShape returns the path of one ride leg: GET /api/shape?date=YYYY-MM-DD&trip=&from=&to=
func (s *Server) legShape(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	date, err := time.ParseInLocation("2006-01-02", q.Get("date"), s.eng.Now().Location())
	if err != nil {
		writeError(w, http.StatusBadRequest, "date must be YYYY-MM-DD")
		return
	}
	trip, from, to := q.Get("trip"), q.Get("from"), q.Get("to")
	if trip == "" || from == "" || to == "" || len(trip)+len(from)+len(to) > 300 {
		writeError(w, http.StatusBadRequest, "trip, from and to are required")
		return
	}
	if d := date.Sub(s.eng.Now()); d < -48*time.Hour || d > 8*24*time.Hour {
		writeError(w, http.StatusBadRequest, "date out of range")
		return
	}
	snap, err := s.eng.SnapshotFor(date.Add(12 * time.Hour))
	if err != nil {
		s.log.Error("shape: timetable unavailable", "err", err)
		writeError(w, http.StatusInternalServerError, "timetable unavailable")
		return
	}
	pts, ok := s.eng.LegGeometry(snap, trip, from, to)
	if !ok {
		writeError(w, http.StatusNotFound, "trip or stops not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"coordinates": coords(pts)})
}

// lineShapes returns the gym's lines as GeoJSON for drawing: GET /api/shapes?gym=
func (s *Server) lineShapes(w http.ResponseWriter, r *http.Request) {
	g, ok := s.eng.Config().Gym(r.URL.Query().Get("gym"))
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown gym")
		return
	}
	type feature struct {
		Type       string         `json:"type"`
		Properties map[string]any `json:"properties"`
		Geometry   map[string]any `json:"geometry"`
	}
	fc := struct {
		Type     string    `json:"type"`
		Features []feature `json:"features"`
	}{Type: "FeatureCollection", Features: []feature{}}
	for _, ls := range s.eng.LineShapes(g.LineSet) {
		multi := make([][][2]float64, len(ls.Coords))
		for i, c := range ls.Coords {
			multi[i] = coords(c)
		}
		fc.Features = append(fc.Features, feature{
			Type:       "Feature",
			Properties: map[string]any{"line": ls.Line.String(), "mode": string(ls.Line.Mode), "name": ls.Line.Name, "color": ls.Color},
			Geometry:   map[string]any{"type": "MultiLineString", "coordinates": multi},
		})
	}
	writeJSON(w, http.StatusOK, fc)
}

// mapTiles serves the self-hosted basemap with range requests (PMTiles is read in pieces).
func (s *Server) mapTiles(w http.ResponseWriter, r *http.Request) {
	path, ok := s.eng.MapFile()
	if !ok {
		writeError(w, http.StatusNotFound, "map not installed on the server")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "map not installed on the server")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "map unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "private, max-age=86400") // the file changes rarely; ETag covers updates
	w.Header().Set("ETag", `"`+fi.ModTime().UTC().Format("20060102150405")+`"`)
	http.ServeContent(w, r, "map.pmtiles", fi.ModTime(), f)
}
