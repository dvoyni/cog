package ui

import "github.com/dvoyni/cog/bundles/ui/internal"

// Name is the ui plugin's name.
const Name = internal.Name

// ProcessOnUpdate is the subscription type of the plugin's per-tick processing
// on app.UpdateEvent. It lays out the tick's Frame, resolves interactions
// against the pointer into Interactions, records every visual into
// canvas.OpQueue and clears the Frame. It is ordered
// After[input.AdvanceOnUpdate] and Before[canvas.FlushOnUpdate], so a producer
// declaring the frame orders Before it, and a reader of this tick's
// Interactions orders After it.
type ProcessOnUpdate = internal.ProcessOnUpdate
