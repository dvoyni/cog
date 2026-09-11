package scene

import (
	"encoding/binary"
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// The whole of scene's index-width rule, at the boundary that matters. The
// threshold is 65535 rather than 65536 because 0xFFFF is WebGPU's
// primitive-restart value for a uint16 strip: at 65535 vertices the largest
// legal index is 65534, so the restart value never appears in the buffer.
func TestIndexWidthFollowsTheVertexCount(t *testing.T) {
	for _, c := range []struct {
		vertices int
		want     gfx.IndexWidth
	}{
		{1, gfx.IndexUint16},
		{3, gfx.IndexUint16},
		{65535, gfx.IndexUint16},
		{65536, gfx.IndexUint32},
		{200000, gfx.IndexUint32},
	} {
		if got := indexWidthFor(c.vertices); got != c.want {
			t.Errorf("indexWidthFor(%d) = %v, want %v", c.vertices, got, c.want)
		}
	}
}

// indexWidthOf reads back the width and the uploaded byte length of one
// durable mesh, which is the only place either is observable: everything
// downstream of the record is a buffer id and a size.
func indexWidthOf(t testing.TB, h *harness, ref MeshRef) (gfx.IndexWidth, int) {
	t.Helper()
	var width gfx.IndexWidth
	var size int
	found := false
	h.kernel.ExecuteCommand[lookupProbeCmd](lookupProbeRequest{lookup: func(lookup *Lookup) {
		record, ok := lookup.mesh(ref)
		found = ok
		width, size = record.indexWidth, record.indices.Size()
	}})
	if !found {
		t.Fatalf("mesh %d is not resident", ref.ID())
	}
	return width, size
}

// A mesh that fits in uint16 stores half its index bytes, and nobody asked for
// it: the width follows from the vertex count.
func TestADurableMeshNarrowsItsIndices(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) { q.Camera(testCamera, testCameraDescr()) })
	ref := h.bake(triangle(), []uint32{0, 1, 2}, gfx.TopologyTriangleList)
	h.frame()

	width, size := indexWidthOf(t, h, ref)
	if width != gfx.IndexUint16 {
		t.Fatalf("a 3-vertex mesh indexes at %v, want uint16", width)
	}
	if size != 6 {
		t.Fatalf("three uint16 indices uploaded %d bytes, want 6", size)
	}
	if len(*h.reported) != 0 {
		t.Fatalf("reported %v", *h.reported)
	}
}

// The narrowed bytes are the same indices, not a reinterpret of half of them.
func TestNarrowedIndexBytesAreTheSameIndices(t *testing.T) {
	bytes := indexBytes([]uint32{0, 1, 65534}, gfx.IndexUint16)
	if len(bytes) != 6 {
		t.Fatalf("three uint16 indices are %d bytes, want 6", len(bytes))
	}
	for i, want := range []uint16{0, 1, 65534} {
		if got := binary.NativeEndian.Uint16(bytes[i*2:]); got != want {
			t.Errorf("index %d = %d, want %d", i, got, want)
		}
	}
	wide := indexBytes([]uint32{0, 1, 2}, gfx.IndexUint32)
	if len(wide) != 12 {
		t.Fatalf("three uint32 indices are %d bytes, want 12", len(wide))
	}
}

// A temporary mesh is re-minted every frame by design, so narrowing would turn
// a zero-copy reinterpret into an allocating O(n) pass every frame rather than
// once. It keeps uint32.
func TestATemporaryMeshKeepsUint32Indices(t *testing.T) {
	var width gfx.IndexWidth
	var size int
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		ref := q.TemporaryMesh(triangle(), []uint32{0, 1, 2}, gfx.TopologyTriangleList)
		q.Mesh(0, ref, MeshDraw{NeverCull: true})
		mesh := q.meshes.temporaries[len(q.meshes.temporaries)-1]
		record := mesh.record(q.meshes.arena)
		width, size = record.indexWidth, record.indices.Size()
	})
	h.frame()

	if width != gfx.IndexUint32 {
		t.Fatalf("a temporary mesh indexes at %v, want uint32", width)
	}
	if size != 12 {
		t.Fatalf("three uint32 indices are %d bytes, want 12", size)
	}
}

