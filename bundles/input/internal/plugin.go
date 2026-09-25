package internal

import (
	"github.com/dvoyni/cog/bundles/mcp"
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
func (p *plugin) Name() kernel.PluginName { return Name }

// Dependencies reports the plugins input requires; it has none.
func (p *plugin) Dependencies() []kernel.PluginName { return nil }

// Register registers the input contract with the kernel.
func (p *plugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.InitResource(NewState())
	registrar.HandleCommand[ApplyCmd](applyCmdImpl)
	registrar.HandleCommand[SynthesizeCmd](synthesizeCmdImpl)
	registrar.HandleCommand[StateCmd](stateCmdImpl)
	registrar.Subscribe[AdvanceOnUpdate](advanceOnUpdate).First()
	registrar.ProvideAdapter[McpProvider](mcp.Provider(provider{}))
	return nil
}

// advanceOnUpdate rolls the per-tick edges at the start of each simulation
// tick: what was just pressed last tick is only held now. It runs before all
// other app.UpdateEvent subscribers.
func advanceOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var state kernel.Write[*State]
	return func(access kernel.ResourceAccess) {
			state = access.GetWrite[*State]()
		}, func(kernel.Kernel, app.UpdateEvent) {
			StateAdvance(state.Get())
		}
}

// publish asynchronously publishes the discrete input event for one change.
func publish(k kernel.Kernel, c Change) {
	switch ChangeKindOf(&c) {
	case ChangeKindKey:
		k.PublishEvent(KeyEvent{Key: ChangeKey(&c), Mods: ChangeMods(&c), Down: ChangeDown(&c)})
	case ChangeKindPointer:
		k.PublishEvent(PointerEvent{Pos: ChangePos(&c)})
	case ChangeKindScroll:
		k.PublishEvent(ScrollEvent{Dx: ChangeDx(&c), Dy: ChangeDy(&c)})
	case ChangeKindText:
		k.PublishEvent(TextEvent{Rune: ChangeRune(&c)})
	case ChangeKindClipboardPaste:
		k.PublishEvent(ClipboardPasteEvent{Text: ChangeText(&c)})
	}
}
