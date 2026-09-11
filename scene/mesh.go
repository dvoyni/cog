package scene

import (
	"encoding/binary"
	"fmt"
	"reflect"
	"unsafe"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// Vertex is the authoring vertex: the struct an app fills in for a scene mesh,
// carrying glTF's eight core attributes at locations 0..7.
//
// It is not the bytes scene uploads. Scene packs every standard-layout vertex
// into the storage layout at bake (scene/vertexpack.go), so what a shader reads
// is that layout's offsets and formats rather than this struct's: 84 bytes of
// float are authored here and 64 are stored, because the normal and the tangent
// each store in four. An app writes directions in the m.Vec3 and m.Vec4 it
// would write anyway and never sees the encoding. Nothing in scene ever hands a
// Vertex back, so there is exactly one authoritative form, the authored one,
// and it flows one way.
//
// Normal and Tangent.XYZ are directions. Their length is divided out by the
// octahedral encode and is unrecoverable after bake - silently, as contract,
// because a check would fire on correct code: a normal computed from a cross
// product is one float of rounding from length 1.0001 and draws correctly.
// Tangent.W is handedness, and only its sign is stored.
//
// Nothing in it is optional. A buffer-built mesh never skins, so its Joints and
// Weights are dead - but their Go zero value is the correct one, because such a
// draw carries SCENE_NOSKIN and the shader never reads them.
//
// Color is included on failure mode rather than on evidence: it is glTF core,
// costs four bytes as Unorm8x4, and leaving it out renders a vertex-coloured
// model silently white instead of erroring. Its zero value is transparent
// black, so anything scene builds itself writes white.
type Vertex struct {
	Position m.Vec3
	Normal   m.Vec3
	Tangent  m.Vec4
	UV0      m.Vec2
	UV1      m.Vec2
	Color    [4]uint8
	Joints   [4]uint16
	Weights  m.Vec4
}

// VertexLayout reports the standard vertex's storage layout, in @location
// order. It is the one implementation whose attributes are not this struct's
// field offsets - see the interface's own documentation below.
func (Vertex) VertexLayout() []gfx.VertexAttr { return standardVertexLayout[:] }

// meshSource discriminates where a MeshRef came from. It is what makes a
// frame-local ref used in a later frame detectable rather than silently wrong.
type meshSource uint8

const (
	meshNone meshSource = iota
	meshDurable
	meshTemporary
)

// temporaryMeshID is the bit a temporary mesh's public id carries. The two
// sources allocate independent dense ranges, and the sort key is one uint32 of
// meshID, so without a bit to tell them apart a temporary mesh and a durable
// one would collide in it and batch as though they were the same geometry.
const temporaryMeshID uint32 = 1 << 31

// VertexLayout is implemented by the plain-data vertex types scene accepts.
// The returned attributes describe the *buffer* scene uploads: they map byte
// offsets within one stored vertex to shader locations, in order, and must
// match the vertex inputs of the material the mesh is drawn with.
//
// For a custom layout the buffer is the caller's slice reinterpreted, so the
// offsets are also the Go struct's field offsets and the two readings coincide.
// scene.Vertex is the one exception: scene packs it, so its method reports the
// storage layout and its Go fields are the authoring ones. The two differ - the
// stored normal and tangent are four bytes each against the struct's twelve and
// sixteen - so nothing may read a scene.Vertex layout as a description of the
// Go struct.
//
// The one direction that fails is a shader input no attribute supplies. A
// layout supplying an attribute the shader never declares is legal and common,
// and gfx checks the pairing at pipeline time either way.
type VertexLayout interface {
	VertexLayout() []gfx.VertexAttr
}

// MeshRef names one mesh scene can draw. It is an opaque value: a source, a
// dense scene id that doubles as the sort key's meshID, and a generation that
// makes a recycled id detectable. Its zero value is no mesh.
type MeshRef struct {
	source     meshSource
	id         uint32
	generation uint32
}

// ID reports the mesh's dense scene id, or 0 when the ref names no mesh. A
// temporary mesh's id carries temporaryMeshID, so the two sources never collide
// in a sort key or a BatchView.
func (r MeshRef) ID() uint32 {
	switch r.source {
	case meshDurable:
		return r.id
	case meshTemporary:
		return temporaryMeshID | r.id
	}
	return 0
}

// meshRecord is one resident mesh: the buffers it draws from and the geometry
// gfx needs to describe it.
type meshRecord struct {
	generation  uint32
	vertices    gfx.BufferDescr
	indices     gfx.BufferDescr
	indexCount  int
	vertexCount int
	topology    gfx.PrimitiveTopology
	// indexWidth is how wide one element of indices is. It is re-derived on
	// every bake rather than frozen for a ref's life the way layout is: layout
	// is part of a mesh's contract with a material, and width is not - for a
	// triangle list it never reaches the pipeline at all.
	indexWidth gfx.IndexWidth
	layout     []gfx.VertexAttr
	// layoutID is the layout's dense index in the cache that resolved it, kept
	// so UpdateMesh can reject a layout change with one integer compare rather
	// than by walking two attribute slices.
	layoutID int
	// standard reports whether the mesh was built from scene.Vertex. It is
	// recognised by type at mint time rather than by comparing attributes,
	// which is what lets the bundled PBR reject a custom layout and what
	// decides whether a bounding sphere could be computed at all.
	standard bool
	// baked reports whether the record's buffers have reached gfx. A mesh baked
	// this frame and released before the flush drained it never had a buffer to
	// release. indexed reports whether a live index buffer is one of them, which
	// an update crossing between indexed and non-indexed geometry changes.
	baked   bool
	indexed bool
	// bounds is the mesh's local-space bounding sphere, or a zero sphere when
	// none was computed - a custom layout, where scene cannot find the
	// positions at all. A draw of such a mesh is never culled.
	bounds m.Sphere
}

// descr builds the gfx geometry for one resident mesh.
func (r meshRecord) descr() gfx.MeshDescr {
	return gfx.MeshIndexed(r.vertices, r.indices, r.indexWidth, r.topology, r.layout...)
}

// layoutCache interns vertex layouts by their Go type, so a per-frame mint pays
// one map probe rather than a method call and a slice copy. Both minting
// surfaces keep one: the ids are only ever compared within the cache that
// issued them.
type layoutCache struct {
	ids     map[reflect.Type]int
	layouts [][]gfx.VertexAttr
}

// resolve returns the layout's dense id, the interned attributes, and whether
// the vertex is scene's own standard one.
func (c *layoutCache) resolve[TVertex VertexLayout]() (int, []gfx.VertexAttr, bool) {
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

// meshInput is one mint's geometry once it has been validated, described and
// written into an arena: vertices and indices are spans of that arena and no
// longer touch the caller's slices at all. Which arena is the minting surface's
// business - the Lookup's staging arena outlives the call, the recording's
// frame-local one does not - and is why the mint is handed one rather than
// owning it.
type meshInput struct {
	vertices    span
	indices     span
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
}

// mintMesh validates one caller's geometry, describes it, and writes its bytes
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
func mintMesh[TVertex VertexLayout](
	cache *layoutCache, arena *[]byte, vertices []TVertex, indices []uint32,
	topology gfx.PrimitiveTopology, durable bool,
) (meshInput, error) {
	if err := validateMesh(len(vertices), indices, topology); err != nil {
		return meshInput{}, err
	}
	layoutID, layout, standard := cache.resolve[TVertex]()
	width := gfx.IndexUint32
	if durable {
		width = indexWidthFor(len(vertices))
	}
	input := meshInput{
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
		packed, bounds := packVertices(arena, any(vertices).([]Vertex))
		input.vertices = packed
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

// mesh resolves a ref to the mesh it names. A released or stale ref resolves to
// nothing, which is what makes drawing one a reportable skip rather than a draw
// of whatever now occupies that slot.
func (l *Lookup) mesh(ref MeshRef) (meshRecord, bool) {
	if ref.source != meshDurable || ref.id == 0 || int(ref.id) > len(l.meshes) {
		return meshRecord{}, false
	}
	record := l.meshes[ref.id-1]
	if record.generation != ref.generation {
		return meshRecord{}, false
	}
	return record, true
}

// bakeFunc uploads one buffer's bytes and returns the durable descriptor for
// them. The Lookup takes one rather than a gfx queue because the facade is
// deliberately GPU-free: baking is the flush's business, and the flush is the
// one place that already knows the backend is ready.
type bakeFunc func(data []byte) gfx.BufferDescr

// bakeTextureFunc uploads one texture's texels and returns the durable
// descriptor for them, the texture twin of bakeFunc.
type bakeTextureFunc func(width, height int, format gfx.TextureFormat, pixels []byte) gfx.TextureDescr

// claimMesh puts one record in the mesh table and returns the ref for it,
// reusing a released slot when there is one. A reused slot keeps the generation
// its release left behind, which is what makes a ref to the mesh that used to
// live there stale rather than a draw of the mesh that lives there now.
func (l *Lookup) claimMesh(record meshRecord) MeshRef {
	if free := len(l.freeMeshes); free > 0 {
		id := l.freeMeshes[free-1]
		l.freeMeshes = l.freeMeshes[:free-1]
		record.generation = l.meshes[id-1].generation
		l.meshes[id-1] = record
		return MeshRef{source: meshDurable, id: id, generation: record.generation}
	}
	record.generation = 1
	l.meshes = append(l.meshes, record)
	return MeshRef{source: meshDurable, id: uint32(len(l.meshes)), generation: record.generation}
}

// bakeMeshNow registers durable geometry that is already at the flush, where
// the bake function is in hand. Scene's own unit meshes take this path; a
// caller's BakeMesh is deferred onto the Lookup instead.
//
// The index buffer is baked only when there is one: a zero-length bake still
// mints a buffer id, and a mesh carrying one would be recorded as indexed with
// no indices in it.
func (l *Lookup) bakeMeshNow(input meshInput, arena []byte, bake bakeFunc) MeshRef {
	record := meshRecord{
		vertices: bake(input.vertices.of(arena)), indexCount: input.indexCount,
		vertexCount: input.vertexCount, topology: input.topology, indexWidth: input.indexWidth,
		layout: input.layout, layoutID: input.layoutID, standard: input.standard,
		baked: true, bounds: input.bounds,
	}
	if input.indexCount > 0 {
		record.indices, record.indexed = bake(input.indices.of(arena)), true
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
