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

// Relation is a relation to write.
type Relation struct {
	ID      int64
	Tags    map[string]string
	Members []Member
}

// Member is one of a relation's members: Type is 0 for a node, 1 for a way, 2 for a relation.
type Member struct {
	Type int
	Ref  int64
	Role string
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

// Options vary how a file is written.
type Options struct {
	// Sorted declares Sort.Type_then_ID in the header, as extracts made by osmium do (the nodes and ways given have
	// to be in order of ID for that to be true).
	Sorted bool
	// PerBlock is how many nodes or ways go in a block (0: all the nodes in one, all the ways in another).
	PerBlock int
	// Relation ends the file with a block holding a relation, as real extracts end.
	Relation bool
	// Relations are written after the ways, in a block of their own.
	Relations []Relation
}

// Encode returns a PBF file: a header block, then one block of dense nodes and one of ways (the order real
// extracts use). Coordinates have 1e-7 degree precision.
func Encode(nodes []Node, ways []Way) []byte { return EncodeWith(nodes, ways, Options{}) }

// EncodeWith is Encode with options.
func EncodeWith(nodes []Node, ways []Way, o Options) []byte {
	hdr := bytesField(nil, 4, []byte("OsmSchema-V0.6"))
	if o.Sorted {
		hdr = bytesField(hdr, 5, []byte("Sort.Type_then_ID"))
	}
	out := blob("OSMHeader", hdr, true)
	per := func(n int) int {
		if o.PerBlock > 0 {
			return o.PerBlock
		}
		return max(n, 1)
	}
	for i := 0; i == 0 || i < len(nodes); i += per(len(nodes)) {
		out = append(out, nodeBlock(nodes[i:min(i+per(len(nodes)), len(nodes))])...)
	}
	for i := 0; i == 0 || i < len(ways); i += per(len(ways)) {
		out = append(out, wayBlock(ways[i:min(i+per(len(ways)), len(ways))])...)
	}
	if len(o.Relations) > 0 {
		out = append(out, relationBlock(o.Relations)...)
	}
	if o.Relation {
		rel := protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.VarintType), 1) // just its ID
		out = append(out, blob("OSMData", bytesField(bytesField(nil, 1, nil), 2, bytesField(nil, 4, rel)), true)...)
	}
	return out
}

// relationBlock is a block of relations.
func relationBlock(rels []Relation) []byte {
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
	var group []byte
	for _, r := range rels {
		var keys, vals, roles, ids, types []uint64
		for k, v := range r.Tags {
			keys, vals = append(keys, str(k)), append(vals, str(v))
		}
		var prev int64
		for _, m := range r.Members {
			roles, ids, types = append(roles, str(m.Role)), append(ids, zig(m.Ref-prev)), append(types, uint64(m.Type))
			prev = m.Ref
		}
		var rb []byte
		rb = protowire.AppendTag(rb, 1, protowire.VarintType)
		rb = protowire.AppendVarint(rb, uint64(r.ID))
		rb = bytesField(rb, 2, packed(keys))
		rb = bytesField(rb, 3, packed(vals))
		rb = bytesField(rb, 8, packed(roles))
		rb = bytesField(rb, 9, packed(ids))
		rb = bytesField(rb, 10, packed(types))
		group = bytesField(group, 4, rb)
	}
	var st []byte
	for _, s := range table {
		st = bytesField(st, 1, s)
	}
	return blob("OSMData", bytesField(bytesField(nil, 1, st), 2, group), true)
}

// nodeBlock is a block of dense nodes.
func nodeBlock(nodes []Node) []byte {
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
	return blob("OSMData", bytesField(bytesField(nil, 1, nil), 2, group), true)
}

// wayBlock is a block of ways.
func wayBlock(ways []Way) []byte {
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
	return blob("OSMData", block, false)
}
