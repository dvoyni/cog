package internal

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

// arrayTextureLayout declares the shape canvas' sprite path declares: a sampler
// and a texture binding at texture_2d_array. It is the only non-2D texture
// binding shape the engine has.
func arrayTextureLayout() shader.ShaderLayout {
	return shader.ShaderLayout{
		Resources: []shader.ShaderResource{
			{Name: "params", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 80, Members: []shader.StorageMember{{Name: "mvp", Offset: 0}}},
			{Name: "canvasSampler", Kind: shader.ResourceSampler, Group: 1, Binding: 0},
			{Name: "canvasTexture", TextureView: shader.TextureView2DArray, Group: 1, Binding: 1},
		},
	}
}

// arrayTextureFrame records draws frames of one draw each against the array
// layout, and reports what the backend was asked to encode alongside what gfx
// reported. More than one frame is how report-once is observed: the plan is
// shared, and only the seen-set can stop the second report.
func arrayTextureFrame(t *testing.T, frames int, params ...ParameterDescr) (*fakeBackend, []error) {
	t.Helper()
	p := newPlugin()
	layout := arrayTextureLayout()
	backend := &fakeBackend{layout: &layout}
	var reported []error
	k := newTestKernelWithErrors(t, p, func(err error) { reported = append(reported, err) })
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	for range frames {
		w := recordRaw(t, k)
		w.Pass(PassDescr{Target: ScreenTarget(), Depth: DepthAuto(), Load: types.LoadClear, Label: "main"})
		w.Draw(triangle(), testMaterial(params...), MatParam("mvp", m.NewMat4()))
		k.ExecuteCommand[PresentCmd](PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
	}
	return backend, reported
}

// texturesOfLayers mints a texture descriptor that reports the layer count it
// was allocated with, which is what the check reads.
func texturesOfLayers(layers int) TextureDescr {
	queue := NewResourceQueue(idsOf(&fakeBackend{}))
	return queue.AllocateTexture(4, 4, layers, FormatRGBA8Srgb)
}

// A texture_2d_array binding no parameter fills is not fatal. It resolves to the
// unknown id every unresolved texture resolves to - one still loading, one that
// failed to load - and the backend answers that with a white view of the
// binding's own dimension. A draw that dropped here would make a sprite sheet
// vanish while it loads rather than flash white.
func TestADrawMissingAnArrayTextureStillRenders(t *testing.T) {
	backend, reported := arrayTextureFrame(t, 1)

	if backend.passDraws[0] != 1 {
		t.Errorf("draws = %d, want the draw rendered against the white fallback", backend.passDraws[0])
	}
	if len(reported) != 0 {
		t.Errorf("reported = %v, want nothing for a fallback", reported)
	}
}

// A texture that is there and is the wrong shape is a different claim from one
// that is not there yet. Nothing can be substituted for it - the caller named
// the texture they meant - and rendering it white would hide a mistake they can
// fix, so the draw is dropped and the mistake named.
func TestADrawSupplyingAFlatTextureForAnArrayBindingIsDroppedAndNamed(t *testing.T) {
	backend, reported := arrayTextureFrame(t, 1, TextureParam("canvasTexture", texturesOfLayers(1)))

	if backend.passDraws[0] != 0 {
		t.Errorf("draws = %d, want the draw supplying a single-layer texture dropped", backend.passDraws[0])
	}
	var mismatch types.ErrTextureViewDimensionMismatch
	if len(reported) != 1 || !errors.As(reported[0], &mismatch) {
		t.Fatalf("reported = %v, want one ErrTextureViewDimensionMismatch", reported)
	}
	if mismatch.Parameter != "canvasTexture" || mismatch.Group != 1 || mismatch.Binding != 1 {
		t.Errorf("reported = %+v, want canvasTexture at group 1 binding 1", mismatch)
	}
	if mismatch.Declared == mismatch.Supplied {
		t.Errorf("reported declared %q and supplied %q, want the two dimensions named apart",
			mismatch.Declared, mismatch.Supplied)
	}
}

// A material that misses a binding misses it until someone fixes the material,
// and the frame reports only its first error - so re-reporting every frame would
// mask every later error in every later frame.
func TestAnArrayBindingMismatchIsNamedOncePerShaderAndParameter(t *testing.T) {
	_, reported := arrayTextureFrame(t, 3, TextureParam("canvasTexture", texturesOfLayers(1)))

	if len(reported) != 1 {
		t.Fatalf("reported = %v over three frames, want exactly one", reported)
	}
}

// An array texture fills an array binding, which is the whole point, and nothing
// is reported for a draw that is correct.
func TestADrawSupplyingAnArrayTextureRenders(t *testing.T) {
	backend, reported := arrayTextureFrame(t, 1, TextureParam("canvasTexture", texturesOfLayers(4)))

	if backend.passDraws[0] != 1 {
		t.Errorf("draws = %d, want the draw rendered", backend.passDraws[0])
	}
	if len(reported) != 0 {
		t.Errorf("reported = %v, want nothing for a correct draw", reported)
	}
}

// A descriptor that cannot say how many layers it has is not evidence that it
// has one. BakedTexture carries an id and nothing behind it, so judging it would
// refuse a correct draw over a descriptor's silence - the worst direction for a
// fatal error. The backend's refused bind group remains the backstop.
func TestADrawSupplyingATextureOfUnknownLayersIsNotJudged(t *testing.T) {
	backend, reported := arrayTextureFrame(t, 1, TextureParam("canvasTexture", BakedTexture(9, 4, 4)))

	if backend.passDraws[0] != 1 {
		t.Errorf("draws = %d, want a descriptor of unknown layers left alone", backend.passDraws[0])
	}
	if len(reported) != 0 {
		t.Errorf("reported = %v, want nothing for a descriptor that cannot say", reported)
	}
}
