package walk

import "testing"

func TestIDSet(t *testing.T) {
	var s idSet
	if s.has(1) || s.hasAny([]int64{0, 5}) {
		t.Error("an empty set has nothing")
	}
	// IDs as a city's are: runs of neighbours, and gaps of millions and billions between them.
	var ids []int64
	id := int64(7)
	for i := 0; i < 1000; i++ {
		switch {
		case i%97 == 0:
			id += 3_000_000_000
		case i%7 == 0:
			id += 40_000
		default:
			id += int64(1 + i%3)
		}
		ids = append(ids, id)
	}
	for _, id := range ids {
		if !s.add(id) {
			t.Fatalf("add(%d) refused", id)
		}
	}
	in := map[int64]bool{}
	for _, id := range ids {
		in[id] = true
	}
	for _, id := range ids {
		if !s.has(id) {
			t.Fatalf("has(%d) = false", id)
		}
		for _, near := range []int64{id - 1, id + 1} {
			if s.has(near) != in[near] {
				t.Fatalf("has(%d) = %v", near, s.has(near))
			}
		}
	}
	if s.has(0) || s.has(ids[len(ids)-1]+1_000_000) {
		t.Error("has an ID below the first or above the last")
	}
	if !s.hasAny([]int64{1, 2, ids[500]}) || s.hasAny([]int64{1, 2, 3}) {
		t.Error("hasAny")
	}
	// Only ascending: the same ID again, or a smaller one, is refused and changes nothing.
	last := ids[len(ids)-1]
	if s.add(last) || s.add(last-1) || s.add(5) {
		t.Error("add took an ID that wasn't greater than the last")
	}
	if !s.add(last+1) || !s.has(last+1) || s.n != len(ids)+1 {
		t.Error("add after a refusal")
	}
	// About a byte or two an ID, not eight.
	if len(s.data) > 3*len(ids) {
		t.Errorf("%d IDs took %d bytes", len(ids), len(s.data))
	}
}
