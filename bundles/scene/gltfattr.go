package scene

import (
	"fmt"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// attrValue is one element of a vertex attribute, widened to four floats
// whatever the file stored. Scene's vertex is float everywhere, so every
// attribute path ends here and the component type stops mattering one function
// earlier than it otherwise would.
type attrValue [4]float32

// readAttribute reads one accessor and hands each element to set, dequantised
// and denormalised. It is the single place scene decodes a component type,
// which is what makes KHR_mesh_quantization a scale factor rather than a second
// code path: a quantised position is an ordinary short accessor, and the
// extension's only effect on reading is that shorts turn up where the core
// specification allows only floats.
//
// Elements the accessor does not carry are left at zero rather than defaulted,
// because what a missing w means is the caller's business - 1 for a tangent
// handedness, 0 for a padded position.
func readAttribute(doc *gltf.Document, accessor *gltf.Accessor, set func(int, attrValue)) error {
	data, err := modeler.ReadAccessor(doc, accessor, nil)
	if err != nil {
		return err
	}
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
// of the array is malformed; scene treats the reference as absent rather than
// failing the model, because the alternative is losing a whole mesh to one bad
// index in an attribute it could have generated.
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
