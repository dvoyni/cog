package internal

import (
	"reflect"

	"github.com/dvoyni/cog/libs/m"
)

// The List itself is m.List, in libs/m. What the ECS knows about it lives here:
// how the registration walk recognises one and reaches its element type, and
// where validation mode finds the backing arrays it stamps. The check List.Set
// makes under -tags ecs_validate is validate_on.go's checkListWritable, which
// that file installs into m.

// listMarkerType is the type of a List's first field, which is m's unexported
// marker. Every List has it and nothing outside m can embed it, so the walk
// recognises a List by it without knowing the element type: a generic cannot
// be instantiated from a reflect.Type, so there is no List[T] to compare
// against. Any instantiation yields the same marker.
var listMarkerType = reflect.TypeFor[m.List[struct{}]]().Field(0).Type

// isList reports whether t is some List[T]. It recognises the marker rather
// than the name, so a user type called List is not mistaken for one and a
// rename of this one cannot silently break the walk.
func isList(t reflect.Type) bool {
	return t.Kind() == reflect.Struct &&
		t.NumField() == 3 &&
		t.Field(0).Type == listMarkerType
}

// listElem is the element type of a List, which the legality walk needs in
// order to recurse into it.
func listElem(t reflect.Type) reflect.Type { return t.Field(1).Type.Elem() }

// listSite is one List within a type, as validation mode needs to find it: the
// offset its slice header sits at, and - for a List whose element type holds
// Lists of its own - the element stride and the sites within one element.
//
// nested is what lets a List of Lists be checked. A Component row names its own
// Lists' backing arrays at fixed offsets, but the arrays an element names are
// one indirection further out, at offsets that exist only once the outer List
// has elements; so a site carries the element layout and the stamp walks the
// outer List's elements at stamp time.
type listSite struct {
	offset uintptr
	stride uintptr
	nested []listSite
}

// listSites is every List within t, found once at registration. It is what
// validation mode stamps from, and it is computed whether or not validation is
// built, because registration-time cost is irrelevant and a class that carries
// the answer is simpler than one that carries a build tag.
//
// A List's header sits at the List's own offset: the marker in front of it is
// zero-size, so the slice field starts where the List does.
func listSites(t reflect.Type) []listSite {
	return appendListSites(nil, t, 0)
}

func appendListSites(into []listSite, t reflect.Type, base uintptr) []listSite {
	if isList(t) {
		elem := listElem(t)
		return append(into, listSite{offset: base, stride: elem.Size(), nested: listSites(elem)})
	}
	switch t.Kind() {
	case reflect.Struct:
		for i := range t.NumField() {
			field := t.Field(i)
			into = appendListSites(into, field.Type, base+field.Offset)
		}
	case reflect.Array:
		stride := t.Elem().Size()
		for i := range t.Len() {
			into = appendListSites(into, t.Elem(), base+uintptr(i)*stride)
		}
	}
	return into
}
