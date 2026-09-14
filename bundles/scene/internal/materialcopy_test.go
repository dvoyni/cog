package internal

import (
	"slices"
	"testing"

	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/bundles/scene/internal/types"
	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/extensions/gfx/gpu"
)

// A Mesh call copies its Material at record, so a caller who changes a pass tag
// on the material after recording changes nothing drawn: the forward pass still
// finds its entry and draws the mesh.
func TestMutatingAMeshMaterialTagAfterRecordingChangesNothingDrawn(t *testing.T) {
	var ref scene.MeshRef
	h := newHarness(t, func(q *scene.OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		material := opaqueMaterial(1)
		q.Mesh(0, ref, scene.MeshDraw{Material: material, NeverCull: true})
		material[0].Tag = "shadow"
	})
	ref = h.bake(triangle(), []uint32{0, 1, 2}, gpu.TopologyTriangleList)
	h.frame()

	if pass := h.passes()[0]; pass.Instances != 1 {
		t.Fatalf("packed %d instances, want the draw recorded under the forward tag", pass.Instances)
	}
}

// The copy reaches each tag's parameters too. The first draw's material has a
// parameter rewritten in place after recording; the second names a material
// built from a copy of the original parameters. Copied at record, the two are
// one material by content; read at flush, the first would carry the rewritten
// value and a material id of its own.
func TestMutatingAMeshMaterialParameterAfterRecordingChangesNothingDrawn(t *testing.T) {
	var ref scene.MeshRef
	h := newHarness(t, func(q *scene.OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		params := pbrTestParams(1)
		// The copy shares the texture descriptors, whose inline pixels a
		// material's content key names by address.
		original := slices.Clone(params)
		material := func(params []gfx.ParameterDescr) scene.Material {
			return scene.Material{{Descr: gfx.MaterialWithState(
				gfx.ShaderWithResource(types.SceneShaderPath), gpu.StateOpaque3D, params...,
			)}}
		}
		q.Mesh(0, ref, scene.MeshDraw{Material: material(params), NeverCull: true})
		params[0] = gfx.FloatParam("key", 9)
		q.Mesh(0, ref, scene.MeshDraw{Material: material(original), NeverCull: true})
	})
	ref = h.bake(triangle(), []uint32{0, 1, 2}, gpu.TopologyTriangleList)
	h.frame()

	batches := h.passes()[0].Batches
	if len(batches) != 2 {
		t.Fatalf("published %d batches, want 2", len(batches))
	}
	if batches[0].MaterialID != batches[1].MaterialID {
		t.Fatalf("material ids %d and %d; the first draw saw the parameter written after it was recorded",
			batches[0].MaterialID, batches[1].MaterialID)
	}
}

// A Model call copies its replacement Material the same way.
func TestMutatingAModelMaterialAfterRecordingChangesNothingDrawn(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, twoMaterialModel(t))), func(q *scene.OpQueue) {
		q.Camera(cameraMain, modelCamera())
		material := opaqueMaterial(1)
		q.Model(scene.LayersAll, modelPath, scene.ModelDraw{Material: material})
		material[0].Tag = "shadow"
	})
	h.frameUntil(t, "the model to draw under the forward tag it was recorded with", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 2
	})
}

// An empty non-nil Material serves no pass, and the copy keeps it that way. The
// first frame's material arena has never held an entry, which is where a copy
// that slices the arena would hand back nil - and nil is the bundled PBR, which
// serves the forward pass and draws the mesh.
func TestAnEmptyMaterialStillServesNoPassAfterTheCopy(t *testing.T) {
	var ref scene.MeshRef
	h := newHarness(t, func(q *scene.OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		q.Mesh(0, ref, scene.MeshDraw{Material: scene.Material{}, NeverCull: true})
	})
	ref = h.bake(triangle(), []uint32{0, 1, 2}, gpu.TopologyTriangleList)
	h.frame()

	if pass := h.passes()[0]; pass.Instances != 0 {
		t.Fatalf("packed %d instances, want none: an empty material serves no pass", pass.Instances)
	}
}
