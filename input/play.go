package input

import (
	"fmt"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/mcp"
)

// The caps, and why a sequence has them at all.
//
// If a sequence outlives its caller's deadline, the dispatch after the wait
// fails and the key stays down forever. It cannot be cleaned up: the caller
// holds only a request-bound executioner, so once that context is dead there is
// nothing to dispatch a release with. Unwinding is not implementable without
// changing the contract, so the window in which a sequence can be orphaned
// mid-hold is bounded instead.
const (
	// maxDuration is the total wall clock a sequence may ask for. Ten seconds
	// sits far enough under a broker's own timeout that the deadline can only
	// be reached by the caller hanging up, and far above any plausible hold.
	maxDuration = 10 * time.Second
	// maxSteps is a sanity bound on a malformed request rather than a
	// performance one.
	maxSteps = 256
)

// Play validates, splits and dispatches a synthetic input sequence, waiting
// between batches. It holds no locks; each batch is one SynthesizeCmd.
//
// The split cannot happen inside the command: a handler that slept would sleep
// under the State write lock, stalling every tick for the delay's duration. So
// splitting, validation and waiting live here, and an agent-facing capability
// over this is an adapter and nothing more.
//
// A sequence has exactly two outcomes — refused with nothing applied, or
// applied whole — which is why the response carries no count of steps applied.
// Every check runs before the first batch is dispatched. The one exception is
// stated rather than papered over: an engine shutting down mid-sequence fails
// the next dispatch, which can leave a key down in a game that is exiting.
func Play(k kernel.Executioner, actions []Action) (StateResponse, error) {
	if err := check(actions); err != nil {
		return StateResponse{}, err
	}
	var seam StateResponse
	for i, one := range plan(actions) {
		if i > 0 {
			if err := wait(k, one.delay); err != nil {
				return StateResponse{}, err
			}
		}
		applied, err := k.ExecuteCommand[SynthesizeCmd](SynthesizeRequest{Actions: one.actions})
		if err != nil {
			return StateResponse{}, err
		}
		seam = applied
	}
	return seam, nil
}

// batch is one SynthesizeCmd dispatch and the wait that precedes it. The first
// batch's delay is always zero.
type batch struct {
	delay   time.Duration
	actions []Action
}

// plan applies the one rule that produces every idiom: consecutive steps with
// no delay between them fold into one dispatch — one lock hold, one tick's
// worth of edges — and a delay starts another, with the wait between them.
//
// A sequence that ends in a delay still gets its trailing batch, so the wait is
// honoured and the response reports the seam as of after it. A sequence with no
// steps at all is one empty batch, which is how Play answers with the current
// state rather than with nothing.
func plan(actions []Action) []batch {
	batches := []batch{{}}
	for _, action := range actions {
		if action.Do == ActionDelay {
			batches = append(batches, batch{delay: time.Duration(action.Ms) * time.Millisecond})
			continue
		}
		last := &batches[len(batches)-1]
		last.actions = append(last.actions, action)
	}
	return batches
}

// wait sleeps between two batches, outside every lock. It selects on the
// caller's context rather than sleeping flat, which is what makes a broker's
// "the timeout bounds the wait" true rather than aspirational: a caller that
// hangs up stops the sequence at the next delay instead of running it out.
//
// The delay is wall clock, not ticks. Ticks would be exact and are not
// reachable from a plain function that holds no locks and subscribes to
// nothing, and milliseconds are what someone tuning a hold thinks in. The cost
// is stated rather than discovered: a delay shorter than a frame may not
// separate ticks, and while the engine is paused no delay separates anything at
// all, because the per-tick edges roll on a tick and nothing else.
func wait(k kernel.Executioner, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-k.Context().Done():
		return k.Context().Err()
	}
}

// check runs every refusal before the first batch, so a sequence is refused
// with nothing applied. A mid-sequence rejection would leave earlier steps
// applied, which is the stuck key the caps exist to bound, reached by an
// ordinary typo instead of by a disconnect.
//
// Releasing a key that is not down is deliberately not among these: the state
// already treats it as a silent no-op, and refusing it would be that same
// half-applied sequence.
func check(actions []Action) error {
	if len(actions) > maxSteps {
		return mcp.Unavailable{Reason: fmt.Sprintf(
			"the sequence has %d steps; send up to %d and call again for the rest",
			len(actions), maxSteps)}
	}
	var total time.Duration
	for i, action := range actions {
		if !actionKinds[action.Do] {
			return mcp.Unavailable{Reason: fmt.Sprintf(
				"step %d asks for %q, which is not a step — use key_down, key_up, move, move_by, "+
					"scroll, text or delay", i, action.Do)}
		}
		if action.Do != ActionDelay {
			continue
		}
		if action.Ms < 0 {
			return mcp.Unavailable{Reason: fmt.Sprintf(
				"step %d delays for %d ms; a delay cannot run backwards", i, action.Ms)}
		}
		total += time.Duration(action.Ms) * time.Millisecond
	}
	if total > maxDuration {
		return mcp.Unavailable{Reason: fmt.Sprintf(
			"the sequence delays for %s in total; one sequence may span up to %s — split it, "+
				"and remember a key you pressed stays down between calls", total, maxDuration)}
	}
	return nil
}
