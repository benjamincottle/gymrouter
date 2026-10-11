package walk

import (
	"encoding/binary"
	"sort"
)

// idSet is a set of node IDs, added in ascending order and kept as the differences between neighbours. The nodes of
// a city are a few million IDs scattered over billions, and cost about a byte each this way (8 as they are).
type idSet struct {
	first []int64 // the first ID of each block of idBlock
	off   []int   // where each block's differences begin in data
	data  []byte  // for every ID after a block's first: how much greater it is than the one before, as a uvarint
	last  int64
	n     int
}

const idBlock = 64

// add puts an ID in the set. It must be greater than every ID added before: add reports whether it was.
func (s *idSet) add(id int64) bool {
	if s.n > 0 && id <= s.last {
		return false
	}
	if s.n%idBlock == 0 {
		s.first, s.off = append(s.first, id), append(s.off, len(s.data))
	} else {
		s.data = binary.AppendUvarint(s.data, uint64(id-s.last))
	}
	s.last = id
	s.n++
	return true
}

// has reports whether an ID is in the set.
func (s *idSet) has(id int64) bool {
	b := sort.Search(len(s.first), func(i int) bool { return s.first[i] > id }) - 1
	if b < 0 || id > s.last {
		return false
	}
	end := len(s.data)
	if b+1 < len(s.off) {
		end = s.off[b+1]
	}
	cur := s.first[b]
	for p := s.off[b]; cur < id && p < end; {
		d, n := binary.Uvarint(s.data[p:end])
		cur, p = cur+int64(d), p+n
	}
	return cur == id
}

// hasAny reports whether any of the IDs is in the set.
func (s *idSet) hasAny(ids []int64) bool {
	for _, id := range ids {
		if s.has(id) {
			return true
		}
	}
	return false
}
