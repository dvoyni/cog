package input

import "github.com/dvoyni/cog/bundles/input/internal"

// Name is the input plugin's name.
const Name = internal.Name

// AdvanceOnUpdate is the subscription type of the plugin's tick-boundary
// handler on app.UpdateEvent. It is registered First and ages the per-tick
// edges, so a key just pressed last tick is held this one, before any other
// subscriber reads State. Order a subscriber that reads those edges
// After[input.AdvanceOnUpdate].
type AdvanceOnUpdate = internal.AdvanceOnUpdate
