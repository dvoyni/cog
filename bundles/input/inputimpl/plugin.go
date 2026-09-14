package inputimpl

import (
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/bundles/input/internal"
	"github.com/dvoyni/cog/extensions/mcp"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// plugin registers the input State resource, the Apply, Synthesize and State
// commands, the discrete input events, and the tick-boundary subscription that
// rolls per-tick edges. It holds no state of its own — the State lives in the
// kernel resource.
type plugin struct{}

// New creates the input plugin.
func New() kernel.Plugin { return &plugin{} }

// Name reports the plugin name.
func (p *plugin) Name() kernel.PluginName { return input.Name }

// Dependencies reports the plugins input requires; it has none.
func (p *plugin) Dependencies() []kernel.PluginName { return nil }

// Register registers the input contract with the kernel.
func (p *plugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.InitResource(internal.NewState())
	registrar.HandleCommand[input.ApplyCmd](applyCmdImpl)
	registrar.HandleCommand[input.SynthesizeCmd](synthesizeCmdImpl)
	registrar.HandleCommand[input.StateCmd](stateCmdImpl)
	registrar.Subscribe[input.AdvanceOnUpdate](advanceOnUpdate).First()
	registrar.ProvideAdapter[input.McpProvider](mcp.Provider(provider{}))
	return nil
}

// advanceOnUpdate rolls the per-tick edges at the start of each simulation
// tick: what was just pressed last tick is only held now. It runs before all
// other app.UpdateEvent subscribers.
func advanceOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var state kernel.Write[*input.State]
	return func(access kernel.ResourceAccess) {
			state = access.GetWrite[*input.State]()
		}, func(kernel.Kernel, app.UpdateEvent) error {
			internal.StateAdvance(state.Get())
			return nil
		}
}

// publish asynchronously publishes the discrete input event for one change.
func publish(k kernel.Kernel, c input.Change) {
	switch internal.ChangeKindOf(&c) {
	case internal.ChangeKindKey:
		k.PublishEvent(input.KeyEvent{Key: internal.ChangeKey(&c), Mods: internal.ChangeMods(&c), Down: internal.ChangeDown(&c)})
	case internal.ChangeKindPointer:
		k.PublishEvent(input.PointerEvent{Pos: internal.ChangePos(&c)})
	case internal.ChangeKindScroll:
		k.PublishEvent(input.ScrollEvent{Dx: internal.ChangeDx(&c), Dy: internal.ChangeDy(&c)})
	case internal.ChangeKindText:
		k.PublishEvent(input.TextEvent{Rune: internal.ChangeRune(&c)})
	}
}
