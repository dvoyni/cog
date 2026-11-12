// Package bindingcost holds throwaway benchmarks for dvoyni/cog#775: what binding
// a timed override to one property of a plain Go struct costs.
package bindingcost

import (
	"fmt"
	"reflect"
	"strings"
	"unsafe"
)

// Vec2 is a 2D position.
type Vec2 struct{ X, Y float32 }

// Camera is a nested struct of the model.
type Camera struct {
	Position Vec2
	Zoom     float32
}

// Unit is one element of the model's slice.
type Unit struct {
	ID       uint32
	Position Vec2
	Health   int32
}

// Prop is one value of the model's map.
type Prop struct {
	Position Vec2
	Angle    float32
}

// Model is a realistic plain-data model: a scalar, a nested struct, a slice of
// units and a map keyed by id.
type Model struct {
	Turn   int32
	Camera Camera
	Units  []Unit
	Props  map[uint32]Prop
}

// Key addresses one property: the id of its registered path plus fixed key slots
// for the slice indices or map keys along that path.
type Key struct {
	Path  uint32
	Slot0 uint64
	Slot1 uint64
}

// NewModel makes a model of count units and count props.
func NewModel(count int) *Model {
	model := &Model{Units: make([]Unit, count), Props: make(map[uint32]Prop, count)}
	for i := range count {
		model.Units[i] = Unit{ID: uint32(i), Position: Vec2{float32(i), float32(-i)}}
		model.Props[uint32(i)] = Prop{Position: Vec2{float32(i), 1}}
	}
	return model
}

// ---- typed accessors ----

// UnitPosition is a top-level accessor into the slice.
func UnitPosition(model *Model, key Key) *Vec2 { return &model.Units[key.Slot0].Position }

// CameraPosition is a top-level accessor into the nested struct.
func CameraPosition(model *Model, _ Key) *Vec2 { return &model.Camera.Position }

// PropPosition cannot return *Vec2: a map value is not addressable, so the
// accessor of a map path can only copy the value out.
func PropPosition(model *Model, key Key) (Vec2, bool) {
	prop, ok := model.Props[uint32(key.Slot0)]
	return prop.Position, ok
}

// ---- unsafe offset path, compiled by reflection at registration ----

type stepKind uint8

const (
	stepField stepKind = iota
	stepSlice
	stepMap
)

type step struct {
	kind        stepKind
	offset      uintptr      // stepField: offset of the field in its struct
	elementSize uintptr      // stepSlice: size of one element
	mapType     reflect.Type // stepMap: the map's type, for the reflective read
}

// Path is a property path compiled once: a list of steps walked at read time.
type Path struct {
	steps     []step
	mapLookup func(mapPointer unsafe.Pointer, key uint64, out unsafe.Pointer) bool // set for typed map steps
	scratch   reflect.Value                                                        // stepMap: reusable key value
}

// CompilePath compiles "Camera.Position", "Units[].Position" or "Props{}.Position"
// against root by reflection. Reflection cannot instantiate a generic map lookup,
// so a map step reads through reflect unless WithMapLookup supplies a typed one.
func CompilePath(root reflect.Type, text string) *Path {
	path := &Path{}
	current := root
	for _, part := range strings.Split(text, ".") {
		name := strings.TrimSuffix(strings.TrimSuffix(part, "[]"), "{}")
		field, ok := current.FieldByName(name)
		if !ok {
			panic(fmt.Sprintf("no field %s in %s", name, current))
		}
		path.steps = append(path.steps, step{kind: stepField, offset: field.Offset})
		current = field.Type
		switch {
		case strings.HasSuffix(part, "[]"):
			path.steps = append(path.steps, step{kind: stepSlice, elementSize: current.Elem().Size()})
			current = current.Elem()
		case strings.HasSuffix(part, "{}"):
			path.steps = append(path.steps, step{kind: stepMap, mapType: current})
			path.scratch = reflect.New(current.Key()).Elem()
			current = current.Elem()
		}
	}
	return path
}

// WithMapLookup gives the path a typed lookup for its map step, as a generic
// registration call (RegisterMap[K, V]) could make it.
func WithMapLookup[K ~uint32, V any](path *Path) *Path {
	path.mapLookup = func(mapPointer unsafe.Pointer, key uint64, out unsafe.Pointer) bool {
		value, ok := (*(*map[K]V)(mapPointer))[K(key)]
		*(*V)(out) = value
		return ok
	}
	return path
}

// Resolve walks the path from base. A map step copies the value into scratch and
// walks on in the copy, so the result of a map path is readable, never writable.
// Every pointer is rebuilt from base on each call: nothing is kept between calls.
func (path *Path) Resolve(base unsafe.Pointer, key Key, scratch unsafe.Pointer) unsafe.Pointer {
	pointer := base
	slot := 0
	for i := range path.steps {
		s := &path.steps[i]
		switch s.kind {
		case stepField:
			pointer = unsafe.Add(pointer, s.offset)
		case stepSlice:
			index := key.Slot0
			if slot == 1 {
				index = key.Slot1
			}
			slot++
			header := (*[]byte)(pointer) // only data and len are read
			if index >= uint64(len(*header)) {
				return nil
			}
			pointer = unsafe.Add(unsafe.Pointer(unsafe.SliceData(*header)), uintptr(index)*s.elementSize)
		case stepMap:
			index := key.Slot0
			if slot == 1 {
				index = key.Slot1
			}
			slot++
			if path.mapLookup != nil {
				if !path.mapLookup(pointer, index, scratch) {
					return nil
				}
			} else {
				mapValue := reflect.NewAt(s.mapType, pointer).Elem()
				path.scratch.SetUint(index)
				value := mapValue.MapIndex(path.scratch)
				if !value.IsValid() {
					return nil
				}
				reflect.NewAt(s.mapType.Elem(), scratch).Elem().Set(value)
			}
			pointer = scratch
		}
	}
	return pointer
}
