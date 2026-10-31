package gltf

import (
	"fmt"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// attrValue is one element of an accessor, widened to four floats whatever the
// file stored. The animation samplers are read through it, where the element
// count is small and the shape varies by channel.
type attrValue [4]float32

// readAttribute reads one accessor and hands each element to set, dequantised
// and denormalised. It is the single place the decoder decodes a component type
// element by element, which is what makes KHR_mesh_quantization a scale factor
// rather than a second code path: a quantised position is an ordinary short
// accessor, and the extension's only effect on reading is that shorts turn up
// where the core specification allows only floats.
//
// Elements the accessor does not carry are left at zero rather than defaulted,
// because what a missing w means is the caller's business - 1 for a tangent
// handedness, 0 for a padded position.
func readAttribute(doc *gltf.Document, accessor *gltf.Accessor, set func(int, attrValue)) error {
	data, err := modeler.ReadAccessor(doc, accessor, nil)
	if err != nil {
		return err
	}
	return fillAttribute(data, accessor, set)
}

// fillAttribute hands each element of an accessor's decoded data to set,
// dequantised and denormalised.
func fillAttribute(data any, accessor *gltf.Accessor, set func(int, attrValue)) error {
	scale, clamp := componentScale(accessor)
	switch values := data.(type) {
	case []int8:
		fillScalar(values, scale, clamp, set)
	case []uint8:
		fillScalar(values, scale, clamp, set)
	case []int16:
		fillScalar(values, scale, clamp, set)
	case []uint16:
		fillScalar(values, scale, clamp, set)
	case []uint32:
		fillScalar(values, scale, clamp, set)
	case []float32:
		fillScalar(values, scale, clamp, set)
	case [][2]int8:
		fillVec2(values, scale, clamp, set)
	case [][2]uint8:
		fillVec2(values, scale, clamp, set)
	case [][2]int16:
		fillVec2(values, scale, clamp, set)
	case [][2]uint16:
		fillVec2(values, scale, clamp, set)
	case [][2]uint32:
		fillVec2(values, scale, clamp, set)
	case [][2]float32:
		fillVec2(values, scale, clamp, set)
	case [][3]int8:
		fillVec3(values, scale, clamp, set)
	case [][3]uint8:
		fillVec3(values, scale, clamp, set)
	case [][3]int16:
		fillVec3(values, scale, clamp, set)
	case [][3]uint16:
		fillVec3(values, scale, clamp, set)
	case [][3]uint32:
		fillVec3(values, scale, clamp, set)
	case [][3]float32:
		fillVec3(values, scale, clamp, set)
	case [][4]int8:
		fillVec4(values, scale, clamp, set)
	case [][4]uint8:
		fillVec4(values, scale, clamp, set)
	case [][4]int16:
		fillVec4(values, scale, clamp, set)
	case [][4]uint16:
		fillVec4(values, scale, clamp, set)
	case [][4]uint32:
		fillVec4(values, scale, clamp, set)
	case [][4]float32:
		fillVec4(values, scale, clamp, set)
	default:
		return fmt.Errorf("accessor is %v of %v, which is not a vertex attribute",
			accessor.Type, accessor.ComponentType)
	}
	return nil
}

// The typed reads hand a vertex attribute over as the glTF library's own slice
// whenever the file already stored it as floats, which is every unquantised
// file: the accessor is decoded once, by modeler, and passed through
// untouched. Only a quantised or normalised accessor is widened into a slice of
// its own, through the one element-wise path above.
//
// This is what the seam measured 14% faster than filling a vertex struct
// element by element through a callback: the common case makes no call per
// element at all, and the pack reads the slices in a plain loop.

// readVec2 reads one accessor as two floats an element.
func readVec2(doc *gltf.Document, accessor *gltf.Accessor) ([][2]float32, error) {
	data, err := modeler.ReadAccessor(doc, accessor, nil)
	if err != nil {
		return nil, err
	}
	if values, ok := data.([][2]float32); ok {
		return values, nil
	}
	values := make([][2]float32, accessor.Count)
	err = fillAttribute(data, accessor, func(i int, v attrValue) {
		if i < len(values) {
			values[i] = [2]float32{v[0], v[1]}
		}
	})
	return values, err
}

// readVec3 reads one accessor as three floats an element.
func readVec3(doc *gltf.Document, accessor *gltf.Accessor) ([][3]float32, error) {
	data, err := modeler.ReadAccessor(doc, accessor, nil)
	if err != nil {
		return nil, err
	}
	if values, ok := data.([][3]float32); ok {
		return values, nil
	}
	values := make([][3]float32, accessor.Count)
	err = fillAttribute(data, accessor, func(i int, v attrValue) {
		if i < len(values) {
			values[i] = [3]float32{v[0], v[1], v[2]}
		}
	})
	return values, err
}

// readVec4 reads one accessor as four floats an element.
func readVec4(doc *gltf.Document, accessor *gltf.Accessor) ([][4]float32, error) {
	data, err := modeler.ReadAccessor(doc, accessor, nil)
	if err != nil {
		return nil, err
	}
	if values, ok := data.([][4]float32); ok {
		return values, nil
	}
	values := make([][4]float32, accessor.Count)
	err = fillAttribute(data, accessor, func(i int, v attrValue) {
		if i < len(values) {
			values[i] = v
		}
	})
	return values, err
}

// attrComponent is every component type a glTF accessor can hold. Signed 32-bit
// integers are absent because glTF has no such component type.
type attrComponent interface {
	~int8 | ~uint8 | ~int16 | ~uint16 | ~uint32 | ~float32
}

// componentScale reports the multiplier that takes one stored component to its
// float value, and whether the result needs clamping at -1.
//
// The clamp is not decoration. glTF's normalised signed formulas divide by 127
// and 32767 rather than 128 and 32768, so that 0 and 1 are both exactly
// representable - which leaves the most negative value at -1.008, and an
// unclamped normal of that length is a visible shading error on a quantised
// mesh.
func componentScale(accessor *gltf.Accessor) (float32, bool) {
	if !accessor.Normalized {
		return 1, false
	}
	switch accessor.ComponentType {
	case gltf.ComponentByte:
		return 1.0 / 127, true
	case gltf.ComponentUbyte:
		return 1.0 / 255, false
	case gltf.ComponentShort:
		return 1.0 / 32767, true
	case gltf.ComponentUshort:
		return 1.0 / 65535, false
	}
	return 1, false
}

// component converts one stored component. It is generic over the six types
// rather than taking a float, because the conversion is where the integer width
// is still known.
func component[T attrComponent](value T, scale float32, clamp bool) float32 {
	converted := float32(value) * scale
	if clamp && converted < -1 {
		return -1
	}
	return converted
}

func fillScalar[T attrComponent](values []T, scale float32, clamp bool, set func(int, attrValue)) {
	for i := range values {
		set(i, attrValue{component(values[i], scale, clamp)})
	}
}

func fillVec2[T attrComponent](values [][2]T, scale float32, clamp bool, set func(int, attrValue)) {
	for i := range values {
		set(i, attrValue{
			component(values[i][0], scale, clamp),
			component(values[i][1], scale, clamp),
		})
	}
}

func fillVec3[T attrComponent](values [][3]T, scale float32, clamp bool, set func(int, attrValue)) {
	for i := range values {
		set(i, attrValue{
			component(values[i][0], scale, clamp),
			component(values[i][1], scale, clamp),
			component(values[i][2], scale, clamp),
		})
	}
}

func fillVec4[T attrComponent](values [][4]T, scale float32, clamp bool, set func(int, attrValue)) {
	for i := range values {
		set(i, attrValue{
			component(values[i][0], scale, clamp),
			component(values[i][1], scale, clamp),
			component(values[i][2], scale, clamp),
			component(values[i][3], scale, clamp),
		})
	}
}

// accessorAt resolves an index into the accessor it names, and reports whether
// the document actually has one there. A file naming an accessor past the end
// of the array is malformed; the decoder treats the reference as absent rather
// than failing the model, because the alternative is losing a whole mesh to one
// bad index in an attribute it could have generated.
func accessorAt(doc *gltf.Document, index *int) (*gltf.Accessor, bool) {
	if index == nil || *index < 0 || *index >= len(doc.Accessors) || doc.Accessors[*index] == nil {
		return nil, false
	}
	return doc.Accessors[*index], true
}

// attributeAccessor resolves one named vertex attribute of a primitive.
func attributeAccessor(
	doc *gltf.Document, attributes gltf.PrimitiveAttributes, name string,
) (*gltf.Accessor, bool) {
	index, named := attributes[name]
	if !named {
		return nil, false
	}
	return accessorAt(doc, &index)
}
