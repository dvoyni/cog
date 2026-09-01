package input

import (
	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"
)

// Name is the input plugin's name.
const Name kernel.PluginName = "input"

// Plugin registers the input State resource, the Apply command, the discrete
// input events, and the tick-boundary subscription that rolls per-tick edges. It
// holds no state of its own — the State lives in the kernel resource.
type Plugin struct{}

// New creates the input plugin.
func New() *Plugin { return &Plugin{} }

// Name reports the plugin name.
func (p *Plugin) Name() kernel.PluginName { return Name }

// Dependencies reports the plugins input requires; it has none.
func (p *Plugin) Dependencies() []kernel.PluginName { return nil }

// Register registers the input contract with the kernel.
func (p *Plugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.InitResource(newState())
	registrar.HandleCommand[ApplyCmd](applyChanges)
	registrar.Subscribe[UpdateEventHandler](handleUpdateEvent).First()
	return nil
}

// applyChanges folds a batch of input changes into the State (under its write
// lock) and publishes the discrete event for each change.
func applyChanges() (kernel.Lock, kernel.Execute[ApplyRequest, ApplyResponse]) {
	var state kernel.Write[*State]
	return func(access kernel.ResourceAccess) {
			state = access.GetWrite[*State]()
		}, func(k kernel.Kernel, request ApplyRequest) (ApplyResponse, error) {
			s := state.Get()
			for _, c := range request.Changes {
				s.apply(c)
				publish(k, c)
			}
			return ApplyResponse{}, nil
		}
}

// handleUpdateEvent rolls the per-tick edges at the start of each simulation tick.
// It runs before all other app.UpdateEvent subscribers.
func handleUpdateEvent() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var state kernel.Write[*State]
	return func(access kernel.ResourceAccess) {
			state = access.GetWrite[*State]()
		}, func(kernel.Kernel, app.UpdateEvent) error {
			state.Get().advance()
			return nil
		}
}

// publish asynchronously publishes the discrete input event for one change.
func publish(k kernel.Kernel, c Change) {
	switch c.kind {
	case changeKey:
		k.PublishEvent(KeyEvent{Key: c.key, Mods: c.mods, Down: c.down})
	case changePointer:
		k.PublishEvent(PointerEvent{Pos: c.pos})
	case changeScroll:
		k.PublishEvent(ScrollEvent{Dx: c.dx, Dy: c.dy})
	case changeText:
		k.PublishEvent(TextEvent{Rune: c.r})
	}
}
