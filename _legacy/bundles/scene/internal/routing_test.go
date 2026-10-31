package internal

import (
	"testing"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// The routing tests are scene's first on layers and passes: which camera
// and which pass an instance reaches. Layers, culling and the camera are not in
// the Batch key, so each filters a Batch's instances per pass, and a test here
// spawns Entities that share a Batch and are routed apart. They assert which
// instances each pass drew, never how many draws it took.

// xs lists the X of every instance a list holds, for comparing sets of
// Entities placed along X.
func xs(instances []drawnInstance) map[float32]int {
	out := map[float32]int{}
	for _, instance := range instances {
		out[instance.position().X]++
	}
	return out
}

func expectXs(t *testing.T, what string, got []drawnInstance, want ...float32) {
	t.Helper()
	wanted := map[float32]int{}
	for _, x := range want {
		wanted[x]++
	}
	have := xs(got)
	if len(have) != len(wanted) {
		t.Errorf("%s drew at %v, want x=%v", what, positions(got), want)
		return
	}
	for x, n := range wanted {
		if have[x] != n {
			t.Errorf("%s drew at %v, want x=%v", what, positions(got), want)
			return
		}
	}
}

// TestLayersRouteABatchsInstancesToEachCamera is layer routing: one model and
// one material make one key, and each camera's cull mask picks its own subset
// of that Batch's instances. An Entity on two layers reaches both cameras, and
// one with no layers is on every layer, as a camera with no mask draws every
// layer.
func TestLayersRouteABatchsInstancesToEachCamera(t *testing.T) {
	h := newCameralessHarness(t, 256)
	h.spawn(t, spawnRequest{Place: defaultEye, Camera: &Camera{
		FovY: 1.0472, Near: 0.1, Far: 200, CullMask: Layer(1),
	}})
	h.spawn(t, spawnRequest{Place: defaultEye, Camera: &Camera{
		ID: 1, FovY: 1.0472, Near: 0.1, Far: 200, CullMask: Layer(2),
	}})
	h.spawn(t, spawnRequest{Place: defaultEye, Camera: &Camera{
		ID: 2, FovY: 1.0472, Near: 0.1, Far: 200,
	}})
	crate := func(layers LayerMask) *Model {
		return &Model{Ref: model.ModelRef{Path: crateModel}, Layers: layers}
	}
	h.spawn(t, spawnRequest{Place: m.At(-4, 0, 0), Model: crate(Layer(1))})
	h.spawn(t, spawnRequest{Place: m.At(-2, 0, 0), Model: crate(Layer(1))})
	h.spawn(t, spawnRequest{Place: m.At(2, 0, 0), Model: crate(Layer(2))})
	h.spawn(t, spawnRequest{Place: m.At(4, 0, 0), Model: crate(Layer(1) | Layer(2))})
	h.spawn(t, spawnRequest{Place: m.At(6, 0, 0), Model: crate(0)})
	h.spawn(t, spawnRequest{Place: m.At(8, 0, 0), Model: crate(Layer(7))})

	h.frameUntil(t, "the crates to draw", func() bool {
		return len(where(h.drawn(), inPass("scene.camera2.forward"))) == 6
	})

	expectXs(t, "the layer-1 camera", where(h.drawn(), inPass("scene.camera0.forward")), -4, -2, 4, 6)
	expectXs(t, "the layer-2 camera", where(h.drawn(), inPass("scene.camera1.forward")), 2, 4, 6)
	expectXs(t, "the maskless camera", where(h.drawn(), inPass("scene.camera2.forward")), -4, -2, 2, 4, 6, 8)
	h.noErrors(t)
}

// TestPassesRouteByTheMaterialsTags is pass routing: a pass draws exactly the
// instances whose material has an entry for its tag. A file's own material
// serves the forward pass alone, a Material serves the tags it names, and a
// pass whose tag no material serves is still emitted, with its clear, and
// draws nothing.
func TestPassesRouteByTheMaterialsTags(t *testing.T) {
	h := newCameralessHarness(t, 256)
	h.spawn(t, spawnRequest{Place: defaultEye, Camera: &Camera{
		FovY: 1.0472, Near: 0.1, Far: 200, Passes: m.NewList(
			Pass{ClearDepth: m.Some[float32](1)},
			Pass{Tag: "shadow", Order: 1},
			Pass{Tag: "overlay", Order: 2},
			Pass{Tag: "unused", Order: 3, ClearColor: m.Some(m.Color{A: 1})},
		),
	}})
	ref := h.bake(t)
	shadowOnly := &Material{Tags: m.NewList(
		MaterialTag{Tag: "shadow", Shader: gfx.ShaderWithText("shadow")})}
	forwardAndOverlay := &Material{Tags: m.NewList(
		MaterialTag{Shader: gfx.ShaderWithText("flat")},
		MaterialTag{Tag: "overlay", Shader: gfx.ShaderWithText("outline")})}
	h.spawn(t, spawnRequest{Place: m.At(-2, 0, 0), Model: crateModelComponent()})
	h.spawn(t, spawnRequest{Count: 2, Step: 1, Place: m.At(0, 0, 0),
		Mesh: &Mesh{Ref: ref, NeverCull: true}, Material: shadowOnly})
	h.spawn(t, spawnRequest{Count: 2, Step: 1, Place: m.At(3, 0, 0),
		Mesh: &Mesh{Ref: ref, NeverCull: true}, Material: forwardAndOverlay})

	h.frameUntil(t, "the crate to draw", func() bool {
		return len(where(h.drawn(), at(m.Vec3{X: -2}))) > 0
	})

	expectXs(t, "the forward pass", where(h.drawn(), inPass("scene.camera0.forward")), -2, 3, 4)
	expectXs(t, "the shadow pass", where(h.drawn(), inPass("scene.camera0.shadow")), 0, 1)
	expectXs(t, "the overlay pass", where(h.drawn(), inPass("scene.camera0.overlay")), 3, 4)
	for _, instance := range where(h.drawn(), inPass("scene.camera0.overlay")) {
		if got := viewOf(instance).Shader; got != "outline" {
			t.Errorf("the overlay pass drew the instance at %v with %q, want its overlay entry's shader",
				instance.position(), got)
		}
	}
	var unused *recordedPass
	for _, pass := range h.backend.passes() {
		if pass.desc.Label == "scene.camera0.unused" {
			unused = &pass
		}
	}
	switch {
	case unused == nil:
		t.Error("the pass no material serves was not emitted")
	case len(unused.draws) != 0:
		t.Errorf("the pass no material serves drew %d times", len(unused.draws))
	case unused.desc.Load != gfx.LoadClear:
		t.Errorf("the pass no material serves did not clear: %+v", unused.desc)
	}
	h.noErrors(t)
}

// TestAnInstanceOutsideTheFrustumLeavesItsBatch is culling inside a Batch: two
// crates share a key, and the one the camera cannot see is filtered out of the
// pass while the other draws. A Mesh that never culls draws wherever it
// stands.
func TestAnInstanceOutsideTheFrustumLeavesItsBatch(t *testing.T) {
	h := newDrawingHarness(t, 256)
	ref := h.bake(t)
	h.spawn(t, spawnRequest{Place: m.At(0, 0, 0), Model: crateModelComponent()})
	h.spawn(t, spawnRequest{Place: m.At(500, 0, 0), Model: crateModelComponent()})
	h.spawn(t, spawnRequest{Place: m.At(-500, 0, 0), Mesh: &Mesh{Ref: ref, NeverCull: true}})

	h.frameUntil(t, "the crate to draw", func() bool {
		return len(where(h.drawn(), ofTriangle)) >= 2
	})

	expectXs(t, "the forward pass", where(h.drawn(), inPass("scene.camera0.forward")), 0, -500)
	h.noErrors(t)
}
