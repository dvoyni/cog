package proto

import (
	"fmt"
	"reflect"
	"testing"

	"protoecs/ecs"
)

// Two questions from review about Hash: why the Domain parameter rather than
// just Hash[string], and who cleans the table up.

// String is the degenerate domain: one tag for everything, which *is* the
// simple Hash[String] spelling. It costs nothing to fall back to, and what it
// gives up is visible right here -- under one tag a clip name and a model name
// are the same type and assign to each other freely.
type String struct{}

func TestTheSingleDomainFormIsTheSimpleOne(t *testing.T) {
	clip := ecs.HashOf[String]("Walk")
	model := ecs.HashOf[String]("models/crate.glb")
	// Both are ecs.Hash[String], so this compiles. With separate domains it
	// would not, and that is the whole of what the parameter buys.
	clip = model
	_ = clip
	if reflect.TypeFor[ecs.Hash[String]]() == reflect.TypeFor[ecs.Hash[Clip]]() {
		t.Fatalf("Hash[String] and Hash[Clip] are the same type")
	}
	t.Logf("one tag is the simple form; separate tags cost the same and add a compile error")
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
	table := ecs.NewNames[Model, int]()
	for i := range 8 {
		if err := table.Register(fmt.Sprintf("models/declared_%d.glb", i), i); err != nil {
			t.Fatal(err)
		}
	}
	before := table.Len()

	// Everything a running game might hash: procedural ids, player input, names
	// out of a save file. None of it was declared.
	for i := range 1_000_000 {
		h := ecs.HashOf[Model](fmt.Sprintf("runtime/generated/%d", i))
		if _, ok := table.Lookup(h); ok {
			t.Fatalf("an undeclared name resolved")
		}
	}
	if table.Len() != before {
		t.Fatalf("the table grew from %d to %d by being asked questions", before, table.Len())
	}
	t.Logf("1 000 000 hashes of undeclared strings; the table still holds %d entries", table.Len())
}
