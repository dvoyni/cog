package rendertest

import (
	"errors"

	"github.com/dvoyni/cog/plugins/gfx/recording"
)

// Calls answers every call gfx's recording adapter has recorded so far, for
// rendertest's tests to check the order of what render did on gfx.
func (h *Harness) Calls() []recording.Call {
	return h.instance.Recording().Calls
}

// RecordedFrames answers the frames gfx's recording adapter has recorded so
// far, for rendertest's tests to read the commands of render's own list.
func (h *Harness) RecordedFrames() []recording.Frame {
	return h.instance.Recording().Frames()
}

// AnswerNext makes the next call of kind gfx's recording adapter takes
// answer answer, for rendertest's tests to give a shader's reflection an
// answer of their own, or an error, in place of its declaration's.
func (h *Harness) AnswerNext(kind recording.CallKind, answer recording.Answer) {
	h.declarations.queueAnswer(kind, answer)
}

// ExpectLoggedError makes err, logged wrapped or not, fail no test, for
// rendertest's tests to fail a call with an answer of AnswerNext, such as
// gfx.ErrDeviceLost to lose the device.
func (h *Harness) ExpectLoggedError(err error) {
	h.ExpectError(func(logged error) bool { return errors.Is(logged, err) })
}

// WaitUntil ticks the main thread until done answers true, failing the test
// naming what after the harness's timeout, for rendertest's tests to wait
// for what gfx publishes outside a beat.
func (h *Harness) WaitUntil(what string, done func() bool) {
	h.t.Helper()
	h.tickUntil(what, done)
}

// WaitForReceive waits, as WaitUntil does, until a value is sent on channel
// or it is closed.
func (h *Harness) WaitForReceive(what string, channel <-chan struct{}) {
	h.t.Helper()
	h.tickUntil(what, makeReceiveCheck(channel))
}

// EnableBackendTimestamps gives gfx's recording adapter timestamp queries
// without gfx requiring them, for rendertest's tests to turn pass timings on
// where gfx's device lacks them.
func (h *Harness) EnableBackendTimestamps() {
	h.instance.EnableTimestamps()
}

// RunOnMainThread runs run on the kernel's main thread as the harness runs
// its Spawn and ticks, for rendertest's tests to check which goroutine that
// is.
func (h *Harness) RunOnMainThread(run func()) {
	h.runOnMainThread(run)
}
