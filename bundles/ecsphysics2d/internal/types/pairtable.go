package types

import (
	"unsafe"

	"github.com/dvoyni/cog/bundles/ecs"
)

// The open-addressed map from an unordered Entity pair to an int32, used twice
// over: the Contact list keys this tick's and the previous tick's entries by
// it in contacts.go, and JointedPairs is a set built on it in
// jointedpairs.go.

// pairKey is one cell. A cell with A of NoEntity is empty, which an Entity
// never is: generations start at 1.
type pairKey struct {
	a, b ecs.Entity
	slot int32
}

// pairTableMin is where a fresh table starts. It doubles whenever it is half
// full, and never shrinks on its own.
const pairTableMin = 64

// pairTable is the open-addressed map from an unordered Entity pair to a slot
// in the buffer beside it. There are two of them and they swap with the
// buffers, so detection inserts into one as it appends and looks the pair up in
// the other — about two probes an entry a tick, and no sort.
//
// It is a table of its own rather than cp's persistent arbiter table at stable
// slots: that would give persistence for free but turn the app's two walks a
// tick into an indirect walk over a table with holes, and the contiguous walk is
// what those two passes are paying for.
type pairTable struct {
	keys []pairKey
	used int
}

// clear empties the table, keeping the memory it has grown.
func (t *pairTable) clear() {
	for i := range t.keys {
		t.keys[i] = pairKey{}
	}
	t.used = 0
}

// find is the slot the pair is at, or false. The full pair is verified rather
// than trusted, the entry carrying it anyway.
func (t *pairTable) find(a, b ecs.Entity) (int32, bool) {
	if len(t.keys) == 0 {
		return 0, false
	}
	low, high := order(a, b)
	mask := uint64(len(t.keys) - 1)
	for at := hashPair(low, high) & mask; ; at = (at + 1) & mask {
		cell := &t.keys[at]
		switch {
		case cell.a == ecs.NoEntity:
			return 0, false
		case cell.a == low && cell.b == high:
			return cell.slot, true
		}
	}
}

// insert records where the pair's entry is. A pair is inserted at most once a
// tick, so there is no replacement path and no tombstone.
func (t *pairTable) insert(a, b ecs.Entity, slot int32) {
	if t.used*2 >= len(t.keys) {
		t.grow()
	}
	low, high := order(a, b)
	mask := uint64(len(t.keys) - 1)
	for at := hashPair(low, high) & mask; ; at = (at + 1) & mask {
		if t.keys[at].a == ecs.NoEntity {
			t.keys[at] = pairKey{a: low, b: high, slot: slot}
			t.used++
			return
		}
	}
}

// grow doubles the table and re-inserts what it held.
func (t *pairTable) grow() {
	size := len(t.keys) * 2
	if size < pairTableMin {
		size = pairTableMin
	}
	old := t.keys
	t.keys = make([]pairKey, size)
	t.used = 0
	for i := range old {
		if old[i].a != ecs.NoEntity {
			t.insert(old[i].a, old[i].b, old[i].slot)
		}
	}
}

// shrink rebuilds the table at the smallest size that holds what it has at
// under half load, which is the load factor insert keeps it at. A table already
// at that size is left alone, so a second shrink allocates nothing.
func (t *pairTable) shrink() {
	size := pairTableMin
	for size < t.used*2 {
		size *= 2
	}
	if t.used == 0 {
		t.keys, t.used = nil, 0
		return
	}
	if size == len(t.keys) {
		return
	}
	old := t.keys
	t.keys = make([]pairKey, size)
	t.used = 0
	for i := range old {
		if old[i].a != ecs.NoEntity {
			t.insert(old[i].a, old[i].b, old[i].slot)
		}
	}
}

// bytes is what the table holds, by capacity.
func (t *pairTable) bytes() uintptr {
	return uintptr(cap(t.keys)) * unsafe.Sizeof(pairKey{})
}

// order is the pair as the table keys it, lower Entity first, so that a change
// of which party is A never loses the pair.
func order(a, b ecs.Entity) (ecs.Entity, ecs.Entity) {
	if b < a {
		return b, a
	}
	return a, b
}

// hashPair mixes the ordered pair into a bucket. cp's own HashPair multiplies
// and adds two pointers, which needs no mixing because a pointer is already
// spread; Entity ids are small dense integers and would cluster, so this is
// splitmix's finaliser over the two.
func hashPair(a, b ecs.Entity) uint64 {
	x := uint64(a)*0x9E3779B97F4A7C15 ^ uint64(b)*0xC2B2AE3D27D4EB4F
	x ^= x >> 29
	x *= 0xBF58476D1CE4E5B9
	x ^= x >> 32
	return x
}
