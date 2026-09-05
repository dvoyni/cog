package scene

import (
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
)

// MeshRef names one mesh scene can draw. It is an opaque value: a source, a
// dense scene id that doubles as the sort key's meshID, and a generation that
// makes a recycled id detectable. Its zero value is no mesh.
type MeshRef struct {
	source     meshSource
	id         uint32
	generation uint32
}

// ID reports the mesh's dense scene id, or 0 when the ref names no mesh.
func (r MeshRef) ID() uint32 {
	if r.source == meshNone {
		return 0
	}
	return r.id
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
}

// descr builds the gfx geometry for one resident mesh.
func (r meshRecord) descr() gfx.MeshDescr {
	return gfx.MeshIndexed(r.vertices, r.indices, r.topology, r.layout...)
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

// bakeMesh registers durable geometry and returns its ref.
func (l *Lookup) bakeMesh(
	vertices, indices []byte, indexCount, vertexCount int,
	topology gfx.PrimitiveTopology, layout []gfx.VertexAttr, bake bakeFunc,
) MeshRef {
	l.meshes = append(l.meshes, meshRecord{
		generation: 1, vertices: bake(vertices), indices: bake(indices),
		indexCount: indexCount, vertexCount: vertexCount,
		topology: topology, layout: layout,
	})
	id := uint32(len(l.meshes))
	return MeshRef{source: meshDurable, id: id, generation: l.meshes[id-1].generation}
}

// ensureUnitBox bakes scene's own unit cube the first time something draws one
// and returns the same ref forever after. It is lazy because the backend may
// not be ready at startup, and a mesh baked then would either panic or silently
// not exist.
func (l *Lookup) ensureUnitBox(bake bakeFunc) MeshRef {
	if l.unitBox.source != meshNone {
		return l.unitBox
	}
	vertices, indices := unitBoxGeometry()
	l.unitBox = l.bakeMesh(
		vertexBytes(vertices), indexBytes(indices), len(indices), len(vertices),
		gfx.TopologyTriangleList, standardVertexLayout[:], bake,
	)
	return l.unitBox
}

// unitBoxGeometry builds the 1x1x1 cube centred on the origin: four vertices
// per face, so every face keeps its own flat normal, and 12 triangles wound
// counter-clockwise when seen from outside.
func unitBoxGeometry() ([]Vertex, []uint32) {
	faces := [6]struct {
		normal, tangent, right, up m.Vec3
	}{
		{normal: m.Vec3{X: 1}, tangent: m.Vec3{Z: -1}, right: m.Vec3{Z: -1}, up: m.Vec3{Y: 1}},
		{normal: m.Vec3{X: -1}, tangent: m.Vec3{Z: 1}, right: m.Vec3{Z: 1}, up: m.Vec3{Y: 1}},
		{normal: m.Vec3{Y: 1}, tangent: m.Vec3{X: 1}, right: m.Vec3{X: 1}, up: m.Vec3{Z: -1}},
		{normal: m.Vec3{Y: -1}, tangent: m.Vec3{X: 1}, right: m.Vec3{X: 1}, up: m.Vec3{Z: 1}},
		{normal: m.Vec3{Z: 1}, tangent: m.Vec3{X: 1}, right: m.Vec3{X: 1}, up: m.Vec3{Y: 1}},
		{normal: m.Vec3{Z: -1}, tangent: m.Vec3{X: -1}, right: m.Vec3{X: -1}, up: m.Vec3{Y: 1}},
	}
	corners := [4]m.Vec2{{X: -1, Y: -1}, {X: 1, Y: -1}, {X: 1, Y: 1}, {X: -1, Y: 1}}
	vertices := make([]Vertex, 0, 24)
	indices := make([]uint32, 0, 36)
	for _, face := range faces {
		base := uint32(len(vertices))
		for _, corner := range corners {
			position := face.normal.
				Add(face.right.MulS(corner.X)).
				Add(face.up.MulS(corner.Y)).
				MulS(0.5)
			vertices = append(vertices, Vertex{
				Position: position,
				Normal:   face.normal,
				Tangent:  m.Vec4{X: face.tangent.X, Y: face.tangent.Y, Z: face.tangent.Z, W: 1},
				UV0:      m.Vec2{X: (corner.X + 1) / 2, Y: 1 - (corner.Y+1)/2},
				Color:    [4]uint8{255, 255, 255, 255},
			})
		}
		indices = append(indices, base, base+1, base+2, base, base+2, base+3)
	}
	return vertices, indices
}

// vertexBytes and indexBytes reinterpret geometry as its upload bytes, the same
// way the instance arena reinterprets its records.
func vertexBytes(vertices []Vertex) []byte {
	if len(vertices) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&vertices[0])), len(vertices)*int(unsafe.Sizeof(Vertex{})))
}

func indexBytes(indices []uint32) []byte {
	if len(indices) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&indices[0])), len(indices)*4)
}
