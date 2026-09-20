package sound

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// Name is the sound plugin name and configuration key.
const Name kernel.PluginName = "sound"

// FlushOnUpdate is the subscription type of the plugin's once-per-tick flush on
// app.UpdateEvent. It runs last, so every recorder has finished with the queue
// by the time it drains it, and it is exported so a recording System - ecsaudio's,
// or a game's own - can order itself Before it.
type FlushOnUpdate kernel.Subscription[app.UpdateEvent]
