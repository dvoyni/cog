package input

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// Name is the input plugin's name.
const Name kernel.PluginName = "input"

// AdvanceOnUpdate is the subscription type of the plugin's tick-boundary
// handler on app.UpdateEvent. It is registered First and ages the per-tick
// edges, so a key just pressed last tick is held this one, before any other
// subscriber reads State. Order a subscriber that reads those edges
// After[input.AdvanceOnUpdate].
type AdvanceOnUpdate kernel.Subscription[app.UpdateEvent]
