package sceneimpl

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// The glTF document builders the model tests share. They are copies of the
// ones internal's loader tests build with, because Go shares no test file
// between two packages.

// testDoc is an empty document with the one buffer the modeler writers append
// into. Tests build documents rather than reading files, because the vendored
// asset set lives in cog-examples and this package has no testdata.
func testDoc() *gltf.Document {
	return &gltf.Document{Asset: gltf.Asset{Version: "2.0"}}
}

// triangleAttributes writes one flat triangle's positions and returns the
// primitive attributes naming them.
func triangleAttributes(doc *gltf.Document, positions [][3]float32) gltf.PrimitiveAttributes {
	return gltf.PrimitiveAttributes{gltf.POSITION: modeler.WritePosition(doc, positions)}
}

// triangleMesh adds one flat triangle as a mesh and returns its index.
func triangleMesh(doc *gltf.Document, material *int) int {
	attributes := triangleAttributes(doc, [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}})
	doc.Meshes = append(doc.Meshes, &gltf.Mesh{
		Name:       "triangle",
		Primitives: []*gltf.Primitive{{Attributes: attributes, Material: material}},
	})
	return len(doc.Meshes) - 1
}

// sceneOf wires a node list into the document's one scene.
func sceneOf(doc *gltf.Document, roots ...int) {
	doc.Scenes = append(doc.Scenes, &gltf.Scene{Name: "scene", Nodes: roots})
}

// namedSceneOf appends one named scene, so a Scene selector has something to
// match. sceneOf is the single-scene shorthand every other test uses.
func namedSceneOf(doc *gltf.Document, name string, roots ...int) {
	doc.Scenes = append(doc.Scenes, &gltf.Scene{Name: name, Nodes: roots})
}

// testSampleRate is the rate the bake tests convert at. It is the default, so
// the frame counts below are the ones a real load produces.
const testSampleRate = 60

// skinnedDoc builds a two-joint skin: a root bone at the origin and a child
// bone one unit up, with a mesh bound entirely to the child.
func skinnedDoc() *gltf.Document {
	doc := testDoc()
	positions := [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}
	doc.Meshes = append(doc.Meshes, &gltf.Mesh{Name: "body", Primitives: []*gltf.Primitive{{
		Attributes: gltf.PrimitiveAttributes{
			gltf.POSITION:  modeler.WritePosition(doc, positions),
			gltf.JOINTS_0:  modeler.WriteJoints(doc, [][4]uint16{{1}, {1}, {1}}),
			gltf.WEIGHTS_0: modeler.WriteWeights(doc, [][4]float32{{1}, {1}, {1}}),
		},
	}}})
	doc.Skins = append(doc.Skins, &gltf.Skin{
		Name:   "rig",
		Joints: []int{1, 2},
		// The tip bone binds one unit up, so its inverse bind translates one
		// unit down - which is a value the assertions below can read directly
		// off the packed record.
		InverseBindMatrices: gltf.Index(modeler.WriteInverseBindMatrices(doc, [][4][4]float32{
			inverseBind(m.NewMat4()), inverseBind(m.Translation4(0, -1, 0)),
		})),
	})
	doc.Nodes = []*gltf.Node{
		{Name: "body", Mesh: gltf.Index(0), Skin: gltf.Index(0), Translation: [3]float64{7, 0, 0}},
		{Name: "root", Children: []int{2}},
		{Name: "tip", Translation: [3]float64{0, 1, 0}},
	}
	sceneOf(doc, 0, 1)
	return doc
}

// inverseBind takes a matrix in cog's own column-major layout and returns what
// the modeler needs in order to serialise it, which is its transpose: glTF's
// buffer is column-major, but the modeler indexes a [4][4]float32 as
// [row][column].
//
// Spelling the transpose out here, once, is the point. A fixture built the way
// the loader reads cancels the loader's mistakes - the round trip agrees with
// itself whichever way both halves are wrong - and a real rig then loads with
// its skeleton inside out while every test passes. Writing the fixture from a
// matrix the test can read at a glance is what makes the convention assertable
// rather than merely self-consistent.
func inverseBind(matrix m.Mat4) [4][4]float32 {
	var value [4][4]float32
	for column := range 4 {
		for row := range 4 {
			value[row][column] = matrix[column*4+row]
		}
	}
	return value
}

// rotationClip adds an animation that spins one node about Z over a second,
// and returns the document.
func rotationClip(doc *gltf.Document, name string, node int, times []float32, values [][4]float32) {
	sampler := &gltf.AnimationSampler{
		Input:  modeler.WriteAccessor(doc, gltf.TargetNone, times),
		Output: modeler.WriteAccessor(doc, gltf.TargetNone, values),
	}
	doc.Animations = append(doc.Animations, &gltf.Animation{
		Name:     name,
		Samplers: []*gltf.AnimationSampler{sampler},
		Channels: []*gltf.AnimationChannel{{
			Sampler: 0,
			Target:  gltf.AnimationChannelTarget{Node: gltf.Index(node), Path: gltf.TRSRotation},
		}},
	})
}

// morphedMesh adds one triangle mesh carrying morph targets and returns its
// index.
//
// The base attributes are written a slot at a time rather than through a
// helper, because which of them the file authored is exactly what decides the
// record mask - a fixture that always writes all three could not express the
// case the mask exists for.
func morphedMesh(
	doc *gltf.Document, normals, tangents bool, targets []gltf.PrimitiveAttributes,
) int {
	positions := [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}}
	attributes := gltf.PrimitiveAttributes{gltf.POSITION: modeler.WritePosition(doc, positions)}
	if normals {
		attributes[gltf.NORMAL] = modeler.WriteNormal(doc, [][3]float32{
			{0, 0, 1}, {0, 0, 1}, {0, 0, 1},
		})
	}
	if tangents {
		attributes[gltf.TANGENT] = modeler.WriteTangent(doc, [][4]float32{
			{1, 0, 0, 1}, {1, 0, 0, 1}, {1, 0, 0, 1},
		})
	}
	doc.Meshes = append(doc.Meshes, &gltf.Mesh{
		Name:       "face",
		Primitives: []*gltf.Primitive{{Attributes: attributes, Targets: targets}},
	})
	return len(doc.Meshes) - 1
}

// deltaTarget writes one morph target's delta accessors. glTF stores every one
// of them as VEC3, tangent included - the handedness in w is not a thing a
// shape can move.
func deltaTarget(doc *gltf.Document, position, normal, tangent [][3]float32) gltf.PrimitiveAttributes {
	target := gltf.PrimitiveAttributes{}
	if position != nil {
		target[gltf.POSITION] = modeler.WriteAccessor(doc, gltf.TargetNone, position)
	}
	if normal != nil {
		target[gltf.NORMAL] = modeler.WriteAccessor(doc, gltf.TargetNone, normal)
	}
	if tangent != nil {
		target[gltf.TANGENT] = modeler.WriteAccessor(doc, gltf.TargetNone, tangent)
	}
	return target
}
