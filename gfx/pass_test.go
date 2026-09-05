package gfx

import (
	"errors"
	"testing"

	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/m"
)

// passFrame records one frame through the plugin and reports the passes the
// backend was asked to encode.
func passFrame(t *testing.T, record func(*OpQueue)) (*fakeBackend, kernel.Executioner) {
	t.Helper()
	p := New()
	k := newTestKernel(t, p)
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})
	record(recordRaw(t, k))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()
	return backend, k
}

func drawInto(q *OpQueue) {
	q.Draw(triangle(), testMaterial(), MatParam("mvp", m.NewMat4()))
}

func TestAPassCarriesItsTargetAndClearToTheBackend(t *testing.T) {
	backend, _ := passFrame(t, func(q *OpQueue) {
		q.Pass(PassDescr{
			Target: ScreenTarget(), Depth: DepthAuto(),
			Load: LoadClear, Clear: m.Color{R: 1, A: 1}, Label: "screen",
		})
		drawInto(q)
	})
	if len(backend.lastPasses) != 1 {
		t.Fatalf("passes = %d, want 1", len(backend.lastPasses))
	}
	pass := backend.lastPasses[0]
	if !pass.Screen || !pass.DepthAuto {
		t.Errorf("pass = %+v, want the screen target with automatic depth", pass)
	}
	if pass.Load != LoadClear || pass.Clear != (m.Color{R: 1, A: 1}) {
		t.Errorf("pass load = %v clear = %v, want LoadClear with the declared colour", pass.Load, pass.Clear)
	}
	if backend.passDraws[0] != 1 {
		t.Errorf("draws in the pass = %d, want 1", backend.passDraws[0])
	}
}

func TestDrawsOutsideAnyPassAreDroppedAndReported(t *testing.T) {
	p := New()
	var reported []error
	k := newTestKernelWithErrors(t, p, func(err error) { reported = append(reported, err) })
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

	// No pass declared: there is no implicit one to absorb the draw.
	drawInto(recordRaw(t, k))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if len(backend.lastPasses) != 0 {
		t.Errorf("passes = %d, want none", len(backend.lastPasses))
	}
	var dropped ErrDrawWithoutPass
	for _, err := range reported {
		if errors.As(err, &dropped) && dropped.Count == 1 {
			return
		}
	}
	t.Errorf("reported errors = %v, want one ErrDrawWithoutPass for 1 draw", reported)
}

func TestPassesRunInDeclaredOrderNotStreamOrder(t *testing.T) {
	backend, _ := passFrame(t, func(q *OpQueue) {
		late := q.Pass(PassDescr{Order: 10, Target: ScreenTarget(), Depth: DepthAuto(), Load: LoadClear, Label: "late"})
		drawInto(q)
		early := q.Pass(PassDescr{Order: -10, Target: ScreenTarget(), Depth: DepthAuto(), Load: LoadClear, Label: "early"})
		drawInto(q)
		// Re-selecting appends to a pass declared earlier in the frame.
		q.SetPass(late)
		drawInto(q)
		_ = early
	})
	if len(backend.lastPasses) != 2 {
		t.Fatalf("passes = %d, want 2", len(backend.lastPasses))
	}
	if backend.lastPasses[0].Label != "early" || backend.lastPasses[1].Label != "late" {
		t.Fatalf("pass order = %q then %q, want early then late", backend.lastPasses[0].Label, backend.lastPasses[1].Label)
	}
	if backend.passDraws[0] != 1 || backend.passDraws[1] != 2 {
		t.Errorf("draws per pass = %v, want 1 in early and 2 in late", backend.passDraws)
	}
}

func TestEqualOrderKeepsDeclarationSequence(t *testing.T) {
	backend, _ := passFrame(t, func(q *OpQueue) {
		q.Pass(PassDescr{Target: ScreenTarget(), Depth: DepthAuto(), Load: LoadClear, Label: "first"})
		drawInto(q)
		q.Pass(PassDescr{Target: ScreenTarget(), Depth: DepthAuto(), Load: LoadClear, Label: "second"})
		drawInto(q)
	})
	if len(backend.lastPasses) != 2 || backend.lastPasses[0].Label != "first" {
		t.Fatalf("passes = %+v, want first then second", backend.lastPasses)
	}
}

