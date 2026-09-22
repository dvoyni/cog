package types

import (
	"encoding/binary"
	"fmt"
	"reflect"
	"unsafe"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// MeshSource discriminates where a MeshRef came from. model knows only its own
// durable meshes; a renderer that mints frame-local meshes of its own declares
// its sources past MeshDurable and builds their refs with NewMeshRef. It is
// what makes a frame-local ref used in a later frame detectable rather than
// silently wrong.
type MeshSource uint8

const (
	MeshNone MeshSource = iota
	MeshDurable
)

// rendererMeshID is the bit the public id of a mesh from any source past
// MeshDurable carries. The sources allocate independent dense ranges, and a
// sort key is one uint32 of meshID, so without a bit to tell them apart a
// renderer's own mesh and a durable one would collide in it and batch as though
// they were the same geometry. scene names it TemporaryMeshID.
const rendererMeshID uint32 = 1 << 31

// MeshRef names one mesh a renderer can draw. It is an opaque value: a source,
// a dense id that doubles as the sort key's meshID, and a generation that
// makes a recycled id detectable. Its zero value is no mesh.
//
// The padding after source is spelled out, because a MeshRef held in an ECS
// Component that a Hooks reader watches for Changed is compared by its bytes,
// and Go leaves implicit padding holding whatever was there.
type MeshRef struct {
	source     MeshSource
	_          [3]byte
	id         uint32
	generation uint32
}

// NewMeshRef builds the ref of a mesh a renderer minted itself, under a source
// it declared past MeshDurable. Nothing in model resolves such a ref: the
// renderer that minted it does.
func NewMeshRef(source MeshSource, id, generation uint32) MeshRef {
	return MeshRef{source: source, id: id, generation: generation}
}

// ID reports the mesh's dense id, or 0 when the ref names no mesh. A mesh from
// a renderer's own source carries rendererMeshID, so the sources never collide
// in a sort key or a BatchView.
func (r MeshRef) ID() uint32 {
	switch r.source {
	case MeshNone:
		return 0
	case MeshDurable:
		return r.id
	}
	return rendererMeshID | r.id
}

// Source reports where the ref came from.
func (r MeshRef) Source() MeshSource { return r.source }

// Index reports the ref's dense id within its own source, without the bit ID
// adds for a renderer's source.
func (r MeshRef) Index() uint32 { return r.id }

// Generation reports the generation the ref was minted at.
func (r MeshRef) Generation() uint32 { return r.generation }

// MeshRecord is one resident mesh: the buffers it draws from and the geometry
// gfx needs to describe it.
type MeshRecord struct {
	generation  uint32
	Vertices    gfx.BufferDescr
	Indices     gfx.BufferDescr
	indexCount  int
	VertexCount int
	Topology    gfx.PrimitiveTopology
	// IndexWidth is how wide one element of indices is. It is re-derived on
	// every bake rather than frozen for a ref's life the way layout is: layout
	// is part of a mesh's contract with a material, and width is not - for a
	// triangle list it never reaches the pipeline at all.
	IndexWidth gfx.IndexWidth
	Layout     []gfx.VertexAttr
	// layoutID is the layout's dense index in the cache that resolved it, kept
	// so UpdateMesh can reject a layout change with one integer compare rather
	// than by walking two attribute slices.
	layoutID int
	// Standard reports whether the mesh carries one of the two layouts the
	// bundled PBR knows - the standard one every model.Vertex mesh takes, or
	// the skinned one a model's geometry takes where some placement skins it.
	// It is recognised by type at mint time rather than by comparing
	// attributes, which is what lets the bundled PBR reject a custom layout and
	// what decides whether a bounding sphere could be computed at all.
	Standard bool
	// baked reports whether the record's buffers have reached gfx. A mesh baked
	// this frame and released before the flush drained it never had a buffer to
	// release. indexed reports whether a live index buffer is one of them, which
	// an update crossing between indexed and non-indexed geometry changes.
	baked   bool
	Indexed bool
	// Bounds is the mesh's local-space bounding sphere, or a zero sphere when
	// none was computed - a custom layout, where scene cannot find the
	// positions at all. A draw of such a mesh is never culled.
	Bounds m.Sphere
	// uv is the per-mesh record the mesh's stored UVs decode against, and it is
	// re-derived on every bake and every update because a mesh's UV precision
	// follows the spread of the UVs in that same bake. Its zero value is the
	// record of a mesh with no range of its own - a custom layout, or a
	// standard mesh whose every UV is zero - which names the identity at slot
	// 0 instead of a record of its own.
	UV SceneMesh
}

// Descr builds the gfx geometry for one resident mesh.
func (r MeshRecord) Descr() gfx.MeshDescr {
	return gfx.MeshIndexed(r.Vertices, r.Indices, r.IndexWidth, r.Topology, r.Layout...)
}

// LayoutCache interns vertex layouts by their Go type, so a per-frame mint pays
// one map probe rather than a method call and a slice copy. Both minting
// surfaces keep one: the ids are only ever compared within the cache that
// issued them.
type LayoutCache struct {
	ids     map[reflect.Type]int
	layouts [][]gfx.VertexAttr
}

// resolve returns the layout's dense id, the interned attributes, and whether
// the vertex is model.Vertex itself - which is what decides that the mint packs
// rather than reinterprets, and is narrower than the MeshRecord flag it feeds:
// a model's geometry is a layout the bundled PBR knows without ever passing
// through here, and bakeModelGeometry says so directly.
func (c *LayoutCache) resolve[TVertex VertexLayout]() (int, []gfx.VertexAttr, bool) {
	vertexType := reflect.TypeFor[TVertex]()
	standard := vertexType == reflect.TypeFor[Vertex]()
	if id, ok := c.ids[vertexType]; ok {
		return id, c.layouts[id], standard
	}
	if c.ids == nil {
		c.ids = map[reflect.Type]int{}
	}
	var vertex TVertex
	id := len(c.layouts)
	c.ids[vertexType] = id
	c.layouts = append(c.layouts, append([]gfx.VertexAttr(nil), vertex.VertexLayout()...))
	return id, c.layouts[id], standard
}

// MeshInput is one mint's geometry once it has been validated, described and
// written into an arena: vertices and indices are spans of that arena and no
// longer touch the caller's slices at all. Which arena is the minting surface's
// business - the Lookup's staging arena outlives the call, the recording's
// frame-local one does not - and is why the mint is handed one rather than
// owning it.
type MeshInput struct {
	vertices    Span
	indices     Span
	vertexCount int
	indexCount  int
	topology    gfx.PrimitiveTopology
	indexWidth  gfx.IndexWidth
	layout      []gfx.VertexAttr
	layoutID    int
	standard    bool
	// bounds is the local-space sphere, computed only for standard-layout
	// vertices on the durable path; every other mesh gets a zero sphere and is
	// therefore never culled.
	bounds m.Sphere
	// uv is the per-mesh record the mint's UVs were quantised against, empty
	// for a custom layout. Unlike bounds it is kept for a temporary mesh too:
	// it is the only thing that makes its stored UVs mean anything, and it
	// costs the walk that was already made.
	uv SceneMesh
}

// MintMesh validates one caller's geometry, describes it, and writes its bytes
// into arena, whichever surface is minting it.
//
// A standard-layout mesh is packed into the arena rather than reinterpreted and
// copied: the walk that writes it is the walk that bounds it, so the pack, the
// staging copy and the bounding sphere are one traversal where the first two
// were already two. A custom layout is still reinterpreted and copied, because
// scene cannot find an attribute inside a struct it does not know.
//
// durable says the geometry outlives the frame. It buys the bounding sphere -
// which a temporary mesh would recompute for a sphere thrown away with the
// geometry, and which would start culling draws that are not culled today - and
// narrowing the indices, which turns a zero-copy reinterpret into an allocating
// conversion. A temporary mesh keeps uint32 indices and reinterprets them.
//
// The bytes are written before a caller of UpdateMesh can reject the mint for
// changing layout, so a rejected update leaves its bytes in the arena with
// nothing pointing at them. The arena is discarded whole at the next drain, so
// that is a frame's worth of unread staging rather than a leak.
func MintMesh[TVertex VertexLayout](
	cache *LayoutCache, arena *[]byte, vertices []TVertex, indices []uint32,
	topology gfx.PrimitiveTopology, durable bool,
) (MeshInput, error) {
	if err := validateMesh(len(vertices), indices, topology); err != nil {
		return MeshInput{}, err
	}
	layoutID, layout, standard := cache.resolve[TVertex]()
	width := gfx.IndexUint32
	if durable {
		width = indexWidthFor(len(vertices))
	}
	input := MeshInput{
		vertexCount: len(vertices),
		indexCount:  len(indices),
		topology:    topology,
		indexWidth:  width,
		layout:      layout,
		layoutID:    layoutID,
		standard:    standard,
	}
	if standard {
		// The assertion holds because standard is exactly TVertex == Vertex,
		// which makes []TVertex and []Vertex the same type.
		packed, bounds, uv := PackVertices(arena, any(vertices).([]Vertex))
		input.vertices, input.uv = packed, uv
		if durable {
			input.bounds = bounds
		}
	} else {
		input.vertices = appendArena(arena, uploadBytes(vertices))
	}
	input.indices = appendArena(arena, indexBytes(indices, width))
	return input, nil
}

// validateMesh rejects geometry that could only draw garbage. Every failure
// yields a zero MeshRef, which then skips at draw time, so a caller that
// ignores the report still gets nothing rather than a crash.
func validateMesh(vertexCount int, indices []uint32, topology gfx.PrimitiveTopology) error {
	if vertexCount == 0 {
		return ErrMeshGeometryInvalid{Reason: "it has no vertices"}
	}
	for _, index := range indices {
		if int(index) >= vertexCount {
			return ErrMeshGeometryInvalid{Reason: fmt.Sprintf(
				"index %d is past its %d vertices", index, vertexCount)}
		}
	}
	// A non-indexed mesh assembles its vertices directly, so the count that has
	// to divide is whichever one the topology walks.
	count, kind := len(indices), "indices"
	if count == 0 {
		count, kind = vertexCount, "vertices"
	}
	if topology == gfx.TopologyTriangleList && count%3 != 0 {
		return ErrMeshGeometryInvalid{Reason: fmt.Sprintf(
			"it is a triangle list of %d %s, which is not a multiple of three", count, kind)}
	}
	return nil
}

// uploadBytes reinterprets a vertex slice as its upload bytes, copying nothing.
// It is what a custom layout uploads - scene cannot pack a struct it does not
// know, so a custom layout's Go memory is its buffer - and what the glTF
// loader hands its already-converted geometry over as.
func uploadBytes[TVertex VertexLayout](vertices []TVertex) []byte {
	if len(vertices) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&vertices[0])), len(vertices)*int(unsafe.Sizeof(vertices[0])))
}

