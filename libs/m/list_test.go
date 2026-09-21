package m

import (
	"testing"
	"unsafe"
)

func TestAListCopiesRatherThanAdoptingWhatItWasBuiltFrom(t *testing.T) {
	source := []uint32{1, 2, 3}
	list := ListOf(source)
	source[0] = 99
	if got := list.At(0); got != 1 {
		t.Fatalf("the List saw the caller's write: element 0 is %d, want 1", got)
	}
	if list.Len() != 3 {
		t.Fatalf("Len is %d, want 3", list.Len())
	}
	if empty := ListOf[uint32](nil); empty.Len() != 0 {
		t.Fatalf("the zero List has length %d, want 0", empty.Len())
	}
	seen := 0
	for i, value := range NewList[uint32](7, 8).All() {
		if value != uint32(7+i) {
			t.Fatalf("element %d is %d", i, value)
		}
		seen++
	}
	if seen != 2 {
		t.Fatalf("All yielded %d elements, want 2", seen)
	}
}

// TestAListHeaderIsThirtyTwoBytes pins the width every List user pays: the
// slice header and the generation Set adds one to. A wider header is a wider
// row in every ECS Store holding a List, so it changes only with its
// measurements in ecs.md § The List.
func TestAListHeaderIsThirtyTwoBytes(t *testing.T) {
	if size := unsafe.Sizeof(List[uint32]{}); size != 32 {
		t.Fatalf("a List header is %d bytes, want 32", size)
	}
}

// TestASetWritesTheElementAndMovesTheGeneration is the List's half of what a
// Changed Hook relies on: a Set changes the header, not only the backing array
// outside it, and does so even when the value written is the one already there.
func TestASetWritesTheElementAndMovesTheGeneration(t *testing.T) {
	list := NewList[uint32](1, 2)
	before := list.gen
	list.Set(0, 1)
	if list.gen == before {
		t.Fatal("a Set left the List header as it was")
	}
	list.Set(1, 5)
	if got := list.At(1); got != 5 {
		t.Fatalf("element 1 is %d, want 5", got)
	}
}
