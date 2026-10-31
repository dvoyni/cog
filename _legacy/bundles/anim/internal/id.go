package internal

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// Name is the anim plugin's name.
const Name kernel.PluginName = "anim"

// AdvanceOnUpdate is the subscription type of the plugin's tick handler on
// app.UpdateEvent, which advances every timeline. It runs in the First phase,
// so ordinary-phase handlers see the current tick's values and fired cues; a
// First-phase handler that reads timelines must order itself
// After[anim.AdvanceOnUpdate].
type AdvanceOnUpdate kernel.Subscription[app.UpdateEvent]