// Mesh resolves a durable ref to the mesh it names. A released or stale ref resolves to
// nothing, which is what makes drawing one a reportable skip rather than a draw
// of whatever now occupies that slot.
func (l *Lookup) Mesh(ref MeshRef) (MeshRecord, bool) {
	if ref.source != MeshDurable || ref.id == 0 || int(ref.id) > len(l.meshes) {
		return MeshRecord{}, false
	}
	record := l.meshes[ref.id-1]
	if record.generation != ref.generation {
		return MeshRecord{}, false
	}
	return record, true
}

// BakeFunc uploads one buffer's bytes and returns the durable descriptor for
// them. The Lookup takes one rather than a gfx queue because the facade is
// deliberately GPU-free: baking is the flush's business, and the flush is the
// one place that already knows the backend is ready.
type BakeFunc func(data []byte) gfx.BufferDescr

// BakeTextureFunc uploads one texture's texels and returns the durable
// descriptor for them, the texture twin of BakeFunc.
type BakeTextureFunc func(width, height int, format gfx.TextureFormat, pixels []byte) gfx.TextureDescr

// claimMesh puts one record in the mesh table and returns the ref for it,
// reusing a released slot when there is one. A reused slot keeps the generation
// its release left behind, which is what makes a ref to the mesh that used to
// live there stale rather than a draw of the mesh that lives there now.
func (l *Lookup) claimMesh(record MeshRecord) MeshRef {
	if free := len(l.freeMeshes); free > 0 {
		id := l.freeMeshes[free-1]
		l.freeMeshes = l.freeMeshes[:free-1]
		record.generation = l.meshes[id-1].generation
		l.meshes[id-1] = record
		return MeshRef{source: MeshDurable, id: id, generation: record.generation}
	}
	record.generation = 1
	l.meshes = append(l.meshes, record)
	return MeshRef{source: MeshDurable, id: uint32(len(l.meshes)), generation: record.generation}
}

