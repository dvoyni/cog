package internal

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// frameViewProjectionAt is the sceneFrame block's third matrix.
const frameViewProjectionAt = 128

// TestViewProjectionIsTheMatrixTheRecordingSystemDrawsWith pins the picking
// matrix to the one each projection's pass uploads: a caller projecting a
// point through ViewProjection lands where the renderer drew it.
func TestViewProjectionIsTheMatrixTheRecordingSystemDrawsWith(t *testing.T) {
	h := newCameralessHarness(t, 256)
	eye := m.LookAt(m.Vec3{X: 2, Y: 3, Z: 10}, m.Vec3{}, m.Vec3{Y: 1})
	cameras := map[string]Camera{
		"scene.camera1.forward": {ID: 1, Projection: Perspective, FovY: 1, Near: 0.1, Far: 100},
		"scene.camera2.forward": {ID: 2, Projection: Orthographic, Height: 8, Near: 0.1, Far: 100},
		"scene.camera3.forward": {ID: 3, Projection: Oblique, Height: 20, Shear: 0.5, Near: -50, Far: 50},
	}
	for _, camera := range cameras {
		camera := camera
		h.spawn(t, spawnRequest{Place: eye, Camera: &camera})
	}
	material := &Material{Tags: m.NewList(MaterialTag{Shader: gfx.ShaderWithText("flat")})}
	h.spawn(t, spawnRequest{Place: m.At(0, 0, 0), Mesh: &Mesh{Ref: h.bake(t), NeverCull: true}, Material: material})

	h.frameUntil(t, "every camera to draw", func() bool {
		for label := range cameras {
			if len(where(h.drawn(), inPass(label))) == 0 {
				return false
			}
		}
		return true
	})

	viewport := m.Vec2{X: 1600, Y: 1200}
	for label, camera := range cameras {
		want, err := ViewProjection(camera, eye, viewport)
		if err != nil {
			t.Fatalf("%s: ViewProjection failed: %v", label, err)
		}
		frame := where(h.drawn(), inPass(label))[0].frame
		expectMat4(t, label+" view-projection", mat4At(frame, frameViewProjectionAt), want)
	}
	h.noErrors(t)
}

// TestViewProjectionRefusesWhatTheRendererRefuses gives each camera the
// renderer skips the error the renderer reports for it, and an unsized
// viewport its own, rather than a matrix a pick silently misses through.
func TestViewProjectionRefusesWhatTheRendererRefuses(t *testing.T) {
	eye := m.At(0, 0, 10)
	good := Camera{ID: 7, Projection: Perspective, FovY: 1, Near: 0.1, Far: 100}
	viewport := m.Vec2{X: 800, Y: 600}

	noFar := good
	noFar.Far = 0
	var clip ErrCameraClipPlanesMissing
	if _, err := ViewProjection(noFar, eye, viewport); !errors.As(err, &clip) || clip.Camera != 7 {
		t.Errorf("a camera with no Far gave %v, want ErrCameraClipPlanesMissing for camera 7", err)
	}

	flat := good
	flat.FovY = 0
	var degenerate ErrCameraProjectionDegenerate
	if _, err := ViewProjection(flat, eye, viewport); !errors.As(err, &degenerate) || degenerate.Camera != 7 {
		t.Errorf("a camera with no FovY gave %v, want ErrCameraProjectionDegenerate for camera 7", err)
	}

	for _, size := range []m.Vec2{{}, {X: 800}, {Y: 600}, {X: -800, Y: 600}} {
		var unsized ErrViewportUnsized
		if _, err := ViewProjection(good, eye, size); !errors.As(err, &unsized) || unsized.Camera != 7 || unsized.Viewport != size {
			t.Errorf("a %v viewport gave %v, want ErrViewportUnsized for camera 7 and that size", size, err)
		}
	}
}
