package container

import "reflect"

// IsPointerFree reports whether t holds no pointer at any depth: no pointer,
// unsafe pointer, slice, map, string, interface, channel or func. Numbers,
// bools, and arrays and structs of those are pointer-free.
func IsPointerFree(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return true
	case reflect.Array:
		return IsPointerFree(t.Elem())
	case reflect.Struct:
		for i := range t.NumField() {
			if !IsPointerFree(t.Field(i).Type) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