// bakeMeshNow registers durable geometry that is already at the flush, where
// the bake function is in hand. Scene's own unit meshes take this path; a
// caller's BakeMesh is deferred onto the Lookup instead.
//
// The index buffer is baked only when there is one: a zero-length bake still
// mints a buffer id, and a mesh carrying one would be recorded as indexed with
// no indices in it.
func (l *Lookup) bakeMeshNow(input MeshInput, arena []byte, bake BakeFunc) MeshRef {
	record := MeshRecord{
		Vertices: bake(input.vertices.Of(arena)), indexCount: input.indexCount,
		VertexCount: input.vertexCount, Topology: input.topology, IndexWidth: input.indexWidth,
		Layout: input.layout, layoutID: input.layoutID, Standard: input.standard,
		baked: true, Bounds: input.bounds, UV: input.uv,
	}
	if input.indexCount > 0 {
		record.Indices, record.Indexed = bake(input.indices.Of(arena)), true
	}
	return l.claimMesh(record)
}

// narrowIndexLimit is the largest vertex count that still indexes in uint16.
//
// It is 65535 rather than 65536 because 0xFFFF is WebGPU's primitive-restart
// value for a uint16 strip and scene keeps indexed strips legal. Every index is
// below the vertex count, so a mesh at this limit indexes no higher than 65534
// and the restart value never appears in a buffer at all - one vertex of
// headroom in place of a special case to document.
const narrowIndexLimit = 0xFFFF

