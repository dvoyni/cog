package internal

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// Name is the scene plugin's name.
const Name kernel.PluginName = "scene"

// FlushOnUpdate is the subscription type of the plugin's flush on
// app.UpdateEvent. It is registered Last and ordered
// Before[gfx.PresentOnUpdate]: it culls, sorts and packs the tick's recording
// into gfx passes and draws and republishes the queue, so everything that
// records into *OpQueue during the tick orders before it.
type FlushOnUpdate kernel.Subscription[app.UpdateEvent]
