package internal

import (
	"bytes"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dvoyni/cog/bundles/ecsscene"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

// stubBackend is a gfx.Backend that mints ids and does nothing else. Everything
// this file asserts — residency, expansion, culling, packing — scene decides
// before a backend is reached, which is what makes the whole path assertable
// with no GPU. It declares no shader layout, so gfx drops every parameter the
// frame binds, which is also fine: what a draw binds is scene's business and is
// tested there.
type stubBackend struct {
	nextTexture gfx.TextureID
	nextBuffer  gfx.BufferID
	nextID      uint32
}

func (b *stubBackend) Ready() bool { return true }

func (b *stubBackend) NewTexture() gfx.TextureID {
	b.nextTexture++
	return b.nextTexture
}

func (b *stubBackend) NewBuffer() gfx.BufferID {
	b.nextBuffer++
	return b.nextBuffer
}

func (b *stubBackend) next() uint32 {
	b.nextID++
	return b.nextID
}

func (b *stubBackend) NewSampler(gfx.SamplerDesc) (gfx.SamplerID, error) {
	return gfx.SamplerID(b.next()), nil
}

func (b *stubBackend) FreeSampler(gfx.SamplerID) {}

func (b *stubBackend) NewShader(gfx.ShaderDesc) (gfx.ShaderID, error) {
	return gfx.ShaderID(b.next()), nil
}

func (b *stubBackend) FreeShader(gfx.ShaderID) {}

func (b *stubBackend) ShaderLayout(gfx.ShaderID) gfx.ShaderLayout { return gfx.ShaderLayout{} }

func (b *stubBackend) NewPipeline(gfx.PipelineDesc) (gfx.PipelineID, error) {
	return gfx.PipelineID(b.next()), nil
}

func (b *stubBackend) FreePipeline(gfx.PipelineID) {}

func (b *stubBackend) ScreenFramebuffer() (gfx.TextureViewID, int, int) {
	return gfx.TextureViewID(1), 1600, 1200
}

func (b *stubBackend) TextureView(gfx.TextureID, int, int) gfx.TextureViewID {
	return gfx.TextureViewID(b.next())
}

func (b *stubBackend) Limits() gfx.Limits { return gfx.DefaultLimits() }

func (b *stubBackend) Execute(*gfx.Queue) {}

func (b *stubBackend) TakeCapture() (gfx.Capture, bool) { return gfx.Capture{}, false }

// crateGLB is the smallest drawable file: one triangle, one node, one scene. It
// is built rather than read, because this package has no testdata and the point
// is that the path a Model Component holds is a file scene really loads.
func crateGLB(t testing.TB) []byte {
	t.Helper()
	doc := &gltf.Document{Asset: gltf.Asset{Version: "2.0"}}
	positions := modeler.WritePosition(doc, [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}})
	doc.Meshes = []*gltf.Mesh{{
		Name:       "crate",
		Primitives: []*gltf.Primitive{{Attributes: gltf.PrimitiveAttributes{gltf.POSITION: positions}}},
	}}
	doc.Nodes = []*gltf.Node{{Name: "crate", Mesh: gltf.Index(0)}}
	doc.Scenes = []*gltf.Scene{{Name: "scene", Nodes: []int{0}}}
	doc.Scene = gltf.Index(0)
	var buffer bytes.Buffer
	if err := gltf.NewEncoder(&buffer).Encode(doc); err != nil {
		t.Fatalf("encoding the model: %v", err)
	}
	return buffer.Bytes()
}

// crateModelComponent is a Model naming the crate file's whole default scene.
func crateModelComponent() *ecsscene.Model {
	return &ecsscene.Model{Ref: scene.ModelRef{Path: crateModel}}
}

// newDrawingHarness is the harness with a backend, a viewport and the crate on
// disk: the whole engine, able to decide a frame.
func newDrawingHarness(t testing.TB, ids uint32) *harness {
	t.Helper()
	files := fstest.MapFS{crateModel: &fstest.MapFile{Data: crateGLB(t)}}
	h := newHarnessWith(t, files, ids, &stubBackend{})
	if _, err := h.kernel.ExecuteCommand[gfx.SetViewportCmd](gfx.SetViewportRequest{
		Width: 800, Height: 600, FramebufferWidth: 1600, FramebufferHeight: 1200,
	}); err != nil {
		t.Fatalf("setting the viewport: %v", err)
	}
	h.spawn(t, spawnRequest{
		Place:  ecsscene.Transform(scene.LookAt(m.Vec3{Z: 30}, m.Vec3{}, m.Vec3{Y: 1})),
		Camera: &ecsscene.Camera{FovY: 1.0472, Near: 0.1, Far: 200},
	})
	return h
}

