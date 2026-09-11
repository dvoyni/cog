package scene

import (
	"errors"
	"fmt"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// gltfGeometry is one glTF primitive converted to scene's own vertex layout:
// one interleaved buffer, one index list, one topology.
//
// Missing attributes are generated and repacked here rather than bound from a
// second buffer, because gfx binds exactly one vertex buffer per mesh. Total
// memory is identical either way, so the only saving a second buffer would
// bring is a copy this one-pass conversion largely already spends.
type gltfGeometry struct {
	vertices []Vertex
	indices  []uint32
	topology gfx.PrimitiveTopology
	// box is the primitive's local-space bounds, taken from the POSITION
	// accessor's min and max, which glTF requires. hasBox is false when the
	// file omitted them, which makes the whole model never-cull.
	box    m.Box3
	hasBox bool
	// uv0 and uv1 are the ranges the two TEXCOORD accessors spanned, filled as
	// the accessors are read rather than by a walk of their own: the read
	// already visits every UV element, so the ranges cost this path no
	// traversal. An attribute the file does not carry leaves an unseen range,
	// which is the zero-width one at the origin - exactly what the unwritten
	// UVs it left behind decode to.
	//
	// They outlive unwelding, which only duplicates and drops vertices: a range
	// taken before it holds every UV that survives it, and a dropped vertex can
	// only leave it wider than it strictly needed to be.
	uv0, uv1 uvRange
	// skinned reports whether the vertices carry a joint binding the shader
	// should follow. It is decided after conversion, by the node's skin and by
	// whether the primitive actually carried weights: glTF requires a skinned
	// node's mesh to have JOINTS_0 and WEIGHTS_0, and one that does not draws
	// unskinned at the skin's root rather than being lost.
	//
	// It is not the answer to "is this draw skinned" - the placement is. A
	// plain-bound node names its joint on the instance and writes nothing
	// here, so the same converted mesh is plain-bound under one node and
	// static under another.
	skinned bool
	// morph is the primitive's converted morph targets, empty for the
	// overwhelming majority of primitives. It belongs to the geometry rather
	// than to the placement because the deltas are shared per mesh: two nodes
	// referencing one head morph independently through their own weight slots
	// and read the same records.
	morph gltfMorph
}

// errPointTopology reports a POINTS primitive, which gfx has no topology for -
// it carries triangle lists, triangle strips and line lists and nothing else.
// The primitive is skipped and the rest of the model loads, because a model
// that is mostly triangles should not be lost to one debug point cloud.
var errPointTopology = errors.New("POINTS has no gfx topology")

// convertPrimitive turns one glTF primitive into scene geometry.
//
// needTangents asks for generated tangents when the primitive's material has a
// normal map and the file carried none; without one the tangent frame is never
// read, so generating it would be per-vertex work for a value the shader
// multiplies by nothing.
func convertPrimitive(doc *gltf.Document, primitive *gltf.Primitive, needTangents bool) (gltfGeometry, error) {
	position, ok := attributeAccessor(doc, primitive.Attributes, gltf.POSITION)
	if !ok {
		return gltfGeometry{}, errors.New("it has no POSITION attribute")
	}
	geometry := gltfGeometry{vertices: make([]Vertex, position.Count)}
	// White rather than the Go zero value, which is transparent black: the
	// shader multiplies the vertex colour into base colour unconditionally, so
	// a primitive with no COLOR_0 has to carry the identity for that multiply.
	for i := range geometry.vertices {
		geometry.vertices[i].Color = [4]uint8{0xff, 0xff, 0xff, 0xff}
	}
	if err := readVertexAttributes(doc, primitive, &geometry); err != nil {
		return gltfGeometry{}, err
	}
	geometry.box, geometry.hasBox = accessorBox(position)
	// Targets are read before anything reorders the vertices, because a delta
	// is addressed by its own vertex's index and unwelding renumbers them.
	geometry.morph = readMorphTargets(doc, primitive, len(geometry.vertices))
	geometry.expandBoxByMorph()

	indices, err := readIndices(doc, primitive)
	if err != nil {
		return gltfGeometry{}, err
	}
	geometry.topology, geometry.indices, err = convertTopology(primitive.Mode, indices, len(geometry.vertices))
	if err != nil {
		return gltfGeometry{}, err
	}
	if geometry.topology != gfx.TopologyTriangleList {
		// Normals and tangents are a triangle's properties. A line list has no
		// faces to take them from, and the bundled shader lights it by whatever
		// the file supplied - which for a line is nothing, so it renders by its
		// emissive and base colour alone.
		return geometry, nil
	}
	if _, has := primitive.Attributes[gltf.NORMAL]; !has {
		// glTF requires flat normals when NORMAL is absent, and a flat normal
		// belongs to a face rather than to a vertex, so shared vertices have to
		// come apart first. The specification also says the file's tangents are
		// ignored in this case, which unwelding gives for free: the generator
		// below rebuilds them against the normals scene just made.
		var source []uint32
		geometry.vertices, geometry.indices, source = unweld(geometry.vertices, geometry.indices)
		geometry.morph.remap(source)
		generateFlatNormals(geometry.vertices)
	}
	if _, has := primitive.Attributes[gltf.TANGENT]; !has && needTangents {
		generateTangents(geometry.vertices, geometry.indices)
	}
	return geometry, nil
}

// readVertexAttributes fills one primitive's vertices from the accessors it
// names, and accumulates the two UV ranges as it goes. Every attribute but
// POSITION is optional, and an attribute the file does not carry leaves scene's
// default in place.
//
// The UV ranges ride the reads rather than taking a walk of their own: the
// TEXCOORD callbacks below already visit every element, so the per-mesh record
// costs this path one compare pair per coordinate and no second traversal.
func readVertexAttributes(doc *gltf.Document, primitive *gltf.Primitive, geometry *gltfGeometry) error {
	vertices := geometry.vertices
	position, _ := attributeAccessor(doc, primitive.Attributes, gltf.POSITION)
	if err := readAttribute(doc, position, func(i int, v attrValue) {
		vertices[i].Position = m.Vec3{X: v[0], Y: v[1], Z: v[2]}
	}); err != nil {
		return fmt.Errorf("POSITION: %w", err)
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.NORMAL); ok {
		if err := readAttribute(doc, accessor, func(i int, v attrValue) {
			vertices[i].Normal = m.Vec3{X: v[0], Y: v[1], Z: v[2]}.Normalize()
		}); err != nil {
			return fmt.Errorf("NORMAL: %w", err)
		}
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.TANGENT); ok {
		if err := readAttribute(doc, accessor, func(i int, v attrValue) {
			vertices[i].Tangent = m.Vec4{X: v[0], Y: v[1], Z: v[2], W: v[3]}
		}); err != nil {
			return fmt.Errorf("TANGENT: %w", err)
		}
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.TEXCOORD_0); ok {
		if err := readAttribute(doc, accessor, func(i int, v attrValue) {
			vertices[i].UV0 = m.Vec2{X: v[0], Y: v[1]}
			geometry.uv0.add(vertices[i].UV0)
		}); err != nil {
			return fmt.Errorf("TEXCOORD_0: %w", err)
		}
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.TEXCOORD_1); ok {
		if err := readAttribute(doc, accessor, func(i int, v attrValue) {
			vertices[i].UV1 = m.Vec2{X: v[0], Y: v[1]}
			geometry.uv1.add(vertices[i].UV1)
		}); err != nil {
			return fmt.Errorf("TEXCOORD_1: %w", err)
		}
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.COLOR_0); ok {
		colors, err := modeler.ReadColor(doc, accessor, nil)
		if err != nil {
			return fmt.Errorf("COLOR_0: %w", err)
		}
		for i := range colors {
			vertices[i].Color = colors[i]
		}
	}
	// JOINTS_0 and WEIGHTS_0 are read into the vertex here so that the one
	// vertex layout is filled by the one conversion pass. What is read is the
	// skin's own numbering and the file's own weights; bindGeometryJoints
	// remaps the indices into the model's single joint space and normalises
	// the weights once the skin behind the primitive is known, and the pack
	// then narrows each to a byte.
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.JOINTS_0); ok {
		joints, err := modeler.ReadJoints(doc, accessor, nil)
		if err != nil {
			return fmt.Errorf("JOINTS_0: %w", err)
		}
		for i := range joints {
			vertices[i].Joints = joints[i]
		}
	}
	if accessor, ok := attributeAccessor(doc, primitive.Attributes, gltf.WEIGHTS_0); ok {
		weights, err := modeler.ReadWeights(doc, accessor, nil)
		if err != nil {
			return fmt.Errorf("WEIGHTS_0: %w", err)
		}
		for i := range weights {
			vertices[i].Weights = m.Vec4{
				X: weights[i][0], Y: weights[i][1], Z: weights[i][2], W: weights[i][3],
			}
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

// unweld gives every index its own vertex, so a value that belongs to a face
// rather than to a point in space can be written without one face overwriting
// another's. It is what flat normals cost, and it is charged only to primitives
// that omitted NORMAL.
// It also returns the permutation it applied, so anything else addressed by
// vertex index - a primitive's morph deltas - can follow its vertices.
func unweld(vertices []Vertex, indices []uint32) ([]Vertex, []uint32, []uint32) {
	source := sequence(indices, len(vertices))
	expanded := make([]Vertex, len(source))
	unwelded := make([]uint32, len(source))
	for i, index := range source {
		expanded[i] = vertices[index]
		unwelded[i] = uint32(i)
	}
	return expanded, unwelded, source
}

// expandBoxByMorph grows the primitive's declared bounds by the reach its morph
// targets can pull a vertex.
//
// The expansion is conservative - it assumes every target at weight 1 at once -
// and it over-draws rather than under-draws, which is the right direction: a
// culled face that should have been on screen is a hole, and an uncalled draw
// is a cost you can profile.
func (g *gltfGeometry) expandBoxByMorph() {
	if !g.hasBox || g.morph.reach == 0 {
		return
	}
	reach := m.Vec3{X: g.morph.reach, Y: g.morph.reach, Z: g.morph.reach}
	g.box.Min, g.box.Max = g.box.Min.Sub(reach), g.box.Max.Add(reach)
}

// generateFlatNormals writes each triangle's geometric normal onto its three
// vertices. It runs on unwelded geometry, so the write is unambiguous.
func generateFlatNormals(vertices []Vertex) {
	for i := 0; i+2 < len(vertices); i += 3 {
		edge0 := vertices[i+1].Position.Sub(vertices[i].Position)
		edge1 := vertices[i+2].Position.Sub(vertices[i].Position)
		normal := edge0.Cross(edge1).Normalize()
		vertices[i].Normal, vertices[i+1].Normal, vertices[i+2].Normal = normal, normal, normal
	}
}

// generateTangents builds a tangent frame from the UV gradient across each
// triangle, accumulated per vertex and orthonormalised against the normal.
//
// This is explicitly not MikkTSpace. MikkTSpace is the baking convention most
// normal maps are authored against, and matching it exactly requires its
// welding and its angle weighting; scene generates a frame that is continuous
// and correctly handed instead. A model whose normal map was baked against
// MikkTSpace and which ships no TANGENT is getting an approximation, and the
// fix is to ship the tangents the file format has a slot for.
//
// A degenerate triangle in UV space - a seam collapsed to a point, or a
// primitive with no TEXCOORD_0 at all - contributes nothing, and a vertex left
// with no contribution takes an arbitrary basis orthogonal to its normal. That
// is the honest answer: with no UV gradient there is no tangent direction to
// recover, only one that will not produce a black or NaN frame.
func generateTangents(vertices []Vertex, indices []uint32) {
	accumulated := make([]m.Vec3, len(vertices))
	bitangents := make([]m.Vec3, len(vertices))
	for _, triangle := range triangles(indices, len(vertices)) {
		a, b, c := &vertices[triangle[0]], &vertices[triangle[1]], &vertices[triangle[2]]
		edge0, edge1 := b.Position.Sub(a.Position), c.Position.Sub(a.Position)
		deltaUV0 := m.Vec2{X: b.UV0.X - a.UV0.X, Y: b.UV0.Y - a.UV0.Y}
		deltaUV1 := m.Vec2{X: c.UV0.X - a.UV0.X, Y: c.UV0.Y - a.UV0.Y}
		determinant := deltaUV0.X*deltaUV1.Y - deltaUV1.X*deltaUV0.Y
		if determinant == 0 {
			continue
		}
		inverse := 1 / determinant
		tangent := edge0.MulS(deltaUV1.Y * inverse).Sub(edge1.MulS(deltaUV0.Y * inverse))
		bitangent := edge1.MulS(deltaUV0.X * inverse).Sub(edge0.MulS(deltaUV1.X * inverse))
		for _, index := range triangle {
			accumulated[index] = accumulated[index].Add(tangent)
			bitangents[index] = bitangents[index].Add(bitangent)
		}
	}
	for i := range vertices {
		normal := vertices[i].Normal
		tangent := accumulated[i].Sub(normal.MulS(normal.Dot(accumulated[i])))
		if tangent.LengthSquared() == 0 {
			tangent = orthogonal(normal)
		}
		tangent = tangent.Normalize()
		// glTF's w is the bitangent's handedness, which is what lets the shader
		// rebuild the third axis with one cross product.
		handedness := float32(1)
		if normal.Cross(tangent).Dot(bitangents[i]) < 0 {
			handedness = -1
		}
		vertices[i].Tangent = m.Vec4{X: tangent.X, Y: tangent.Y, Z: tangent.Z, W: handedness}
	}
}

// triangles walks a triangle list's faces, whether or not it is indexed.
func triangles(indices []uint32, vertexCount int) [][3]uint32 {
	source := sequence(indices, vertexCount)
	faces := make([][3]uint32, 0, len(source)/3)
	for i := 0; i+2 < len(source); i += 3 {
		faces = append(faces, [3]uint32{source[i], source[i+1], source[i+2]})
	}
	return faces
}

// orthogonal returns some unit vector perpendicular to v, picking the basis
// axis v leans on least so the cross product never degenerates.
func orthogonal(v m.Vec3) m.Vec3 {
	axis := m.Vec3{X: 1}
	if abs32(v.X) > abs32(v.Y) || abs32(v.X) > abs32(v.Z) {
		axis = m.Vec3{Y: 1}
	}
	perpendicular := v.Cross(axis)
	if perpendicular.LengthSquared() == 0 {
		return m.Vec3{Z: 1}
	}
	return perpendicular.Normalize()
}

// accessorBox reads a POSITION accessor's declared bounds. glTF requires them,
// so a file without them is malformed - and scene answers by never culling the
// model rather than by guessing, because a wrong box is a model that vanishes
// at some camera angle and nowhere else.
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
