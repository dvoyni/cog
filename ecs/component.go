package ecs

import (
	"fmt"
	"reflect"

	"github.com/dvoyni/cog/kernel"
)

// componentClass is one Component type as registration left it: how wide one
// row is, and the two baked closures that declare a lock on its Store and hand
// back the erased view a Query fills from.
//
// The closures exist because a generic cannot be instantiated from a
// reflect.Type. RegisterComponent[C] is generic, so the generic call is written
// once where C is a compile-time type and kept here; registration-time
// reflection looks it up by reflect.Type afterwards and never needs the
// instantiation at all.
type componentClass struct {
	size         uintptr
	declareRead  func(access kernel.ResourceAccess) func() *storeHeader
	declareWrite func(access kernel.ResourceAccess) func() *storeHeader
}

// RegisterComponent declares that C is a Component of this world, and is the
// only thing that makes a Store for it exist. It checks the pointer-free rule,
// creates the Store, enrols it with the authority so a despawn can empty it,
// hands it to the kernel as an ordinary resource of type *Store[C] — owned by
// the calling plugin — and bakes the per-type closures a Query is later planned
// against.
//
// Register a Component in the plugin that defines its Go type. That is what
// keeps the coupling check working on Component data: the Store is owned by
// that plugin, so a System elsewhere that locks it must declare a dependency on
// it, which the Go import graph already forces. Ownership by ecs would not make
// the check lenient, it would make it vacuous.
//
// Registration is explicit rather than derived, and half of that is forced: a
// Lock must bind every handle it will use and a declared resource with no
// initial value fails finalisation, so a Store that does not exist at
// registration cannot be locked and discovery-on-first-use is off the table.
//
// ids is the peak population hint the Store reserves for; it is not a cap.
//
// It panics if C is not pointer-free, naming the offending field by path. The
// plugin boundary turns that into a composition failure naming the plugin.
func RegisterComponent[C any](registrar *kernel.Registrar, en *Entities, ids uint32) *Store[C] {
	if en == nil {
		panic("ecs: RegisterComponent needs the Entities the Component belongs to")
	}
	componentType := reflect.TypeFor[C]()
	if err := PointerFree(componentType); err != nil {
		panic("ecs: " + err.Error())
	}
	store := NewStore[C](en, ids)
	registrar.InitResource(store)
	en.declare(componentType, &componentClass{
		size: componentType.Size(),
		declareRead: func(access kernel.ResourceAccess) func() *storeHeader {
			handle := access.GetRead[*Store[C]]()
			return func() *storeHeader { return handle.Get().erase() }
		},
		declareWrite: func(access kernel.ResourceAccess) func() *storeHeader {
			handle := access.GetWrite[*Store[C]]()
			return func() *storeHeader { return handle.Get().erase() }
		},
	})
	return store
}

// PointerFree reports whether a type may be a Component: a Component contains
// no pointers, transitively. That one sentence replaces "a plain copyable
// struct", and it is better because it is mechanically checkable — this walk,
// run once when the type is registered, where cost is irrelevant.
//
// It forbids pointers, slices, maps, channels, funcs, interfaces and strings,
// and permits numerics, bools, fixed-size arrays, Entity, and structs of those.
// The reason is copying before it is the collector: a Component is copied into
// and out of a Store by value and must stay meaningful after the thing it was
// copied from is gone, and every forbidden kind names memory the Store does not
// own and cannot keep alive. Being pointer-free is also what makes a Component
// trivially serialisable, and what puts the dense array in a span the mark
// phase never walks.
//
// The error names the offending field by path, because the field that fails is
// usually several structs down and naming only the Component is useless:
//
//	ecs: proto.PathedDrawable.Path is a string, which is not pointer-free
//
// Variable-length data has three answers instead: a child entity with an owning
// reference, a fixed-capacity array where the bound is small and real, or —
// for the name of an engine-side thing — a hash of that name, which is a plain
// number.
func PointerFree(t reflect.Type) error { return pointerFree(t, t.String()) }

func pointerFree(t reflect.Type, path string) error {
	switch t.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Uintptr, reflect.Float32, reflect.Float64,
		reflect.Complex64, reflect.Complex128:
		return nil
	case reflect.Array:
		return pointerFree(t.Elem(), path+"[_]")
	case reflect.Struct:
		for i := range t.NumField() {
			field := t.Field(i)
			if err := pointerFree(field.Type, path+"."+field.Name); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf(
			"%s is a %s, which is not pointer-free: a Component carries no pointer of any kind, transitively",
			path, t.Kind())
	}
}
