package types

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// MeshDraw is everything one Mesh call says beyond which mesh it draws.
//
// A zero MeshDraw is a valid draw at the origin with the bundled PBR, culled by
// the mesh's own baked sphere.
type MeshDraw struct {
	// Transform places the single instance. Transforms, when it is non-empty,
	// overrides it and places one instance per entry - a forest, a particle
	// field, a tile floor, from one call.
	//
	// The entries are culled one at a time and the survivors pack contiguously,
	// so the draw costs N sphere tests - exactly what N separate calls would
	// have paid - and one draw call. A blended instanced draw is the exception:
	// it splits back into one single-instance batch per surviving entry,
	// because sorting the set by its nearest instance would composite visibly
	// wrong. Per-instance animation is out of scope either way: the instances
	// share the draw's animation, so a hundred trees sway in lockstep and a
	// hundred independently-animated characters need a hundred calls.
	Transform  Transform
	Transforms []Transform
	// Material is the scene material to draw with; nil is the bundled PBR. A
	// mesh built from a custom vertex layout must name one, because the bundled
	// PBR's vertex stage reads scene.Vertex's eight attributes and nothing else.
	//
	// It is copied into the frame's own arenas at record - its tag entries and
	// each entry's parameters - so a caller may reuse or change it the moment
	// the call returns. The bytes a parameter carries are not copied: they are
	// m.Blobs, static by contract.
	Material Material
	// Params are extra gfx parameters bound to every instance of this draw, on
	// top of the three ranges scene binds itself. They are for what a custom
	// material declares and scene knows nothing about.
	Params []gfx.ParameterDescr
	// Bounds is the draw's local-space bounding sphere - xyz centre, w radius -
	// overriding the mesh's own. A zero Bounds falls through to the mesh's
	// baked sphere, and a mesh with none of its own is never culled.
	Bounds m.Vec4
	// NeverCull exempts the draw from culling outright, whatever Bounds says.
	NeverCull bool
}

// sphere reads the draw's explicit bounds as the sphere the culler wants.
func (d MeshDraw) sphere() m.Sphere {
	return m.Sphere{Center: m.Vec3{X: d.Bounds.X, Y: d.Bounds.Y, Z: d.Bounds.Z}, Radius: d.Bounds.W}
}

// Mesh records one draw of ref. It is the single recording call both minting
// paths feed: a durable ref from LookupAccess.BakeMesh and a frame-local one
// from TemporaryMesh record through the same call and are indistinguishable
// downstream.
//
// An anonymous inline call - canvas's DrawTriangles shape - was rejected
// because sorting requires a dense meshID on every draw and an anonymous call
// has none. The extra statement buys one MeshDraw, one sort key and one culling
// rule instead of two of each, which is affordable because scene's throwaway
// geometry floor is the debug vocabulary, not this API.
//
// A ref that names no mesh - a mint that was rejected, a released mesh, a
// temporary from an earlier frame - records nothing here and is reported and
// skipped by the flush, once per ref rather than once per draw.
func (q *OpQueue) Mesh(layers LayerMask, ref MeshRef, draw MeshDraw) {
	transforms := draw.Transforms
	if len(transforms) > 0 {
		start := len(q.meshes.transforms)
		q.meshes.transforms = append(q.meshes.transforms, transforms...)
		transforms = q.meshes.transforms[start:len(q.meshes.transforms):len(q.meshes.transforms)]
	}
	params := draw.Params
	if len(params) > 0 {
		start := len(q.meshes.params)
		q.meshes.params = append(q.meshes.params, params...)
		params = q.meshes.params[start:len(q.meshes.params):len(q.meshes.params)]
	}
	draw.Material = q.meshes.copyMaterial(draw.Material)
	draw.Transforms, draw.Params = transforms, params
	q.calls = append(q.calls, Op{Kind: OpMesh, Layers: layers, Mesh: ref, Draw: draw})
	record := DrawRecord{
		Layers: layers, Transform: draw.Transform, Material: draw.Material,
		Mesh: ref, Params: params, Bounds: draw.sphere(), NeverCull: draw.NeverCull,
	}
	if len(transforms) == 0 {
		q.draw(record)
		return
	}
	// One record per instance, tied together by a group. Culling is per
	// instance and a blended draw sorts per instance, both of which fall out of
	// that; the flush then packs an opaque group's survivors back into one
	// batch, so the N records cost one draw call and one material record.
	record.Group = uint32(len(q.draws)) + 1
	for _, transform := range transforms {
		record.Transform = transform
		q.draw(record)
	}
}