// Layout is frozen for a ref's life because it is part of the mesh's contract
// with a material. Width is not: for a triangle list it never reaches the
// pipeline at all, so a mesh updated past the threshold simply widens.
func TestUpdateMeshRederivesTheIndexWidth(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) { q.Camera(testCamera, testCameraDescr()) })
	ref := h.bake(triangle(), []uint32{0, 1, 2}, gfx.TopologyTriangleList)
	h.frame()
	if width, _ := indexWidthOf(t, h, ref); width != gfx.IndexUint16 {
		t.Fatalf("the baked mesh indexes at %v, want uint16", width)
	}

	wide := make([]Vertex, 65536)
	h.lookup(func(la LookupAccess) {
		if !la.UpdateMesh(ref, wide, []uint32{0, 1, 2}) {
			t.Fatal("the widening update was refused")
		}
	})
	h.frame()
	if width, size := indexWidthOf(t, h, ref); width != gfx.IndexUint32 || size != 12 {
		t.Fatalf("after growing to 65536 vertices the mesh indexes at %v in %d bytes, want uint32 in 12", width, size)
	}

	h.lookup(func(la LookupAccess) {
		if !la.UpdateMesh(ref, wide[:65535], []uint32{0, 1, 2}) {
			t.Fatal("the narrowing update was refused")
		}
	})
	h.frame()
	if width, size := indexWidthOf(t, h, ref); width != gfx.IndexUint16 || size != 6 {
		t.Fatalf("back at 65535 vertices the mesh indexes at %v in %d bytes, want uint16 in 6", width, size)
	}
	if len(*h.reported) != 0 {
		t.Fatalf("reported %v", *h.reported)
	}
}

// indexedTriangleModel is one indexed primitive: the glTF path never calls
// mintMesh, so its width derivation is its own and has to be tested on it.
func indexedTriangleModel(t testing.TB) *gltf.Document {
	t.Helper()
	doc := testDoc()
	attributes := triangleAttributes(doc, [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}})
	indices := modeler.WriteIndices(doc, []uint16{0, 1, 2})
	doc.Meshes = append(doc.Meshes, &gltf.Mesh{
		Name:       "triangle",
		Primitives: []*gltf.Primitive{{Attributes: attributes, Indices: gltf.Index(indices)}},
	})
	doc.Nodes = []*gltf.Node{{Name: "prop", Mesh: gltf.Index(0)}}
	sceneOf(doc, 0)
	doc.Scene = gltf.Index(0)
	return doc
}

// A loaded model's geometry is baked without a pass over its indices at all -
// the vertex count is the whole test - and it narrows like any other durable
// mesh.
func TestAModelPrimitiveNarrowsItsIndices(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, indexedTriangleModel(t))),
		drawModel(modelPath, ModelDraw{}))
	h.frameUntil(t, "the model to become resident", func() bool {
		passes := h.passes()
		return len(passes) == 1 && passes[0].Instances == 1
	})

	var widths []gfx.IndexWidth
	var sizes []int
	h.kernel.ExecuteCommand[lookupProbeCmd](lookupProbeRequest{lookup: func(lookup *Lookup) {
		for i := range lookup.meshes {
			if lookup.meshes[i].indexed {
				widths = append(widths, lookup.meshes[i].indexWidth)
				sizes = append(sizes, lookup.meshes[i].indices.Size())
			}
		}
	}})
	if len(widths) != 1 {
		t.Fatalf("the load left %d indexed meshes, want 1", len(widths))
	}
	if widths[0] != gfx.IndexUint16 || sizes[0] != 6 {
		t.Fatalf("the loaded primitive indexes at %v in %d bytes, want uint16 in 6", widths[0], sizes[0])
	}
}

// Scene's own unit meshes bake through bakeMeshNow rather than through
// BakeMesh, so they derive the width on a path of their own. The unit box is 24
// vertices and 36 indices: 72 bytes where it used to be 144.
func TestAUnitMeshNarrowsItsIndices(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		q.Box(0, At(0, 0, 0), testBoxColor)
	})
	h.frame()

	var width gfx.IndexWidth
	var size int
	found := 0
	h.kernel.ExecuteCommand[lookupProbeCmd](lookupProbeRequest{lookup: func(lookup *Lookup) {
		for i := range lookup.meshes {
			if lookup.meshes[i].indexed {
				width, size = lookup.meshes[i].indexWidth, lookup.meshes[i].indices.Size()
				found++
			}
		}
	}})
	if found != 1 {
		t.Fatalf("the frame left %d indexed meshes, want the unit box alone", found)
	}
	if width != gfx.IndexUint16 || size != 72 {
		t.Fatalf("the unit box indexes at %v in %d bytes, want uint16 in 72", width, size)
	}
}

// The width the mesh record carries is what the render pass binds the buffer
// at, all the way through gfx.
func TestTheNarrowedWidthReachesTheRenderPass(t *testing.T) {
	var ref MeshRef
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		q.Mesh(0, ref, MeshDraw{})
	})
	ref = h.bake(triangle(), []uint32{0, 1, 2}, gfx.TopologyTriangleList)
	h.frame()

	if len(h.backend.indexBinds) != 1 {
		t.Fatalf("%d index buffers were bound, want 1", len(h.backend.indexBinds))
	}
	if got := h.backend.indexBinds[0]; got != gfx.IndexUint16 {
		t.Fatalf("the pass bound the index buffer at %v, want uint16", got)
	}
}
