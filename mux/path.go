package mux

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
)

// Path names a property of type T in model M: a path string compiled once, by
// MakePath, into the offsets and the steps through arrays, slices and maps
// that reach it. It holds a pointer to its compiled form, which nothing
// changes once the kernel starts, so a path may live in a plugin's field or a
// package variable. A path keeps no pointer into the model: every read starts
// from the model it is given, so it reads a slice the model moved to a new
// array, a map the model replaced, and a copy of the model, as it reads the
// model. Paths only read: nothing writes the model through one.
type Path[M, T any] struct {
	compiled *compiledPath
}

// MakePath compiles path, a route through M to a property of type T, written
// as the names of the fields it crosses joined by dots, each followed by an
// index for every array, slice or map it then crosses:
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
// A map is keyed by any comparable type, with nothing registered for it. A
// hole in a map keyed by an integer type is the key itself, which a literal
// index may name too. A hole in a map keyed by any other type is the index
// the model's timeline interned the key at, with WriteTimeline.Intern; making
// the path makes the timeline's table for that key type.
//
// It is a composition error, and panics, to make a path before mux's plugin
// or after Init, for a model that is not registered, that is malformed,
// through a field that does not exist, indexing what is no array, slice or
// map, an array past its length, or a map whose keys are interned, or one of
// integer keys at a key its type does not hold, through or to a pointer or
// an interface, or keyed by one, with more holes than M's depth holds, to a
// property that is not a T, or overlapping a path of M made before, one
// reaching a property the other reaches or contains.
func MakePath[M, T any](configurator *kernel.Configurator, path string) Path[M, T] {
	modelType := reflect.TypeFor[M]()
	registry := findRegistry(configurator, "path %q of model %v is made", path, modelType)
	registration, ok := registry.models[modelType]
	if !ok {
		panic(fmt.Sprintf("mux: path %q is made for %v, which is not a registered model", path, modelType))
	}
	compiled := compilePath(modelType, path, registration.keyTables)
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
// array or slice, a literal index past its slice's end, or a key its map
// lacks, a nil map included. A hole in a map of interned keys misses too
// where no key was interned, and one in a map of integer keys where no key
// of its type is that hole. A property in a map is read in place, and only
// it is copied out.
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
	// within its array, slice or map, holeIndex at a hole.
	crossedIndices []int
	// steps holds the path's step through each slice and map it crosses, and
	// through each array it crosses at a hole, in the path's order.
	steps []pathStep
	// holeCount is how many holes the path has: how many of its steps are at
	// a hole.
	holeCount int
	// beforeLastHole and afterLastHole are the path's steps before the step
	// at its last hole and after it, as paths of their own, which All follows
	// to what the last hole walks, and from each element it walks to the
	// property. Both are nil when the path has no hole.
	beforeLastHole *compiledPath
	afterLastHole  *compiledPath
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

// compilePath compiles text over modelType, adding to keyTables the table
// for the key type of each map it crosses whose keys are interned, unless
// one is there. It panics naming what is wrong.
func compilePath(modelType reflect.Type, text string, keyTables *[]*keyTable) *compiledPath {
	compiled := &compiledPath{text: text}
	// lastHoleStepIndex is the index among the steps of the step at the
	// last hole, -1 until a hole is met.
	lastHoleStepIndex := -1
	current := modelType
	checkCrossed := func(crossed string, crossedType reflect.Type) {
		switch crossedType.Kind() {
		case reflect.Pointer, reflect.UnsafePointer:
			panic(fmt.Sprintf("mux: path %q of model %v crosses %s, a pointer %v", text, modelType, crossed, crossedType))
		case reflect.Interface:
			panic(fmt.Sprintf("mux: path %q of model %v crosses %s, an interface %v", text, modelType, crossed, crossedType))
		}
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
			checkCrossed("field "+crossed.Name, crossed.Type)
			current = crossed.Type
		}
		for indices != "" {
			index, rest, closed := strings.Cut(indices[1:], "]")
			if indices[0] != '[' || !closed {
				panic(fmt.Sprintf("mux: path %q of model %v is malformed at %q", text, modelType, indices))
			}
			indices = rest
			indexedType := current
			step := pathStep{kind: indexedType.Kind(), offset: compiled.offset, index: holeIndex}
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
			switch step.kind {
			case reflect.Array:
				step.length = int64(indexedType.Len())
				if step.index != holeIndex && step.index >= step.length {
					panic(fmt.Sprintf("mux: path %q of model %v indexes %v at %d, past its length %d", text, modelType, indexedType, step.index, step.length))
				}
				step.elementSize = indexedType.Elem().Size()
			case reflect.Slice:
				// A slice's length is read from its header, on every read.
				step.elementSize = indexedType.Elem().Size()
			case reflect.Map:
				keyType := indexedType.Key()
				checkCrossed("a key of "+indexedType.String(), keyType)
				// A reflect.Type is a pointer to the runtime's descriptor of
				// its type.
				step.mapType = reflect.ValueOf(indexedType).UnsafePointer()
				// The integer kinds run from reflect.Int to reflect.Uintptr, the
				// signed ones up to reflect.Int64. A key of any other kind is
				// interned.
				step.mapReflectType = indexedType
				step.mapAccess = makeMapAccess(indexedType)
				step.elementSize = keyType.Size()
				keyKind := keyType.Kind()
				if reflect.Int <= keyKind && keyKind <= reflect.Uintptr {
					// A key as wide as an int64 is any index, as the int64
					// its bits make.
					keyBits := 8 * keyType.Size()
					step.keyMinimum = math.MinInt64
					step.keyMaximum = math.MaxInt64
					if keyBits < 64 {
						step.keyMinimum = 0
						step.keyMaximum = 1<<keyBits - 1
						if keyKind <= reflect.Int64 {
							step.keyMinimum = -1 << (keyBits - 1)
							step.keyMaximum = 1<<(keyBits-1) - 1
						}
					}
					// A narrower key's bytes are the int64's low-order ones,
					// which a big-endian machine lays out last.
					one := int64(1)
					if *(*byte)(unsafe.Pointer(&one)) == 0 {
						step.keyOffset = 8 - keyType.Size()
					}
					if step.index != holeIndex && (step.index < step.keyMinimum || step.keyMaximum < step.index) {
						panic(fmt.Sprintf("mux: path %q of model %v indexes %v at %d, which no %v holds", text, modelType, indexedType, step.index, keyType))
					}
				} else {
					// An interned key's index lives one run, so no literal
					// names one.
					if step.index != holeIndex {
						panic(fmt.Sprintf("mux: path %q of model %v indexes %v at %d, but a map whose keys are interned takes only a hole", text, modelType, indexedType, step.index))
					}
					tableIndex := slices.IndexFunc(*keyTables, func(table *keyTable) bool { return table.keyType == keyType })
					if tableIndex < 0 {
						tableIndex = len(*keyTables)
						*keyTables = append(*keyTables, newKeyTable(keyType))
					}
					table := (*keyTables)[tableIndex]
					step.keys = reflect.ValueOf(table.keys).UnsafePointer()
					keyIndicesType := reflect.TypeOf(table.indices)
					step.keyIndicesType = reflect.ValueOf(keyIndicesType).UnsafePointer()
					step.keyIndices = reflect.ValueOf(table.indices).UnsafePointer()
					step.keyIndicesAccess = makeMapAccess(keyIndicesType)
				}
			default:
				panic(fmt.Sprintf("mux: path %q of model %v reaches %v, which is no array, slice or map to index", text, modelType, indexedType))
			}
			checkCrossed("an element of "+indexedType.String(), indexedType.Elem())
			current = indexedType.Elem()
			compiled.crossedIndices = append(compiled.crossedIndices, int(step.index))
			if step.kind == reflect.Array && step.index != holeIndex {
				// An array lies within its element, so a literal index of one
				// compiles into the offset and takes no step. A slice's or a
				// map's elements lie wherever its header or its slots hold
				// them, so even a literal index of one takes a step.
				compiled.offset += uintptr(step.index) * step.elementSize
				continue
			}
			if step.index == holeIndex {
				lastHoleStepIndex = len(compiled.steps)
			}
			compiled.steps = append(compiled.steps, step)
			compiled.offset = 0
		}
	}
	compiled.propertyType = current
	if lastHoleStepIndex >= 0 {
		compiled.beforeLastHole = &compiledPath{text: text, steps: compiled.steps[:lastHoleStepIndex], holeCount: compiled.holeCount - 1}
		compiled.afterLastHole = &compiledPath{text: text, steps: compiled.steps[lastHoleStepIndex+1:], offset: compiled.offset}
	}
	return compiled
}

