package gltf

import (
	"errors"
	"fmt"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// Geometry is one glTF primitive as the file stored it: its vertex attributes
// structure of arrays, its index list in one of the three topologies gfx
// carries, its declared bounds and its morph targets. Nothing is packed and
// nothing is generated: flat normals, tangents, the joint remap and the storage
// layout are all model's, applied after the decoder returns.
//
// Each attribute is the glTF library's own typed slice, handed over untouched
// when the file stored floats and widened into a slice of the same shape when
// it stored a quantised or normalised form. An attribute the file does not
// carry is nil, and one it carries is at most as long as Positions - a longer
// accessor is a malformed file, and only its first len(Positions) elements
// mean anything.
type Geometry struct {
	Positions [][3]float32
	// Normals are as stored: dequantised, and not yet of unit length.
	Normals  [][3]float32
	Tangents [][4]float32
	UV0, UV1 [][2]float32
	// Colors are COLOR_0 widened by the library to eight-bit RGBA, which glTF
	// defines as linear whatever component type the file wrote.
	Colors [][4]uint8
	// Joints are JOINTS_0 in the skin's own numbering, and Weights WEIGHTS_0 as
	// the file wrote them, neither normalised nor remapped.
	Joints  [][4]uint16
	Weights [][4]float32

	// Indices are the primitive's index list after its mode was expanded to a
	// list, nil for a non-indexed triangle or line list.
	Indices  []uint32
	Topology gfx.PrimitiveTopology

	// Box is the primitive's local-space bounds, taken from the POSITION
	// accessor's min and max, which glTF requires. HasBox is false when the
	// file omitted them, which makes the whole model never-cull.
	Box    m.Box3
	HasBox bool

	// NormalNamed and TangentNamed report whether the primitive names a NORMAL
	// and a TANGENT attribute at all, which is what glTF's rules turn on: a
	// primitive naming no NORMAL takes flat normals, and its TANGENT is then
	// ignored. They are not whether the attribute was readable.
	NormalNamed, TangentNamed bool
	// NeedTangents says the material some node draws this primitive with has
	// a normal map, so tangents the file did not carry have to be generated.
	// It is part of the geometry's identity: the same mesh under a
	// normal-mapped material and a plain one is two geometries.
	NeedTangents bool

	// Skin is the glTF skin whose joints array Joints indexes, or -1. Skinned
	// reports whether some vertex actually carries weight under that skin: a
	// skinned node's mesh that carries none draws unskinned at the skin's root
	// rather than being lost.
	Skin    int
	Skinned bool
	// SkinnedLayout is the union over every placement of this geometry of
	// "draws through the pose buffer", which the plain-bound case makes
	// broader than Skinned. It says which of model's two storage layouts the
	// geometry needs, and it is the placement walk's answer, so it is complete
	// only once every scene is flattened.
	SkinnedLayout bool

	// Targets are the primitive's morph targets, empty for the overwhelming
	// majority of primitives. Only the attributes the base primitive authored
	// are read: a NORMAL delta on a primitive naming no NORMAL is dropped, and
	// so is a TANGENT delta on one naming no NORMAL or no TANGENT.
	Targets []MorphTarget
}

// MorphTarget is one morph target's deltas, one array per attribute the base
// primitive authored, each indexed by the base primitive's own vertices. An
// attribute the target names but whose accessor could not be read is named and
// nil, and reads as a zero delta.
type MorphTarget struct {
	Position, Normal, Tangent                [][3]float32
	PositionNamed, NormalNamed, TangentNamed bool
}

// Morphed reports whether the primitive carries any target delta at all.
func (g *Geometry) Morphed() bool {
	for i := range g.Targets {
		target := &g.Targets[i]
		if target.PositionNamed || target.NormalNamed || target.TangentNamed {
			return true
		}
	}
	return false
}

// errPointTopology reports a POINTS primitive, which gfx has no topology for -
// it carries triangle lists, triangle strips and line lists and nothing else.
// The primitive is skipped and the rest of the model loads, because a model
// that is mostly triangles should not be lost to one debug point cloud.
var errPointTopology = errors.New("POINTS has no gfx topology")

// readGeometry reads one glTF primitive.
func readGeometry(doc *gltf.Document, primitive *gltf.Primitive, needTangents bool) (Geometry, error) {
	position, ok := attributeAccessor(doc, primitive.Attributes, gltf.POSITION)
	if !ok {
		return Geometry{}, errors.New("it has no POSITION attribute")
	}
	geometry := Geometry{NeedTangents: needTangents, Skin: -1}
	_, geometry.NormalNamed = primitive.Attributes[gltf.NORMAL]
	_, geometry.TangentNamed = primitive.Attributes[gltf.TANGENT]
	if err := readVertexAttributes(doc, primitive, position, &geometry); err != nil {
		return Geometry{}, err
	}
	geometry.Box, geometry.HasBox = accessorBox(position)
	geometry.Targets = readMorphTargets(doc, primitive, &geometry)

	indices, err := readIndices(doc, primitive)
	if err != nil {
		return Geometry{}, err
	}
	geometry.Topology, geometry.Indices, err = convertTopology(primitive.Mode, indices, len(geometry.Positions))
	if err != nil {
		return Geometry{}, err
	}
	return geometry, nil
}

// readVertexAttributes reads one primitive's attributes from the accessors it
// names. Every attribute but POSITION is optional, and one the file does not
// carry is left nil.
func readVertexAttributes(
	doc *gltf.Document, primitive *gltf.Primitive, position *gltf.Accessor, geometry *Geometry,
) error {
	var err error
	if geometry.Positions, err = readVec3(doc, position); err != nil {
		return fmt.Errorf("POSITION: %w", err)
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.NORMAL); ok {
		if geometry.Normals, err = readVec3(doc, accessor); err != nil {
			return fmt.Errorf("NORMAL: %w", err)
		}
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.TANGENT); ok {
		if geometry.Tangents, err = readVec4(doc, accessor); err != nil {
			return fmt.Errorf("TANGENT: %w", err)
		}
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.TEXCOORD_0); ok {
		if geometry.UV0, err = readVec2(doc, accessor); err != nil {
			return fmt.Errorf("TEXCOORD_0: %w", err)
		}
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.TEXCOORD_1); ok {
		if geometry.UV1, err = readVec2(doc, accessor); err != nil {
			return fmt.Errorf("TEXCOORD_1: %w", err)
		}
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.COLOR_0); ok {
		if geometry.Colors, err = modeler.ReadColor(doc, accessor, nil); err != nil {
			return fmt.Errorf("COLOR_0: %w", err)
		}
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.JOINTS_0); ok {
		if geometry.Joints, err = modeler.ReadJoints(doc, accessor, nil); err != nil {
			return fmt.Errorf("JOINTS_0: %w", err)
		}
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.WEIGHTS_0); ok {
		if geometry.Weights, err = modeler.ReadWeights(doc, accessor, nil); err != nil {
			return fmt.Errorf("WEIGHTS_0: %w", err)
		}
	}
	return nil
}

// readIndices reads a primitive's index accessor, or reports that it has none.
// modeler widens whatever width the file used to uint32, which is the whole of
// the u8-index gap: WebGPU has no uint8 index format, and two of the vendored
// assets carry one.
func readIndices(doc *gltf.Document, primitive *gltf.Primitive) ([]uint32, error) {
	accessor, ok := accessorAt(doc, primitive.Indices)
	if !ok {
		return nil, nil
	}
	indices, err := modeler.ReadIndices(doc, accessor, nil)
	if err != nil {
		return nil, fmt.Errorf("indices: %w", err)
	}
	return indices, nil
}

// convertTopology maps a glTF primitive mode onto the three topologies gfx
// carries, expanding the two strips, the fan and the loop into lists.
//
// Expanding is what keeps every model mesh one topology, so batching, the index
// buffer and the skinning path never branch on it. It is also the only way to
// draw them at all: gfx has a triangle strip, but a strip's restart rule and a
// fan's shared first vertex are not expressible as one draw call each anyway.
//
// A non-indexed strip, loop or fan gains an index buffer here rather than
// duplicating its vertices, which is the cheaper half of the same conversion.
func convertTopology(
	mode gltf.PrimitiveMode, indices []uint32, vertexCount int,
) (gfx.PrimitiveTopology, []uint32, error) {
	switch mode {
	case gltf.PrimitiveTriangles:
		return gfx.TopologyTriangleList, indices, nil
	case gltf.PrimitiveLines:
		return gfx.TopologyLineList, indices, nil
	case gltf.PrimitivePoints:
		return 0, nil, errPointTopology
	case gltf.PrimitiveLineStrip:
		return gfx.TopologyLineList, expandLineStrip(sequence(indices, vertexCount), false), nil
	case gltf.PrimitiveLineLoop:
		return gfx.TopologyLineList, expandLineStrip(sequence(indices, vertexCount), true), nil
	case gltf.PrimitiveTriangleStrip:
		return gfx.TopologyTriangleList, expandTriangleStrip(sequence(indices, vertexCount)), nil
	case gltf.PrimitiveTriangleFan:
		return gfx.TopologyTriangleList, expandTriangleFan(sequence(indices, vertexCount)), nil
	}
	return 0, nil, fmt.Errorf("primitive mode %v is not a glTF mode", mode)
}

// sequence returns the indices a primitive assembles from, synthesising the
// identity sequence for non-indexed geometry so the expansions have one input
// shape rather than two.
func sequence(indices []uint32, vertexCount int) []uint32 {
	if len(indices) > 0 {
		return indices
	}
	indices = make([]uint32, vertexCount)
	for i := range indices {
		indices[i] = uint32(i)
	}
	return indices
}

// expandLineStrip turns a strip into a line list, closing it when it is a loop.
func expandLineStrip(strip []uint32, loop bool) []uint32 {
	if len(strip) < 2 {
		return nil
	}
	segments := len(strip) - 1
	if loop {
		segments++
	}
	list := make([]uint32, 0, segments*2)
	for i := 0; i < len(strip)-1; i++ {
		list = append(list, strip[i], strip[i+1])
	}
	if loop {
		list = append(list, strip[len(strip)-1], strip[0])
	}
	return list
}

// expandTriangleStrip turns a strip into a triangle list, flipping every odd
// triangle so the whole list keeps one winding - which is the point, since the
// pipeline's face culling is per material and cannot alternate per triangle.
func expandTriangleStrip(strip []uint32) []uint32 {
	if len(strip) < 3 {
		return nil
	}
	list := make([]uint32, 0, (len(strip)-2)*3)
	for i := 0; i+2 < len(strip); i++ {
		if i%2 == 0 {
			list = append(list, strip[i], strip[i+1], strip[i+2])
		} else {
			list = append(list, strip[i+1], strip[i], strip[i+2])
		}
	}
	return list
}

// expandTriangleFan turns a fan into a triangle list around its first vertex.
func expandTriangleFan(fan []uint32) []uint32 {
	if len(fan) < 3 {
		return nil
	}
	list := make([]uint32, 0, (len(fan)-2)*3)
	for i := 1; i+1 < len(fan); i++ {
		list = append(list, fan[0], fan[i], fan[i+1])
	}
	return list
}

// accessorBox reads a POSITION accessor's declared bounds. glTF requires them,
// so a file without them is malformed - and the answer is never culling the
// model rather than guessing, because a wrong box is a model that vanishes at
// some camera angle and nowhere else.
func accessorBox(accessor *gltf.Accessor) (m.Box3, bool) {
	if len(accessor.Min) < 3 || len(accessor.Max) < 3 {
		return m.Box3{}, false
	}
	return m.Box3{
		Min: m.Vec3{
			X: float32(accessor.Min[0]), Y: float32(accessor.Min[1]), Z: float32(accessor.Min[2]),
		},
		Max: m.Vec3{
			X: float32(accessor.Max[0]), Y: float32(accessor.Max[1]), Z: float32(accessor.Max[2]),
		},
	}, true
}

// weighted reports whether any vertex carries positive total weight, which is
// what makes a primitive under a skin actually skinned. Only the first
// vertexCount weights belong to a vertex.
func weighted(weights [][4]float32, vertexCount int) bool {
	for _, weight := range weights[:min(len(weights), vertexCount)] {
		if weight[0]+weight[1]+weight[2]+weight[3] > 0 {
			return true
		}
	}
	return false
}
