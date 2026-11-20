package canvas

import (
	"fmt"
	"reflect"

	"github.com/dvoyni/cog/plugins/render"
)

// ArgumentType is the Go type of a 2D material's argument, the value each
// of its entries gives it, made by ArgumentOf; the zero ArgumentType is
// none.
type ArgumentType struct {
	goType reflect.Type
	// checkLayout answers why a value of goType cannot be the member
	// argument of an element of shader's canvas_instances, nil when it can.
	checkLayout func(inventory render.Inventory, shader render.Shader) error
}

// ArgumentOf answers T as a 2D material's argument type: a struct of any
// number of fields, or a plain value such as a float32, plain data with no
// pointers mirroring the shader's WGSL struct Argument, by the rules
// render's Inventory.CheckLayout checks it by: of its size, a struct's
// fields at its members' offsets, in order, names ignored and fields named _
// left out as padding. An entry gives it with DrawSpriteWith,
// DrawSpritesWith or DrawShapeWith, and a fragment stage reads it with the
// include's canvas_argument. It panics on a T holding a pointer, a string,
// slice, map, channel, function or interface anywhere within it, which
// canvas, copying the argument's bytes to the GPU, cannot carry.
func ArgumentOf[T any]() ArgumentType {
	goType := reflect.TypeFor[T]()
	if holdsPointer(goType) {
		panic(fmt.Sprintf("canvas: a material's argument type %v holds a pointer, which instance data cannot carry", goType))
	}
	return ArgumentType{goType: goType, checkLayout: checkArgumentLayout[T]}
}

// holdsPointer answers whether a value of goType holds a pointer anywhere
// within it, as a string, slice, map, channel, function or interface does.
func holdsPointer(goType reflect.Type) bool {
	switch goType.Kind() {
	case reflect.Pointer, reflect.UnsafePointer, reflect.String, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func, reflect.Interface:
		return true
	case reflect.Array:
		return holdsPointer(goType.Elem())
	case reflect.Struct:
		for field := range goType.Fields() {
			if holdsPointer(field.Type) {
				return true
			}
		}
	}
	return false
}

// checkArgumentLayout answers why a value of T cannot be the member argument
// of an element of shader's canvas_instances, through inventory, nil when it
// can.
func checkArgumentLayout[T any](inventory render.Inventory, shader render.Shader) error {
	return inventory.CheckLayout[T](shader, instancesBindingName, "[].argument")
}
