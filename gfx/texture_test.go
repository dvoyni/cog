package gfx

import (
	"slices"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/m"
)

func TestFormatScreenResolvesToTheFrameBufferFormat(t *testing.T) {
	if got := FormatScreen.Resolve(); got != FrameBufferFormat {
		t.Errorf("FormatScreen.Resolve() = %v, want the frame buffer's %v", got, FrameBufferFormat)
	}
	for _, format := range []TextureFormat{FormatRGBA8, FormatRGBA8Srgb, FormatDepth32F} {
		if got := format.Resolve(); got != format {
			t.Errorf("%v.Resolve() = %v, want itself", format, got)
		}
	}
}

// bakedTextureFormats reports the format of every texture bake the backend saw,
// in recording order.
func bakedTextureFormats(backend *fakeBackend) []TextureFormat {
	var formats []TextureFormat
	for _, op := range backend.lastOps {
		if op.kind == gpuBakeTexture {
			formats = append(formats, TextureFormat(op.arg2))
		}
	}
	return formats
}

func TestResourceTextureAlwaysBakesSrgb(t *testing.T) {
	filesystem := fstest.MapFS{"normal.png": &fstest.MapFile{Data: testPNG(t)}}
	p := New()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

	w := recordList(t, k)
	w.Draw(triangle(), testMaterial(TextureParam("MainTexture", TextureWithResource("normal.png"))), MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	got := bakedTextureFormats(backend)
	if len(got) != 1 || got[0] != FormatRGBA8Srgb {
		t.Fatalf("baked formats = %v, want one FormatRGBA8Srgb: a decoded image is sRGB whatever it is named", got)
	}
}

func TestSameResourcePathBakesOnce(t *testing.T) {
	filesystem := &countingFS{FS: fstest.MapFS{
		"hero.png": &fstest.MapFile{Data: testPNG(t)},
	}}
	p := New()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

	w := recordList(t, k)
	w.Draw(triangle(), testMaterial(TextureParam("MainTexture", TextureWithResource("hero.png"))), MatParam("mvp", m.NewMat4()))
	w.Draw(triangle(), testMaterial(TextureParam("MainTexture", TextureWithResource("hero.png"))), MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	got := bakedTextureFormats(backend)
	if len(got) != 1 || got[0] != FormatRGBA8Srgb {
		t.Fatalf("baked formats = %v, want one FormatRGBA8Srgb: a path has one colour space, so it bakes once", got)
	}
	if filesystem.opens != 1 {
		t.Fatalf("opens of hero.png = %d, want 1", filesystem.opens)
	}
}

// A texture a pass renders into carries a render-attachment usage the backend
// only asks for when it is told to, and ResourceQueue is the only allocator of
// textures that outlive the frame. Plain AllocateTexture deliberately does not
// ask: a sampled-only texture - an atlas array, a decoded picture - would pay a
// usage it never uses, and on some backends a wider usage costs a compression
// format.
func TestOnlyAllocateRenderTargetAsksForARenderableTexture(t *testing.T) {
	p := New()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

	withResourceQueue(t, k, func(resources *ResourceQueue) {
		resources.AllocateTexture(64, 64, 1, FormatRGBA8Srgb)
		resources.AllocateRenderTarget(64, 64, 1, FormatRGBA8Srgb)
	})
	k.PublishEvent(app.RenderEvent{}).Wait()

	var renderable []bool
	for _, op := range backend.lastOps {
		if op.kind == gpuAllocateTexture {
			renderable = append(renderable, op.arg4 != 0)
		}
	}
	if len(renderable) != 2 {
		t.Fatalf("texture allocations = %d, want 2", len(renderable))
	}
	if renderable[0] {
		t.Error("AllocateTexture asked for a renderable texture; a sampled-only texture should not")
	}
	if !renderable[1] {
		t.Error("AllocateRenderTarget produced a texture no pass can render into")
	}
}

// The durable round trip, which is the whole point of the method: a texture
// allocated once is rendered into on one frame and sampled on the next. A
// TemporaryTarget cannot do this - its contents do not survive the frame - so
// anything cached across frames (a canvas layer baked into a scene material, a
// shadow map, a UI panel drawn once) needs this allocator.
func TestARenderTargetIsRenderedIntoAndSampledOnALaterFrame(t *testing.T) {
	p := New()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

	var texture TextureDescr
	withResourceQueue(t, k, func(resources *ResourceQueue) {
		texture = resources.AllocateRenderTarget(64, 64, 1, FormatRGBA8Srgb)
	})
	q := recordRaw(t, k)
	q.Pass(PassDescr{Target: TextureTarget(texture, 0, 0), Depth: DepthNone(), Load: LoadClear, Label: "bake"})
	drawInto(q)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(backend.transitions) != 0 {
		t.Fatalf("first frame transitions = %v, want none: nothing sampled what it wrote", backend.transitions)
	}

	q = recordRaw(t, k)
	q.Pass(PassDescr{Target: ScreenTarget(), Depth: DepthNone(), Load: LoadClear, Label: "use"})
	q.Draw(triangle(), testMaterial(TextureParam("MainTexture", texture)), MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(backend.boundTextures) == 0 || !slices.Contains(backend.boundTextures, texture.ID()) {
		t.Fatalf("bound textures = %v, want the render target %v sampled a frame after it was written",
			backend.boundTextures, texture.ID())
	}
	// The write was a frame ago and the backend's own submit ordered it, so the
	// second frame has no write of its own to order this read against.
	if len(backend.transitions) != 0 {
		t.Errorf("second frame transitions = %v, want none: the write was on an earlier frame", backend.transitions)
	}
}
