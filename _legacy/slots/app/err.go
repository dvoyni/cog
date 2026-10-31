package app

import "github.com/dvoyni/cog/slots/app/internal"

// ErrInvalidConfig is returned by the plugin's Register when the config value
// handed to it is neither nil nor an app.Config.
type ErrInvalidConfig = internal.ErrInvalidConfig

// ErrUnknownTimeAction is returned by the TimeCmd handler when the action is
// not one the tick source knows. A caller's mistyped action is a system fault
// rather than an expected outcome: the contract is a Go enum, so there is no
// wire to have mangled it.
type ErrUnknownTimeAction = internal.ErrUnknownTimeAction

// ErrHoldTooLong is returned by the TimeCmd handler when a hold asks to keep
// the step window open for longer than the tick source will honour. It is an
// expected outcome rather than a fault — the agent-facing surface maps it to
// words — and the hold does not begin, because a caller quietly given ten
// seconds of a minute it asked for would meet the difference as a split.
type ErrHoldTooLong = internal.ErrHoldTooLong

// ErrStepNotPublished is returned by the TimeCmd handler when a TimeStep's Wait
// expires before the ticks it raised were published. It is an expected outcome
// rather than a fault: a paused engine that nothing renders has no frame to
// publish them in, and the agent-facing surface maps it to words. The ticks
// stay raised and are published by the next frame.
type ErrStepNotPublished = internal.ErrStepNotPublished
