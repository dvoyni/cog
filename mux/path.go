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
// MakePath, into the offsets and the steps through arrays and slices that
// reach it. It holds a pointer to its compiled form, which nothing changes
// once the kernel starts, so a path may live in a plugin's field or a package
// variable. A path keeps no pointer into the model: every read starts from the
// model it is given, so it reads a slice the model moved to a new array, and a
// copy of the model, as it reads the model. Paths only read: nothing writes
// the model through one.
type Path[M, T any] struct {
	compiled *compiledPath
}

// MakePath compiles path, a route through M to a property of type T, written
// as the names of the fields it crosses joined by dots, each followed by an
// index for every array or slice it then crosses:
// "Squads[].Members[2].Position". An empty index, [], is a hole, which every
// read and override of the path fills with an int64; a literal index compiles
// into the path and leaves no hole. It is called from the Init of a plugin
// listed after mux's, once M is registered; reflection runs here, and nowhere
// a path is read.
//
// A hole in a slice picks an element by its index, so the game moves no
// element of a slice while an override addresses it: it frees elements into
// a free list, or marks them dead, rather than closing the gaps.
//
// It is a composition error, and panics, to make a path before mux's plugin
// or after Init, for a model that is not registered, that is malformed,
// through a field that does not exist, indexing what is no array or slice, or
// an array past its length, through or to a pointer or an interface, with
// more holes than M's depth holds, to a property that is not a T, or
// overlapping a path of M made before, one reaching a property the other
// reaches or contains.
func MakePath[M, T any](configurator *kernel.Configurator, path string) Path[M, T] {
	modelType := reflect.TypeFor[M]()
	registry := findRegistry(configurator, "path %q of model %v is made", path, modelType)
	registration, ok := registry.models[modelType]
	if !ok {
		panic(fmt.Sprintf("mux: path %q is made for %v, which is not a registered model", path, modelType))
	}
	compiled := compilePath(modelType, path)
	compiled.id = len(registration.paths)
	if compiled.holeCount > registration.depthType.Len() {
		panic(fmt.Sprintf("mux: path %q of model %v has more holes, %d, than its depth %v holds", path, modelType, compiled.holeCount, registration.depthType))
	}
	if want := reflect.TypeFor[T](); compiled.propertyType != want {
		panic(fmt.Sprintf("mux: path %q of model %v reaches %v, not %v", path, modelType, compiled.propertyType, want))
	}
	compiled.interpolator = makeBuiltInInterpolator[T]()
	for _, earlier := range registration.paths {
		sharedLength := min(len(earlier.crossedIndices), len(compiled.crossedIndices))
		// A hole may pick any element, so it meets every index at its place.
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
// holes fill, and true, or false when an index misses: a hole outside its
// array or slice, or a literal index past its slice's end.
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
	// within its array or slice, holeIndex at a hole.
	crossedIndices []int
	// steps holds the path's step through each slice it crosses, and through
	// each array it crosses at a hole, in the path's order.
	steps []pathStep
	// holeCount is how many holes the path has: how many of its steps are at
	// a hole.
	holeCount int
	// offset is where the property lies from the element the last step
	// picks, or from the model's base when the path has no step.
	offset uintptr
	// propertyType is the type of the property the path reaches.
	propertyType reflect.Type
	// interpolator is the interpolator[T] that tweens the property, nil when
	// none does. MakePath sets the one built in for the property's kind, and
	// the registry, once Init is over, the one a game registered for its
	// type, if any.
	interpolator any
}

// holeIndex stands in a path's crossed indices, and in the index of its
// steps, for the index of the element a hole picks, which may be any.
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
			indices = rest
			indexedType := current
			step := pathStep{kind: indexedType.Kind(), offset: compiled.offset, index: holeIndex}
			switch step.kind {
			case reflect.Array:
				step.length = int64(indexedType.Len())
			case reflect.Slice:
				// A slice's length is read from its header, on every read.
			default:
				panic(fmt.Sprintf("mux: path %q of model %v reaches %v, which is no array or slice to index", text, modelType, indexedType))
			}
			cross("an element of "+indexedType.String(), indexedType.Elem())
			step.elementSize = current.Size()
			if index == "" {
				compiled.holeCount++
			} else {
				// One bit short of an int, so a literal index fits an int, as
				// Go's own indices do.
				literal, err := strconv.ParseUint(index, 10, strconv.IntSize-1)
				if err != nil {
					panic(fmt.Sprintf("mux: path %q of model %v indexes %v with %q, which is no index", text, modelType, indexedType, index))
				}
				step.index = int64(literal)
			}
			compiled.crossedIndices = append(compiled.crossedIndices, int(step.index))
			if step.kind == reflect.Array && step.index != holeIndex {
				if step.index >= step.length {
					panic(fmt.Sprintf("mux: path %q of model %v indexes %v at %d, past its length %d", text, modelType, indexedType, step.index, step.length))
				}
				// An array lies within its element, so a literal index of one
				// compiles into the offset and takes no step. A slice's elements
				// lie wherever its header points, so even a literal index of one
				// takes a step.
				compiled.offset += uintptr(step.index) * step.elementSize
				continue
			}
			compiled.steps = append(compiled.steps, step)
			compiled.offset = 0
		}
	}
	compiled.propertyType = current
	return compiled
}

