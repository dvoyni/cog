package kernel

import (
	"context"
	"errors"
	"testing"
)

// errRegisterFailed stands in for a Register that finds its environment missing,
// the way jsfs does without localStorage.
var errRegisterFailed = errors.New("test: backing store is unavailable")

// unboundReader is a plugin whose command handler reads an adapter handle that
// composition only binds on success, so running the handler on a failed
// composition panics inside plugin code.
func unboundReader(ran *bool) *testPlugin {
	plugin := &testPlugin{name: "reader"}
	plugin.register = func(r *Registrar) error {
		backend := r.RequireAdapter[testBackendPort]()
		r.HandleCommand[testDoubleCmd](executing(func(_ Kernel, request int) (int, error) {
			*ran = true
			_ = backend.Get().Label()
			return request * 2, nil
		}))
		return nil
	}
	return plugin
}

// A composition failure closes Ready, so a caller waiting on it goes on to
// dispatch. That dispatch is refused before the handler runs, and the error
// names the Register failure rather than the unbound handle the handler would
// have read.
func TestKernel_FailedCompositionRefusesDispatchWithoutEnteringPluginCode(t *testing.T) {
	handlerRan := false
	failing := testPlugin{name: "failing", register: func(*Registrar) error { return errRegisterFailed }}
	e := New(nil).Handler(func(error) bool { return false }).
		WithPlugins(unboundReader(&handlerRan), failing).
		Run(context.Background())

	<-e.Ready()
	got, err := e.Executioner().ExecuteCommand[testDoubleCmd](21)

	if handlerRan {
		t.Fatal("the handler of a plugin whose composition failed was entered")
	}
	var terminated ErrEngineTerminated
	if !errors.As(err, &terminated) {
		t.Fatalf("err = %v (%T), want ErrEngineTerminated", err, err)
	}
	if !errors.Is(err, errRegisterFailed) {
		t.Fatalf("err = %v, want it to unwrap to the Register failure", err)
	}
	if got != 0 {
		t.Fatalf("got = %d, want the zero response", got)
	}
}

// The refusal keys on termination, not on the absent scheduler: a healthy engine
// still runs a dispatch made before Run, where there is no coordinator and the
// task runs directly.
func TestKernel_DispatchBeforeRunStillRunsOnAHealthyEngine(t *testing.T) {
	p := testPlugin{name: "p", register: func(r *Registrar) error {
		r.HandleCommand[testDoubleCmd](executing(func(_ Kernel, request int) (int, error) {
			return request * 2, nil
		}))
		return nil
	}}
	e := New(nil).Handler(func(err error) bool {
		t.Errorf("unexpected kernel error: %v", err)
		return true
	}).WithPlugins(p)

	got, err := e.Executioner().ExecuteCommand[testDoubleCmd](21)
	if err != nil || got != 42 {
		t.Fatalf("got = %d, %v; want 42, nil", got, err)
	}
	if e.Err() != nil {
		t.Fatalf("Err = %v, want nil on a healthy engine", e.Err())
	}
}

// A subscriber panic is reported, and a reported panic terminates the engine.
// Every dispatch after it is refused with the same error, unwrapping to the
// panic rather than to whatever the next handler would have tripped over.
func TestKernel_DispatchAfterAMidRunPanicIsRefused(t *testing.T) {
	type panicEvent struct{}
	handlerRan := false
	p := testPlugin{name: "p", register: func(r *Registrar) error {
		r.Subscribe[testHandlerA[panicEvent]](observing(func(Kernel, panicEvent) error {
			panic("test: subscriber exploded")
		}))
		r.HandleCommand[testDoubleCmd](executing(func(_ Kernel, request int) (int, error) {
			handlerRan = true
			return request * 2, nil
		}))
		return nil
	}}
	e := startEngineWithHandler(t, func(error) bool { return false }, p)

	if err := e.Executioner().PublishEvent(panicEvent{}).Wait(); err == nil {
		t.Fatal("panicking subscriber returned no error")
	}

	_, err := e.Executioner().ExecuteCommand[testDoubleCmd](21)
	if handlerRan {
		t.Fatal("a handler ran on an engine a panic had terminated")
	}
	var terminated ErrEngineTerminated
	if !errors.As(err, &terminated) {
		t.Fatalf("err = %v (%T), want ErrEngineTerminated", err, err)
	}
	var panicErr ErrPluginPanic
	if !errors.As(err, &panicErr) {
		t.Fatalf("err = %v, want it to unwrap to the plugin panic", err)
	}
}

