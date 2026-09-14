package internal

import (
	"time"

	"github.com/dvoyni/cog/slots/app"
)

// withDefaults fills every zero field of config with the value the loop runs
// with when it is given none: a 1/60s step, a 250ms frame clamp and four
// pending steps.
func withDefaults(config app.Config) app.Config {
	if config.Step == 0 {
		config.Step = time.Second / 60
	}
	if config.MaxFrame == 0 {
		config.MaxFrame = 250 * time.Millisecond
	}
	if config.MaxPending == 0 {
		config.MaxPending = 4
	}
	return config
}
