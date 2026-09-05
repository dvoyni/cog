package scene

import (
	"math"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

func TestVertexIsTheEightyFourByteStandardLayout(t *testing.T) {
	if size := unsafe.Sizeof(Vertex{}); size != 84 {
		t.Fatalf("Vertex is %d bytes, want 84", size)
	}
	layout := Vertex{}.VertexLayout()
	want := []gfx.VertexAttr{
		gfx.Attr(0, gfx.Float32x3),  // POSITION
		gfx.Attr(12, gfx.Float32x3), // NORMAL
		gfx.Attr(24, gfx.Float32x4), // TANGENT
		gfx.Attr(40, gfx.Float32x2), // TEXCOORD_0
		gfx.Attr(48, gfx.Float32x2), // TEXCOORD_1
		gfx.Attr(56, gfx.Unorm8x4),  // COLOR_0
		gfx.Attr(60, gfx.Uint16x4),  // JOINTS_0
		gfx.Attr(68, gfx.Float32x4), // WEIGHTS_0
	}
	if len(layout) != len(want) {
		t.Fatalf("layout has %d attributes, want %d", len(layout), len(want))
	}
	for i := range want {
		if layout[i] != want[i] {
			t.Fatalf("attribute %d is %+v, want %+v", i, layout[i], want[i])
		}
	}
}

func TestUnitBoxIsACentredCubeWithPerFaceNormals(t *testing.T) {
	vertices, indices := unitBoxGeometry()
	if len(vertices) != 24 {
		t.Fatalf("the unit box has %d vertices, want 24 (four per face)", len(vertices))
	}
	if len(indices) != 36 {
		t.Fatalf("the unit box has %d indices, want 36", len(indices))
	}
	for i, vertex := range vertices {
		for _, axis := range [3]float32{vertex.Position.X, vertex.Position.Y, vertex.Position.Z} {
			if axis != 0.5 && axis != -0.5 {
				t.Fatalf("vertex %d is at %v, which is not on the unit cube", i, vertex.Position)
			}
		}
		if length := vertex.Normal.Length(); length < 0.999 || length > 1.001 {
			t.Fatalf("vertex %d has normal %v of length %v, want a unit normal", i, vertex.Normal, length)
		}
		if vertex.Color != [4]uint8{255, 255, 255, 255} {
			t.Fatalf("vertex %d has colour %v, want opaque white", i, vertex.Color)
		}
	}
	for _, index := range indices {
		if int(index) >= len(vertices) {
			t.Fatalf("index %d is past the %d vertices", index, len(vertices))
		}
	}
}

// The winding is what decides whether a back-face-culled cube is solid or
// inside out, and it is the one thing about the box a test can settle without a
// GPU: every triangle of a convex hull must face away from the centre.
func TestUnitBoxTrianglesWindCounterClockwiseOutwards(t *testing.T) {
	vertices, indices := unitBoxGeometry()
	for i := 0; i < len(indices); i += 3 {
		a := vertices[indices[i]].Position
		b := vertices[indices[i+1]].Position
		c := vertices[indices[i+2]].Position
		face := b.Sub(a).Cross(c.Sub(a))
		centroid := a.Add(b).Add(c).MulS(1.0 / 3)
		if face.Dot(centroid) <= 0 {
			t.Fatalf("triangle %d faces inwards: normal %v against centroid %v", i/3, face, centroid)
		}
		if face.Dot(vertices[indices[i]].Normal) <= 0 {
			t.Fatalf("triangle %d disagrees with its own vertex normal %v", i/3, vertices[indices[i]].Normal)
		}
	}
}

func TestMeshRefZeroValueIsNoMesh(t *testing.T) {
	var ref MeshRef
	if ref.ID() != 0 {
		t.Fatalf("the zero MeshRef has id %d, want 0", ref.ID())
	}
}

func TestUnitBoxBakesOnceAndOnlyOnFirstUse(t *testing.T) {
	lookup := NewLookup(DefaultConfig())
	if lookup.unitBox.ID() != 0 {
		t.Fatal("the unit box was baked before anything asked for it")
	}
	var baked []gfx.BufferDescr
	bake := func(data []byte) gfx.BufferDescr {
		baked = append(baked, gfx.BufferWithBytes(data, false))
		return baked[len(baked)-1]
	}
	first := lookup.ensureUnitBox(bake)
	second := lookup.ensureUnitBox(bake)
	if first.ID() == 0 {
		t.Fatal("the unit box did not bake on first use")
	}
	if first != second {
		t.Fatalf("the second use minted a different ref: %+v then %+v", first, second)
	}
	if len(baked) != 2 {
		t.Fatalf("the unit box baked %d buffers, want 2 (vertices and indices)", len(baked))
	}
}

func TestMeshLookupResolvesABakedRef(t *testing.T) {
	lookup := NewLookup(DefaultConfig())
	ref := lookup.ensureUnitBox(func(data []byte) gfx.BufferDescr {
		return gfx.BufferWithBytes(data, false)
	})
	mesh, ok := lookup.mesh(ref)
	if !ok {
		t.Fatal("the baked unit box does not resolve")
	}
	if mesh.indexCount != 36 {
		t.Fatalf("the resolved mesh has %d indices, want 36", mesh.indexCount)
	}
	if _, ok := lookup.mesh(MeshRef{}); ok {
		t.Fatal("the zero MeshRef resolved to a mesh")
	}
	if _, ok := lookup.mesh(MeshRef{id: ref.id, generation: ref.generation + 1}); ok {
		t.Fatal("a stale generation resolved to a mesh")
	}
}

// A durable mesh with the standard layout bakes its bounding sphere once, at
// bake time, so the unit box culls by a sphere of radius sqrt(3)/2 about the
// origin without anybody computing it per frame.
func TestUnitBoxBakesItsBoundingSphere(t *testing.T) {
	lookup := NewLookup(DefaultConfig())
	ref := lookup.ensureUnitBox(func(data []byte) gfx.BufferDescr {
		return gfx.BufferWithBytes(data, false)
	})
	mesh, _ := lookup.mesh(ref)
	if mesh.bounds.Center != (m.Vec3{}) {
		t.Fatalf("the unit box's sphere is centred at %v, want the origin", mesh.bounds.Center)
	}
	if want := float32(math.Sqrt(3)) / 2; !near(mesh.bounds.Radius, want) {
		t.Fatalf("the unit box's sphere has radius %v, want %v", mesh.bounds.Radius, want)
	}
}

// The sphere is the circumsphere of the positions' axis-aligned box, the same
// shape glTF's POSITION min/max gives a loaded primitive.
func TestVertexBoundsIsTheCircumsphereOfThePositions(t *testing.T) {
	vertices := []Vertex{
		{Position: m.Vec3{X: 1, Y: 2, Z: 3}},
		{Position: m.Vec3{X: 5, Y: 2, Z: 3}},
		{Position: m.Vec3{X: 3, Y: 0, Z: 3}},
	}
	sphere := vertexBounds(vertices)
	if sphere.Center != (m.Vec3{X: 3, Y: 1, Z: 3}) {
		t.Fatalf("centre %v, want the box centre (3,1,3)", sphere.Center)
	}
	if want := float32(math.Sqrt(5)); !near(sphere.Radius, want) {
		t.Fatalf("radius %v, want half the box diagonal %v", sphere.Radius, want)
	}
	if vertexBounds(nil) != (m.Sphere{}) {
		t.Fatal("no vertices gave a non-zero sphere")
	}
}
