package internal

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/libs/assets"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"

	"github.com/dvoyni/cog/slots/app"
)

// storageLayout declares one sampler, one texture and one storage buffer, which
// is the shape that tells the three behaviours apart: the first two fall back
// and the third is fatal.
func storageLayout() shader.ShaderLayout {
	return shader.ShaderLayout{
		Resources: []shader.ShaderResource{
			{Name: "mvp", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 64},
			{Name: "MainSampler", Kind: shader.ResourceSampler, Group: 1, Binding: 0},
			{Name: "MainTexture", Group: 1, Binding: 1},
			{Name: "Data", Kind: shader.ResourceStorageBuffer, Group: 1, Binding: 2},
		},
	}
}

// storageFrame records one frame drawing through a set of the given parameters and
// reports what the backend was asked to encode alongside what gfx reported.
func storageFrame(t *testing.T, params ...types.ShaderParameterDescr) (*fakeBackend, []error) {
	t.Helper()
	p := newPlugin()
	layout := storageLayout()
	backend := &fakeBackend{layout: &layout}
	var reported []error
	k := newTestKernelWithErrors(t, p, func(err error) { reported = append(reported, err) })
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	w := recordRaw(t, k)
	ref := w.NewPass(types.PassDescr{Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto(), Load: types.LoadClear, Label: "main"})
	w.Draw(ref, triangle(), testSet(t, k, params...), 1, 0)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	return backend, reported
}

// A storage binding no parameter fills leaves the group one entry short of its
// bind group layout, so the draw encodes with nothing bound for that group and
// the geometry silently does not render. It is dropped and named instead.
func TestADrawMissingAStorageBindingIsDroppedAndNamed(t *testing.T) {
	backend, reported := storageFrame(t)

	if backend.passDraws[0] != 0 {
		t.Errorf("draws = %d, want the draw missing its storage binding dropped", backend.passDraws[0])
	}
	var unsupplied types.ErrStorageBufferUnsupplied
	if len(reported) != 1 || !errors.As(reported[0], &unsupplied) {
		t.Fatalf("reported = %v, want one ErrStorageBufferUnsupplied", reported)
	}
	if unsupplied.Parameter != "Data" || unsupplied.Group != 1 || unsupplied.Binding != 2 {
		t.Errorf("reported = %+v, want Data at group 1 binding 2", unsupplied)
	}
}

// A parameter that names the binding but carries a buffer nothing baked would
// reach the same short bind group, so it is dropped and named the same way,
// saying which of the two it was.
func TestADrawSupplyingAnUnbakedStorageBufferIsDroppedAndNamed(t *testing.T) {
	backend, reported := storageFrame(t, types.ShaderParameterBuffer("Data", types.BufferDescr{}))

	if backend.passDraws[0] != 0 {
		t.Errorf("draws = %d, want the draw supplying an unbaked buffer dropped", backend.passDraws[0])
	}
	var unsupplied types.ErrStorageBufferUnsupplied
	if len(reported) != 1 || !errors.As(reported[0], &unsupplied) {
		t.Fatalf("reported = %v, want one ErrStorageBufferUnsupplied", reported)
	}
	if !unsupplied.Unbaked {
		t.Error("the report does not say the parameter supplied an unbaked buffer")
	}
	// The two causes are different mistakes, so the message has to tell them
	// apart rather than only naming the binding.
	missing := types.ErrStorageBufferUnsupplied{Shader: "s", Parameter: "Data", Group: 1, Binding: 2}
	if unsupplied.Error() == missing.Error() {
		t.Error("an unbaked buffer reads exactly like a binding no parameter names")
	}
}

// Only the storage binding is fatal. A texture falls back to white - which is
// also what an unresolved texture resource renders as - and a sampler to clamp
// and linear, so a draw missing both still renders.
func TestADrawMissingOnlyItsTextureAndSamplerStillRenders(t *testing.T) {
	buffer := types.BufferDescrWithBlob(assets.NewBlob([]byte{1, 2, 3, 4}), true)
	backend, reported := storageFrame(t, types.ShaderParameterBuffer("Data", buffer))

	if backend.passDraws[0] != 1 {
		t.Errorf("draws = %d, want the draw rendered against the fallbacks", backend.passDraws[0])
	}
	if len(reported) != 0 {
		t.Errorf("reported = %v, want nothing for a fallback", reported)
	}
	if countOps(backend.lastOps, testOpSetTexture) != 1 || countOps(backend.lastOps, testOpSetSampler) != 1 {
		t.Error("the fallback texture and sampler were not bound")
	}
}

// A set that misses a binding misses it until someone fixes the set,
// and firstErr carries only the frame's first error - so re-reporting would
// mask every later error in every later frame. The report is made once; the
// draw is dropped every time.
func TestAnUnfilledStorageBindingIsReportedOnceAndDroppedAlways(t *testing.T) {
	p := newPlugin()
	layout := storageLayout()
	backend := &fakeBackend{layout: &layout}
	var reported []error
	k := newTestKernelWithErrors(t, p, func(err error) { reported = append(reported, err) })
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	broken := testSet(t, k)
	drop := func() int {
		w := recordRaw(t, k)
		ref := w.NewPass(types.PassDescr{Target: types.TargetDescrScreen(), Depth: types.DepthDescrAuto(), Load: types.LoadClear, Label: "main"})
		w.Draw(ref, triangle(), broken, 1, 0)
		k.ExecuteCommand[PresentCmd](PresentRequest{})
		k.PublishEvent(app.RenderEvent{}).Wait()
		return backend.passDraws[0]
	}
	if draws := drop(); draws != 0 {
		t.Errorf("first frame draws = %d, want the draw dropped", draws)
	}
	if draws := drop(); draws != 0 {
		t.Errorf("second frame draws = %d, want the draw dropped again", draws)
	}
	if len(reported) != 1 {
		t.Fatalf("reported = %v, want the binding named once", reported)
	}

	// And the quiet is what keeps the channel open. A third frame draws the
	// broken set first and a draw sampling its own attachment behind it.
	// firstErr keeps only the frame's first error, so without the latch the
	// binding would win it again and the mistake behind it would never be
	// heard - which is the whole cost of reporting at frame rate.
	sampler := testSet(t, k)
	w := recordRaw(t, k)
	target, texture := w.NewTemporaryTarget(8, 8, types.FormatRGBA8Srgb)
	ref := w.NewPass(types.PassDescr{Target: target, Depth: types.DepthDescrNone(), Load: types.LoadClear, Label: "feedback"})
	w.Draw(ref, triangle(), broken, 1, 0)
	drawSampling(w, ref, sampler, texture)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	var feedback types.ErrDrawSamplesAttachment
	if len(reported) != 2 || !errors.As(reported[1], &feedback) {
		t.Fatalf("reported = %v, want the later feedback draw through", reported)
	}
}
