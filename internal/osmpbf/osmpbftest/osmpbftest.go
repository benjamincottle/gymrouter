// Package osmpbftest writes small .osm.pbf files for tests.
package osmpbftest

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"

	"google.golang.org/protobuf/encoding/protowire"
)

// Node is a node to write.
type Node struct {
	ID       int64
	Lat, Lon float64
}

// Way is a way to write.
type Way struct {
	ID   int64
	Refs []int64
	Tags map[string]string
}

func zig(v int64) uint64 { return uint64(v<<1) ^ uint64(v>>63) }

func packed(vals []uint64) []byte {
	var b []byte
	for _, v := range vals {
		b = protowire.AppendVarint(b, v)
	}
	return b
}

func bytesField(b []byte, num protowire.Number, v []byte) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendBytes(b, v)
}

func blob(kind string, payload []byte, compress bool) []byte {
	var body []byte
	if compress {
		var z bytes.Buffer
		w := zlib.NewWriter(&z)
		w.Write(payload)
		w.Close()
		body = protowire.AppendTag(body, 2, protowire.VarintType)
		body = protowire.AppendVarint(body, uint64(len(payload)))
		body = bytesField(body, 3, z.Bytes())
	} else {
		body = bytesField(body, 1, payload)
	}
	var hdr []byte
	hdr = bytesField(hdr, 1, []byte(kind))
	hdr = protowire.AppendTag(hdr, 3, protowire.VarintType)
	hdr = protowire.AppendVarint(hdr, uint64(len(body)))
	var out []byte
	out = binary.BigEndian.AppendUint32(out, uint32(len(hdr)))
	out = append(out, hdr...)
	return append(out, body...)
}

// Encode returns a PBF file: a header block, then one block of dense nodes and one of ways (the order real
// extracts use). Coordinates have 1e-7 degree precision.
func Encode(nodes []Node, ways []Way) []byte {
	out := blob("OSMHeader", bytesField(nil, 4, []byte("OsmSchema-V0.6")), true)

	var ids, lats, lons []uint64
	var pid, plat, plon int64
	for _, n := range nodes {
		lat, lon := int64(n.Lat*1e7), int64(n.Lon*1e7) // granularity 100 nm
		ids = append(ids, zig(n.ID-pid))
		lats = append(lats, zig(lat-plat))
		lons = append(lons, zig(lon-plon))
		pid, plat, plon = n.ID, lat, lon
	}
	var dense []byte
	dense = bytesField(dense, 1, packed(ids))
	dense = bytesField(dense, 8, packed(lats))
	dense = bytesField(dense, 9, packed(lons))
	var group []byte
	group = bytesField(group, 2, dense)
	out = append(out, blob("OSMData", bytesField(bytesField(nil, 1, nil), 2, group), true)...)

	// Ways share one string table; index 0 is the required empty string.
	table := [][]byte{{}}
	index := map[string]uint64{}
	str := func(s string) uint64 {
		if i, ok := index[s]; ok {
			return i
		}
		table = append(table, []byte(s))
		index[s] = uint64(len(table) - 1)
		return index[s]
	}
	var wgroup []byte
	for _, w := range ways {
		var keys, vals, refs []uint64
		for k, v := range w.Tags {
			keys, vals = append(keys, str(k)), append(vals, str(v))
		}
		var prev int64
		for _, r := range w.Refs {
			refs = append(refs, zig(r-prev))
			prev = r
		}
		var wb []byte
		wb = protowire.AppendTag(wb, 1, protowire.VarintType)
		wb = protowire.AppendVarint(wb, uint64(w.ID))
		wb = bytesField(wb, 2, packed(keys))
		wb = bytesField(wb, 3, packed(vals))
		wb = bytesField(wb, 8, packed(refs))
		wgroup = bytesField(wgroup, 3, wb)
	}
	var st []byte
	for _, s := range table {
		st = bytesField(st, 1, s)
	}
	block := bytesField(nil, 1, st)
	block = bytesField(block, 2, wgroup)
	return append(out, blob("OSMData", block, false)...)
}
