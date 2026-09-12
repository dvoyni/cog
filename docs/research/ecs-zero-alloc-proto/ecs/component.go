package ecs

import (
	"fmt"
	"reflect"
)

// PointerFree reports whether a type may be a Component: CONTEXT.md's rule is
// that a Component "contains no pointers of any kind, transitively, which is
// checked when the type is registered" (cog#237), and this is that check.
//
// It is not a GC question but a copying one. A Component is copied into and out
// of a Store by value, may be replicated, and must stay meaningful after the
// thing it was copied from is gone -- so a string, a slice, a map, a channel, a
// func and an interface are all rejected along with a bare pointer. Each one
// would name memory the Store does not own and cannot keep alive.
//
// The error names the offending field by path rather than just the type,
// because the field that fails is usually several structs down: it is scene's
// Transform.Matrix, not the Component the author wrote.
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
			f := t.Field(i)
			if err := pointerFree(f.Type, path+"."+f.Name); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf(
			"%s is a %s, which is not pointer-free: a Component carries no pointer of any kind, transitively (cog#237)",
			path, t.Kind())
	}
}
