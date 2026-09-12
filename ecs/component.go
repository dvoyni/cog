package ecs

import (
	"fmt"
	"reflect"
)

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
