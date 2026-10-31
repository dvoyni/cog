package canvas

import "github.com/dvoyni/cog/bundles/canvas/internal"

// Name is the canvas plugin's name.
const Name = internal.Name

// FlushOnUpdate is the subscription type of the plugin's flush on
// app.UpdateEvent. It is registered Last and ordered
// Before[gfx.PresentOnUpdate]: it translates the tick's recorded operations
// into gfx draws and resets the queue, so everything that records into
// *OpQueue during the tick orders before it.
type FlushOnUpdate = internal.FlushOnUpdate
