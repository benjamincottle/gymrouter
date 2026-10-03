// Package osmpbf is a minimal reader for OpenStreetMap .osm.pbf files: just nodes and ways, which is all
// a walking network needs. It decodes the wire format directly with protowire, so it adds no dependency.
package osmpbf

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"google.golang.org/protobuf/encoding/protowire"
)

// Limits guard against corrupt or hostile files (the file comes from the internet).
const (
	maxHeaderSize = 64 << 10
	maxBlobSize   = 32 << 20 // OSM's own limit is 32 MiB, uncompressed
)

// Node is a point with its tags.
type Node struct {
	ID       int64
	Lat, Lon float64 // degrees
}

// Way is an ordered list of node IDs with tags.
type Way struct {
	ID   int64
	Refs []int64
	Tags map[string]string
}

// Handler receives what was read. Return an error to stop. Nodes and ways are delivered in file order.
// A Way's Refs and Tags are only valid during the call.
type Handler struct {
	Node func(Node) error
	Way  func(Way) error
	// WantNodes and WantWays let a pass skip decoding what it doesn't need.
	WantNodes, WantWays bool
}

// Read streams a PBF file through h.
func Read(r io.Reader, h Handler) error {
	for {
		var lenBuf [4]byte
		if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("osmpbf: reading block header length: %w", err)
		}
		hdrLen := binary.BigEndian.Uint32(lenBuf[:])
		if hdrLen == 0 || hdrLen > maxHeaderSize {
			return fmt.Errorf("osmpbf: implausible block header size %d", hdrLen)
		}
		hdr := make([]byte, hdrLen)
		if _, err := io.ReadFull(r, hdr); err != nil {
			return fmt.Errorf("osmpbf: reading block header: %w", err)
		}
		kind, dataSize, err := parseBlobHeader(hdr)
		if err != nil {
			return err
		}
		if dataSize < 0 || dataSize > maxBlobSize {
			return fmt.Errorf("osmpbf: implausible blob size %d", dataSize)
		}
		blob := make([]byte, dataSize)
		if _, err := io.ReadFull(r, blob); err != nil {
			return fmt.Errorf("osmpbf: reading blob: %w", err)
		}
		if kind != "OSMData" {
			continue // OSMHeader and anything unknown
		}
		raw, err := inflate(blob)
		if err != nil {
			return err
		}
		if err := decodeBlock(raw, h); err != nil {
			return err
		}
	}
}

func parseBlobHeader(b []byte) (kind string, size int, err error) {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return "", 0, errors.New("osmpbf: bad blob header")
		}
		b = b[n:]
		switch {
		case num == 1 && typ == protowire.BytesType:
			v, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return "", 0, errors.New("osmpbf: bad blob header type")
			}
			kind, b = string(v), b[n:]
		case num == 3 && typ == protowire.VarintType:
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return "", 0, errors.New("osmpbf: bad blob header size")
			}
			size, b = int(v), b[n:]
		default:
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return "", 0, errors.New("osmpbf: bad blob header field")
			}
			b = b[n:]
		}
	}
	return kind, size, nil
}

// inflate returns the uncompressed bytes of a Blob (raw or zlib).
func inflate(blob []byte) ([]byte, error) {
	var raw, z []byte
	b := blob
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, errors.New("osmpbf: bad blob")
		}
		b = b[n:]
		switch {
		case num == 1 && typ == protowire.BytesType:
			v, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return nil, errors.New("osmpbf: bad raw data")
			}
			raw, b = v, b[n:]
		case num == 3 && typ == protowire.BytesType:
			v, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return nil, errors.New("osmpbf: bad zlib data")
			}
			z, b = v, b[n:]
		case (num == 4 || num == 5 || num == 6 || num == 7) && typ == protowire.BytesType:
			return nil, fmt.Errorf("osmpbf: unsupported blob compression (field %d)", num)
		default:
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return nil, errors.New("osmpbf: bad blob field")
			}
			b = b[n:]
		}
	}
	if z == nil {
		return raw, nil
	}
	zr, err := zlib.NewReader(bytes.NewReader(z))
	if err != nil {
		return nil, fmt.Errorf("osmpbf: %w", err)
	}
	defer zr.Close()
	out, err := io.ReadAll(io.LimitReader(zr, maxBlobSize+1))
	if err != nil {
		return nil, fmt.Errorf("osmpbf: inflating: %w", err)
	}
	if len(out) > maxBlobSize {
		return nil, errors.New("osmpbf: block too large")
	}
	return out, nil
}

type block struct {
	strings                     [][]byte
	granularity, latOff, lonOff int64
}

func decodeBlock(b []byte, h Handler) error {
	bl := block{granularity: 100}
	var groups [][]byte
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return errors.New("osmpbf: bad primitive block")
		}
		b = b[n:]
		switch {
		case num == 1 && typ == protowire.BytesType: // StringTable
			v, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return errors.New("osmpbf: bad string table")
			}
			var err error
			if bl.strings, err = decodeStrings(v); err != nil {
				return err
			}
			b = b[n:]
		case num == 2 && typ == protowire.BytesType: // PrimitiveGroup
			v, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return errors.New("osmpbf: bad primitive group")
			}
			groups, b = append(groups, v), b[n:]
		case (num == 17 || num == 19 || num == 20) && typ == protowire.VarintType:
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return errors.New("osmpbf: bad block field")
			}
			switch num {
			case 17:
				bl.granularity = int64(v)
			case 19:
				bl.latOff = int64(v)
			case 20:
				bl.lonOff = int64(v)
			}
			b = b[n:]
		default:
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return errors.New("osmpbf: bad block field")
			}
			b = b[n:]
		}
	}
	for _, g := range groups {
		if err := bl.decodeGroup(g, h); err != nil {
			return err
		}
	}
	return nil
}

