package m

import (
	"encoding/json"
	"iter"
	"slices"
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

// A List crosses JSON as the array of its elements, so a Component holding one
// reads as its data rather than as {}: empty is [], never null, and a List of
// Lists nests through the same method.
func TestAListEncodesAsTheArrayOfItsElements(t *testing.T) {
	type point struct{ X, Y int }
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"the zero List", List[int]{}, `[]`},
		{"an empty List", ListOf([]int{}), `[]`},
		{"a populated List", NewList(1, 2, 3), `[1,2,3]`},
		{"a List of structs", NewList(point{1, 2}), `[{"X":1,"Y":2}]`},
		{"nested Lists", NewList(NewList(1), List[int]{}, NewList(2, 3)), `[[1],[],[2,3]]`},
		{"a List in a struct", struct{ Items List[string] }{NewList("a")}, `{"Items":["a"]}`},
	}
	for _, c := range cases {
		encoded, err := json.Marshal(c.value)
		if err != nil {
			t.Fatalf("%s: marshal: %v", c.name, err)
		}
		if got := string(encoded); got != c.want {
			t.Errorf("%s encoded as %s, want %s", c.name, got, c.want)
		}
	}
}

// A List decodes from the array it encodes as, into an array of its own: what
// was there before is replaced rather than written through, so a List decoded
// over a copy of a stored one leaves the Store's array untouched. [] is the
// zero List, and null leaves the List as it was, as encoding/json leaves every
// other value it meets null for.
func TestAListDecodesFromAnArrayIntoAnArrayOfItsOwn(t *testing.T) {
	type holder struct{ Items List[int] }
	stored := NewList(7, 8, 9)
	decoded := holder{Items: stored}
	if err := json.Unmarshal([]byte(`{"Items":[1,2]}`), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := slices.Collect(listValues(decoded.Items)); !slices.Equal(got, []int{1, 2}) {
		t.Errorf("decoded %v, want [1 2]", got)
	}
	if got := slices.Collect(listValues(stored)); !slices.Equal(got, []int{7, 8, 9}) {
		t.Errorf("the List decoded over became %v; it must keep [7 8 9]", got)
	}

	nested := List[List[int]]{}
	if err := json.Unmarshal([]byte(`[[1],[],[2,3]]`), &nested); err != nil {
		t.Fatalf("decode nested: %v", err)
	}
	if encoded, _ := json.Marshal(nested); string(encoded) != `[[1],[],[2,3]]` {
		t.Errorf("nested Lists round-tripped as %s", encoded)
	}

	cleared := NewList(1)
	if err := json.Unmarshal([]byte(`[]`), &cleared); err != nil {
		t.Fatalf("decode []: %v", err)
	}
	if cleared.Len() != 0 || cleared.data != nil {
		t.Errorf("[] decoded to a List of %d with an array; want the zero List", cleared.Len())
	}
	kept := NewList(1)
	if err := json.Unmarshal([]byte(`null`), &kept); err != nil {
		t.Fatalf("decode null: %v", err)
	}
	if kept.Len() != 1 {
		t.Errorf("null left a List of %d; want the List it was", kept.Len())
	}

	var refused List[int]
	if err := json.Unmarshal([]byte(`{"len":3}`), &refused); err == nil {
		t.Error("an object decoded into a List; want an error")
	}
}

func listValues[T any](l List[T]) iter.Seq[T] {
	return func(yield func(T) bool) {
		for _, v := range l.All() {
			if !yield(v) {
				return
			}
		}
	}
}
