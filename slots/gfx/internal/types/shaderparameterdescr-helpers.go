package types

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/dvoyni/cog/libs/m"
)

// rawLayouts caches one validation per type. The check walks the struct by
// reflection, which is far too much work to repeat per draw and exactly the
// right amount to do once.
var rawLayouts sync.Map // reflect.Type -> struct{}

func validateRawLayout(t reflect.Type) {
	if _, done := rawLayouts.Load(t); done {
		return
	}
	if _, _, err := wgslLayoutOf(t); err != nil {
		panic(fmt.Sprintf("gfx.ShaderParameterRaw: %s does not match its WGSL layout: %v", t, err))
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