func TestAdjacentPassesMergeWhenTheSuccessorOnlyContinues(t *testing.T) {
	backend, _ := passFrame(t, func(q *OpQueue) {
		q.Pass(PassDescr{Order: 0, Target: ScreenTarget(), Depth: DepthAuto(), Load: LoadClear, Label: "layer0"})
		drawInto(q)
		// Same attachments, preserving both, after a pass that kept both: by
		// definition indistinguishable from continuing the first.
		q.Pass(PassDescr{Order: 1, Target: ScreenTarget(), Depth: DepthAuto(), Label: "layer1"})
		drawInto(q)
	})
	if len(backend.lastPasses) != 1 {
		t.Fatalf("passes = %d, want the two merged into 1", len(backend.lastPasses))
	}
	if backend.lastPasses[0].Load != LoadClear {
		t.Errorf("merged load = %v, want the first pass's LoadClear", backend.lastPasses[0].Load)
	}
	if backend.passDraws[0] != 2 {
		t.Errorf("draws in the merged pass = %d, want 2", backend.passDraws[0])
	}
}

func TestAPassThatClearsAgainDoesNotMerge(t *testing.T) {
	backend, _ := passFrame(t, func(q *OpQueue) {
		q.Pass(PassDescr{Order: 0, Target: ScreenTarget(), Depth: DepthAuto(), Load: LoadClear, Label: "first"})
		drawInto(q)
		q.Pass(PassDescr{Order: 1, Target: ScreenTarget(), Depth: DepthAuto(), DepthLoad: LoadClear, Label: "second"})
		drawInto(q)
	})
	if len(backend.lastPasses) != 2 {
		t.Fatalf("passes = %d, want 2: clearing depth is an effect the merge would lose", len(backend.lastPasses))
	}
}

func TestPassWithoutDrawsRunsOnlyWhenAnAttachmentLoads(t *testing.T) {
	backend, _ := passFrame(t, func(q *OpQueue) {
		// A camera that culled everything, but still clears its target.
		q.Pass(PassDescr{Order: 0, Target: ScreenTarget(), Depth: DepthAuto(), Load: LoadClear, Label: "clearing"})
		// Nothing to draw and nothing to load: unobservable, so it is not encoded.
		q.Pass(PassDescr{Order: 5, Target: ScreenTarget(), Depth: DepthAuto(), Label: "empty"})
	})
	if len(backend.lastPasses) != 1 || backend.lastPasses[0].Label != "clearing" {
		t.Fatalf("passes = %+v, want only the clearing one", backend.lastPasses)
	}
}

func TestDrawSamplingItsOwnAttachmentIsRejected(t *testing.T) {
	p := New()
	var reported []error
	k := newTestKernelWithErrors(t, p, func(err error) { reported = append(reported, err) })
	backend := &fakeBackend{}
	k.ExecuteCommand[SetBackendCmd](SetBackendRequest{Backend: backend})

	var target TextureDescr
	withResourceQueue(t, k, func(resources *ResourceQueue) {
		target = resources.AllocateTexture(64, 64, 1, FormatRGBA8Srgb)
	})
	w := recordRaw(t, k)
	w.Pass(PassDescr{Target: TextureTarget(target, 0, 0), Depth: DepthNone(), Load: LoadClear, Label: "feedback"})
	w.Draw(triangle(), testMaterial(TextureParam("MainTexture", target)), MatParam("mvp", m.NewMat4()))
	k.ExecuteCommand[PresentCmd](PresentRequest{})
	k.PublishEvent(app.RenderEvent{}).Wait()

	if backend.passDraws[0] != 0 {
		t.Errorf("draws = %d, want the feedback draw dropped", backend.passDraws[0])
	}
	if len(reported) == 0 {
		t.Error("no error reported for a draw sampling its own attachment")
	}
}