// Err is the signal next to Ready: nil while the engine is live, the terminating
// cause once it is not, whether that cause was a failed Register or a panic.
func TestKernel_ErrReportsTheTerminatingCause(t *testing.T) {
	failing := testPlugin{name: "failing", register: func(*Registrar) error { return errRegisterFailed }}
	failed := New(nil).Handler(func(error) bool { return false }).WithPlugins(failing)
	<-failed.Ready()
	if err := failed.Err(); !errors.Is(err, errRegisterFailed) {
		t.Fatalf("Err = %v, want the Register failure", err)
	}

	type panicEvent struct{}
	p := testPlugin{name: "p", register: func(r *Registrar) error {
		r.Subscribe[testHandlerA[panicEvent]](observing(func(Kernel, panicEvent) error {
			panic("test: subscriber exploded")
		}))
		return nil
	}}
	live := startEngineWithHandler(t, func(error) bool { return false }, p)
	if err := live.Err(); err != nil {
		t.Fatalf("Err = %v, want nil while the engine is live", err)
	}
	if err := live.Executioner().PublishEvent(panicEvent{}).Wait(); err == nil {
		t.Fatal("panicking subscriber returned no error")
	}
	var panicErr ErrPluginPanic
	if err := live.Err(); !errors.As(err, &panicErr) {
		t.Fatalf("Err = %v, want the plugin panic", err)
	}
}

// A publication on a terminated engine reaches no subscriber, and the refusal
// carries the same cause a command's would.
func TestKernel_FailedCompositionRefusesPublication(t *testing.T) {
	type terminatedEvent struct{}
	observed := false
	watcher := testPlugin{name: "watcher", register: func(r *Registrar) error {
		r.Subscribe[testHandlerA[terminatedEvent]](observing(func(Kernel, terminatedEvent) error {
			observed = true
			return nil
		}))
		return nil
	}}
	failing := testPlugin{name: "failing", register: func(*Registrar) error { return errRegisterFailed }}
	e := New(nil).Handler(func(error) bool { return false }).
		WithPlugins(watcher, failing).
		Run(context.Background())

	<-e.Ready()
	if err := e.Executioner().PublishEvent(terminatedEvent{}).Wait(); !errors.Is(err, errRegisterFailed) {
		t.Fatalf("publication error = %v, want the Register failure", err)
	}
	if observed {
		t.Fatal("a subscriber ran on a terminated engine")
	}
}

// Composition stops registering at the plugin whose Register failed, so a
// command declared after that plugin is simply absent. The dispatch names the
// termination rather than reporting the command unknown, so the caller reads
// the same cause whichever side of the failure its plugin sat on.
func TestKernel_TerminatedEngineNamesTheCauseRatherThanAnUnknownCommand(t *testing.T) {
	failing := testPlugin{name: "failing", register: func(*Registrar) error { return errRegisterFailed }}
	late := testPlugin{name: "late", register: func(r *Registrar) error {
		r.HandleCommand[testDoubleCmd](executing(func(_ Kernel, request int) (int, error) {
			return request * 2, nil
		}))
		return nil
	}}
	e := New(nil).Handler(func(error) bool { return false }).
		WithPlugins(failing, late).
		Run(context.Background())

	<-e.Ready()
	_, err := e.Executioner().ExecuteCommand[testDoubleCmd](21)

	var terminated ErrEngineTerminated
	if !errors.As(err, &terminated) {
		t.Fatalf("err = %v (%T), want ErrEngineTerminated", err, err)
	}
	if !errors.Is(err, errRegisterFailed) {
		t.Fatalf("err = %v, want it to unwrap to the Register failure", err)
	}
}

// An unknown command on a healthy engine still says so: the refusal replaces
// that error only when the engine is terminated.
func TestKernel_HealthyEngineStillReportsAnUnknownCommand(t *testing.T) {
	e := startEngine(t, testPlugin{name: "p"})
	_, err := e.Executioner().ExecuteCommand[testMissingCmd](struct{}{})
	var unknown ErrExecutingUnknownCommand[testMissingCmd]
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v (%T), want ErrExecutingUnknownCommand", err, err)
	}
}
