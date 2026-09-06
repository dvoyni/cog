package scene

import (
	"fmt"
	"reflect"
	"unsafe"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// Vertex is the one vertex layout every scene mesh uses: glTF's eight core
// attributes at locations 0..7, 84 bytes, interleaved in one buffer.
//
// Nothing in it is optional. The bundled shader is one module with one vertex
// stage and no entry-point selection, so its inputs are these attributes at
// these types; a variant would be a whole second module carrying its own copy
// of the shading code. A buffer-built mesh never skins, so its last 24 bytes
// are dead — but the Go zero value of Joints and Weights is the correct one,
// because such a draw carries SCENE_NOSKIN and the shader never reads them.
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

// VertexLayout reports the attribute layout of the standard vertex, in
// @location order.
func (Vertex) VertexLayout() []gfx.VertexAttr { return standardVertexLayout[:] }

var standardVertexLayout = [...]gfx.VertexAttr{
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.Position)), gfx.Float32x3),
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.Normal)), gfx.Float32x3),
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.Tangent)), gfx.Float32x4),
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.UV0)), gfx.Float32x2),
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.UV1)), gfx.Float32x2),
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.Color)), gfx.Unorm8x4),
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.Joints)), gfx.Uint16x4),
	gfx.Attr(int(unsafe.Offsetof(Vertex{}.Weights)), gfx.Float32x4),
}

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
// The returned attributes map struct byte offsets to shader locations in
// order, and must match both the struct's memory layout and the vertex inputs
// of the material the mesh is drawn with.
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
	layout      []gfx.VertexAttr
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
	return gfx.MeshIndexed(r.vertices, r.indices, r.topology, r.layout...)
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

// meshInput is one mint's geometry once it has been validated and described,
// but before it has been copied anywhere: vertices still alias the caller's
// slice. The copy is the minting path's business, because the durable and
// frame-local paths copy into arenas with different lifetimes.
type meshInput struct {
	vertices    []byte
	indices     []byte
	vertexCount int
	indexCount  int
	topology    gfx.PrimitiveTopology
	layout      []gfx.VertexAttr
	layoutID    int
	standard    bool
	// bounds is the local-space sphere, computed only for standard-layout
	// vertices on the durable path; every other mesh gets a zero sphere and is
	// therefore never culled.
	bounds m.Sphere
}

// mintMesh validates one caller's geometry and describes it, whichever surface
// is minting it. It computes the bounding sphere only when asked, because the
// pass is O(n) and a temporary mesh would pay it every frame for a sphere that
// is thrown away with the geometry.
func mintMesh[TVertex VertexLayout](
	cache *layoutCache, vertices []TVertex, indices []uint32,
	topology gfx.PrimitiveTopology, wantBounds bool,
) (meshInput, error) {
	if err := validateMesh(len(vertices), indices, topology); err != nil {
		return meshInput{}, err
	}
	layoutID, layout, standard := cache.resolve[TVertex]()
	input := meshInput{
		vertices:    uploadBytes(vertices),
		indices:     indexBytes(indices),
		vertexCount: len(vertices),
		indexCount:  len(indices),
		topology:    topology,
		layout:      layout,
		layoutID:    layoutID,
		standard:    standard,
	}
	if wantBounds && standard {
		// The assertion holds because standard is exactly TVertex == Vertex,
		// which makes []TVertex and []Vertex the same type.
		input.bounds = vertexBounds(any(vertices).([]Vertex))
	}
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

// uploadBytes reinterprets any vertex slice as its upload bytes. It is the one
// place both mesh paths copy from, and it copies nothing itself.
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
func (l *Lookup) bakeMeshNow(input meshInput, bake bakeFunc) MeshRef {
	record := meshRecord{
		vertices: bake(input.vertices), indexCount: input.indexCount,
		vertexCount: input.vertexCount, topology: input.topology, layout: input.layout,
		layoutID: input.layoutID, standard: input.standard, baked: true, bounds: input.bounds,
	}
	if input.indexCount > 0 {
		record.indices, record.indexed = bake(input.indices), true
	}
	return l.claimMesh(record)
}

// indexBytes reinterprets indices as their upload bytes, the same way
// uploadBytes reinterprets vertices.
func indexBytes(indices []uint32) []byte {
	if len(indices) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&indices[0])), len(indices)*4)
}

// vertexBounds is the bounding sphere of standard-layout vertices: the
// circumsphere of their axis-aligned box, the same shape a glTF primitive gets
// from its POSITION accessor's min and max. It runs once, at bake time, which
// is why a temporary mesh never gets one - it is rebuilt every frame, and the
// O(n) pass would run every frame rather than once.
func vertexBounds(vertices []Vertex) m.Sphere {
	if len(vertices) == 0 {
		return m.Sphere{}
	}
	box := m.Box3{Min: vertices[0].Position, Max: vertices[0].Position}
	for i := range vertices[1:] {
		position := vertices[i+1].Position
		box.Min, box.Max = box.Min.Min(position), box.Max.Max(position)
	}
	return box.Sphere()
}
