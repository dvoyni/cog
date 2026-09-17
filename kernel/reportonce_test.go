package kernel

import (
	"errors"
	"strings"
	"testing"
)

// collectingEngine builds a started engine whose handler appends every error it
// is given, and returns both. The handler never terminates, so a test can watch
// a condition report and then go quiet without the engine cancelling underneath
// it.
func collectingEngine(t *testing.T) (*Engine, *[]error) {
	t.Helper()
	var reported []error
	e := startEngineWithHandler(t, func(err error) bool {
		reported = append(reported, err)
		return false
	})
	return e, &reported
}

func TestKernel_ReportErrorOnceReportsOnlyTheFirstTime(t *testing.T) {
	e, reported := collectingEngine(t)
	k := e.Executioner().Kernel
	boom := errors.New("boom")
	for range 5 {
		if k.ReportErrorOnce("sprite:hero.png", boom) {
			t.Fatal("a non-terminating handler asked for termination")
		}
	}
	if len(*reported) != 1 || !errors.Is((*reported)[0], boom) {
		t.Fatalf("reported %v, want one boom", *reported)
	}
}

func TestKernel_ForgetReportedErrorLetsTheKeySpeakAgain(t *testing.T) {
	e, reported := collectingEngine(t)
	k := e.Executioner().Kernel
	boom := errors.New("boom")
	k.ReportErrorOnce("sprite:hero.png", boom)
	k.ReportErrorOnce("sprite:hero.png", boom)
	k.ForgetReportedError("sprite:hero.png")
	k.ReportErrorOnce("sprite:hero.png", boom)
	k.ReportErrorOnce("sprite:hero.png", boom)
	if len(*reported) != 2 {
		t.Fatalf("reported %d errors, want one per episode", len(*reported))
	}
}

func TestKernel_ReportErrorOnceGatesABurstAsAWhole(t *testing.T) {
	e, reported := collectingEngine(t)
	k := e.Executioner().Kernel
	missing, unbounded := errors.New("missing texture"), errors.New("unbounded primitive")
	k.ReportErrorOnce("model:crate.glb", missing, unbounded)
	k.ReportErrorOnce("model:crate.glb", missing, unbounded)
	if len(*reported) != 2 {
		t.Fatalf("reported %v, want both facts of the first load and neither of the second", *reported)
	}
}

func TestKernel_ReportErrorOnceWithNoErrorsMarksNothing(t *testing.T) {
	e, reported := collectingEngine(t)
	k := e.Executioner().Kernel
	k.ReportErrorOnce("model:crate.glb")
	boom := errors.New("boom")
	k.ReportErrorOnce("model:crate.glb", boom)
	if len(*reported) != 1 || !errors.Is((*reported)[0], boom) {
		t.Fatalf("reported %v; an empty burst must not claim the key", *reported)
	}
}

// Two keys of different types never collide, because the boxed key carries its
// dynamic type. The two singleton conditions gfx and gogpu report depend on it:
// each names its own empty struct type rather than sharing struct{}.
func TestKernel_ReportErrorOnceSeparatesKeysByType(t *testing.T) {
	type backendNotReady struct{}
	type backendAttachFailed struct{}
	e, reported := collectingEngine(t)
	k := e.Executioner().Kernel
	k.ReportErrorOnce(backendNotReady{}, errors.New("not ready"))
	k.ReportErrorOnce(backendAttachFailed{}, errors.New("attach failed"))
	k.ReportErrorOnce(backendNotReady{}, errors.New("not ready"))
	if len(*reported) != 2 {
		t.Fatalf("reported %v, want one per condition", *reported)
	}
}

type reportKeyString string

func TestKernel_ReportErrorOnceSeparatesADefinedTypeFromItsUnderlying(t *testing.T) {
	e, reported := collectingEngine(t)
	k := e.Executioner().Kernel
	k.ReportErrorOnce("shared", errors.New("plain string"))
	k.ReportErrorOnce(reportKeyString("shared"), errors.New("defined type"))
	if len(*reported) != 2 {
		t.Fatalf("reported %v, want both: equal values of unequal types are unequal keys", *reported)
	}
}

func TestKernel_ForgetReportedErrorsClearsOneFamily(t *testing.T) {
	e, reported := collectingEngine(t)
	k := e.Executioner().Kernel
	boom := errors.New("boom")
	k.ReportErrorOnce("model:crate.glb", boom)
	k.ReportErrorOnce("model:crate.glb#hatch", boom)
	k.ReportErrorOnce("texture:rust.png", boom)
	k.ForgetReportedErrors(func(key string) bool {
		return key == "model:crate.glb" || strings.HasPrefix(key, "model:crate.glb#")
	})
	k.ReportErrorOnce("model:crate.glb", boom)
	k.ReportErrorOnce("model:crate.glb#hatch", boom)
	k.ReportErrorOnce("texture:rust.png", boom)
	if len(*reported) != 5 {
		t.Fatalf("reported %d errors, want the model family twice and the texture once", len(*reported))
	}
}

// A predicate over one key type never sees a key of another, so a plugin that
// clears a family of its own string keys cannot clear another plugin's.
func TestKernel_ForgetReportedErrorsSeesOnlyItsOwnKeyType(t *testing.T) {
	type pipelineKey struct{ shader int }
	e, reported := collectingEngine(t)
	k := e.Executioner().Kernel
	boom := errors.New("boom")
	k.ReportErrorOnce(pipelineKey{shader: 7}, boom)
	k.ForgetReportedErrors(func(string) bool { return true })
	k.ReportErrorOnce(pipelineKey{shader: 7}, boom)
	if len(*reported) != 1 {
		t.Fatalf("reported %d errors, want the struct key untouched by a string predicate", len(*reported))
	}
}

func TestKernel_ReportErrorOnceReportsTermination(t *testing.T) {
	var handled error
	e := startEngineWithHandler(t, func(err error) bool { handled = err; return true })
	boom := errors.New("boom")
	if !e.Executioner().Kernel.ReportErrorOnce("key", boom) {
		t.Fatal("a terminating handler was not reported as one")
	}
	if !errors.Is(handled, boom) {
		t.Fatalf("handled %v, want %v", handled, boom)
	}
}

func TestKernel_ZeroKernelPanicsOnReportErrorOnce(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("zero Kernel did not panic")
		}
	}()
	var zero Kernel
	zero.ReportErrorOnce("key", errors.New("boom"))
}
