package gfx

import (
	"fmt"
	"reflect"
	"sync"
	"unsafe"

	"github.com/dvoyni/cog/m"
)

// RawParameter creates a parameter carrying an arbitrary plain-data struct by
// copying its bytes, so a shader value that is a record rather than a scalar or
// a vector can be named like any other parameter.
//
// T's memory layout is validated against WGSL's alignment rules once per type,
// and a mismatch panics. That check is the whole point of the constructor. Go
// aligns a struct to the widest alignment of its fields, which for float32 data
// is 4; WGSL aligns vec2 to 8 and vec3, vec4 and matrices to 16. So
//
//	type bad struct { A float32; B m.Vec3 }
//
// is 16 bytes in Go and 32 in WGSL: it passes any size-based check, and every
// element after the first reads the wrong memory with no error anywhere. The
// panic names the field, both offsets, and the padding that would fix it.
//
// Only plain-data members are accepted - float32, int32, uint32, m.Vec2, m.Vec3,
// m.Vec4, m.Color, m.Quat, m.Mat4, arrays of those, and structs of those. Any
// other member type panics, which is what keeps a pointer out of a byte copy.
func RawParameter[T any](name string, value T) ParameterDescr {
	validateRawLayout(reflect.TypeFor[T]())
	size := int(unsafe.Sizeof(value))
	raw := make([]byte, size)
	copy(raw, unsafe.Slice((*byte)(unsafe.Pointer(&value)), size))
	return ParameterDescr{name: name, kind: paramRaw, raw: raw}
}

// rawLayouts caches one validation per type. The check walks the struct by
// reflection, which is far too much work to repeat per draw and exactly the
// right amount to do once.
var rawLayouts sync.Map // reflect.Type -> struct{}

func validateRawLayout(t reflect.Type) {
	if _, done := rawLayouts.Load(t); done {
		return
	}
	if _, _, err := wgslLayoutOf(t); err != nil {
		panic(fmt.Sprintf("gfx.RawParameter: %s does not match its WGSL layout: %v", t, err))
	}
	rawLayouts.Store(t, struct{}{})
}

// wgslLayoutOf reports the WGSL alignment and size of a Go type, and the error
// describing the first field whose Go offset disagrees with the offset WGSL
// would give it.
func wgslLayoutOf(t reflect.Type) (align, size int, err error) {
	switch t {
	case reflect.TypeFor[m.Vec2]():
		return 8, 8, nil
	case reflect.TypeFor[m.Vec3]():
		return 16, 12, nil
	case reflect.TypeFor[m.Vec4](), reflect.TypeFor[m.Color](), reflect.TypeFor[m.Quat]():
		return 16, 16, nil
	case reflect.TypeFor[m.Mat4]():
		return 16, 64, nil
	}
	switch t.Kind() {
	case reflect.Float32, reflect.Int32, reflect.Uint32:
		return 4, 4, nil
	case reflect.Array:
		elementAlign, elementSize, err := wgslLayoutOf(t.Elem())
		if err != nil {
			return 0, 0, err
		}
		// A WGSL array element is padded up to its own alignment, so the stride
		// is the rounded size and a vec3 array strides at 16, not 12.
		return elementAlign, roundUp(elementSize, elementAlign) * t.Len(), nil
	case reflect.Struct:
		return wgslStructLayoutOf(t)
	}
	return 0, 0, fmt.Errorf("member type %s is not plain shader data", t)
}

func wgslStructLayoutOf(t reflect.Type) (align, size int, err error) {
	offset := 0
	align = 1
	for i := range t.NumField() {
		field := t.Field(i)
		fieldAlign, fieldSize, err := wgslLayoutOf(field.Type)
		if err != nil {
			return 0, 0, fmt.Errorf("field %s: %w", field.Name, err)
		}
		align = max(align, fieldAlign)
		offset = roundUp(offset, fieldAlign)
		if offset != int(field.Offset) {
			return 0, 0, fmt.Errorf(
				"field %s is at Go offset %d but WGSL offset %d; insert %d bytes of padding before it",
				field.Name, field.Offset, offset, offset-int(field.Offset))
		}
		offset += fieldSize
	}
	size = roundUp(offset, align)
	if size != int(t.Size()) {
		return 0, 0, fmt.Errorf(
			"%s is %d bytes in Go but %d in WGSL; append %d bytes of padding",
			t, t.Size(), size, size-int(t.Size()))
	}
	return align, size, nil
}

func roundUp(value, alignment int) int {
	if alignment <= 1 {
		return value
	}
	return (value + alignment - 1) / alignment * alignment
}
