package mux

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
)

// Path names a property of type T in model M: a path string compiled once, by
// MakePath, into the offsets and array steps that reach it. It holds a pointer
// to its compiled form, which nothing changes once the kernel starts, so a
// path may live in a plugin's field or a package variable. Paths only read:
// nothing writes the model through one.
type Path[M, T any] struct {
	compiled *compiledPath
}

// MakePath compiles path, a route through M to a property of type T, written
// as the names of the fields it crosses joined by dots, each followed by an
// index for every array it then crosses: "Squads[].Members[2].Position". An
// empty index, [], is a hole, which every read and override of the path fills
// with an int64; a literal index compiles into the path and leaves no hole. It
// is called from the Init of a plugin listed after mux's, once M is
// registered; reflection runs here, and nowhere a path is read.
//
// It is a composition error, and panics, to make a path before mux's plugin
// or after Init, for a model that is not registered, that is malformed,
// through a field that does not exist, indexing what is no array or past its
// length, through or to a pointer or an interface, with more holes than M's
// depth holds, to a property that is not a T, or overlapping a path of M made
// before, one reaching a property the other reaches or contains.
func MakePath[M, T any](configurator *kernel.Configurator, path string) Path[M, T] {
	modelType := reflect.TypeFor[M]()
	registry := findRegistry(configurator, "path %q of model %v is made", path, modelType)
	registration, ok := registry.models[modelType]
	if !ok {
		panic(fmt.Sprintf("mux: path %q is made for %v, which is not a registered model", path, modelType))
	}
	compiled := compilePath(modelType, path)
	compiled.id = len(registration.paths)
	if len(compiled.arraySteps) > registration.depthType.Len() {
		panic(fmt.Sprintf("mux: path %q of model %v has more holes, %d, than its depth %v holds", path, modelType, len(compiled.arraySteps), registration.depthType))
	}
	if want := reflect.TypeFor[T](); compiled.propertyType != want {
		panic(fmt.Sprintf("mux: path %q of model %v reaches %v, not %v", path, modelType, compiled.propertyType, want))
	}
	compiled.interpolator = makeBuiltInInterpolator[T]()
	for _, earlier := range registration.paths {
		sharedLength := min(len(earlier.crossedIndices), len(compiled.crossedIndices))
		// A hole may pick any element, so it meets every index of its array.
		if slices.EqualFunc(earlier.crossedIndices[:sharedLength], compiled.crossedIndices[:sharedLength], func(earlierIndex, index int) bool {
			return earlierIndex == index || earlierIndex == holeIndex || index == holeIndex
		}) {
			panic(fmt.Sprintf("mux: paths %q and %q of model %v overlap", earlier.text, path, modelType))
		}
	}
	registration.paths = append(registration.paths, compiled)
	return Path[M, T]{compiled: compiled}
}

// Get answers the model's own value of the property p names, at the address
// holes fill, and true, or false when a hole misses: an index outside its
// array.
//
// Holes the path does not take panic, naming the path and their count.
func (p Path[M, T]) Get(model *M, holes ...int64) (T, bool) {
	property, ok := p.compiled.findProperty(unsafe.Pointer(model), holes)
	if !ok {
		var zero T
		return zero, false
	}
	return *(*T)(property), true
}

// compiledPath is a path MakePath compiled.
type compiledPath struct {
	// text is the path as it was made.
	text string
	// id is the path's index among its model's paths, in the order made: the
	// timeline keeps the path's overrides under it.
	id int
	// crossedIndices is the index of each field the path crosses within its
	// struct, embedded fields a promoted name passes through included, so two
	// spellings of one field compile alike, and of each element it crosses
	// within its array, holeIndex at a hole.
	crossedIndices []int
	// arraySteps holds the step of each hole, in the path's order.
	arraySteps []arrayStep
	// offset is where the property lies from the element the last hole
	// picks, or from the model's base when the path has no hole.
	offset uintptr
	// propertyType is the type of the property the path reaches.
	propertyType reflect.Type
	// interpolator is the interpolator[T] that tweens the property, nil when
	// none does. MakePath sets the one built in for the property's kind, and
	// the registry, once Init is over, the one a game registered for its
	// type, if any.
	interpolator any
}

// holeIndex stands in a path's crossed indices for the index of the element
// a hole picks, which may be any.
const holeIndex = -1