// findProperty answers where the property p names lies in the model at
// model, at the address holes fill, and true, or false when an index misses:
// a hole outside its array or slice, a literal index past its slice's end,
// or a key its map lacks. Holes p does not take panic with a
// holeMismatchError.
func (p *compiledPath) findProperty(model unsafe.Pointer, holes []int64) (unsafe.Pointer, bool) {
	if len(holes) != p.holeCount {
		panic(holeMismatchError{path: p, givenCount: len(holes)})
	}
	reached := model
	nextHoleIndex := 0
	// A step is read in place, not copied, and through a local slice, which
	// the call into a map step leaves as it is.
	steps := p.steps
	for stepIndex := range steps {
		step := &steps[stepIndex]
		index := step.index
		if index == holeIndex {
			index = holes[nextHoleIndex]
			nextHoleIndex++
		}
		elements := unsafe.Add(reached, step.offset)
		length := step.length
		switch step.kind {
		case reflect.Slice:
			// Every slice's header is laid out alike, so it reads as a
			// []byte's: its length counts its elements, and its data is
			// where they lie.
			slice := *(*[]byte)(elements)
			elements = unsafe.Pointer(unsafe.SliceData(slice))
			length = int64(len(slice))
		case reflect.Map:
			// The element is read in place, nothing copied out of the map,
			// through the access a typed lookup of the map takes. It is
			// dispatched here, not in a function of its own, which would not
			// inline: the call saved is a tenth of a map step.
			key, ok := step.findKey(&index)
			if !ok {
				return nil, false
			}
			mapPointer := *(*unsafe.Pointer)(elements)
			// A fast access answers a pointer to a zero value for a key the
			// map lacks, and false.
			switch step.mapAccess {
			case mapAccessFast32:
				reached, ok = accessMapFast32(step.mapType, mapPointer, *(*uint32)(key))
			case mapAccessFast64:
				reached, ok = accessMapFast64(step.mapType, mapPointer, *(*uint64)(key))
			case mapAccessFastString:
				reached, ok = accessMapFastString(step.mapType, mapPointer, *(*string)(key))
			default:
				reached = accessMap(step.mapType, mapPointer, key)
				ok = reached != nil
			}
			if !ok {
				return nil, false
			}
			continue
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

// fillHoles writes holes, the holes of the address p names, into the first
// places of filledHoles, a depth D, and leaves its places past them as they
// are: a depth declared fresh, as every caller's is, holds zero there. D is
// the depth of p's model, which MakePath checked holds every hole of p. Holes
// p does not take panic with a holeMismatchError.
//
// It fills the caller's depth in place, rather than answering one: a depth
// built aside is copied in wider moves than the hole's own, each of which
// would wait for the writes it spans to land.
func (p *compiledPath) fillHoles[D comparable](filledHoles *D, holes []int64) {
	if len(holes) != p.holeCount {
		panic(holeMismatchError{path: p, givenCount: len(holes)})
	}
	// A loop, not copy: copy calls memmove, which is slower over a few holes.
	for index, hole := range holes {
		*(*int64)(unsafe.Add(unsafe.Pointer(filledHoles), uintptr(index)*unsafe.Sizeof(hole))) = hole
	}
}
