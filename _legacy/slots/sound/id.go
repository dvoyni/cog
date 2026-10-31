package sound

import "github.com/dvoyni/cog/slots/sound/internal"

// Name is the sound plugin name and configuration key.
const Name = internal.Name

// FlushOnUpdate is the subscription type of the plugin's once-per-tick flush on
// app.UpdateEvent. It runs last, so every recorder has finished with the queue
// by the time it drains it, and it is exported so a recording System - ecsaudio's,
// or a game's own - can order itself Before it.
type FlushOnUpdate = internal.FlushOnUpdate

// SuspendOnPauseChange is the subscription type of the plugin's response to an
// engine Pause: every live Voice is suspended, and resumed on the same sample.
//
// It is its own subscription rather than part of the flush because pause stops
// app.UpdateEvent, and an Adapter left holding the last batch it was given
// keeps mixing it. It is exported for the reason FlushOnUpdate is: a System
// that must be sure it has stopped recording before sound suspends can order
// itself Before it.
type SuspendOnPauseChange = internal.SuspendOnPauseChange
