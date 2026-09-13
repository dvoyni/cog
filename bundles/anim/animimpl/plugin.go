package animimpl

import (
	"github.com/dvoyni/cog/bundles/anim"
	"github.com/dvoyni/cog/bundles/anim/internal"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// plugin registers the Timelines resource and the tick subscription. It holds
// no state of its own — the timelines live in the kernel resource.
type plugin struct{}

// New creates the anim plugin.
func New() kernel.Plugin { return &plugin{} }

// Name reports the plugin name.
func (p *plugin) Name() kernel.PluginName { return anim.Name }

// Dependencies reports the plugins anim requires; it has none.
func (p *plugin) Dependencies() []kernel.PluginName { return nil }

// Register registers the Timelines resource and the per-tick advance.
func (p *plugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.InitResource(internal.NewTimelines())
	registrar.Subscribe[anim.AdvanceOnUpdate](advanceOnUpdate).First()
	return nil
}

// advanceOnUpdate advances every timeline by the fixed step before gameplay
// runs, promoting due cues into the fired view and dropping finished tracks.
func advanceOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var timelines kernel.Write[*anim.Timelines]
	return func(access kernel.ResourceAccess) {
			timelines = access.GetWrite[*anim.Timelines]()
		}, func(_ kernel.Kernel, event app.UpdateEvent) error {
			internal.TimelinesAdvance(timelines.Get(), float32(event.Dt))
			return nil
		}
}
