package gfx

import (
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/m"
)

func TestFormatScreenResolvesToSrgb(t *testing.T) {
	if got := FormatScreen.Resolve(); got != FormatRGBA8Srgb {
		t.Errorf("FormatScreen.Resolve() = %v, want FormatRGBA8Srgb", got)
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

func TestResourceTextureBakesWithItsDeclaredFormat(t *testing.T) {
	filesystem := fstest.MapFS{"normal.png": &fstest.MapFile{Data: testPNG(t)}}
	p := New()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

	w := recordList(t, k)
	w.Draw(triangle(), testMaterial(TextureParam("MainTexture", TextureWithResource("normal.png", FormatRGBA8))), MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	got := bakedTextureFormats(backend)
	if len(got) != 1 || got[0] != FormatRGBA8 {
		t.Fatalf("baked formats = %v, want one FormatRGBA8", got)
	}
}

func TestSameResourcePathInTwoFormatsBakesTwoTextures(t *testing.T) {
	filesystem := &countingFS{FS: fstest.MapFS{
		"hero.png": &fstest.MapFile{Data: testPNG(t)},
	}}
	p := New()
	k := newTestKernelWithFS(t, p, filesystem)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

	w := recordList(t, k)
	w.Draw(triangle(), testMaterial(TextureParam("MainTexture", TextureWithResource("hero.png", FormatRGBA8Srgb))), MatParam("mvp", m.NewMat4()))
	w.Draw(triangle(), testMaterial(TextureParam("MainTexture", TextureWithResource("hero.png", FormatRGBA8))), MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	got := bakedTextureFormats(backend)
	if len(got) != 2 || got[0] != FormatRGBA8Srgb || got[1] != FormatRGBA8 {
		t.Fatalf("baked formats = %v, want [FormatRGBA8Srgb FormatRGBA8]: one path in two formats is two textures", got)
	}
}
