package ecsscene

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// Name is the binding plugin's kernel name, the owner of every Component Store
// it registers, and the name a plugin whose Systems read those Components
// declares a dependency on.
const Name kernel.PluginName = "ecsscene"

// RecordOnUpdate is the subscription type of the binding's recording System on
// app.UpdateEvent: it copies every matching Entity into scene's op queue. A
// game System that moves Transforms orders itself Before it.
//
// It declares no ordering of its own: scene.FlushOnUpdate is subscribed Last,
// so anything that does not ask to be last already runs before it.
type RecordOnUpdate kernel.Subscription[app.UpdateEvent]

// LoadOnUpdate is the subscription type of the binding's load System on
// app.UpdateEvent. It runs on the Model, Mesh, Material and Params Components
// that changed since its last run, resolves each changed Entity's model and
// keys its draws, and drives model's bake and release queues. It is ordered
// Before RecordOnUpdate. A game System that spawns drawables or writes those
// Components orders itself Before it, so the change is keyed in the same tick.
type LoadOnUpdate kernel.Subscription[app.UpdateEvent]
