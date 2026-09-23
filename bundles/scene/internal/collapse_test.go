package internal

import (
	"testing"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// Two calls recorded separately that share a mesh, a material, their per-draw
// data and their animation pack as one batch: the sort already put them side
// by side, and the batch they make is the one a single instanced call of both
// would have made, instance for instance.
func TestSeparatelyRecordedEqualDrawsPackAsOneBatch(t *testing.T) {
	var ref model.MeshRef
	h := newHarness(t, func(q *scene.OpQueue) {
		q.Camera(testCamera, forwardCamera())
		q.Mesh(0, ref, scene.MeshDraw{Transforms: []m.Transform{m.At(0, 0, -5), m.At(1, 0, -5)}})
		q.Mesh(0, ref, scene.MeshDraw{Transform: m.At(2, 0, -5)})
		q.Mesh(0, ref, scene.MeshDraw{Transform: m.At(3, 0, -5)})
	})
	ref = h.bake(triangle(), nil, gfx.TopologyTriangleList)
	h.frame()

	batches := h.passes()[0].Batches
	if len(batches) != 1 {
		t.Fatalf("published %d batches, want the four equal draws as one: %+v", len(batches), batches)
	}
	if batches[0].FirstInstance != 0 || batches[0].InstanceCount != 4 {
		t.Fatalf("the batch spans %+v, want [0, 4)", batches[0])
	}
	if len(h.backend.draws) != 1 {
		t.Fatalf("the backend saw %d draw calls, want one", len(h.backend.draws))
	}
}

// A different key is a different batch: the draws do not even sort together
// unless they share a mesh and a material.
func TestDrawsOfDifferentMeshesStaySeparate(t *testing.T) {
	var a, b model.MeshRef
	h := newHarness(t, func(q *scene.OpQueue) {
		q.Camera(testCamera, forwardCamera())
		q.Mesh(0, a, scene.MeshDraw{Transform: m.At(0, 0, -5)})
		q.Mesh(0, b, scene.MeshDraw{Transform: m.At(1, 0, -5)})
		q.Mesh(0, a, scene.MeshDraw{Material: opaqueMaterial(1), Transform: m.At(2, 0, -5)})
	})
	a = h.bake(triangle(), nil, gfx.TopologyTriangleList)
	b = h.bake(triangle(), nil, gfx.TopologyTriangleList)
	h.frame()

	if batches := h.passes()[0].Batches; len(batches) != 3 {
		t.Fatalf("published %d batches, want one per key: %+v", len(batches), batches)
	}
}

// Equal keys with different per-draw parameters stay separate, because a batch
// binds one draw's parameters for all of its instances. Equal parameters held
// in different backings still merge: what is compared is the values.
func TestDrawsWithDifferentParamsStaySeparate(t *testing.T) {
	var ref model.MeshRef
	h := newHarness(t, func(q *scene.OpQueue) {
		q.Camera(testCamera, forwardCamera())
		material := opaqueMaterial(1)
		q.Mesh(0, ref, scene.MeshDraw{Material: material, Transform: m.At(0, 0, -5),
			Params: []gfx.ParameterDescr{gfx.FloatParam("key", 2)}})
		q.Mesh(0, ref, scene.MeshDraw{Material: material, Transform: m.At(1, 0, -5),
			Params: []gfx.ParameterDescr{gfx.FloatParam("key", 2)}})
		q.Mesh(0, ref, scene.MeshDraw{Material: material, Transform: m.At(2, 0, -5),
			Params: []gfx.ParameterDescr{gfx.FloatParam("key", 3)}})
	})
	ref = h.bake(triangle(), nil, gfx.TopologyTriangleList)
	h.frame()

	batches := h.passes()[0].Batches
	if len(batches) != 2 {
		t.Fatalf("published %d batches, want the equal pair and the odd one: %+v", len(batches), batches)
	}
	if batches[0].InstanceCount != 2 || batches[1].InstanceCount != 1 {
		t.Fatalf("the batches hold %d and %d instances, want 2 and 1",
			batches[0].InstanceCount, batches[1].InstanceCount)
	}
}

// Equal keys with a different bundled-PBR record stay separate: a batch writes
// one paint. Two debug boxes of different colours are the plainest case, since
// the colour is their paint and nothing else about them differs.
func TestDrawsWithDifferentPaintStaySeparate(t *testing.T) {
	h := newHarness(t, func(q *scene.OpQueue) {
		q.Camera(testCamera, forwardCamera())
		q.Box(0, m.At(0, 0, -5), testBoxColor)
		q.Box(0, m.At(1, 0, -5), testBoxColor)
		q.Box(0, m.At(2, 0, -5), testLineColor)
	})
	h.frame()

	batches := h.passes()[0].Batches
	if len(batches) != 2 {
		t.Fatalf("published %d batches, want the equal pair and the odd one: %+v", len(batches), batches)
	}
	if len(h.backend.draws) != 2 {
		t.Fatalf("the backend saw %d draw calls, want one per colour", len(h.backend.draws))
	}
}

// Two model calls with equal overrides merge and a third with its own stays
// apart: the overrides are the draws' gfx parameters, and a batch binds one set.
func TestModelDrawsWithDifferentOverridesStaySeparate(t *testing.T) {
	h := newHarnessWithFiles(t, modelFiles(glb(t, skinnedModel(t))), func(q *scene.OpQueue) {
		q.Camera(cameraMain, modelCamera())
		red := []gfx.ParameterDescr{gfx.ColorParam("baseColorFactor", m.NewColorLinear(1, 0, 0, 1))}
		q.Model(scene.LayersAll, modelPath, scene.ModelDraw{Transform: m.At(0, 0, 0), OverrideParams: red})
		q.Model(scene.LayersAll, modelPath, scene.ModelDraw{Transform: m.At(1, 0, 0),
			OverrideParams: []gfx.ParameterDescr{gfx.ColorParam("baseColorFactor", m.NewColorLinear(1, 0, 0, 1))}})
		q.Model(scene.LayersAll, modelPath, scene.ModelDraw{Transform: m.At(2, 0, 0),
			OverrideParams: []gfx.ParameterDescr{gfx.ColorParam("baseColorFactor", m.NewColorLinear(0, 1, 0, 1))}})
	})
	h.frameUntil(t, "the model to become resident", func() bool {
		return len(h.passes()) == 1 && h.passes()[0].Instances == 3
	})
	batches := h.passes()[0].Batches
	if len(batches) != 2 {
		t.Fatalf("published %d batches, want the two red calls as one and the green apart: %+v",
			len(batches), batches)
	}
}

// Two unanimated calls of one model merge, which is the instancing demo's
// crate stack. Two animated calls do not, even at the same clip time: each
// call packs its own sceneAnim block and a batch carries one animation.
func TestSeparatelyAnimatedDrawsStaySeparate(t *testing.T) {
	still := newHarnessWithFiles(t, modelFiles(glb(t, skinnedModel(t))), func(q *scene.OpQueue) {
		q.Camera(cameraMain, modelCamera())
		q.Model(scene.LayersAll, modelPath, scene.ModelDraw{Transform: m.At(0, 0, 0)})
		q.Model(scene.LayersAll, modelPath, scene.ModelDraw{Transform: m.At(1, 0, 0)})
	})
	still.frameUntil(t, "the model to become resident", func() bool {
		return len(still.passes()) == 1 && still.passes()[0].Instances == 2
	})
	if batches := still.passes()[0].Batches; len(batches) != 1 {
		t.Fatalf("two unanimated calls published %d batches, want one: %+v", len(batches), batches)
	}

	play := []model.ClipPlay{{Clip: "spin", Time: 0.5, Weight: 1}}
	moving := newHarnessWithFiles(t, modelFiles(glb(t, skinnedModel(t))), func(q *scene.OpQueue) {
		q.Camera(cameraMain, modelCamera())
		q.Model(scene.LayersAll, modelPath, scene.ModelDraw{Transform: m.At(0, 0, 0), Plays: play})
		q.Model(scene.LayersAll, modelPath, scene.ModelDraw{Transform: m.At(1, 0, 0), Plays: play})
	})
	moving.frameUntil(t, "the model to become resident", func() bool {
		return len(moving.passes()) == 1 && moving.passes()[0].Instances == 2
	})
	batches := moving.passes()[0].Batches
	if len(batches) != 2 {
		t.Fatalf("two animated calls published %d batches, want one each: %+v", len(batches), batches)
	}
	if batches[0].MaterialID != batches[1].MaterialID || batches[0].MeshID != batches[1].MeshID {
		t.Fatalf("the animated calls keyed apart, %+v; the test wants them split only by animation", batches)
	}
}

// The blend class never merges, not even two separately recorded draws that
// are equal in everything and adjacent in the depth sort: each blended entry
// is its own batch so that each lands at its own depth.
func TestEqualAdjacentBlendedDrawsStayOneBatchEach(t *testing.T) {
	var ref model.MeshRef
	h := newHarness(t, func(q *scene.OpQueue) {
		q.Camera(testCamera, forwardCamera())
		material := blendMaterial(1)
		q.Mesh(0, ref, scene.MeshDraw{Material: material, Transform: m.At(0, 0, -5)})
		q.Mesh(0, ref, scene.MeshDraw{Material: material, Transform: m.At(0, 0, -6)})
	})
	ref = h.bake(triangle(), nil, gfx.TopologyTriangleList)
	h.frame()

	batches := h.passes()[0].Batches
	if len(batches) != 2 {
		t.Fatalf("published %d batches, want one per blended draw: %+v", len(batches), batches)
	}
	for i, batch := range batches {
		if batch.InstanceCount != 1 {
			t.Fatalf("blended batch %d holds %d instances, want 1", i, batch.InstanceCount)
		}
	}
}
