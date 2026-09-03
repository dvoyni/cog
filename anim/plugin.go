package anim

import (
	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

// Name is the anim plugin's name.
const Name kernel.PluginName = "anim"

// UpdateEventHandler identifies the tick subscription that advances every
// timeline. It runs in the First phase, so ordinary-phase handlers see the
// current tick's values and fired cues; a First-phase handler that reads
// timelines must order itself After it.
type UpdateEventHandler kernel.Subscription[app.UpdateEvent]

// Plugin registers the Timelines resource and the tick subscription. It holds
// no state of its own.
type Plugin struct{}

// New creates the anim plugin.
func New() *Plugin { return &Plugin{} }

// Name reports the plugin name.
func (p *Plugin) Name() kernel.PluginName { return Name }

// Dependencies reports the plugins anim requires; it has none.
func (p *Plugin) Dependencies() []kernel.PluginName { return nil }

// Register registers the Timelines resource and the per-tick advance.
func (p *Plugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.InitResource(newTimelines())
	registrar.Subscribe[UpdateEventHandler](handleUpdateEvent).First()
	return nil
}

// handleUpdateEvent advances every timeline by the fixed step before gameplay
// runs, promoting due cues into the fired view and dropping finished tracks.
func handleUpdateEvent() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var timelines kernel.Write[*Timelines]
	return func(access kernel.ResourceAccess) {
			timelines = access.GetWrite[*Timelines]()
		}, func(_ kernel.Kernel, event app.UpdateEvent) error {
			timelines.Get().advance(float32(event.Dt))
			return nil
		}
}
