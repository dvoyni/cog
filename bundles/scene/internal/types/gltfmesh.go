package types

import (
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// gltfGeometry is one glTF primitive converted to scene's own vertices: one
// interleaved buffer, one index list, one topology. Which of the two named
// layouts that buffer is packed into is skinnedLayout below, which the
// decoder's placement walk settled.
//
// Missing attributes are generated and repacked here rather than bound from a
// second buffer, because gfx binds exactly one vertex buffer per mesh. Total
// memory is identical either way, so the only saving a second buffer would
// bring is a copy this one-pass conversion largely already spends.
type gltfGeometry struct {
	vertices []skinnedVertex
	indices  []uint32
	topology gfx.PrimitiveTopology
	// box is the primitive's local-space bounds, taken from the POSITION
	// accessor's min and max, which glTF requires. hasBox is false when the
	// file omitted them, which makes the whole model never-cull.
	box    m.Box3
	hasBox bool
	// uv0 and uv1 are the ranges the two TEXCOORD arrays spanned, filled as
	// the arrays are copied into the vertices rather than by a walk of their
	// own. An attribute the file does not carry leaves an unseen range, which
	// is the zero-width one at the origin - exactly what the unwritten UVs it
	// left behind decode to.
	//
	// They outlive unwelding, which only duplicates and drops vertices: a range
	// taken before it holds every UV that survives it, and a dropped vertex can
	// only leave it wider than it strictly needed to be.
	uv0, uv1 uvRange
	// skinned reports whether the vertices carry a joint binding the shader
	// should follow. It is decided by the node's skin and by whether the
	// primitive actually carried weights: glTF requires a skinned node's mesh
	// to have JOINTS_0 and WEIGHTS_0, and one that does not draws unskinned at
	// the skin's root rather than being lost.
	//
	// It is not the answer to "is this draw skinned" - the placement is. A
	// plain-bound node names its joint on the instance and writes nothing
	// here, so the same converted mesh is plain-bound under one node and
	// static under another.
	skinned bool
	// skinnedLayout is which of the two named layouts this geometry stores in,
	// and it is the union over the walk: the skinned layout iff *any* placement
	// draws this geometry under SCENE_SKIN, which the plain-bound case makes
	// broader than skinned above.
	//
	// The union is what makes a 32-byte layout under a skinning variant
	// unrepresentable rather than merely unchecked. Both answers come from the
	// one fact the decoder's walk computes per placement, so a variant that
	// declares locations 6 and 7 can only ever be paired with a buffer that
	// supplies them. It is a union rather than a key so that one geometry
	// stays one conversion however many nodes place it.
	//
	// A geometry plain-bound under one node and static under another therefore
	// stores joints and weights of zero for the static placements too. That is
	// 6.7 KiB across the vendored corpus, and it is paid rather than bought
	// with a fifth and sixth shader variant.
	skinnedLayout bool
	// morph is the primitive's converted morph targets, empty for the
	// overwhelming majority of primitives. It belongs to the geometry rather
	// than to the placement because the deltas are shared per mesh: two nodes
	// referencing one head morph independently through their own weight slots
	// and read the same records.
	morph gltfMorph
}

// convertGeometry turns one decoded primitive into scene geometry: it copies
// the decoder's attribute arrays into conversion vertices, generates what the
// file left out, and remaps the skin's joints into the model's numbering.
//
// The copy is one plain loop per attribute over the glTF library's own slices,
// with no call per element. Flat normals are generated for a triangle list
// whose primitive named no NORMAL, and tangents for one whose material has a
// normal map and whose primitive carried none; without a normal map the
// tangent frame is never read, so generating it would be per-vertex work for a
// value the shader multiplies by nothing.
func convertGeometry(decoded *model.DecodedGeometry, skins []model.DecodedSkin) gltfGeometry {
	geometry := gltfGeometry{
		indices:  decoded.Indices,
		topology: decoded.Topology,
		box:      decoded.Box, hasBox: decoded.HasBox,
		skinnedLayout: decoded.SkinnedLayout,
	}
	geometry.vertices, geometry.uv0, geometry.uv1 = fillVertices(decoded)
	// Targets are converted before anything reorders the vertices, because a
	// delta is addressed by its own vertex's index and unwelding renumbers
	// them.
	geometry.morph = convertMorphTargets(decoded, len(geometry.vertices))
	geometry.expandBoxByMorph()
	if geometry.topology == gfx.TopologyTriangleList {
		if !decoded.NormalNamed {
			// glTF requires flat normals when NORMAL is absent, and a flat
			// normal belongs to a face rather than to a vertex, so shared
			// vertices have to come apart first. The specification also says
			// the file's tangents are ignored in this case, which unwelding
			// gives for free: the generator below rebuilds them against the
			// normals scene just made.
			var source []uint32
			geometry.vertices, geometry.indices, source = unweld(geometry.vertices, geometry.indices)
			geometry.morph.remap(source)
			generateFlatNormals(geometry.vertices)
		}
		if !decoded.TangentNamed && decoded.NeedTangents {
			generateTangents(geometry.vertices, geometry.indices)
		}
	}
	// Normals and tangents are a triangle's properties. A line list has no
	// faces to take them from, and the bundled shader lights it by whatever
	// the file supplied - which for a line is nothing, so it renders by its
	// emissive and base colour alone.
	if decoded.Skin >= 0 && decoded.Skin < len(skins) {
		geometry.bindJoints(skins[decoded.Skin].Joints)
	}
	return geometry
}

// fillVertices copies one decoded primitive's attribute arrays into its
// conversion vertices. Every attribute but POSITION is optional, and an
// attribute the file does not carry leaves scene's default in place.
func fillVertices(decoded *model.DecodedGeometry) (vertices []skinnedVertex, uv0, uv1 uvRange) {
	vertices = make([]skinnedVertex, len(decoded.Positions))
	for i, position := range decoded.Positions {
		vertices[i].Position = m.Vec3{X: position[0], Y: position[1], Z: position[2]}
		// White rather than the Go zero value, which is transparent black:
		// the shader multiplies the vertex colour into base colour
		// unconditionally, so a primitive with no COLOR_0 has to carry the
		// identity for that multiply.
		vertices[i].Color = m.White
	}
	for i, normal := range decoded.Normals[:min(len(decoded.Normals), len(vertices))] {
		vertices[i].Normal = m.Vec3{X: normal[0], Y: normal[1], Z: normal[2]}.Normalize()
	}
	for i, tangent := range decoded.Tangents[:min(len(decoded.Tangents), len(vertices))] {
		vertices[i].Tangent = m.Vec4{X: tangent[0], Y: tangent[1], Z: tangent[2], W: tangent[3]}
	}
	for i, uv := range decoded.UV0[:min(len(decoded.UV0), len(vertices))] {
		vertices[i].UV0 = m.Vec2{X: uv[0], Y: uv[1]}
		uv0.add(vertices[i].UV0)
	}
	for i, uv := range decoded.UV1[:min(len(decoded.UV1), len(vertices))] {
		vertices[i].UV1 = m.Vec2{X: uv[0], Y: uv[1]}
		uv1.add(vertices[i].UV1)
	}
	// glTF's COLOR_0 is linear whatever component type it was written in, and
	// the decoder has already widened the file's form to eight-bit RGBA. The
	// bake quantises straight back to those same eight bits, so this round
	// trip through the float form is exact.
	const scale = 1.0 / unorm8CodeMax
	for i, colour := range decoded.Colors[:min(len(decoded.Colors), len(vertices))] {
		vertices[i].Color = m.NewColorLinear(
			float32(colour[0])*scale, float32(colour[1])*scale,
			float32(colour[2])*scale, float32(colour[3])*scale)
	}
	// JOINTS_0 and WEIGHTS_0 are copied into the conversion vertex whichever
	// layout the geometry ends up storing in. They are the skin's own numbering
	// and the file's own weights; bindJoints remaps the indices into the
	// model's single joint space and normalises the weights, and a pack into
	// the skinned layout then narrows each to a byte - where a pack into the
	// standard layout drops them entirely.
	for i, joints := range decoded.Joints[:min(len(decoded.Joints), len(vertices))] {
		vertices[i].Joints = joints
	}
	for i, weights := range decoded.Weights[:min(len(decoded.Weights), len(vertices))] {
		vertices[i].Weights = m.Vec4{X: weights[0], Y: weights[1], Z: weights[2], W: weights[3]}
	}
	return vertices, uv0, uv1
}

// bindJoints rewrites one converted primitive's joint indices into the model's
// single numbering, and decides whether it is skinned at all. slots is the
// primitive's skin's joints array resolved into that numbering.
//
// A skin's JOINTS_0 indexes that skin's own joints array, which is local to
// the skin; the model's numbering is what makes rows addressable by
// clipBase + frame*jointCount + joint with no per-skin offset anywhere.
//
// Only a real skin reaches here. A node joint rides the instance record and
// touches no vertex at all.
//
// Weights are normalised here as well as in the shader, and the two are not
// redundant. This pass is what puts a weight inside [0, 1] so that it has a
// unorm8 code to land on at all - a file writing 3 and 1 would otherwise clamp
// both to full influence - and the shader's divide is what covers the sum the
// rounding then misses, which no bake-time scheme can prevent and which a
// mesh authored through the public API would never have had a bake to fix.
func (g *gltfGeometry) bindJoints(slots []int) {
	bound := false
	for i := range g.vertices {
		vertex := &g.vertices[i]
		total := vertex.Weights.X + vertex.Weights.Y + vertex.Weights.Z + vertex.Weights.W
		if total <= 0 {
			vertex.Joints, vertex.Weights = [4]uint16{}, m.Vec4{}
			continue
		}
		bound = true
		vertex.Weights = m.Vec4{
			X: vertex.Weights.X / total, Y: vertex.Weights.Y / total,
			Z: vertex.Weights.Z / total, W: vertex.Weights.W / total,
		}
		for influence, slot := range vertex.Joints {
			// A slot past the skin's joints array is a malformed file. It
			// resolves to joint 0, whose weight the file has already decided;
			// the alternative is dropping a whole primitive over one bad
			// index.
			if int(slot) < len(slots) {
				vertex.Joints[influence] = uint16(slots[slot])
				continue
			}
			vertex.Joints[influence] = 0
		}
	}
	g.skinned = bound
}

// unweld gives every index its own vertex, so a value that belongs to a face
// rather than to a point in space can be written without one face overwriting
// another's. It is what flat normals cost, and it is charged only to primitives
// that omitted NORMAL.
// It also returns the permutation it applied, so anything else addressed by
// vertex index - a primitive's morph deltas - can follow its vertices.
func unweld(vertices []skinnedVertex, indices []uint32) ([]skinnedVertex, []uint32, []uint32) {
	source := sequence(indices, len(vertices))
	expanded := make([]skinnedVertex, len(source))
	unwelded := make([]uint32, len(source))
	for i, index := range source {
		expanded[i] = vertices[index]
		unwelded[i] = uint32(i)
	}
	return expanded, unwelded, source
}

// sequence returns the indices a primitive assembles from, synthesising the
// identity sequence for non-indexed geometry so the walks over it have one
// input shape rather than two.
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
func generateFlatNormals(vertices []skinnedVertex) {
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
func generateTangents(vertices []skinnedVertex, indices []uint32) {
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
