package ecs

import (
	"fmt"
	"reflect"
	"slices"
)

// Schema describes what a world holds: its component types and its refs, for
// code that works on the world without the types, such as a serializer or
// the network layer. Each call builds it anew; the Markers and Fields of each
// component are the registry's own, to read only.
type Schema struct {
	// Components holds every component type, indexed by ComponentID.
	Components []ComponentInfo
	// Refs holds the refs of each type, in registration order.
	Refs []RefInfo
}

// ComponentName is a component type's name in its world, the one saves know
// it by. It stays when the Go type is renamed or moved.
type ComponentName string

// ComponentInfo is what the registry keeps of a component type, for code
// that works on rows as bytes without the type, such as a serializer or the
// network layer.
type ComponentInfo struct {
	ID    ComponentID
	Name  ComponentName
	Type  reflect.Type
	Size  uintptr
	Align uintptr
	// Markers holds the type of each embedded zero-size struct field, in
	// declaration order: what the component is, as Transient or a network
	// marker says, beyond what it holds. Markers come before every field.
	Markers []reflect.Type
	// Fields holds the component's exported fields, markers excluded, with
	// nested structs flattened and embedded ones promoted, as Go promotes
	// their fields. An array, an Entity and a Ref are one field each. A
	// component that is not a struct, or is a Ref, is one field with an
	// empty path.
	Fields []FieldInfo
}

// FieldInfo describes one exported field of a component.
type FieldInfo struct {
	// Path names the field from the component, its nested struct fields
	// joined by dots: "Position.X".
	Path string
	Type reflect.Type
	// Offset is the field's offset from the start of the component.
	Offset uintptr
	Size   uintptr
	Tag    reflect.StructTag
}

// RefName is the name of a world's refs of one type, the one saves know them
// by. It stays when the Go type is renamed or moved.
type RefName string

// RefInfo is what the registry keeps of refs of one type.
type RefInfo struct {
	Name RefName
	// Type is the type of the refs' values.
	Type      reflect.Type
	Keyed     bool
	Transient bool
}

// Schema describes the components and refs the world holds.
func (w World[W]) Schema() Schema {
	return w.world.schema()
}

// Schema describes the components and refs the world holds.
func (l WorldLookup[W]) Schema() Schema {
	return l.world.schema()
}

// HasMarker reports whether the component info describes embeds marker M.
func HasMarker[M any](info ComponentInfo) bool {
	return slices.Contains(info.Markers, reflect.TypeFor[M]())
}

// schema describes the components and refs the world holds.
func (w *worldResource[W]) schema() Schema {
	schema := Schema{
		Components: make([]ComponentInfo, len(w.stores)),
		Refs:       make([]RefInfo, len(w.refs)),
	}
	for i, s := range w.stores {
		schema.Components[i] = s.component
	}
	for i, r := range w.refs {
		schema.Refs[i] = r.info()
	}
	return schema
}

// makeComponentInfo describes component type T, registered as id under
// name. It is a composition error, and panics, for a marker to follow a
// field or for two fields to have one path.
func makeComponentInfo[T any](id ComponentID, name ComponentName) ComponentInfo {
	t := reflect.TypeFor[T]()
	info := ComponentInfo{ID: id, Name: name, Type: t, Size: t.Size(), Align: uintptr(t.Align())}
	if t.Kind() != reflect.Struct || t.Implements(serializedRefType) {
		info.Fields = []FieldInfo{{Type: t, Size: t.Size()}}
		return info
	}
	for i := range t.NumField() {
		if f := t.Field(i); f.Anonymous && f.Type.Kind() == reflect.Struct && f.Type.Size() == 0 {
			if i != len(info.Markers) {
				panic(fmt.Sprintf("ecs: marker %v of component %v follows a field: markers come first", f.Type, t))
			}
			info.Markers = append(info.Markers, f.Type)
		}
	}
	info.Fields = appendFields(nil, t, "", 0)
	paths := map[string]bool{}
	for _, f := range info.Fields {
		if paths[f.Path] {
			panic(fmt.Sprintf("ecs: component %v has two fields at path %q", t, f.Path))
		}
		paths[f.Path] = true
	}
	return info
}

// appendFields appends the exported fields of struct type t, at offset in
// the component, to fields, with prefix before each path.
func appendFields(fields []FieldInfo, t reflect.Type, prefix string, offset uintptr) []FieldInfo {
	for i := range t.NumField() {
		f := t.Field(i)
		isStruct := f.Type.Kind() == reflect.Struct && !f.Type.Implements(serializedRefType)
		switch {
		case f.Anonymous && f.Type.Kind() == reflect.Struct && f.Type.Size() == 0:
			// A marker.
		case f.Anonymous && isStruct:
			fields = appendFields(fields, f.Type, prefix, offset+f.Offset)
		case !f.IsExported():
		case isStruct:
			fields = appendFields(fields, f.Type, prefix+f.Name+".", offset+f.Offset)
		default:
			fields = append(fields, FieldInfo{
				Path:   prefix + f.Name,
				Type:   f.Type,
				Offset: offset + f.Offset,
				Size:   f.Type.Size(),
				Tag:    f.Tag,
			})
		}
	}
	return fields
}
