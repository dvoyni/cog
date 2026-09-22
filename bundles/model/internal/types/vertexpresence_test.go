package types

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

// mixedModel puts the same mesh under an animated node and a static one, which
// is the shape that makes "is this draw skinned" unanswerable from the
// primitive: one converted geometry, two placements, one of them plain-bound.
func mixedModel(t testing.TB) *gltf.Document {
	t.Helper()
	doc := testDoc()
	triangleMesh(doc, nil)
	doc.Nodes = []*gltf.Node{
		{Name: "wheel", Mesh: gltf.Index(0)},
		{Name: "prop", Mesh: gltf.Index(0), Translation: [3]float64{2, 0, 0}},
	}
	sceneOf(doc, 0, 1)
	doc.Scene = gltf.Index(0)
	rotationClip(doc, "spin", 0, []float32{0, 1}, [][4]float32{{0, 0, 0, 1}, {0, 0, 1, 0}})
	return doc
}
