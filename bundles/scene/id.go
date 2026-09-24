package scene

import "github.com/dvoyni/cog/bundles/scene/internal"

// Name is the scene plugin's name.
const Name = internal.Name

// FlushOnUpdate is the subscription type of the plugin's flush on
// app.UpdateEvent. It is registered Last and ordered
// Before[gfx.PresentOnUpdate]: it culls, sorts and packs the tick's recording
// into gfx passes and draws and republishes the queue, so everything that
// records into *OpQueue during the tick orders before it.
type FlushOnUpdate = internal.FlushOnUpdate