// TemporaryMesh registers geometry for this frame alone and returns a
// frame-local ref to it. It is the surface for geometry rebuilt every frame -
// a deforming procedural mesh, a debug hull, a stroke of terrain being edited.
//
// The vertices are written into the queue's arena here, so the caller's slice
// is free the moment the call returns. The ref carries the frame it was minted
// in: used in a later frame it is reported and skipped, rather than silently
// drawing whatever geometry now occupies its slot.
//
// A temporary mesh built on scene.Vertex pays an O(n) pack every frame, where a
// custom layout pays an O(n) memcpy: scene packs the standard vertex and cannot
// hand a caller's slice to gfx. That cost is stated rather than designed
// around - carving the standard vertex out would mean a second stored layout
// for one Go struct, which is two pipeline keys and two shader variants for a
// case that has no caller: every temporary mesh anything builds today ships a
// custom layout.
//
// The line to remember is that Mesh covers "the vertices change" and Model
// covers "the vertices are deformed by weights or bones". A caller wanting
// deforming procedural geometry owns its vertices and blends them on the CPU,
// through this call or through UpdateMesh.
//
// A temporary mesh is never culled by a sphere of its own: computing one is an
// O(n) pass over geometry that is thrown away at the end of the frame. Give the
// draw an explicit MeshDraw.Bounds when it should cull. Its indices stay uint32
// for the same reason: narrowing them would turn a zero-copy reinterpret into
// an allocating pass paid every frame rather than once.
func (q *OpQueue) TemporaryMesh[TVertex VertexLayout](
	vertices []TVertex, indices []uint32, topology gfx.PrimitiveTopology,
) MeshRef {
	input, err := mintMesh[TVertex](
		&q.meshes.layouts, &q.meshes.Arena, vertices, indices, topology, false)
	if err != nil {
		q.meshes.Reports = append(q.meshes.Reports, err)
		return MeshRef{}
	}
	q.meshes.Temporaries = append(q.meshes.Temporaries, temporaryMesh{
		vertices: input.vertices, indices: input.indices,
		vertexCount: input.vertexCount, indexCount: input.indexCount,
		topology: input.topology, layout: input.layout, standard: input.standard,
		uv: input.uv,
	})
	return MeshRef{
		source: MeshTemporary, id: uint32(len(q.meshes.Temporaries)), generation: q.frame,
	}
}

// temporaryMesh is one frame's worth of caller geometry, held as spans of the
// recording's arena until the flush turns it into a mesh record.
type temporaryMesh struct {
	vertices    Span
	indices     Span
	vertexCount int
	indexCount  int
	topology    gfx.PrimitiveTopology
	layout      []gfx.VertexAttr
	standard    bool
	// uv is the per-mesh record this frame's pack quantised the UVs against,
	// carried through to the mesh record so the draw names the right slot.
	uv SceneMesh
}

// MeshRecording is everything the frame's mesh calls own: the temporary meshes
// minted into it, the bytes behind them, the transforms, parameters and
// materials each MeshDraw and ModelDraw copied into it, and the mint errors the
// flush reports.
//
// It swaps with its published twin at the frame boundary along with the rest of
// the recording, so a published Op's borrowed slices stay valid while the next
// frame records over the other half. The layout cache is the one thing that
// does not swap: interning is what makes the per-frame path cheap, and it is
// keyed by Go type, which no frame boundary changes.
type MeshRecording struct {
	Temporaries []temporaryMesh
	Arena       []byte
	transforms  []Transform
	params      []gfx.ParameterDescr
	// materials holds the tag entries of every Material a draw named; each
	// entry's parameters are copied into params beside the draw's own.
	materials []MaterialTag
	Reports   []error
	layouts   layoutCache
}

func (r *MeshRecording) reset() {
	r.Temporaries = r.Temporaries[:0]
	r.Arena = r.Arena[:0]
	r.transforms = r.transforms[:0]
	clear(r.params)
	r.params = r.params[:0]
	clear(r.materials)
	r.materials = r.materials[:0]
	r.Reports = r.Reports[:0]
}

// copyMaterial copies a caller's Material into the recording's arenas - its tag
// entries into materials, each entry's parameters into params - and returns the
// copy, which aliases the arenas for exactly as long as a draw's copied
// Transforms and Params do.
//
// It is what takes away the obligation the flush used to put on every caller:
// reading the caller's slices at flush meant a material had to be kept alive
// and unchanged until then, with nothing saying so. Batching does not notice
// the difference, because a material is keyed by content and never by where
// its bytes live.
//
// A nil Material stays nil, because nil is the bundled PBR and an empty
// non-nil Material is a different answer - a material serving no pass. An empty
// one is returned as a zero-capacity window of itself rather than sliced out of
// the arena, because an arena that has never held an entry is nil and would
// turn the empty material into the bundled PBR.
func (r *MeshRecording) copyMaterial(material Material) Material {
	if material == nil {
		return nil
	}
	if len(material) == 0 {
		return material[:0:0]
	}
	start := len(r.materials)
	for _, entry := range material {
		entry.Descr, r.params = entry.Descr.CloneTo(r.params)
		r.materials = append(r.materials, entry)
	}
	return r.materials[start:len(r.materials):len(r.materials)]
}

// Record builds the mesh record one temporary draws from. Its buffers are
// inline bytes rather than baked ones, which is what makes gfx re-bake them
// into its own pooled per-frame buffers; the bytes are snapshotted there, so
// the recording is free to reuse this arena on the next frame.
//
// It names no index width, and the zero value is the uint32 a temporary mesh
// stages its indices at.
func (t temporaryMesh) Record(arena []byte) MeshRecord {
	record := MeshRecord{
		Vertices:    gfx.BufferWithBytes(t.vertices.Of(arena), true),
		VertexCount: t.vertexCount, indexCount: t.indexCount,
		Topology: t.topology, Layout: t.layout, Standard: t.standard, UV: t.uv,
	}
	if t.indexCount > 0 {
		record.Indices, record.Indexed = gfx.BufferWithBytes(t.indices.Of(arena), true), true
	}
	return record
}
