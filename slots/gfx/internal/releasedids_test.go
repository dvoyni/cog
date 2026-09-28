package internal

import (
	"testing"

	"github.com/dvoyni/cog/libs/assets"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/kernel"
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
		release func(vertices, indices types.BufferDescr) types.BufferDescr
		want    int
	}{
		{"vertex buffer", func(v, _ types.BufferDescr) types.BufferDescr { return v }, 0},
		{"index buffer", func(_, i types.BufferDescr) types.BufferDescr { return i }, 0},
		{"an unrelated buffer", nil, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := newPlugin()
			k := newTestKernel(t, p)
			backend := &fakeBackend{}
			k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

			var vertices, indices, unrelated types.BufferDescr
			withResourceQueue(t, k, func(resources *ResourceQueue) {
				vertices = resources.UploadBuffer(resources.NewBuffer(), make([]byte, 3*28), true)
				indices = resources.UploadBuffer(resources.NewBuffer(), make([]byte, 12), true)
				unrelated = resources.UploadBuffer(resources.NewBuffer(), make([]byte, 4), true)
			})
			mesh := types.MeshDescrWithIndices(
				vertices, indices, types.IndexUint32, types.TopologyTriangleList,
				types.VertexAttribute{Offset: 0, Type: types.Float32x3}, types.VertexAttribute{Offset: 12, Type: types.Float32x4},
			)
			w, ref := recordList(t, k)
			w.Draw(ref, mesh, testSet(t, k), 1, 0)
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

// A re-rendered frame naming a set drops the draw, silently, once the set is
// released, once the shader it is built on is, and once a storage buffer it
// binds is - whether the set itself or the frame's version of it names the
// buffer. A storage buffer is the binding with no default, so a released one is
// an unsupplied one, and the draw has no data; a released texture falls back
// to white, as an unsupplied one does, and keeps the draw. Releasing an id the
// draw does not name keeps it too, and so does an update replacing an inline
// buffer the set baked, which releases the old bake and binds the new.
func TestARerenderedFrameDropsADrawWhoseSetWasReleased(t *testing.T) {
	type world struct {
		*setWorld
		set              types.DrawStateID
		other, versioned types.BufferDescr
		texture          types.TextureDescr
		unrelated        types.DrawStateID
	}
	for _, c := range []struct {
		name    string
		version bool
		release func(k kernel.Kernel, q *ResourceQueue, w *world)
		want    int
	}{
		{"the set", false, func(k kernel.Kernel, q *ResourceQueue, w *world) { q.ReleaseDrawParams(k, w.set) }, 0},
		{"the set, drawn through a version", true, func(k kernel.Kernel, q *ResourceQueue, w *world) { q.ReleaseDrawParams(k, w.set) }, 0},
		{"its shader", false, func(k kernel.Kernel, q *ResourceQueue, w *world) { q.ReleaseShader(k, w.shader) }, 0},
		{"its shader, drawn through a version", true, func(k kernel.Kernel, q *ResourceQueue, w *world) { q.ReleaseShader(k, w.shader) }, 0},
		{"the storage buffer the set binds", false, func(_ kernel.Kernel, q *ResourceQueue, w *world) { q.ReleaseBuffer(w.records) }, 0},
		{"the storage buffer the version binds", true, func(_ kernel.Kernel, q *ResourceQueue, w *world) { q.ReleaseBuffer(w.versioned) }, 0},
		{"the texture the set binds", false, func(_ kernel.Kernel, q *ResourceQueue, w *world) { q.ReleaseTexture(w.texture) }, 1},
		{"an unrelated set", true, func(k kernel.Kernel, q *ResourceQueue, w *world) { q.ReleaseDrawParams(k, w.unrelated) }, 1},
		{"an unrelated buffer", true, func(_ kernel.Kernel, q *ResourceQueue, w *world) { q.ReleaseBuffer(w.other) }, 1},
		{"the set's inline buffer, by an update", false, func(k kernel.Kernel, q *ResourceQueue, w *world) {
			q.UpdateDrawParams(k, w.set, types.ShaderParameterBuffer("instances", types.BufferDescrWithBlob(assets.NewBlob(make([]byte, 64)), true)))
		}, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := &world{setWorld: newSetWorld(t)}
			w.resources(func(k kernel.Kernel, q *ResourceQueue) {
				w.other = q.UploadBuffer(q.NewBuffer(), make([]byte, 64), true)
				w.versioned = q.UploadBuffer(q.NewBuffer(), make([]byte, 64), true)
				w.texture = q.NewTexture(1, 1, 1, types.FormatRGBA8, false)
				q.UploadTexture(w.texture, 0, types.Region{}, []byte{1, 2, 3, 4}, true)
			})
			w.set = w.newSet(types.DrawState{}, types.ShaderParameterTexture("albedo", w.texture))
			w.unrelated = w.newSet(types.DrawState{})
			w.k.ExecuteCommand[recordCmd](recordRequest{withKernel: func(k kernel.Kernel, q *OpQueue) {
				if c.version {
					q.SetDrawParams(k, w.set, types.ShaderParameterBuffer("instances", w.versioned))
				}
				q.Draw(screenPass(q, 0, "main"), triangle(), w.set, 1, 0)
			}})
			w.k.ExecuteCommand[PresentCmd](PresentRequest{})
			w.k.PublishEvent(app.RenderEvent{}).Wait()
			if got := countOps(w.backend.lastOps, testOpDraw); got != 1 {
				t.Fatalf("the recorded frame encoded %d draws, want 1", got)
			}

			w.resources(func(k kernel.Kernel, q *ResourceQueue) { c.release(k, q, w) })
			// No present: the render translates the frame it already has again.
			for frame := range 2 {
				w.k.PublishEvent(app.RenderEvent{}).Wait()
				if got := countOps(w.backend.lastOps, testOpDraw); got != c.want {
					t.Fatalf("re-rendered frame %d encoded %d draws, want %d", frame, got, c.want)
				}
			}
			if len(*w.reported) != 0 {
				t.Errorf("reported %v, want nothing", *w.reported)
			}
		})
	}
}

// A set's inline bytes are baked into ids it owns, and releasing the set
// releases them with it, in the same render that first drops the draw: the
// re-rendered frame never binds a bake the backend no longer holds.
func TestReleasingASetReleasesItsBakesAsItsDrawIsDropped(t *testing.T) {
	w := newSetWorld(t)
	var set types.DrawStateID
	w.resources(func(k kernel.Kernel, q *ResourceQueue) {
		set = q.NewDrawParams(k, w.shader, types.DrawState{},
			types.ShaderParameterBuffer("instances", types.BufferDescrWithBlob(assets.NewBlob(make([]byte, 64)), true)),
			types.ShaderParameterTexture("albedo", types.TextureWithBytes(1, 1, types.FormatRGBA8, []byte{1, 2, 3, 4}, true, false)))
	})
	w.frame(func(_ kernel.Kernel, q *OpQueue) { q.Draw(screenPass(q, 0, "main"), triangle(), set, 1, 0) })
	var buffer types.BufferID
	var texture types.TextureID
	for _, op := range w.backend.lastOps {
		if op.kind == testOpSetBuffer {
			buffer = op.buffer
		}
		if op.kind == testOpAllocateTexture {
			texture = op.texture
		}
	}
	if buffer == 0 || texture == 0 || countOps(w.backend.lastOps, testOpDraw) != 1 {
		t.Fatalf("baked buffer %d, texture %d, draws %d, want both bakes drawn", buffer, texture, countOps(w.backend.lastOps, testOpDraw))
	}

	w.resources(func(k kernel.Kernel, q *ResourceQueue) { q.ReleaseDrawParams(k, set) })
	w.k.PublishEvent(app.RenderEvent{}).Wait()

	var releasedBuffer, releasedTexture, bound bool
	for _, op := range w.backend.lastOps {
		releasedBuffer = releasedBuffer || (op.kind == testOpReleaseBuffer && op.buffer == buffer)
		releasedTexture = releasedTexture || (op.kind == testOpReleaseTexture && op.texture == texture)
		bound = bound || (op.kind == testOpSetBuffer && op.buffer == buffer) || (op.kind == testOpSetTexture && op.texture == texture)
	}
	if !releasedBuffer || !releasedTexture || bound || countOps(w.backend.lastOps, testOpDraw) != 0 {
		t.Errorf("released buffer %v, texture %v, bound after %v, draws %d, want both released and nothing bound or drawn",
			releasedBuffer, releasedTexture, bound, countOps(w.backend.lastOps, testOpDraw))
	}
	if len(*w.reported) != 0 {
		t.Errorf("reported %v", *w.reported)
	}
}
