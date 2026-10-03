package walk

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// The cache file lets a restart skip parsing the OSM extract. Format: magic, version, counts, then the arrays
// (little endian). Bump version when anything about the graph's meaning changes (e.g. the way classification).
const (
	fileMagic   = "GRWALK"
	fileVersion = 1
)

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
	for _, a := range []any{g.lat, g.lon, g.start, g.to, g.cost} {
		if err := binary.Write(w, binary.LittleEndian, a); err != nil {
			return err
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
	g := &Graph{lat: make([]int32, n), lon: make([]int32, n), start: make([]int32, n+1), to: make([]int32, e), cost: make([]float32, e)}
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
	g.finish()
	return g, nil
}
