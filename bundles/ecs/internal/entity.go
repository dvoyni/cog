package internal

import "strconv"

// Entity is an opaque handle to one thing. It is comparable, copyable and
// usable as a map key, and the zero value means no Entity.
//
// It is a uint64 with the index in the low 32 bits and the generation in the
// high 32, and that split is deliberately not public contract: there is no
// exported Index or Generation, only the unexported accessors below. Both this
// and a struct of two uint32s are 8 bytes and both are comparable, but the struct
// leaks its layout into every call site and can never be re-cut.
//
// Generations start at 1, so Entity(0) unambiguously means "no Entity" while
// index 0 stays an ordinary usable slot. The top generation bit is the free
// bit, so a live generation carries thirty-one: about 2e9 reuses of one slot
// before a stale handle could alias.
type Entity uint64

// NoEntity is the absent handle. Compare with ==.
const NoEntity Entity = 0

// freeGeneration is the top bit of the generation half: the authority sets it
// on the generation an index carries while the index is free and clears it when
// the index goes live. No generation an Entity carries has it.
//
// It is what makes Entities.Alive exact in one compare. A freed index stores
// the generation it will carry next with this bit set, so the handle fabricated
// from that index and that generation — the one handle a bare generation
// compare used to say was alive — does not match until alloc clears the bit.
//
// It costs one bit of the generation space, about 2e9 reuses of one slot rather
// than 4e9. At 30 Hz that is still never.
const freeGeneration uint32 = 0x80000000

// absentGeneration is the generation half a Store writes into a sparse slot it
// holds nothing for. No live entity ever carries it — it is in the free half,
// which a live generation never reaches — and that is what lets the membership
// test be one load and one compare with no tombstone branch.
const absentGeneration uint32 = 0xFFFFFFFF

func newEntity(index, generation uint32) Entity {
	return Entity(uint64(generation)<<32 | uint64(index))
}

func (e Entity) idx() uint32 { return uint32(e) }

func (e Entity) gen() uint32 { return uint32(e >> 32) }

// String renders an entity as its index and generation, "Entity(7v2)" being
// index 7 at generation 2. The halves are shown because a log that cannot
// distinguish a recycled index from the handle that preceded it is useless;
// reading them back in code is what the type refuses.
func (e Entity) String() string {
	if e == NoEntity {
		return "NoEntity"
	}
	return "Entity(" + strconv.FormatUint(uint64(e.idx()), 10) + "v" +
		strconv.FormatUint(uint64(e.gen()), 10) + ")"
}