// frameUntil runs frames until ready. A model load is two commands on their own
// goroutines, so residency lands some frames after the draw that asked for it.
func (h *harness) frameUntil(t testing.TB, what string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.frame(t)
		if ready() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s; errors so far: %v", what, h.errs.snapshot())
		}
		time.Sleep(time.Millisecond)
	}
}

// TestDrawableEntitiesBecomeInstancesInAPass is the end-to-end: Components on a
// real world, through the binding's one System, into the real scene plugin,
// which loads the file a Model names and packs the Entities into a pass.
//
// Nothing between the Component and the instance was written for this test.
func TestDrawableEntitiesBecomeInstancesInAPass(t *testing.T) {
	h := newDrawingHarness(t, 256)
	h.spawn(t, spawnRequest{Count: 3, Step: 2, Model: crateModelComponent()})

	h.frameUntil(t, "the crate to become resident", func() bool {
		passes := h.passes(t)
		return len(passes) == 1 && passes[0].Instances == 3
	})

	passes := h.passes(t)
	if passes[0].Recorded != 3 || passes[0].Culled != 0 {
		t.Fatalf("the pass recorded %d draws and culled %d, want 3 and 0",
			passes[0].Recorded, passes[0].Culled)
	}
	// Three separate Model calls are three batches: the binding records one
	// call per Entity and batches nothing, and scene does not merge calls.
	if len(passes[0].Batches) != 3 {
		t.Fatalf("the pass emitted %d batches for three separate calls", len(passes[0].Batches))
	}
	for _, err := range h.errs.snapshot() {
		t.Errorf("the frame reported %v", err)
	}
}

// TestADespawnedDrawableStopsDrawing is the other end of the lifecycle, and it
// needs nothing from the ECS: there are no lifecycle hooks, the Components are
// the source of truth, and a frame records what is there when it runs.
func TestADespawnedDrawableStopsDrawing(t *testing.T) {
	h := newDrawingHarness(t, 256)
	first := h.spawn(t, spawnRequest{Count: 2, Step: 2, Model: crateModelComponent()})

	h.frameUntil(t, "the crate to become resident", func() bool {
		passes := h.passes(t)
		return len(passes) == 1 && passes[0].Instances == 2
	})

	h.despawn(t, first)
	h.frame(t)

	if passes := h.passes(t); len(passes) != 1 || passes[0].Instances != 1 {
		t.Fatalf("after the despawn the pass packed %v, want one instance", passes)
	}
}

// TestAPresentMaterialWithNoTagsDrawsNothing is presence meaning what scene's
// empty material means: a material serving no pass. Every Entity here reaches
// the pass, and only the two with no Material are packed — the bundled PBR for
// the mesh and the file's own material for the model.
func TestAPresentMaterialWithNoTagsDrawsNothing(t *testing.T) {
	h := newDrawingHarness(t, 256)
	ref := h.bake(t)
	h.spawn(t, spawnRequest{Mesh: &ecsscene.Mesh{Ref: ref, NeverCull: true}})
	h.spawn(t, spawnRequest{Mesh: &ecsscene.Mesh{Ref: ref, NeverCull: true}, Material: &ecsscene.Material{}})
	h.spawn(t, spawnRequest{Place: ecsscene.Transform{Position: m.Vec3{X: 2}}, Model: crateModelComponent()})
	h.spawn(t, spawnRequest{Place: ecsscene.Transform{Position: m.Vec3{X: 4}}, Model: crateModelComponent(), Material: &ecsscene.Material{}})

	h.frameUntil(t, "every draw to reach the pass", func() bool {
		passes := h.passes(t)
		return len(passes) == 1 && passes[0].Recorded == 4
	})

	if pass := h.passes(t)[0]; pass.Instances != 2 {
		t.Fatalf("the pass packed %d instances of 4 recorded draws, want the 2 without a Material", pass.Instances)
	}
}
