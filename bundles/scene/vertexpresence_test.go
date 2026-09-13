package scene

import (
	"testing"

	"github.com/qmuntal/gltf"
)

// The rule, stated as a table: a converted geometry takes the skinned layout
// iff any placement draws it under SCENE_SKIN.
//
// The three rows are the three ways a placement can answer. A real skin binds
// the vertices and rewrites them, so it is part of the geometry key and every
// placement of that geometry skins. A node joint binds nothing and is the
// interesting one: it is per-placement, so the same converted mesh is
// plain-bound under one node and static under another, and the union is what
// decides its layout once for both. A prop no clip touches is neither.
func TestAGeometryTakesTheSkinnedLayoutIffSomePlacementSkinsIt(t *testing.T) {
	staticDoc := func() *gltf.Document {
		doc := testDoc()
		mesh := triangleMesh(doc, nil)
		doc.Nodes = []*gltf.Node{{Name: "prop", Mesh: gltf.Index(mesh)}}
		sceneOf(doc, 0)
		return doc
	}
	for _, c := range []struct {
		what    string
		doc     *gltf.Document
		skinned bool
	}{
		{"a prop no clip touches", staticDoc(), false},
		{"a mesh bound to a skin", skinnedDoc(), true},
		// One geometry, two placements, one of them plain-bound: the union is
		// what makes the static placement's buffer carry joints it never reads.
		{"a mesh shared by an animated node and a static one", mixedModel(t), true},
	} {
		model := convertTest(t, c.doc)
		if len(model.geometries) != 1 {
			t.Fatalf("%s converted %d geometries, want the one", c.what, len(model.geometries))
		}
		if got := model.geometries[0].skinnedLayout; got != c.skinned {
			t.Errorf("%s stores in the skinned layout = %v, want %v", c.what, got, c.skinned)
		}
		// Whatever the layout, the variant is still the placement's answer and
		// the two cannot disagree: a placement that skins is a placement whose
		// geometry supplies the joints and the weights.
		for i := range model.primitives {
			if model.primitives[i].skinned && !model.geometries[0].skinnedLayout {
				t.Errorf("%s: placement %d skins a geometry stored in the standard layout", c.what, i)
			}
		}
	}
}

// The bytes, end to end. A static primitive's vertex buffer is 32 bytes a
// vertex and its layout is the standard six; a skinned one is 40 and eight.
// Everything downstream of the mesh record is a buffer id and a size, so this
// is where the saving is observable at all.
func TestAModelPrimitiveStoresTheStrideItsLayoutNames(t *testing.T) {
	for _, c := range []struct {
		what   string
		doc    *gltf.Document
		attrs  int
		stride int
	}{
		{"a static primitive", indexedTriangleModel(t), standardVertexAttrs, storageStride},
		{"a skinned primitive", skinnedDoc(), 8, storageSkinnedStride},
	} {
		h := residentModel(t, c.doc)
		var attrs, bytes, vertices int
		found := 0
		h.kernel.ExecuteCommand[lookupProbeCmd](lookupProbeRequest{lookup: func(lookup *Lookup) {
			for i := range lookup.meshes {
				if lookup.meshes[i].vertexCount == 0 {
					continue
				}
				found++
				attrs = len(lookup.meshes[i].layout)
				bytes = lookup.meshes[i].vertices.Size()
				vertices = lookup.meshes[i].vertexCount
			}
		}})
		if found != 1 {
			t.Fatalf("%s: the load left %d meshes, want the one", c.what, found)
		}
		if attrs != c.attrs {
			t.Errorf("%s supplies %d attributes, want %d", c.what, attrs, c.attrs)
		}
		if bytes != vertices*c.stride {
			t.Errorf("%s stores %d bytes for %d vertices, want %d a vertex",
				c.what, bytes, vertices, c.stride)
		}
	}
}
