package ecs

import (
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

type inner struct {
	Path string
}

type middle struct {
	Inner inner
}

type pathedDrawable struct {
	Model uint64
	Deep  middle
}

type cage struct {
	Occupants [4]*inner
}

func TestPointerFreeAcceptsWhatAComponentMayHold(t *testing.T) {
	type legal struct {
		Flag      bool
		Count     int32
		Weight    float64
		Name      [32]byte
		Target    Entity
		Nested    struct{ X, Y float32 }
		Unsized   struct{}
		Addressed uintptr
	}
	cases := []struct {
		name string
		t    reflect.Type
	}{
		{"a tag", reflect.TypeFor[disabled]()},
		{"a plain value", reflect.TypeFor[position]()},
		{"an entity reference", reflect.TypeFor[Entity]()},
		{"a bounded run of references", reflect.TypeFor[[4]Entity]()},
		{"every permitted kind at once", reflect.TypeFor[legal]()},
	}
	for _, test := range cases {
		if err := PointerFree(test.t); err != nil {
			t.Fatalf("PointerFree(%s) = %v, want nil", test.name, err)
		}
	}
}

func TestPointerFreeRejectsEveryKindThatNamesMemoryTheStoreDoesNotOwn(t *testing.T) {
	cases := []struct {
		kind string
		t    reflect.Type
	}{
		{"pointer", reflect.TypeFor[*inner]()},
		{"unsafe.Pointer", reflect.TypeFor[unsafe.Pointer]()},
		{"slice", reflect.TypeFor[[]byte]()},
		{"map", reflect.TypeFor[map[string]int]()},
		{"chan", reflect.TypeFor[chan int]()},
		{"func", reflect.TypeFor[func()]()},
		{"interface", reflect.TypeFor[any]()},
		{"string", reflect.TypeFor[string]()},
	}
	for _, test := range cases {
		err := PointerFree(test.t)
		if err == nil {
			t.Fatalf("PointerFree(%s) = nil, want a rejection", test.kind)
		}
		if !strings.Contains(err.Error(), "not pointer-free") {
			t.Fatalf("PointerFree(%s) = %q, want it to say what the rule is", test.kind, err)
		}
	}
}

// The field that fails is usually several structs down, so naming only the
// Component is useless. This is the diagnostic the spec writes out.
func TestPointerFreeNamesTheOffendingFieldByPath(t *testing.T) {
	err := PointerFree(reflect.TypeFor[pathedDrawable]())
	if err == nil {
		t.Fatalf("PointerFree(pathedDrawable) = nil, want a rejection: it holds a string two structs down")
	}
	const want = "ecs.pathedDrawable.Deep.Inner.Path is a string, which is not pointer-free"
	if !strings.HasPrefix(err.Error(), want) {
		t.Fatalf("PointerFree(pathedDrawable) = %q, want it to begin %q", err, want)
	}
}

func TestPointerFreeWalksIntoArrayElements(t *testing.T) {
	err := PointerFree(reflect.TypeFor[cage]())
	if err == nil {
		t.Fatalf("PointerFree(cage) = nil, want a rejection: its array holds pointers")
	}
	if !strings.HasPrefix(err.Error(), "ecs.cage.Occupants[_] is a ptr") {
		t.Fatalf("PointerFree(cage) = %q, want the array element named", err)
	}
}

// A type that is itself illegal has no field to name, and the message says so
// without inventing one — this is scene.Material, which is a slice.
func TestPointerFreeNamesTheTypeWhenTheTypeItselfIsIllegal(t *testing.T) {
	type material []byte
	err := PointerFree(reflect.TypeFor[material]())
	if err == nil {
		t.Fatalf("PointerFree(material) = nil, want a rejection")
	}
	if !strings.HasPrefix(err.Error(), "ecs.material is a slice") {
		t.Fatalf("PointerFree(material) = %q, want the type named", err)
	}
}