// findProperty answers where the property p names lies in the model at
// model, at the address holes fill, and true, or false when an index misses:
// a hole outside its array or slice, or a literal index past its slice's end.
// Holes p does not take panic with a holeMismatchError.
func (p *compiledPath) findProperty(model unsafe.Pointer, holes []int64) (unsafe.Pointer, bool) {
	if len(holes) != p.holeCount {
		panic(holeMismatchError{path: p, givenCount: len(holes)})
	}
	reached := model
	nextHoleIndex := 0
	for _, step := range p.steps {
		index := step.index
		if index == holeIndex {
			index = holes[nextHoleIndex]
			nextHoleIndex++
		}
		elements := unsafe.Add(reached, step.offset)
		length := step.length
		if step.kind == reflect.Slice {
			// Every slice's header is laid out alike, so it reads as a
			// []byte's: its length counts its elements, and its data is
			// where they lie.
			slice := *(*[]byte)(elements)
			elements = unsafe.Pointer(unsafe.SliceData(slice))
			length = int64(len(slice))
		}
		// No length is negative, so one unsigned compare misses a negative
		// index too.
		if uint64(index) >= uint64(length) {
			return nil, false
		}
		reached = unsafe.Add(elements, uintptr(index)*step.elementSize)
	}
	return unsafe.Add(reached, p.offset), true
}

// makeHoles answers the holes of the address p names with holes, as a depth
// D, whose places past p's holes stay zero. D is the depth of p's model,
// which MakePath checked holds every hole of p. Holes p does not take panic
// with a holeMismatchError.
func (p *compiledPath) makeHoles[D comparable](holes []int64) D {
	if len(holes) != p.holeCount {
		panic(holeMismatchError{path: p, givenCount: len(holes)})
	}
	var filledHoles D
	// A loop, not copy: copy calls memmove, which is slower over a few holes.
	for index, hole := range holes {
		*(*int64)(unsafe.Add(unsafe.Pointer(&filledHoles), uintptr(index)*unsafe.Sizeof(hole))) = hole
	}
	return filledHoles
}

// pathStep is a path's step through a slice, or through an array at a hole:
// a literal index of an array compiles into the offset instead.
type pathStep struct {
	// kind is the kind of what the step crosses: reflect.Array, at a hole, or
	// reflect.Slice, at a hole or a literal index.
	kind reflect.Kind
	// offset is where the array, or the slice's header, lies from the
	// element the step before picks, or from the model's base at the first
	// step.
	offset uintptr
	// index is the index of the element the step picks: a literal one, or
	// holeIndex at a hole, which the next of the holes given fills.
	index int64
	// length is the array's length, past which an index misses. A slice's
	// is read from its header.
	length int64
	// elementSize is the size of the elements.
	elementSize uintptr
}
