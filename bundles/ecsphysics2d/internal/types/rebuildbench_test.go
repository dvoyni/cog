package types

import (
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// What keeping BodyIndex current is made of.
//
// The specification quotes 9 µs per 1 024 Bodies for the rebuild and argues the
// Index/Detect System split from it. That number was taken on the float32
// prototype; BenchmarkBodyIndexRebuild beside this file is the float64 one, and
// it is several times larger. These three break the rebuild into the parts its
// author named — a rotation's cos and sin per Body, which the spec prescribes,
// and the two Go map operations per Body the Entity-to-slot table costs — so
// that the gap is reported as a decomposition rather than as one number.
//
// Nothing here changes the index. They are measurements of the pieces, priced
// against BenchmarkBodyIndexRebuild's whole, and any optimisation they suggest
// is a finding for a later ticket rather than this one's work.

// rebuiltBodies is the population every number in this file and the rebuild
// benchmark beside it is quoted per.
const rebuiltBodies = 1024

// BenchmarkTheRebuildsRotations is the transform build alone: one cos and one
// sin a Body, which is what the world cache is placed with. The angles differ
// per Body, so nothing is hoisted.
func BenchmarkTheRebuildsRotations(b *testing.B) {
	angles := make([]float64, rebuiltBodies)
	for i := range angles {
		angles[i] = float64(i) * 0.013
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		at := i & (rebuiltBodies - 1)
		transformSink = NewTransformRigid(m.Vec2d{X: float64(at)}, angles[at])
	}
}

// BenchmarkTheRebuildsSlotTable is the two Go map operations a Body the
// Entity-to-slot table costs on the rebuild path: the miss Remove asks on an
// index a Clear has just emptied, and the store Insert makes.
//
// It is measured a whole rebuild at a time — the clear and then 1 024 Bodies —
// because a map's cost is what its occupancy makes it, and a benchmark that
// asked one Entity over and over would measure a table of one. ns/op is per
// rebuild of 1 024 Bodies, which is how the 9 µs it is read against is quoted.
func BenchmarkTheRebuildsSlotTable(b *testing.B) {
	entities := make([]ecs.Entity, rebuiltBodies)
	for i := range entities {
		entities[i] = testEntity(i)
	}
	slots := make(map[ecs.Entity]int32, rebuiltBodies)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		clear(slots)
		for slot, entity := range entities {
			if _, held := slots[entity]; held {
				delete(slots, entity)
			}
			slots[entity] = int32(slot)
		}
	}
	intSink = len(slots)
}

// BenchmarkTheRebuildsCellListing is the third part: one Insert's listing of an
// entry into every cell its box covers, over a grid the rebuild has just
// cleared. It is the part no one has ever named a number for, and it is here so
// the rebuild's cost adds up rather than being attributed by subtraction alone.
func BenchmarkTheRebuildsCellListing(b *testing.B) {
	idx := NewBodyIndex(2)
	shape := NewCircleShape(0.4, m.Vec2d{})
	for i := range rebuiltBodies {
		idx.Insert(testEntity(i), shape,
			m.Vec2d{X: float64(i%32) * 1.9, Y: float64(i/32) * 1.9}, 0, nil)
	}
	// The entries stay where they are; what is measured is the listing pass
	// growBuckets already runs, over a table the rebuild's own Clear emptied.
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		idx.clearBuckets()
		idx.links = idx.links[:0]
		idx.freeLinks = idx.freeLinks[:0]
		idx.listings = 0
		for slot := range idx.entries {
			idx.pushCells(int32(slot))
		}
	}
	intSink = idx.listings
}

var intSink int
