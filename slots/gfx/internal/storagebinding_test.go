package internal

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/descriptors"

	"github.com/dvoyni/cog/slots/gfx/internal/types"

	"github.com/dvoyni/cog/slots/gfx/internal/shader"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

// storageLayout declares one sampler, one texture and one storage buffer, which
// is the shape that tells the three behaviours apart: the first two fall back
// and the third is fatal.
func storageLayout() shader.ShaderLayout {
	return shader.ShaderLayout{
		Resources: []shader.ShaderResource{
			{Name: "params", Kind: shader.ResourceUniformBuffer, Group: 0, Binding: 0, Size: 80, Members: []shader.StorageMember{{Name: "mvp", Offset: 0}}},
			{Name: "MainSampler", Kind: shader.ResourceSampler, Group: 1, Binding: 0},
			{Name: "MainTexture", Group: 1, Binding: 1},
			{Name: "Data", Kind: shader.ResourceStorageBuffer, Group: 1, Binding: 2},
		},
	}
}

// storageFrame records one frame drawing with the given material parameters and
// reports what the backend was asked to encode alongside what gfx reported.
func storageFrame(t *testing.T, params ...descriptors.ParameterDescr) (*fakeBackend, []error) {
	t.Helper()
	p := newPlugin()
	layout := storageLayout()
	backend := &fakeBackend{layout: &layout}
	var reported []error
	k := newTestKernelWithErrors(t, p, func(err error) { reported = append(reported, err) })
	k.ExecuteCommand[attachBackendCmd](attachBackendRequest{Backend: backend})

	w := recordRaw(t, k)
	ref := w.NewPass(descriptors.PassDescr{Target: descriptors.ScreenTarget(), Depth: descriptors.DepthAuto(), Load: types.LoadClear, Label: "main"})
	w.Draw(ref, triangle(), testMaterial(params...), 1, 0, descriptors.MatParam("mvp", m.NewMat4()))
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

// A parameter that names the binding but carries a buffer nothing baked reaches
// the same short bind group: emitResources skips a zero id. The plan cannot see
// it, because a plan is cached per parameter shape and the id is per draw.
func TestADrawSupplyingAnUnbakedStorageBufferIsDroppedAndNamed(t *testing.T) {
	backend, reported := storageFrame(t, descriptors.BufferParam("Data", descriptors.BufferDescr{}))

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
	buffer := descriptors.BufferWithBytes([]byte{1, 2, 3, 4}, true)
	backend, reported := storageFrame(t, descriptors.BufferParam("Data", buffer))

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

// A material that misses a binding misses it until someone fixes the material,
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

	drop := func() int {
		w := recordRaw(t, k)
		ref := w.NewPass(descriptors.PassDescr{Target: descriptors.ScreenTarget(), Depth: descriptors.DepthAuto(), Load: types.LoadClear, Label: "main"})
		w.Draw(ref, triangle(), testMaterial(), 1, 0, descriptors.MatParam("mvp", m.NewMat4()))
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
	// broken material first and a second, differently broken one behind it.
	// firstErr keeps only the frame's first error, so without the latch the
	// binding would win it again and the mismatch behind it would never be
	// heard - which is the whole cost of reporting at frame rate.
	w := recordRaw(t, k)
	ref := w.NewPass(descriptors.PassDescr{Target: descriptors.ScreenTarget(), Depth: descriptors.DepthAuto(), Load: types.LoadClear, Label: "main"})
	w.Draw(ref, triangle(), testMaterial(), 1, 0, descriptors.MatParam("mvp", m.NewMat4()))
	w.Draw(ref, triangle(), testMaterial(descriptors.BufferParam("mvp", descriptors.BufferWithBytes([]byte{1, 2, 3, 4}, true))), 1, 0)
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	var mismatch types.ErrParameterKindMismatch
	if len(reported) != 2 || !errors.As(reported[1], &mismatch) {
		t.Fatalf("reported = %v, want the later kind mismatch through", reported)
	}
}
