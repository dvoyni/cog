package mux

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
)

// Path names a property of type T in model M: a path string compiled once, by
// MakePath, into the offset that reaches it. It holds a pointer to its
// compiled form, which nothing changes once Init is over, so a path may live
// in a plugin's field or a package variable. Paths only read: nothing writes
// the model through one.
type Path[M, T any] struct {
	compiled *compiledPath
}

// MakePath compiles path, a route through M's struct fields to a property of
// type T, written as the fields' names joined by dots: "Hero.Position". It is
// called from the Init of a plugin listed after mux's, once M is registered;
// reflection runs here, and nowhere a path is read.
//
// It is a composition error, and panics, to make a path before mux's plugin
// or after Init, for a model that is not registered, through a field that
// does not exist, through or to a pointer or an interface, to a property that
// is not a T, or overlapping a path of M made before, one reaching a field
// the other reaches or contains.
func MakePath[M, T any](configurator *kernel.Configurator, path string) Path[M, T] {
	modelType := reflect.TypeFor[M]()
	registry := findRegistry(configurator, "path %q of model %v is made", path, modelType)
	registration, ok := registry.models[modelType]
	if !ok {
		panic(fmt.Sprintf("mux: path %q is made for %v, which is not a registered model", path, modelType))
	}
	compiled, propertyType := compilePath(modelType, path)
	if want := reflect.TypeFor[T](); propertyType != want {
		panic(fmt.Sprintf("mux: path %q of model %v reaches %v, not %v", path, modelType, propertyType, want))
	}
	for _, earlier := range registration.paths {
		sharedLength := min(len(earlier.fieldIndices), len(compiled.fieldIndices))
		if slices.Equal(earlier.fieldIndices[:sharedLength], compiled.fieldIndices[:sharedLength]) {
			panic(fmt.Sprintf("mux: paths %q and %q of model %v overlap", earlier.text, path, modelType))
		}
	}
	registration.paths = append(registration.paths, compiled)
	return Path[M, T]{compiled: compiled}
}

// Get answers the model's own value of the property p names, and true: a
// path of fields always reaches its property.
func (p Path[M, T]) Get(model *M) (T, bool) {
	return *(*T)(unsafe.Add(unsafe.Pointer(model), p.compiled.offset)), true
}

// compiledPath is a path MakePath compiled.
type compiledPath struct {
	// text is the path as it was made.
	text string
	// fieldIndices is the index of each field the path crosses within its
	// struct, embedded fields a promoted name passes through included, so two
	// spellings of one field compile alike.
	fieldIndices []int
	// offset is where the property lies from the model's base.
	offset uintptr
}

// compilePath compiles text over modelType, and answers it with the type of
// the property it reaches. It panics naming what is wrong.
func compilePath(modelType reflect.Type, text string) (*compiledPath, reflect.Type) {
	compiled := &compiledPath{text: text}
	current := modelType
	for name := range strings.SplitSeq(text, ".") {
		if current.Kind() != reflect.Struct {
			panic(fmt.Sprintf("mux: path %q of model %v reaches %v, which is no struct to hold field %q", text, modelType, current, name))
		}
		field, ok := current.FieldByName(name)
		if !ok {
			panic(fmt.Sprintf("mux: path %q of model %v reaches %v, which has no field %q", text, modelType, current, name))
		}
		for _, index := range field.Index {
			crossed := current.Field(index)
			compiled.fieldIndices = append(compiled.fieldIndices, index)
			compiled.offset += crossed.Offset
			current = crossed.Type
			switch current.Kind() {
			case reflect.Pointer, reflect.UnsafePointer:
				panic(fmt.Sprintf("mux: path %q of model %v crosses field %s, a pointer %v", text, modelType, crossed.Name, current))
			case reflect.Interface:
				panic(fmt.Sprintf("mux: path %q of model %v crosses field %s, an interface %v", text, modelType, crossed.Name, current))
			}
		}
	}
	return compiled, current
}
