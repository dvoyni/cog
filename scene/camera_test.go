package scene

import (
	"errors"
	"math"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

const cameraMain CameraID = 0

func simpleCamera() CameraDescr {
	return CameraDescr{
		Transform: LookAt(m.Vec3{X: 3, Y: 2, Z: 4}, m.Vec3{}, m.Vec3{Y: 1}),
		FovY:      1.0472,
		Near:      0.1, Far: 100,
	}
}

func TestARecordedCameraEmitsOneDefaultPass(t *testing.T) {
	// The floor of the API: one camera and nothing else. It emits exactly one
	// forward pass into the screen, at the camera's own order.
	h := newHarness(t, func(q *OpQueue) { q.Camera(cameraMain, simpleCamera()) })
	h.frame()

	passes := h.passes()
	if len(passes) != 1 {
		t.Fatalf("passes = %d, want 1", len(passes))
	}
	pass := passes[0]
	if pass.CameraID != cameraMain || pass.Order != gfx.Order(cameraMain) {
		t.Errorf("pass camera/order = %d/%d, want %d/%d", pass.CameraID, pass.Order, cameraMain, cameraMain)
	}
	if pass.Tag != TagForward {
		t.Errorf("pass tag = %q, want %q", pass.Tag, TagForward)
	}
	// The frustum is published so a culling assertion can name the volume that
	// rejected a sphere, rather than only counting what survived.
	if !pass.Frustum.ContainsSphere(m.Vec3{}, 1) {
		t.Error("the frustum of a camera looking at the origin excludes the origin")
	}
	if pass.Frustum.ContainsSphere(m.Vec3{X: 300, Y: 200, Z: 400}, 1) {
		t.Error("the frustum contains a point far behind the camera")
	}
	if len(h.backend.passes) != 1 {
		t.Fatalf("gfx passes = %d, want 1", len(h.backend.passes))
	}
}

func TestTheDefaultPassPreservesColourAndClearsDepthToFar(t *testing.T) {
	// Asymmetric on purpose: a default colour clear would let a second camera
	// silently erase the first, but a DepthAuto pass shares its pooled depth
	// texture with every same-size pass in the frame and inherits garbage
	// unless it clears. Under conventional depth that clear is 1.0 - the naive
	// zero clears to the near plane and hides the whole scene.
	h := newHarness(t, func(q *OpQueue) { q.Camera(cameraMain, simpleCamera()) })
	h.frame()

	if len(h.backend.passes) != 1 {
		t.Fatalf("gfx passes = %d, want 1", len(h.backend.passes))
	}
	pass := h.backend.passes[0]
	if !pass.Screen || !pass.DepthAuto {
		t.Errorf("pass = %+v, want the screen target with automatic depth", pass)
	}
	if pass.Load != gfx.LoadPreserve {
		t.Errorf("colour load = %v, want LoadPreserve", pass.Load)
	}
	if pass.DepthLoad != gfx.LoadClear || pass.DepthClear != 1 {
		t.Errorf("depth load = %v clear = %v, want LoadClear at 1.0", pass.DepthLoad, pass.DepthClear)
	}
	if pass.DepthStore != gfx.StoreDiscard {
		t.Errorf("depth store = %v, want StoreDiscard for a pass that named no depth texture", pass.DepthStore)
	}
}

func TestCamerasEmitInIdOrder(t *testing.T) {
	// Never map order: a Go map range would make frame output nondeterministic.
	h := newHarness(t, func(q *OpQueue) {
		for _, id := range []CameraID{2000, -1000, 5, 1500} {
			q.Camera(id, simpleCamera())
		}
	})
	h.frame()

	passes := h.passes()
	want := []CameraID{-1000, 5, 1500, 2000}
	if len(passes) != len(want) {
		t.Fatalf("passes = %d, want %d", len(passes), len(want))
	}
	for i, id := range want {
		if passes[i].CameraID != id {
			t.Fatalf("pass %d is camera %d, want %d", i, passes[i].CameraID, id)
		}
	}
}

func TestARepeatedCameraIsReportedAndTheFirstRecordWins(t *testing.T) {
	// Camera is a registration, not a free parameter, so a repeat means two
	// systems each believe they own it.
	var reported []error
	h := newHarnessWithErrors(t, func(q *OpQueue) {
		q.Camera(cameraMain, simpleCamera())
		second := simpleCamera()
		second.FovY = 0.1
		q.Camera(cameraMain, second)
	}, &reported)
	h.frame()

	if passes := h.passes(); len(passes) != 1 {
		t.Fatalf("passes = %d, want 1: the second record should have been dropped", len(passes))
	}
	ops := h.ops()
	if len(ops) != 1 || ops[0].Descr.FovY != 1.0472 {
		t.Fatalf("recorded camera = %+v, want the first record", ops)
	}
	var duplicate ErrCameraAlreadyRecorded
	if !anyErrorAs(reported, &duplicate) || duplicate.Camera != cameraMain {
		t.Fatalf("reported = %v, want a duplicate report for camera %d", reported, cameraMain)
	}
}

func TestAMissingClipPlaneIsReportedAndTheCameraSkipped(t *testing.T) {
	// Substituting a default would hide a real caller bug behind a degenerate
	// projection that renders nothing for a reason nobody can see.
	for _, test := range []struct {
		name      string
		near, far float32
	}{
		{"no near", 0, 100}, {"no far", 0.1, 0}, {"neither", 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			var reported []error
			descr := simpleCamera()
			descr.Near, descr.Far = test.near, test.far
			h := newHarnessWithErrors(t, func(q *OpQueue) { q.Camera(cameraMain, descr) }, &reported)
			h.frame()

			if passes := h.passes(); len(passes) != 0 {
				t.Fatalf("passes = %d, want none: the camera should have been skipped", len(passes))
			}
			var missing ErrCameraClipPlanesMissing
			if !anyErrorAs(reported, &missing) {
				t.Fatalf("reported = %v, want a missing-clip-plane report", reported)
			}
		})
	}
}

