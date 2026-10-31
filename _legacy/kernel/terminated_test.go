package kernel

import (
	"errors"
	"testing"
)

// errRegisterFailed stands in for a Register that finds its environment missing,
// the way jsstorage does without localStorage.
var errRegisterFailed = errors.New("test: backing store is unavailable")

// An engine whose composition failed answers with the cause from Run, and never
// starts a plugin. There is no separate place to ask: Run's return value is the
// whole of what happened.
func TestKernel_FailedCompositionComesBackFromRun(t *testing.T) {
	started := false
	failing := testPlugin{name: "failing", register: func(*Registrar) error { return errRegisterFailed }}
	healthy := testPlugin{name: "healthy", start: func(Executioner) error { started = true; return nil }}

	cause := New(nil).Handler(func(error) error { return nil }).
		WithPlugins(healthy, failing).
		Run()

	if !errors.Is(cause, errRegisterFailed) {
		t.Fatalf("Run = %v, want %v", cause, errRegisterFailed)
	}
	if started {
		t.Fatal("a plugin started on an engine whose composition failed")
	}
}

// The scheduler is live from New, so a dispatch before Run runs rather than
// waiting for a coordinator that does not exist yet.
func TestKernel_DispatchBeforeRunStillRunsOnAHealthyEngine(t *testing.T) {
	p := testPlugin{name: "p", register: func(r *Registrar) error {
		r.HandleCommand[testDoubleCmd](executing(func(_ Kernel, request int) int { return request * 2 }))
		return nil
	}}
	e := New(nil).Handler(func(err error) error {
		t.Errorf("unexpected kernel error: %v", err)
		return err
	}).WithPlugins(p)
	t.Cleanup(e.Quit)

	if got := e.Executioner().ExecuteCommand[testDoubleCmd](21); got != 42 {
		t.Fatalf("got = %d, want 42", got)
	}
}

// A verdict terminates the run and becomes what Run answers with. The cause is
// the first verdict rather than whatever it knocked over afterwards, and every
// report still reaches the handler.
func TestKernel_AVerdictBecomesWhatRunReturns(t *testing.T) {
	boom := errors.New("mid-run boom")
	later := errors.New("knocked over afterwards")
	quit := make(chan struct{})
	host := &testHostPlugin{
		name: "host",
		run:  func() error { <-quit; return nil },
		quit: func() { close(quit) },
	}
	p := testPlugin{name: "p", start: func(k Executioner) error {
		k.ReportError(boom)
		k.ReportError(later)
		return nil
	}}

	var seen []error
	cause := New(nil).Handler(func(err error) error { seen = append(seen, err); return err }).
		WithPlugins(p, host).
		Run()

	if !errors.Is(cause, boom) {
		t.Fatalf("Run = %v, want the first verdict %v", cause, boom)
	}
	if len(seen) != 2 || !errors.Is(seen[1], later) {
		t.Fatalf("handler saw %v, want both reports in order", seen)
	}
}

// An unknown command is the kernel's own failure on a healthy engine, so it is
// reported like any other.
func TestKernel_HealthyEngineStillReportsAnUnknownCommand(t *testing.T) {
	var handled error
	e := startEngineWithHandler(t, func(err error) error { handled = err; return nil })

	e.Executioner().ExecuteCommand[testMissingCmd](struct{}{})

	var unknown ErrExecutingUnknownCommand[testMissingCmd]
	if !errors.As(handled, &unknown) {
		t.Fatalf("handled = %v, want an unknown-command report", handled)
	}
}
