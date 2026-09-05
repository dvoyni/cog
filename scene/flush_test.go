package scene

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

// forwardCamera looks down -Z from the origin, so "in front" is negative Z and
// "behind" is positive Z.
func forwardCamera() CameraDescr {
	return CameraDescr{FovY: math.Pi / 2, Near: 0.1, Far: 100}
}

// A draw behind the camera is culled, and the frustum the pass publishes is the
// one that rejected it: the whole point of publishing it is that a test can
// say which sphere a specific frustum rejected, rather than only count.
func TestADrawBehindTheCameraIsCulledByThePublishedFrustum(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, forwardCamera())
		q.Box(0, At(0, 0, -5), testBoxColor)
		q.Box(0, At(0, 0, 5), testBoxColor)
	})
	h.frame()

	passes := h.passes()
	if len(passes) != 1 {
		t.Fatalf("published %d passes, want 1", len(passes))
	}
	pass := passes[0]
	if pass.Recorded != 2 || pass.Culled != 1 || pass.Instances != 1 {
		t.Fatalf("recorded %d, culled %d, packed %d; want 2, 1, 1", pass.Recorded, pass.Culled, pass.Instances)
	}
	radius := float32(math.Sqrt(3)) / 2
	if pass.Frustum.ContainsSphere(m.Vec3{Z: 5}, radius) {
		t.Fatal("the published frustum contains the sphere behind the camera that the pass culled")
	}
	if !pass.Frustum.ContainsSphere(m.Vec3{Z: -5}, radius) {
		t.Fatal("the published frustum rejects the sphere in front of the camera that the pass kept")
	}
	if len(h.backend.draws) != 1 {
		t.Fatalf("the backend received %d draws, want only the visible box", len(h.backend.draws))
	}
}

// The sphere is tested against all six planes. A draw past Far is culled just
// like one to the side, which is why Far is required.
func TestADrawPastFarIsCulled(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, forwardCamera())
		q.Box(0, At(0, 0, -200), testBoxColor)
	})
	h.frame()

	if pass := h.passes()[0]; pass.Culled != 1 || pass.Instances != 0 {
		t.Fatalf("a box past Far: culled %d, packed %d; want 1, 0", pass.Culled, pass.Instances)
	}
}

// The world sphere scales with the draw. A box scaled by 20 reaches back past
// the camera and stays visible where a unit box at the same place is culled.
func TestCullingUsesTheScaledWorldSphere(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, forwardCamera())
		q.Box(0, At(0, 0, 5), testBoxColor)
		q.Box(0, At(0, 0, 5).WithScale(20), testBoxColor)
	})
	h.frame()

	if pass := h.passes()[0]; pass.Culled != 1 || pass.Instances != 1 {
		t.Fatalf("culled %d, packed %d; want the unit box culled and the scaled one kept", pass.Culled, pass.Instances)
	}
}

// NeverCull and an explicit sphere both override the mesh's baked one, and a
// never-cull draw is never reported - it is the documented default.
func TestNeverCullAndExplicitBoundsOverrideTheBakedSphere(t *testing.T) {
	var reported []error
	h := newHarnessWithErrors(t, func(q *OpQueue) {
		q.Camera(testCamera, forwardCamera())
		// Behind the camera, but exempt.
		q.draw(drawRecord{transform: At(0, 0, 5), color: testBoxColor, neverCull: true})
		// Behind the camera by its mesh, but its explicit sphere reaches the
		// camera.
		q.draw(drawRecord{transform: At(0, 0, 5), color: testBoxColor, bounds: m.Sphere{Radius: 10}})
		// Behind the camera by its mesh, and its explicit sphere says so too.
		q.draw(drawRecord{transform: At(0, 0, 5), color: testBoxColor, bounds: m.Sphere{Radius: 1}})
	}, &reported)
	h.frame()

	if pass := h.passes()[0]; pass.Culled != 1 || pass.Instances != 2 {
		t.Fatalf("culled %d, packed %d; want only the tightly bounded draw culled", pass.Culled, pass.Instances)
	}
	if len(reported) != 0 {
		t.Fatalf("never-cull reported %v; it is the default, not an error", reported)
	}
}

// opaqueMaterial is a caller material in the opaque class whose identity is
// its one parameter, so two of them are two material ids.
func opaqueMaterial(key float32) Material {
	return Material{{Descr: gfx.MaterialWithState(
		gfx.ShaderWithResource(sceneShaderPath), gfx.StateOpaque3D, pbrTestParams(key)...,
	)}}
}

func blendMaterial(key float32) Material {
	return Material{{Descr: gfx.MaterialWithState(
		gfx.ShaderWithResource(sceneShaderPath), gfx.StateTransparent3D, pbrTestParams(key)...,
	)}}
}