func TestPassOrderIsAnOffsetFromTheCameraId(t *testing.T) {
	// An absolute int has no working zero value, because 0 is a legitimate
	// order; the offset's zero correctly means "at the camera".
	const id CameraID = 1500
	h := newHarness(t, func(q *OpQueue) {
		descr := simpleCamera()
		descr.Passes = []Pass{
			{Tag: "shadow", Order: -1000, Target: gfx.NoTarget(), Depth: gfx.DepthTarget(sizedTexture(1024, 1024))},
			{},
		}
		q.Camera(id, descr)
	})
	h.frame()

	passes := h.passes()
	if len(passes) != 2 {
		t.Fatalf("passes = %d, want 2", len(passes))
	}
	if passes[0].Tag != "shadow" || passes[0].Order != 500 {
		t.Errorf("shadow pass = %q at %d, want the shadow tag at 500", passes[0].Tag, passes[0].Order)
	}
	if passes[1].Tag != TagForward || passes[1].Order != 1500 {
		t.Errorf("forward pass = %q at %d, want %q at 1500", passes[1].Tag, passes[1].Order, TagForward)
	}
}

func TestAPassNamingItsOwnDepthTextureKeepsIt(t *testing.T) {
	// You allocated it, you mean to sample it. Everything else discards, which
	// is the tiled-GPU win a forward pass gets for free.
	h := newHarness(t, func(q *OpQueue) {
		descr := simpleCamera()
		descr.Passes = []Pass{{
			Tag: "shadow", Target: gfx.NoTarget(),
			Depth: gfx.DepthTarget(sizedTexture(1024, 1024)), ClearDepth: &depthClearFar,
		}}
		q.Camera(cameraMain, descr)
	})
	h.frame()

	if len(h.backend.passes) != 1 {
		t.Fatalf("gfx passes = %d, want 1", len(h.backend.passes))
	}
	if store := h.backend.passes[0].DepthStore; store != gfx.StoreKeep {
		t.Errorf("depth store = %v, want StoreKeep", store)
	}
}

func TestProjectionIsResolvedPerPassFromItsOwnTarget(t *testing.T) {
	// One camera, two targets of different shape: there is no single camera
	// aspect, so the two frustums differ in exactly the horizontal extent.
	h := newHarness(t, func(q *OpQueue) {
		descr := CameraDescr{FovY: math.Pi / 2, Near: 1, Far: 10}
		descr.Passes = []Pass{
			{Tag: "wide", Target: gfx.TextureTarget(sizedTexture(2000, 1000), 0, 0)},
			{Tag: "square", Target: gfx.TextureTarget(sizedTexture(1000, 1000), 0, 0)},
		}
		q.Camera(cameraMain, descr)
	})
	h.frame()

	passes := h.passes()
	if len(passes) != 2 {
		t.Fatalf("passes = %d, want 2", len(passes))
	}
	// A point 7.5 units to the side at 5 deep is inside a 2:1 frustum, whose
	// half-width there is 10, and outside a square one, whose half-width is 5.
	side := m.Vec3{X: 7.5, Z: -5}
	if !passes[0].Frustum.ContainsSphere(side, 0) {
		t.Error("the 2:1 pass culled a point its aspect should include")
	}
	if passes[1].Frustum.ContainsSphere(side, 0) {
		t.Error("the square pass kept a point only a wider aspect includes")
	}
}

