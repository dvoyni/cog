package wgpu

import (
	"fmt"
	"time"

	"github.com/dvoyni/cog/app"
)

// ErrInvalidConfig is returned by the plugin's Register when the config value
// handed to it is neither nil nor a wgpu.Config.
type ErrInvalidConfig struct {
	Got any
}

func (e ErrInvalidConfig) Error() string {
	return fmt.Sprintf("wgpu: invalid config: want %T, got %T", Config{}, e.Got)
}

// ErrUnknownTimeAction is returned by the app.TimeCmd handler when the action
// is not one this driver knows. A caller's mistyped action is a system fault
// rather than an expected outcome: the contract is a Go enum, so there is no
// wire to have mangled it.
type ErrUnknownTimeAction struct {
	Action app.TimeAction
}

func (e ErrUnknownTimeAction) Error() string {
	return fmt.Sprintf("wgpu: unknown time action %d", e.Action)
}

// ErrHoldTooLong is returned by the app.TimeCmd handler when a hold asks to
// keep the step window open for longer than the driver will honour. It is an
// expected outcome rather than a fault — the agent-facing surface maps it to
// words — and the hold does not begin, because a caller quietly given ten
// seconds of a minute it asked for would meet the difference as a split.
type ErrHoldTooLong struct {
	For time.Duration
	Max time.Duration
}

func (e ErrHoldTooLong) Error() string {
	return fmt.Sprintf("wgpu: hold of %s exceeds the maximum of %s", e.For, e.Max)
}