// pbrTestParams binds every slot the bundled shader declares, plus one scalar
// that makes materials distinguishable, so a caller material reaches the
// backend without an unbound binding.
func pbrTestParams(key float32) []gfx.ParameterDescr {
	params := []gfx.ParameterDescr{gfx.FloatParam("key", key)}
	for _, slot := range pbrSlots {
		params = append(params,
			gfx.TextureParam(slot.texture, gfx.TextureWithBytes(1, 1, gfx.FormatRGBA8, []byte{255, 255, 255, 255}, true, false)),
			gfx.SamplerParam(slot.sampler, pbrSampler),
		)
	}
	return params
}

// Opaque draws sort by material key regardless of recording order, so every
// draw of one material runs together and a hundred crates are one pipeline
// switch rather than a hundred. Within a pass, recording order is not
// preserved.
func TestOpaqueDrawsGroupByMaterialNotRecordingOrder(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, forwardCamera())
		a, b := opaqueMaterial(1), opaqueMaterial(2)
		for i, material := range []Material{b, a, b, a, b} {
			q.draw(drawRecord{transform: At(float32(i), 0, -5), color: testBoxColor, material: material})
		}
	})
	h.frame()

	batches := h.passes()[0].Batches
	if len(batches) != 5 {
		t.Fatalf("published %d batches, want 5", len(batches))
	}
	for i := 1; i < len(batches); i++ {
		if batches[i].MaterialID < batches[i-1].MaterialID {
			t.Fatalf("batch %d has material %d after material %d; opaque draws are not grouped by material",
				i, batches[i].MaterialID, batches[i-1].MaterialID)
		}
	}
	if batches[0].MaterialID == batches[4].MaterialID {
		t.Fatal("every batch has one material id; the two materials were not told apart")
	}
	if batches[1].MaterialID != batches[0].MaterialID || batches[3].MaterialID != batches[4].MaterialID {
		t.Fatalf("materials interleave: %v", batches)
	}
}

// Blend draws come after every opaque draw and sort back to front by view
// depth, whatever their material. The materials differ only so the batch list
// says which draw landed where: ids are assigned in intern order, which is
// recording order, so near < far < opaque, and the expected emission order
// opaque, far, near is strictly decreasing.
func TestBlendDrawsFollowOpaqueAndSortBackToFront(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(testCamera, forwardCamera())
		// Recorded near first, and with the lower material id, so a sort by
		// either recording order or material would put it first.
		q.draw(drawRecord{transform: At(0, 0, -2), color: testBoxColor, material: blendMaterial(1)})
		q.draw(drawRecord{transform: At(0, 0, -20), color: testBoxColor, material: blendMaterial(2)})
		q.draw(drawRecord{transform: At(0, 0, -50), color: testBoxColor, material: opaqueMaterial(3)})
	})
	h.frame()

	batches := h.passes()[0].Batches
	if len(batches) != 3 {
		t.Fatalf("published %d batches, want 3", len(batches))
	}
	opaque, far, near := batches[0].MaterialID, batches[1].MaterialID, batches[2].MaterialID
	if !(opaque > far && far > near) {
		t.Fatalf("batches are %v; want the opaque draw first, then the blend draws far to near", batches)
	}
	if batches[0].FirstInstance != 0 || batches[1].FirstInstance != 1 || batches[2].FirstInstance != 2 {
		t.Fatalf("instances pack in emission order, got %v", batches)
	}
}

// Two passes of one camera at the same aspect share one cull, and each filters
// the survivors by its own tag: the shadow pass sees the same recorded and
// culled counts but packs nothing, because the bundled PBR serves only forward.
func TestEachPassFiltersTheSharedSurvivorsByTag(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		descr := forwardCamera()
		descr.Passes = []Pass{
			{Tag: "shadow", Order: -1000, Target: gfx.NoTarget(), Depth: gfx.DepthTarget(sizedTexture(800, 600))},
			{Tag: TagForward},
		}
		q.Camera(testCamera, descr)
		q.Box(0, At(0, 0, -5), testBoxColor)
		q.Box(0, At(0, 0, 5), testBoxColor)
	})
	h.frame()

	passes := h.passes()
	if len(passes) != 2 {
		t.Fatalf("published %d passes, want 2", len(passes))
	}
	shadow, forward := passes[0], passes[1]
	if shadow.Recorded != 2 || shadow.Culled != 1 || shadow.Instances != 0 {
		t.Fatalf("shadow pass recorded %d, culled %d, packed %d; want 2, 1, 0",
			shadow.Recorded, shadow.Culled, shadow.Instances)
	}
	if forward.Recorded != 2 || forward.Culled != 1 || forward.Instances != 1 {
		t.Fatalf("forward pass recorded %d, culled %d, packed %d; want 2, 1, 1",
			forward.Recorded, forward.Culled, forward.Instances)
	}
	if shadow.Frustum != forward.Frustum {
		t.Fatal("two passes at one aspect published different frustums; they should share one cull")
	}
}