func decodeStrings(b []byte) ([][]byte, error) {
	var out [][]byte
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, errors.New("osmpbf: bad string table")
		}
		b = b[n:]
		if num == 1 && typ == protowire.BytesType {
			v, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return nil, errors.New("osmpbf: bad string")
			}
			out, b = append(out, v), b[n:]
			continue
		}
		n = protowire.ConsumeFieldValue(num, typ, b)
		if n < 0 {
			return nil, errors.New("osmpbf: bad string table field")
		}
		b = b[n:]
	}
	return out, nil
}

func (bl *block) degrees(offset, v int64) float64 {
	return 1e-9 * float64(offset+bl.granularity*v)
}

func (bl *block) decodeGroup(b []byte, h Handler) error {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 || typ != protowire.BytesType {
			if n >= 0 {
				n2 := protowire.ConsumeFieldValue(num, typ, b[n:])
				if n2 >= 0 {
					b = b[n+n2:]
					continue
				}
			}
			return errors.New("osmpbf: bad primitive group field")
		}
		v, m := protowire.ConsumeBytes(b[n:])
		if m < 0 {
			return errors.New("osmpbf: bad primitive group entry")
		}
		b = b[n+m:]
		var err error
		switch num {
		case 1:
			if h.WantNodes && h.Node != nil {
				err = bl.decodeNode(v, h)
			}
		case 2:
			if h.WantNodes && h.Node != nil {
				err = bl.decodeDense(v, h)
			}
		case 3:
			if h.WantWays && h.Way != nil {
				err = bl.decodeWay(v, h)
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// packed reads a packed repeated varint field into out.
func packed(b []byte, out []uint64) ([]uint64, error) {
	for len(b) > 0 {
		v, n := protowire.ConsumeVarint(b)
		if n < 0 {
			return nil, errors.New("osmpbf: bad packed field")
		}
		out, b = append(out, v), b[n:]
	}
	return out, nil
}

func zigzag(v uint64) int64 { return int64(v>>1) ^ -int64(v&1) }

func (bl *block) decodeDense(b []byte, h Handler) error {
	var ids, lats, lons []uint64
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return errors.New("osmpbf: bad dense nodes")
		}
		b = b[n:]
		if typ == protowire.BytesType && (num == 1 || num == 8 || num == 9) {
			v, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return errors.New("osmpbf: bad dense field")
			}
			var err error
			switch num {
			case 1:
				ids, err = packed(v, ids)
			case 8:
				lats, err = packed(v, lats)
			case 9:
				lons, err = packed(v, lons)
			}
			if err != nil {
				return err
			}
			b = b[n:]
			continue
		}
		n = protowire.ConsumeFieldValue(num, typ, b)
		if n < 0 {
			return errors.New("osmpbf: bad dense field")
		}
		b = b[n:]
	}
	if len(ids) != len(lats) || len(ids) != len(lons) {
		return errors.New("osmpbf: dense node arrays differ in length")
	}
	var id, lat, lon int64
	for i := range ids {
		id += zigzag(ids[i])
		lat += zigzag(lats[i])
		lon += zigzag(lons[i])
		if err := h.Node(Node{ID: id, Lat: bl.degrees(bl.latOff, lat), Lon: bl.degrees(bl.lonOff, lon)}); err != nil {
			return err
		}
	}
	return nil
}

func (bl *block) decodeNode(b []byte, h Handler) error {
	var nd Node
	var lat, lon int64
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return errors.New("osmpbf: bad node")
		}
		b = b[n:]
		if typ == protowire.VarintType && (num == 1 || num == 8 || num == 9) {
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return errors.New("osmpbf: bad node field")
			}
			switch num {
			case 1:
				nd.ID = zigzag(v)
			case 8:
				lat = zigzag(v)
			case 9:
				lon = zigzag(v)
			}
			b = b[n:]
			continue
		}
		n = protowire.ConsumeFieldValue(num, typ, b)
		if n < 0 {
			return errors.New("osmpbf: bad node field")
		}
		b = b[n:]
	}
	nd.Lat, nd.Lon = bl.degrees(bl.latOff, lat), bl.degrees(bl.lonOff, lon)
	return h.Node(nd)
}

func (bl *block) decodeWay(b []byte, h Handler) error {
	var w Way
	var keys, vals, refs []uint64
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return errors.New("osmpbf: bad way")
		}
		b = b[n:]
		switch {
		case num == 1 && typ == protowire.VarintType:
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return errors.New("osmpbf: bad way id")
			}
			w.ID, b = int64(v), b[n:]
		case (num == 2 || num == 3 || num == 8) && typ == protowire.BytesType:
			v, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return errors.New("osmpbf: bad way field")
			}
			var err error
			switch num {
			case 2:
				keys, err = packed(v, keys)
			case 3:
				vals, err = packed(v, vals)
			case 8:
				refs, err = packed(v, refs)
			}
			if err != nil {
				return err
			}
			b = b[n:]
		default:
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return errors.New("osmpbf: bad way field")
			}
			b = b[n:]
		}
	}
	if len(keys) != len(vals) {
		return errors.New("osmpbf: way keys and values differ in length")
	}
	w.Tags = make(map[string]string, len(keys))
	for i := range keys {
		if keys[i] >= uint64(len(bl.strings)) || vals[i] >= uint64(len(bl.strings)) {
			return errors.New("osmpbf: tag string index out of range")
		}
		w.Tags[string(bl.strings[keys[i]])] = string(bl.strings[vals[i]])
	}
	w.Refs = make([]int64, len(refs))
	var id int64
	for i, r := range refs {
		id += zigzag(r)
		w.Refs[i] = id
	}
	return h.Way(w)
}
