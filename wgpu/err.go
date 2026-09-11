package wgpu

import (
	"fmt"

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
