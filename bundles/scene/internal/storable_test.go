package internal

import (
	"reflect"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// A pass and a transform are storable, so a Component can hold scene's own
// types rather than a mirror of them: a clear is an m.Maybe rather than a
// pointer, and a transform has no matrix pointer to override it.
func TestPassAndTransformAreStorable(t *testing.T) {
	for _, tp := range []reflect.Type{reflect.TypeFor[scene.Pass](), reflect.TypeFor[m.Transform]()} {
		if err := ecs.Storable(tp); err != nil {
			t.Errorf("ecs.Storable(%s) = %v, want nil", tp, err)
		}
	}
}

// A zero Pass preserves colour and depth, exactly as the nil clears it replaced
// did: a declared pass that clears nothing draws over whatever is there.
func TestAZeroPassPreservesColourAndDepth(t *testing.T) {
	h := newHarness(t, func(q *scene.OpQueue) {
		descr := simpleCamera()
		descr.Passes = []scene.Pass{{}}
		q.Camera(cameraMain, descr)
		// Something to draw: gfx drops a pass that neither clears nor draws.
		q.Box(0, m.At(0, 0, 0), testBoxColor)
	})
	h.frame()

	if len(h.backend.passes) != 1 {
		t.Fatalf("gfx passes = %d, want 1", len(h.backend.passes))
	}
	pass := h.backend.passes[0]
	if pass.Load != gfx.LoadPreserve || pass.DepthLoad != gfx.LoadPreserve {
		t.Errorf("colour load = %v, depth load = %v; want both LoadPreserve", pass.Load, pass.DepthLoad)
	}
}

// A present clear is taken at its value, zero included: a colour clear to
// transparent black is a clear, not an absent field.
func TestAPresentClearClearsAtItsValue(t *testing.T) {
	h := newHarness(t, func(q *scene.OpQueue) {
		descr := simpleCamera()
		descr.Passes = []scene.Pass{{ClearColor: m.Some(m.Color{}), ClearDepth: m.Some[float32](1)}}
		q.Camera(cameraMain, descr)
	})
	h.frame()

	pass := h.backend.passes[0]
	if pass.Load != gfx.LoadClear || pass.Clear != (m.Color{}) {
		t.Errorf("colour load = %v clear = %v, want LoadClear to transparent black", pass.Load, pass.Clear)
	}
	if pass.DepthLoad != gfx.LoadClear || pass.DepthClear != 1 {
		t.Errorf("depth load = %v clear = %v, want LoadClear at 1.0", pass.DepthLoad, pass.DepthClear)
	}
}

// A draw scaled non-uniformly packs its literal basis and flags
// SCENE_NONUNIFORM, which is what sends its normals through the shader's
// inverse-transpose; a uniformly scaled draw does not pay for it.
func TestANonUniformlyScaledDrawTakesTheInverseTransposeNormalPath(t *testing.T) {
	var ref model.MeshRef
	h := newHarness(t, func(q *scene.OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		q.Mesh(0, ref, scene.MeshDraw{
			Transform: m.Transform{Position: m.Vec3{X: 1, Y: 2, Z: 3}, Scale: m.Vec3{X: 2, Y: 1, Z: 1}},
			NeverCull: true,
		})
	})
	ref = h.bake(triangle(), []uint32{0, 1, 2}, gfx.TopologyTriangleList)
	h.frame()

	instance := firstInstance(t, h)
	want := [3]m.Vec4{{X: 2, W: 1}, {Y: 1, W: 2}, {Z: 1, W: 3}}
	if got := [3]m.Vec4{instance.World0, instance.World1, instance.World2}; got != want {
		t.Errorf("packed world rows %v, want %v", got, want)
	}
	if instance.Flags&model.SceneNonUniform == 0 {
		t.Errorf("flags %#b lack SCENE_NONUNIFORM; the normals would shear with the scale", instance.Flags)
	}
}

func TestAUniformlyScaledDrawKeepsThePlainNormalPath(t *testing.T) {
	var ref model.MeshRef
	h := newHarness(t, func(q *scene.OpQueue) {
		q.Camera(testCamera, testCameraDescr())
		q.Mesh(0, ref, scene.MeshDraw{Transform: m.At(0, 0, 0).WithScale(3), NeverCull: true})
	})
	ref = h.bake(triangle(), []uint32{0, 1, 2}, gfx.TopologyTriangleList)
	h.frame()

	if flags := firstInstance(t, h).Flags; flags&model.SceneNonUniform != 0 {
		t.Errorf("flags %#b carry SCENE_NONUNIFORM for a uniform scale", flags)
	}
}
