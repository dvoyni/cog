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
// carrying the six attributes of the standard layout at locations 0..5.
//
// It is not the bytes scene uploads. Scene packs every standard-layout vertex
// into the storage layout at bake (scene/vertexpack.go), so what a shader reads
// is that layout's offsets and formats rather than this struct's: 72 bytes of
// float are authored here and 32 are stored, because four of the six rows
// narrow - the normal, the tangent and both UV sets to four bytes each - and
// the colour quantises to a unorm byte a channel. An app writes directions in
// the m.Vec3 and m.Vec4 it would write anyway and never sees the encoding.
// Nothing in scene ever hands a Vertex back, so there is exactly one
// authoritative form, the authored one, and it flows one way.
//
// There is no joint and no weight here, and that is contract rather than an
// omission. No public path ever wrote them: a skin binding is set only from a
// loaded model's animation, so a buffer-built mesh never skins and the eight
// bytes would be dead in every mesh an app can build. The skinned layout - the
// same six attributes plus JOINTS_0 and WEIGHTS_0, 40 bytes at eight locations
// - belongs to the glTF loader and is unreachable from here.
//
// Normal and Tangent.XYZ are directions. Their length is divided out by the
// octahedral encode and is unrecoverable after bake - silently, as contract,
// because a check would fire on correct code: a normal computed from a cross
// product is one float of rounding from length 1.0001 and draws correctly.
// Tangent.W is handedness, and only its sign is stored.
//
// UV0 and UV1 store as positions inside the range the whole mesh spans, which
// scene derives at bake and re-derives on every update. A coordinate comes back
// within half a code of that range rather than exactly, and the range is what
// makes that half-code small: over the vendored corpus the worst is under half
// a texel of a 4096 texture, where a half float at the same coordinate is 32.
//
// Color is included on failure mode rather than on evidence: it is glTF core,
// costs four bytes as Unorm8x4, and leaving it out renders a vertex-coloured
// model silently white instead of erroring. It is an m.Color rather than four
// raw bytes because this is the one attribute whose authored and stored forms
// would otherwise have coincided, and a caller should not have to know which
// fields scene packs and which it copies; m.Color also says which space a
// component is in, where a byte cannot - glTF's COLOR_0 is linear, which is
// m.NewColorLinear. Its zero value is transparent black, so anything scene
// builds itself writes m.White.
type Vertex struct {
	Position m.Vec3
	Normal   m.Vec3
	Tangent  m.Vec4
	UV0      m.Vec2
	UV1      m.Vec2
	Color    m.Color
}

// VertexLayout reports the standard vertex's storage layout, in @location
// order. It is the one exported implementation whose attributes are not this
// struct's field offsets - see the interface's own documentation below.
func (Vertex) VertexLayout() []gfx.VertexAttr { return standardVertexLayout }

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
// scene.Vertex is the one exception a caller can see: scene packs it, so its
// method reports the storage layout and its Go fields are the authoring ones.
// The two differ - the stored normal, tangent and two UV sets are four bytes
// each against the struct's twelve, sixteen, eight and eight, and the stored
// colour is four against sixteen - so nothing may read a scene.Vertex layout as
// a description of the Go struct.
//
// There are exactly two layouts scene blesses: the standard one scene.Vertex
// reports, and the skinned one - the same six attributes plus JOINTS_0 and
// WEIGHTS_0 - which the glTF loader alone produces and which no exported type
// reports. Everything else is a custom layout and needs a custom Material.
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
	// standard reports whether the mesh carries one of the two layouts the
	// bundled PBR knows - the standard one every scene.Vertex mesh takes, or
	// the skinned one a model's geometry takes where some placement skins it.
	// It is recognised by type at mint time rather than by comparing
	// attributes, which is what lets the bundled PBR reject a custom layout and
	// what decides whether a bounding sphere could be computed at all.
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
	// uv is the per-mesh record the mesh's stored UVs decode against, and it is
	// re-derived on every bake and every update because a mesh's UV precision
	// follows the spread of the UVs in that same bake. Its zero value is the
	// record of a mesh with no range of its own - a custom layout, or a
	// standard mesh whose every UV is zero - which names the identity at slot
	// 0 instead of a record of its own.
	uv sceneMesh
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
// the vertex is scene.Vertex itself - which is what decides that the mint packs
// rather than reinterprets, and is narrower than the meshRecord flag it feeds:
// a model's geometry is a layout the bundled PBR knows without ever passing
// through here, and bakeModelGeometry says so directly.
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
	// uv is the per-mesh record the mint's UVs were quantised against, empty
	// for a custom layout. Unlike bounds it is kept for a temporary mesh too:
	// it is the only thing that makes its stored UVs mean anything, and it
	// costs the walk that was already made.
	uv sceneMesh
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
		packed, bounds, uv := packVertices(arena, any(vertices).([]Vertex))
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
		baked: true, bounds: input.bounds, uv: input.uv,
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
