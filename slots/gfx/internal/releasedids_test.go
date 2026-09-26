package internal

import (
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

// A frame is rendered again when no new one is pending, so a mesh released
// after the frame was recorded is still named by it. Its buffers are gone from
// the backend, and a draw issued over them draws with whatever the pass bound
// last - so the draw goes, silently, because releasing what a frame still names
// is the re-rendered frame rather than a mistake.
func TestARerenderedFrameDropsADrawWhoseMeshWasReleased(t *testing.T) {
	for _, c := range []struct {
		name    string
		release func(vertices, indices descriptors.BufferDescr) descriptors.BufferDescr
		want    int
	}{
		{"vertex buffer", func(v, _ descriptors.BufferDescr) descriptors.BufferDescr { return v }, 0},
		{"index buffer", func(_, i descriptors.BufferDescr) descriptors.BufferDescr { return i }, 0},
		{"an unrelated buffer", nil, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := newPlugin()
			k := newTestKernel(t, p)
			backend := &fakeBackend{}
			k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

			var vertices, indices, unrelated descriptors.BufferDescr
			withResourceQueue(t, k, func(resources *ResourceQueue) {
				vertices = resources.UploadBuffer(resources.NewBuffer(), make([]byte, 3*28), true)
				indices = resources.UploadBuffer(resources.NewBuffer(), make([]byte, 12), true)
				unrelated = resources.UploadBuffer(resources.NewBuffer(), make([]byte, 4), true)
			})
			mesh := descriptors.MeshIndexed(
				vertices, indices, descriptors.IndexUint32, types.TopologyTriangleList,
				descriptors.Attr(0, descriptors.Float32x3), descriptors.Attr(12, descriptors.Float32x4),
			)
			w, ref := recordList(t, k)
			w.Draw(ref, mesh, testMaterial(), 1, 0, descriptors.MatParam("mvp", m.NewMat4()))
			k.ExecuteCommand[PresentCmd](PresentRequest{})
			k.PublishEvent(app.RenderEvent{}).Wait()
			if got := countOps(backend.lastOps, testOpDraw); got != 1 {
				t.Fatalf("the recorded frame encoded %d draws, want 1", got)
			}

			released := unrelated
			if c.release != nil {
				released = c.release(vertices, indices)
			}
			withResourceQueue(t, k, func(resources *ResourceQueue) { resources.ReleaseBuffer(released) })
			// No present: the render translates the frame it already has again.
			k.PublishEvent(app.RenderEvent{}).Wait()

			if got := countOps(backend.lastOps, testOpDraw); got != c.want {
				t.Fatalf("the re-rendered frame encoded %d draws, want %d", got, c.want)
			}
			// It stays gone on every frame after, and nothing is reported:
			// the test kernel fails on any error.
			k.PublishEvent(app.RenderEvent{}).Wait()
			if got := countOps(backend.lastOps, testOpDraw); got != c.want {
				t.Fatalf("the next re-rendered frame encoded %d draws, want %d", got, c.want)
			}
		})
	}
}