// compilePath compiles text over modelType. It panics naming what is wrong.
func compilePath(modelType reflect.Type, text string) *compiledPath {
	compiled := &compiledPath{text: text}
	current := modelType
	cross := func(crossed string, crossedType reflect.Type) {
		switch crossedType.Kind() {
		case reflect.Pointer, reflect.UnsafePointer:
			panic(fmt.Sprintf("mux: path %q of model %v crosses %s, a pointer %v", text, modelType, crossed, crossedType))
		case reflect.Interface:
			panic(fmt.Sprintf("mux: path %q of model %v crosses %s, an interface %v", text, modelType, crossed, crossedType))
		}
		current = crossedType
	}
	for segment := range strings.SplitSeq(text, ".") {
		nameLength := strings.IndexByte(segment, '[')
		if nameLength < 0 {
			nameLength = len(segment)
		}
		name := segment[:nameLength]
		indices := segment[nameLength:]
		if current.Kind() != reflect.Struct {
			panic(fmt.Sprintf("mux: path %q of model %v reaches %v, which is no struct to hold field %q", text, modelType, current, name))
		}
		field, ok := current.FieldByName(name)
		if !ok {
			panic(fmt.Sprintf("mux: path %q of model %v reaches %v, which has no field %q", text, modelType, current, name))
		}
		for _, index := range field.Index {
			crossed := current.Field(index)
			compiled.crossedIndices = append(compiled.crossedIndices, index)
			compiled.offset += crossed.Offset
			cross("field "+crossed.Name, crossed.Type)
		}
		for indices != "" {
			index, rest, closed := strings.Cut(indices[1:], "]")
			if indices[0] != '[' || !closed {
				panic(fmt.Sprintf("mux: path %q of model %v is malformed at %q", text, modelType, indices))
			}
			if current.Kind() != reflect.Array {
				panic(fmt.Sprintf("mux: path %q of model %v reaches %v, which is no array to index", text, modelType, current))
			}
			arrayType := current
			cross("an element of "+arrayType.String(), arrayType.Elem())
			if index == "" {
				compiled.crossedIndices = append(compiled.crossedIndices, holeIndex)
				compiled.arraySteps = append(compiled.arraySteps, arrayStep{offset: compiled.offset, length: int64(arrayType.Len()), elementSize: current.Size()})
				compiled.offset = 0
			} else {
				literal, err := strconv.ParseUint(index, 10, 64)
				if err != nil {
					panic(fmt.Sprintf("mux: path %q of model %v indexes %v with %q, which is no index", text, modelType, arrayType, index))
				}
				if literal >= uint64(arrayType.Len()) {
					panic(fmt.Sprintf("mux: path %q of model %v indexes %v at %d, past its length %d", text, modelType, arrayType, literal, arrayType.Len()))
				}
				compiled.crossedIndices = append(compiled.crossedIndices, int(literal))
				compiled.offset += uintptr(literal) * current.Size()
			}
			indices = rest
		}
	}
	compiled.propertyType = current
	return compiled
}

// findProperty answers where the property p names lies in the model at
// model, at the address holes fill, and true, or false when a hole misses:
// an index outside its array. Holes p does not take panic with a
// holeMismatchError.
func (p *compiledPath) findProperty(model unsafe.Pointer, holes []int64) (unsafe.Pointer, bool) {
	if len(holes) != len(p.arraySteps) {
		panic(holeMismatchError{path: p, givenCount: len(holes)})
	}
	reached := model
	for stepIndex, step := range p.arraySteps {
		hole := holes[stepIndex]
		if hole < 0 || hole >= step.length {
			return nil, false
		}
		reached = unsafe.Add(reached, step.offset+uintptr(hole)*step.elementSize)
	}
	return unsafe.Add(reached, p.offset), true
}

// makeHoles answers the holes of the address p names with holes, as a depth
// D, whose places past p's holes stay zero. D is the depth of p's model,
// which MakePath checked holds every hole of p. Holes p does not take panic
// with a holeMismatchError.
func (p *compiledPath) makeHoles[D comparable](holes []int64) D {
	if len(holes) != len(p.arraySteps) {
		panic(holeMismatchError{path: p, givenCount: len(holes)})
	}
	var filledHoles D
	// A loop, not copy: copy calls memmove, which is slower over a few holes.
	for index, hole := range holes {
		*(*int64)(unsafe.Add(unsafe.Pointer(&filledHoles), uintptr(index)*unsafe.Sizeof(hole))) = hole
	}
	return filledHoles
}

// arrayStep is a path's step through an array at a hole.
type arrayStep struct {
	// offset is where the array lies from the element the hole before picks,
	// or from the model's base at the first hole.
	offset uintptr
	// length is the array's length: a hole outside it misses.
	length int64
	// elementSize is the size of the array's elements.
	elementSize uintptr
}
