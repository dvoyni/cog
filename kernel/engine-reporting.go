package kernel

import (
	"errors"
	"log"
)

// reportError sends err to the centralized error handler. Handler calls are
// serialized, and the first error the handler terminates on is the one Run
// answers with.
func (e *Engine) reportError(err error) {
	if err == nil {
		return
	}
	e.errorMu.Lock()
	defer e.errorMu.Unlock()
	e.reportLocked(err)
}

// reportLocked is one report with errorMu already held, so that a caller which
// has more to do under that lock - deduping a key, firing the rest of a burst -
// does it without releasing and retaking it.
//
// Every report reaches the handler, including one arriving after the engine is
// already going down: silence there would hide whatever the first failure
// knocked over. Only the first non-nil verdict becomes the cause.
func (e *Engine) reportLocked(err error) {
	verdict := e.errorHandler(err)
	if verdict == nil {
		return
	}
	if e.cause == nil {
		e.cause = verdict
	}
	e.markQuit()
}

// reportErrorOnce fires a burst of reports the first time key is seen and drops
// it every time after.
//
// The burst is claimed and fired under one hold of errorMu, so two threads
// noticing the same condition in the same instant report it once rather than
// racing between the check and the reports.
//
// An empty burst claims nothing: a load that gathered no faults must leave the
// key free for the one that does.
func (e *Engine) reportErrorOnce(key any, errs []error) {
	if len(errs) == 0 {
		return
	}
	e.errorMu.Lock()
	defer e.errorMu.Unlock()
	if _, done := e.reported[key]; done {
		return
	}
	if e.reported == nil {
		e.reported = map[any]struct{}{}
	}
	e.reported[key] = struct{}{}
	for _, err := range errs {
		if err != nil {
			e.reportLocked(err)
		}
	}
}

// forgetReportedError drops one key, so the next report under it speaks again.
func (e *Engine) forgetReportedError(key any) {
	e.errorMu.Lock()
	defer e.errorMu.Unlock()
	delete(e.reported, key)
}

// forgetReportedErrors drops every key match accepts. It scans the whole table,
// which is why the kernel offers it only for the family case a single key
// cannot express.
func (e *Engine) forgetReportedErrors(match func(any) bool) {
	e.errorMu.Lock()
	defer e.errorMu.Unlock()
	for key := range e.reported {
		if match(key) {
			delete(e.reported, key)
		}
	}
}

// shutdownAside drops ErrSchedulerStopped and passes everything else through.
// A dispatch that arrives after the coordinator has gone is the engine ending,
// not a failure in the thing that dispatched: Quit is an ordinary end, and a
// report would turn every in-flight call at shutdown into a fault the handler
// has to recognise and forgive.
func shutdownAside(err error) error {
	var stopped ErrSchedulerStopped
	if errors.As(err, &stopped) {
		return nil
	}
	return err
}

// defaultErrorHandler is the engine's opinion of last resort, and it is only an
// opinion: a game that installs its own replaces it whole.
//
// It says every error out loud and terminates on a plugin panic alone. A panic
// means a handler stopped in the middle of what it was doing, so whatever it
// was mutating is in a state nobody described. Anything else - a missing
// texture, a model that would not parse - is a thing a game is expected to
// survive, and terminating on it would make the frame rate a liability.
func defaultErrorHandler(err error) error {
	log.Printf("kernel: %v", err)
	var panicErr ErrPluginPanic
	if errors.As(err, &panicErr) {
		return err
	}
	return nil
}
