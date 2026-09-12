package proto

import (
	"fmt"
	"reflect"
	"testing"

	"protoecs/ecs"
)

// Two questions from review about Hash: why a named key type at all, and who
// cleans the table up.

// AnyHash is the degenerate case: one named type for everything, which *is* the
// simple single named type. It costs nothing to fall back to, and what it
// gives up is visible right here -- under one type a clip name and a model name
// are the same type and assign to each other freely.
type AnyHash uint64

func TestTheSingleDomainFormIsTheSimpleOne(t *testing.T) {
	clip := ecs.HashOf[AnyHash]("Walk")
	model := ecs.HashOf[AnyHash]("models/crate.glb")
	// Both are AnyHash, so this compiles. With separate named types it would
	// not, and that is the whole of what declaring two of them buys.
	clip = model
	_ = clip
	if reflect.TypeFor[AnyHash]() == reflect.TypeFor[ClipHash]() {
		t.Fatalf("AnyHash and ClipHash are the same type")
	}
	t.Logf("one named type is the simple form; two cost the same and add a compile error")
}

// Who cleans up? Nothing, because nothing accumulates.
//
// This is the question an interner cannot answer and a hash does not have to.
// An interner must record every string it is ever shown, so that the id it
// assigns stays consistent -- so it grows with everything the game ever names,
// and something has to decide when an entry may go. Hashing records nothing:
// the consumer's table holds only what the consumer *registered*, which is its
// asset manifest, fixed at startup and never touched by gameplay.
//
// So a million hashes of strings nobody declared leave the table exactly as it
// was, and each resolves to "not registered" -- which is the right answer, and
// the same one scene already gives a selector that matches nothing.
func TestHashingAccumulatesNothing(t *testing.T) {
	table := ecs.NewNames[ModelHash, int]()
	for i := range 8 {
		if err := table.Register(fmt.Sprintf("models/declared_%d.glb", i), i); err != nil {
			t.Fatal(err)
		}
	}
	before := table.Len()

	// Everything a running game might hash: procedural ids, player input, names
	// out of a save file. None of it was declared.
	for i := range 1_000_000 {
		h := ecs.HashOf[ModelHash](fmt.Sprintf("runtime/generated/%d", i))
		if _, ok := table.Lookup(h); ok {
			t.Fatalf("an undeclared name resolved")
		}
	}
	if table.Len() != before {
		t.Fatalf("the table grew from %d to %d by being asked questions", before, table.Len())
	}
	t.Logf("1 000 000 hashes of undeclared strings; the table still holds %d entries", table.Len())
}