// indexWidthFor is the whole of the rule, and nobody chooses it: a mesh's index
// width follows from how many vertices it has, O(1), on every path.
//
// No pass over the indices is needed to know it. Every index is already
// guaranteed below the vertex count - validateMesh enforces it on the authoring
// path and the glTF path has it by construction - so a max-index scan would
// have been new O(n) load-time work where a comparison does.
func indexWidthFor(vertexCount int) gfx.IndexWidth {
	if vertexCount <= narrowIndexLimit {
		return gfx.IndexUint16
	}
	return gfx.IndexUint32
}

// indexBytes renders indices as their upload bytes at the given width. At
// uint32 it reinterprets and copies nothing, the way uploadBytes reinterprets
// vertices; at uint16 it is an allocating O(n) narrowing pass, which is why
// only durable geometry asks for one.
//
// The narrowing writes native-endian words because the uint32 path is a
// reinterpret of native memory, and a buffer that changed byte order with the
// width would be a difference no caller could see coming.
func indexBytes(indices []uint32, width gfx.IndexWidth) []byte {
	if len(indices) == 0 {
		return nil
	}
	if width != gfx.IndexUint16 {
		return unsafe.Slice((*byte)(unsafe.Pointer(&indices[0])), len(indices)*4)
	}
	narrow := make([]byte, len(indices)*2)
	for i, index := range indices {
		binary.NativeEndian.PutUint16(narrow[i*2:], uint16(index))
	}
	return narrow
}

// InlineRecord builds the mesh record a frame-local mesh draws from, over the
// bytes the mint wrote into arena. Its buffers are inline bytes rather than
// baked ones, which is what makes gfx re-bake them into its own pooled
// per-frame buffers; the bytes are snapshotted there, so the minting renderer
// is free to reuse its arena on the next frame.
//
// It names no index width, and the zero value is the uint32 a frame-local mesh
// stages its indices at. Nor does it carry bounds: a frame-local mesh is never
// culled by a sphere of its own.
func (in MeshInput) InlineRecord(arena []byte) MeshRecord {
	record := MeshRecord{
		Vertices:    gfx.BufferWithBytes(in.vertices.Of(arena), true),
		VertexCount: in.vertexCount, indexCount: in.indexCount,
		Topology: in.topology, Layout: in.layout, Standard: in.standard, UV: in.uv,
	}
	if in.indexCount > 0 {
		record.Indices, record.Indexed = gfx.BufferWithBytes(in.indices.Of(arena), true), true
	}
	return record
}
