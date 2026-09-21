package internal

import (
	"slices"
	"testing"

	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/bundles/scene/internal/types"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
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
	ref = h.bake(triangle(), []uint32{0, 1, 2}, gfx.TopologyTriangleList)
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
				gfx.ShaderWithResource(types.SceneShaderPath), gfx.StateOpaque3D(), params...,
			)}}
		}
		q.Mesh(0, ref, scene.MeshDraw{Material: material(params), NeverCull: true})
		params[0] = gfx.FloatParam("key", 9)
		q.Mesh(0, ref, scene.MeshDraw{Material: material(original), NeverCull: true})
	})
	ref = h.bake(triangle(), []uint32{0, 1, 2}, gfx.TopologyTriangleList)
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

// A material shared across draws is rewritten in place between two of them in
// one frame, and only the later draw sees it. The first and third draws name
// the original content - the third through a material rebuilt from a copy of
// the original parameters - and the second names the rewritten one. Each draw
// carries its own instance count so its batch is found after the sort.
//
// A frame that reused one copy of a shared material by slice identity would
// hand the second draw the first draw's copy, and all three would be one
// material.
func TestMutatingASharedMeshMaterialBetweenDrawsChangesOnlyTheLaterDraw(t *testing.T) {
	var ref scene.MeshRef
	h := newHarness(t, func(q *scene.OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		params := pbrTestParams(1)
		original := slices.Clone(params)
		shared := materialOver(params)
		q.Mesh(0, ref, scene.MeshDraw{Material: shared, NeverCull: true, Transforms: instances(1)})
		params[0] = gfx.FloatParam("key", 9)
		q.Mesh(0, ref, scene.MeshDraw{Material: shared, NeverCull: true, Transforms: instances(2)})
		q.Mesh(0, ref, scene.MeshDraw{Material: materialOver(original), NeverCull: true, Transforms: instances(3)})
	})
	ref = h.bake(triangle(), []uint32{0, 1, 2}, gfx.TopologyTriangleList)
	h.frame()

	ids := materialIDsByInstanceCount(t, h.passes()[0].Batches, 3)
	if ids[1] != ids[3] {
		t.Fatalf("first and third draws have material ids %d and %d, want one: both name the original content", ids[1], ids[3])
	}
	if ids[2] == ids[1] {
		t.Fatalf("second draw has the first draw's material id %d, want its own: it names the rewritten material", ids[2])
	}
}

// A Model call reuses a shared replacement Material's copy by the same rule.
// Every primitive of one draw binds the replacement, so each instance count
// names one material across both primitives.
func TestMutatingASharedModelMaterialBetweenDrawsChangesOnlyTheLaterDraw(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, twoMaterialModel(t))), func(q *scene.OpQueue) {
		q.Camera(cameraMain, modelCamera())
		params := pbrTestParams(1)
		original := slices.Clone(params)
		shared := materialOver(params)
		q.Model(scene.LayersAll, modelPath, scene.ModelDraw{Material: shared, Transforms: instances(1)})
		params[0] = gfx.FloatParam("key", 9)
		q.Model(scene.LayersAll, modelPath, scene.ModelDraw{Material: shared, Transforms: instances(2)})
		q.Model(scene.LayersAll, modelPath, scene.ModelDraw{Material: materialOver(original), Transforms: instances(3)})
	})
	h.frameUntil(t, "the three model draws to pack", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 2*(1+2+3)
	})

	ids := materialIDsByInstanceCount(t, h.passes()[0].Batches, 3)
	if ids[1] != ids[3] {
		t.Fatalf("first and third draws have material ids %d and %d, want one: both name the original content", ids[1], ids[3])
	}
	if ids[2] == ids[1] {
		t.Fatalf("second draw has the first draw's material id %d, want its own: it names the rewritten material", ids[2])
	}
}

// A frame's copies do not outlive it. Each frame draws a material of its own,
// then a material shared by every frame; the shared one's copy from an earlier
// frame sits in an arena since rewritten, and a draw handed it would bind
// whatever that frame put there instead.
func TestASharedMeshMaterialIsCopiedAfreshEachFrame(t *testing.T) {
	var ref scene.MeshRef
	shared := opaqueMaterial(1)
	frame := 0
	h := newHarness(t, func(q *scene.OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		frame++
		q.Mesh(0, ref, scene.MeshDraw{Material: opaqueMaterial(float32(100 + frame)), NeverCull: true, Transforms: instances(1)})
		q.Mesh(0, ref, scene.MeshDraw{Material: shared, NeverCull: true, Transforms: instances(2)})
	})
	ref = h.bake(triangle(), []uint32{0, 1, 2}, gfx.TopologyTriangleList)

	for range 4 {
		h.frame()
		if errs := h.errors(); len(errs) != 0 {
			t.Fatalf("frame %d reported %v", frame, errs)
		}
		var sharedCopy scene.Material
		for _, op := range h.ops() {
			if op.Kind == scene.OpMesh && len(op.Draw.Transforms) == 2 {
				sharedCopy = op.Draw.Material
			}
		}
		if types.MaterialKeyOf(sharedCopy) != types.MaterialKeyOf(shared) {
			t.Fatalf("frame %d's shared draw binds a material other than the shared one", frame)
		}
	}
}

// materialOver builds a one-entry opaque material over params, aliasing them, so
// a caller that rewrites params rewrites the material.
func materialOver(params []gfx.ParameterDescr) scene.Material {
	return scene.Material{{Descr: gfx.MaterialWithState(
		gfx.ShaderWithResource(types.SceneShaderPath), gfx.StateOpaque3D(), params...,
	)}}
}

// instances is n transforms, for a draw whose batch is told apart by its
// instance count.
func instances(n int) []m.Transform {
	transforms := make([]m.Transform, n)
	for i := range transforms {
		transforms[i] = m.At(float32(i), 0, 0)
	}
	return transforms
}

// materialIDsByInstanceCount reads each batch's material id by its instance
// count, requiring every count to agree on one id and every count from 1 to
// draws to be present.
func materialIDsByInstanceCount(t *testing.T, batches []scene.BatchView, draws int) map[int]uint32 {
	t.Helper()
	ids := map[int]uint32{}
	for _, batch := range batches {
		if id, seen := ids[batch.InstanceCount]; seen && id != batch.MaterialID {
			t.Fatalf("batches of %d instances have material ids %d and %d, want one", batch.InstanceCount, id, batch.MaterialID)
		}
		ids[batch.InstanceCount] = batch.MaterialID
	}
	for n := 1; n <= draws; n++ {
		if _, ok := ids[n]; !ok {
			t.Fatalf("no batch of %d instances among %v", n, batches)
		}
	}
	return ids
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
	ref = h.bake(triangle(), []uint32{0, 1, 2}, gfx.TopologyTriangleList)
	h.frame()

	if pass := h.passes()[0]; pass.Instances != 0 {
		t.Fatalf("packed %d instances, want none: an empty material serves no pass", pass.Instances)
	}
}
