package walk

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/benjamincottle/gymrouter/internal/geo"
)

// The cache file lets a restart skip parsing the OSM extract. Format: magic, version, counts, the area the graph was
// clipped to (min lon, min lat, max lon, max lat, in 1e-7 degrees; zeros if it wasn't), then the arrays, then the
// platforms (how many; for each its ref's length and bytes, how many points, and each point's lat and lon in 1e-7
// degrees). All little endian. Bump version when anything about the graph's meaning changes (e.g. the way
// classification).
const (
	fileMagic   = "GRWALK"
	fileVersion = 3
)

// Bounds on what Load will allocate for a corrupt file's platforms.
const (
	maxPlatforms      = 1 << 16
	maxPlatformPoints = 1 << 14
	maxPlatformRef    = 1 << 8
)

func toFixed(deg float64) int32 { return int32(math.Round(deg * degScale)) }

// Save writes the graph atomically.
func (g *Graph) Save(path string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".walk-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	w := bufio.NewWriterSize(tmp, 1<<20)
	w.WriteString(fileMagic)
	hdr := []uint32{fileVersion, uint32(len(g.lat)), uint32(len(g.to))}
	if err := binary.Write(w, binary.LittleEndian, hdr); err != nil {
		return err
	}
	area := []int32{toFixed(g.Area.MinLon), toFixed(g.Area.MinLat), toFixed(g.Area.MaxLon), toFixed(g.Area.MaxLat)}
	if err := binary.Write(w, binary.LittleEndian, area); err != nil {
		return err
	}
	for _, a := range []any{g.lat, g.lon, g.start, g.to, g.cost} {
		if err := binary.Write(w, binary.LittleEndian, a); err != nil {
			return err
		}
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(len(g.Platforms))); err != nil {
		return err
	}
	for _, p := range g.Platforms {
		ref := p.Ref[:min(len(p.Ref), maxPlatformRef)]
		pts := make([]int32, 0, 2*len(p.Outline))
		for _, q := range p.Outline {
			pts = append(pts, toFixed(q.Lat), toFixed(q.Lon))
		}
		for _, v := range []any{uint32(len(ref)), []byte(ref), uint32(len(p.Outline)), pts} {
			if err := binary.Write(w, binary.LittleEndian, v); err != nil {
				return err
			}
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// maxNodes bounds what Load will allocate for a corrupt file.
const maxNodes = 64 << 20

// Load reads a graph written by Save.
func Load(path string) (*Graph, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	magic := make([]byte, len(fileMagic))
	if _, err := io.ReadFull(r, magic); err != nil || string(magic) != fileMagic {
		return nil, errors.New("walk: not a walking graph file")
	}
	var hdr [3]uint32
	if err := binary.Read(r, binary.LittleEndian, &hdr); err != nil {
		return nil, err
	}
	if hdr[0] != fileVersion {
		return nil, fmt.Errorf("walk: graph file version %d, want %d", hdr[0], fileVersion)
	}
	n, e := int(hdr[1]), int(hdr[2])
	if n == 0 || n > maxNodes || e > 4*maxNodes {
		return nil, errors.New("walk: implausible graph size")
	}
	var area [4]int32
	if err := binary.Read(r, binary.LittleEndian, &area); err != nil {
		return nil, fmt.Errorf("walk: truncated graph file: %w", err)
	}
	g := &Graph{lat: make([]int32, n), lon: make([]int32, n), start: make([]int32, n+1), to: make([]int32, e), cost: make([]float32, e)}
	g.Area = geo.Box{MinLon: float64(area[0]) / degScale, MinLat: float64(area[1]) / degScale, MaxLon: float64(area[2]) / degScale, MaxLat: float64(area[3]) / degScale}
	for _, a := range []any{g.lat, g.lon, g.start, g.to, g.cost} {
		if err := binary.Read(r, binary.LittleEndian, a); err != nil {
			return nil, fmt.Errorf("walk: truncated graph file: %w", err)
		}
	}
	if int(g.start[n]) != e {
		return nil, errors.New("walk: inconsistent graph file")
	}
	for i := 0; i < n; i++ {
		if g.start[i] > g.start[i+1] || g.start[i] < 0 {
			return nil, errors.New("walk: inconsistent graph file")
		}
	}
	for _, t := range g.to {
		if t < 0 || int(t) >= n {
			return nil, errors.New("walk: inconsistent graph file")
		}
	}
	var count uint32
	if err := binary.Read(r, binary.LittleEndian, &count); err != nil || count > maxPlatforms {
		return nil, errors.New("walk: graph file's platforms are missing or implausible")
	}
	g.Platforms = make([]Platform, count)
	for i := range g.Platforms {
		var refLen, points uint32
		if err := binary.Read(r, binary.LittleEndian, &refLen); err != nil || refLen > maxPlatformRef {
			return nil, errors.New("walk: implausible platform in graph file")
		}
		ref := make([]byte, refLen)
		if _, err := io.ReadFull(r, ref); err != nil {
			return nil, fmt.Errorf("walk: truncated graph file: %w", err)
		}
		if err := binary.Read(r, binary.LittleEndian, &points); err != nil || points > maxPlatformPoints {
			return nil, errors.New("walk: implausible platform in graph file")
		}
		pts := make([]int32, 2*points)
		if err := binary.Read(r, binary.LittleEndian, pts); err != nil {
			return nil, fmt.Errorf("walk: truncated graph file: %w", err)
		}
		out := make([]geo.Point, points)
		for k := range out {
			out[k] = geo.Point{Lat: float64(pts[2*k]) / degScale, Lon: float64(pts[2*k+1]) / degScale}
		}
		g.Platforms[i] = Platform{Ref: string(ref), Outline: out}
	}
	g.finish()
	return g, nil
}
