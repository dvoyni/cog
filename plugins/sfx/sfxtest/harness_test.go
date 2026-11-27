package sfxtest_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/sfxtest"
)

// failureRecorder is the test a harness runs under, keeping each failure it
// is told of instead of failing, so a test can assert that the harness
// failed it.
type failureRecorder struct {
	testing.TB
	failures []string
}

func (r *failureRecorder) Errorf(format string, args ...any) {
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
}

// playMissingClip records a play of a path that is not there, and steps
// twice, so sfx logs the clip's failure and ends its voice.
func playMissingClip(h *sfxtest.Harness) {
	h.Record(func(queue sfx.Queue) { queue.Play("missing.ogg", 0, sfx.VoiceOptions{}) })
	h.Step()
	h.Step()
}

// An error the test did not expect through ExpectError fails it, at the Step
// that logged it, and Reports answers it all the same.
func TestAnErrorTheTestDidNotExpectFailsIt(t *testing.T) {
	recorder := &failureRecorder{TB: t}
	h := sfxtest.NewHarness(recorder)
	h.Spawn(nil)

	playMissingClip(h)

	if len(recorder.failures) != 1 {
		t.Fatalf("an unexpected error failed the test %d times, want once: %q", len(recorder.failures), recorder.failures)
	}
	if reports := h.Reports(); len(reports) != 1 || !isUnreadable(reports[0]) {
		t.Fatalf("the reports are %v, want the clip's ErrClipUnreadable", reports)
	}
}

// An error a match given ExpectError answers true of fails nothing, and is
// still among the reports.
func TestAnExpectedErrorFailsNothing(t *testing.T) {
	recorder := &failureRecorder{TB: t}
	h := sfxtest.NewHarness(recorder)
	h.Spawn(nil)
	h.ExpectError(isUnreadable)

	playMissingClip(h)

	if len(recorder.failures) != 0 {
		t.Fatalf("an expected error failed the test: %q", recorder.failures)
	}
	if reports := h.Reports(); len(reports) != 1 {
		t.Fatalf("the reports are %v, want the clip's failure", reports)
	}
}

// isUnreadable answers whether err is a clip's ErrClipUnreadable.
func isUnreadable(err error) bool {
	_, unreadable := errors.AsType[sfx.ErrClipUnreadable](err)
	return unreadable
}