func TestAScreenPassFollowsTheWindowAspect(t *testing.T) {
	h := newHarness(t, func(q *OpQueue) {
		q.Camera(cameraMain, CameraDescr{FovY: math.Pi / 2, Near: 1, Far: 10})
	})
	h.frame()

	passes := h.passes()
	if len(passes) != 1 {
		t.Fatalf("passes = %d, want 1", len(passes))
	}
	// The harness window is 800x600, so the horizontal half-extent at 5 deep is
	// 5 * 4/3, a touch under 6.67.
	if !passes[0].Frustum.ContainsSphere(m.Vec3{X: 6.5, Z: -5}, 0) {
		t.Error("the screen pass culled a point inside a 4:3 frustum")
	}
	if passes[0].Frustum.ContainsSphere(m.Vec3{X: 7, Z: -5}, 0) {
		t.Error("the screen pass kept a point outside a 4:3 frustum")
	}
}

func TestAPassWithNoSurvivingDrawsIsStillEmitted(t *testing.T) {
	// Skipping it would drop its clears, making a camera's clear depend on
	// whether anything was visible - a bug that only shows when you turn away.
	h := newHarness(t, func(q *OpQueue) { q.Camera(cameraMain, simpleCamera()) })
	h.frame()

	if len(h.backend.passes) != 1 {
		t.Fatalf("gfx passes = %d, want 1 even with nothing drawn", len(h.backend.passes))
	}
}

func TestInspectionSurvivesTheFlushThatProducedIt(t *testing.T) {
	frames := 0
	h := newHarness(t, func(q *OpQueue) {
		frames++
		q.Camera(CameraID(frames), simpleCamera())
	})
	h.frame()
	if ops := h.ops(); len(ops) != 1 || ops[0].Camera != 1 {
		t.Fatalf("ops after frame 1 = %+v, want camera 1", ops)
	}
	h.frame()
	if ops := h.ops(); len(ops) != 1 || ops[0].Camera != 2 {
		t.Fatalf("ops after frame 2 = %+v, want camera 2", ops)
	}
	if passes := h.passes(); len(passes) != 1 || passes[0].CameraID != 2 {
		t.Fatalf("passes after frame 2 = %+v, want camera 2", passes)
	}
}

func TestDescriptorSlicesAreBorrowedForTheCall(t *testing.T) {
	// A hot-loop caller reuses one backing array, so scene copies into its own
	// frame arena before returning.
	shared := make([]Pass, 1)
	h := newHarness(t, func(q *OpQueue) {
		shared[0] = Pass{Tag: "first"}
		descr := simpleCamera()
		descr.Passes = shared
		q.Camera(cameraMain, descr)
		shared[0] = Pass{Tag: "overwritten"}
	})
	h.frame()

	passes := h.passes()
	if len(passes) != 1 || passes[0].Tag != "first" {
		t.Fatalf("passes = %+v, want the tag recorded at the time of the call", passes)
	}
}

// anyErrorAs reports whether any reported error matches target.
func anyErrorAs(reported []error, target any) bool {
	for _, err := range reported {
		if errors.As(err, target) {
			return true
		}
	}
	return false
}

func TestAFrameBeforeTheWindowIsKnownIsSkippedSilently(t *testing.T) {
	// Every screen-targeted pass would resolve an aspect of zero, and saying so
	// once per camera per frame tells a caller nothing they can act on.
	var reported []error
	h := newHarnessWithErrors(t, func(q *OpQueue) { q.Camera(cameraMain, simpleCamera()) }, &reported)
	h.kernel.ExecuteCommand[app.SetViewportCmd](app.SetViewportRequest{})
	h.frame()

	if passes := h.passes(); len(passes) != 0 {
		t.Fatalf("passes = %d, want none while the window has no area", len(passes))
	}
	if len(reported) != 0 {
		t.Fatalf("reported = %v, want nothing", reported)
	}
}
