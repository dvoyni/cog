package types

import (
	"strings"
	"testing"
)

// laggingReader is the reader these tests let fall behind. It is a named func
// so the panic's naming of the System can be checked against a name a person
// wrote.
func laggingReader(h *Hooks[collider, HookAddedRemoved]) {}

// readerError runs the reader and reports what running it reported, clearing
// it so that the next run starts from nothing.
func (w *hookWorld) readerError() error {
	w.reported = nil
	w.engine.Executioner().ExecuteCommand[hookReadCmd](hookRequest{})
	return w.reported
}

// appendingRuns runs the writer n times, each run appending two records to
// collider's log, and between them a run that appends nothing and so is not
// counted.
func (w *hookWorld) appendingRuns(t *testing.T, n int) {
	t.Helper()
	for range n {
		w.write(t, func(set *Set[collider], remove *Remove[collider]) {
			e := w.entities.alloc()
			set.UpdateFor(e, collider{Radius: 1})
			remove.From(e)
		})
		w.write(t, func(set *Set[collider], remove *Remove[collider]) {})
	}
}

// TestAReaderPanicsPastSixteenCountedRuns is hooks.md § Validation mode checks
// the rule: each run of a System that appended to a Store's log counts once,
// and a reader whose run starts more than 16 counted runs after its last run
// panics, naming its System and the Store. A release build checks nothing.
func TestAReaderPanicsPastSixteenCountedRuns(t *testing.T) {
	w := newHookWorld(t, laggingReader)

	w.appendingRuns(t, 16)
	if err := w.readerError(); err != nil {
		t.Fatalf("a reader 16 counted runs behind was refused: %v", err)
	}

	w.appendingRuns(t, 17)
	err := w.readerError()
	if !validate {
		if err != nil {
			t.Fatalf("a release build refused a reader 17 runs behind: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatal("a reader 17 counted runs behind was not refused")
	}
	for _, want := range []string{"laggingReader", "Store[ecs.collider]", "17"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the panic does not name %q: %v", want, err)
		}
	}
}

// TestAReleaseBuildNeverChecksPace is the other half: a thousand counted runs
// behind is a broken contract a release build pays nothing to notice.
func TestAReleaseBuildNeverChecksPace(t *testing.T) {
	if validate {
		t.Skip("a validating build checks pace")
	}
	w := newHookWorld(t, laggingReader)
	w.appendingRuns(t, 1000)
	if err := w.readerError(); err != nil {
		t.Fatalf("a release build refused a reader 1 000 runs behind: %v", err)
	}
}
