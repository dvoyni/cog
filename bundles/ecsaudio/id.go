package ecsaudio

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// Name is the binding plugin's kernel name, the owner of every Component Store
// it registers, and the name a plugin whose Systems read those Components
// declares a dependency on.
const Name kernel.PluginName = "ecsaudio"

// RecordOnUpdate is the subscription type of the binding's recording System on
// app.UpdateEvent: it reconciles every Emitter against the Voice it already has
// and writes the Listener, into sound's queue.
//
// It declares no ordering of its own: sound.FlushOnUpdate is subscribed Last, so
// anything that does not ask to be last already runs before it. A game System
// that moves an Entity carrying a Transform orders itself Before it.
type RecordOnUpdate kernel.Subscription[app.UpdateEvent]
