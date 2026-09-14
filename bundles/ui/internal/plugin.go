package internal

import (
	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/input"
	"github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/bundles/ui"
	"github.com/dvoyni/cog/bundles/ui/internal/types"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/gfx"
	"github.com/dvoyni/cog/slots/storage"
)

// plugin registers ui's Frame and Interactions resources, its private layout
// resource, ArmLayoutCmd, the processing subscription and the two
// layout-snapshot subscriptions, and contributes ui's mcp Provider.
type plugin struct {
	// snapshots is ui's one layout-snapshot slot, plugin-owned and
	// self-synchronizing. See snapshotState for why it is not a kernel
	// resource.
	snapshots snapshotState
}

// processor is ui's private resource: the layout engine and the scratch it
// keeps across ticks, so a warmed tick allocates nothing. It is a resource
// rather than plugin state so that the snapshot subscriber's Read is ordered
// against processUpdate's Write.
type processor struct{ types.Processor }

// New creates the ui plugin. ui has no configuration.
func New() kernel.Plugin { return &plugin{} }

// Name reports the plugin name.
func (*plugin) Name() kernel.PluginName { return ui.Name }

// Dependencies reports the plugins ui requires: input for the pointer, gfx for
// the viewport, and canvas, which it records into.
func (*plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{input.Name, gfx.Name, canvas.Name}
}

func (p *plugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.InitResource(&ui.Frame{})
	registrar.InitResource(&ui.Interactions{})
	registrar.InitResource(&processor{})
	p.registerCommands(registrar)
	registrar.Subscribe[ui.ProcessOnUpdate](processUpdate).
		After[input.AdvanceOnUpdate]().
		Before[canvas.FlushOnUpdate]()
	registrar.Subscribe[armLayoutOnUpdate](p.armSnapshotOnUpdate).First()
	registrar.Subscribe[layoutOnUpdate](p.snapshotOnUpdate).
		After[ui.ProcessOnUpdate]()
	registrar.ProvideAdapter[ui.McpProvider](mcp.Provider(provider{}))
	return nil
}

// Stop completes any snapshot the engine walked away from. A request armed in
// a tick the engine never finishes would otherwise leave its waiter learning
// nothing until its client's idle abort; abandonment is delivered rather than
// merely true.
//
// It touches the snapshot slot directly because by Stop the scheduler has
// stopped and grants no locks, which is also why nothing else can be touching
// it: the host loop has returned and every handler is done.
func (p *plugin) Stop(kernel.Executioner) error {
	p.snapshots.abandon()
	return nil
}

// armSnapshotOnUpdate admits a waiting snapshot to the tick that has just
// begun. It declares no resources: the snapshot slot carries its own lock.
func (p *plugin) armSnapshotOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	return nil, func(kernel.Kernel, app.UpdateEvent) error {
		p.snapshots.beginTick()
		return nil
	}
}

// snapshotOnUpdate renders the tick's resolved tree for whoever armed a
// snapshot, from a subscriber ordered after ui's own processing.
//
// That is the only window in which the tree can be read. The Processor's nodes
// keep their geometry until the next flatten, but each node's element points
// into the app's borrowed child storage, which the frame releases at the end
// of processUpdate - so afterwards the id, the visual and userData are
// unreadable while the numbers beside them still look right.
//
// A Read conflicts only with a writer, and processUpdate's write is ordered
// ahead of this by the link above.
func (p *plugin) snapshotOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var processorResource kernel.Read[*processor]
	return func(access kernel.ResourceAccess) {
			processorResource = access.GetRead[*processor]()
		}, func(_ kernel.Kernel, event app.UpdateEvent) error {
			p.snapshots.record(processorResource.Get(), event.Tick)
			return nil
		}
}

// processUpdate is ui.ProcessOnUpdate: it lays out the tick's frame against
// the viewport, resolves the pointer's edges against it, records every visual
// into canvas, publishes the interactions and consumes the frame.
func processUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var frameResource kernel.Write[*ui.Frame]
	var interactionsResource kernel.Write[*ui.Interactions]
	var processorResource kernel.Write[*processor]
	var inputResource kernel.Read[*input.State]
	var viewportResource kernel.Read[*gfx.Viewport]
	var queueResource kernel.Write[*canvas.OpQueue]
	var lookupResource kernel.Write[*canvas.Lookup]
	var filesystem kernel.Read[storage.FileSystem]
	return func(access kernel.ResourceAccess) {
			frameResource = access.GetWrite[*ui.Frame]()
			interactionsResource = access.GetWrite[*ui.Interactions]()
			processorResource = access.GetWrite[*processor]()
			inputResource = access.GetRead[*input.State]()
			viewportResource = access.GetRead[*gfx.Viewport]()
			queueResource = access.GetWrite[*canvas.OpQueue]()
			lookupResource = access.GetWrite[*canvas.Lookup]()
			filesystem = access.GetRead[storage.FileSystem]()
		}, func(k kernel.Kernel, _ app.UpdateEvent) error {
			frame := frameResource.Get()
			interactions := interactionsResource.Get()
			processor := processorResource.Get()
			inputState := inputResource.Get()
			viewport := viewportResource.Get()
			queue := queueResource.Get()
			access := canvas.NewLookupAccess(k, lookupResource.Get(), filesystem.Get())
			defer types.ClearFrame(frame)

			pointer := pointerToViewport(inputState.Pointer(), *viewport)
			var eventBuffer [10]types.PointerEvent
			events := eventBuffer[:0]
			for button, key := range mouseButtons {
				if inputState.JustPressed(key) {
					events = append(events, types.PointerEvent{X: float32(pointer.X), Y: float32(pointer.Y), Button: button, Kind: types.PointerEventDown})
				}
			}
			for button, key := range mouseButtons {
				if inputState.JustReleased(key) {
					events = append(events, types.PointerEvent{X: float32(pointer.X), Y: float32(pointer.Y), Button: button, Kind: types.PointerEventUp})
				}
			}

			processor.Process(access, types.FrameRoots(frame), types.FrameLayers(frame), types.GlobalState{
				Screen: ui.Rect{Width: viewport.Width, Height: viewport.Height},
				Pointer: types.PointerState{
					X:      float32(pointer.X),
					Y:      float32(pointer.Y),
					Events: events,
				},
				Materials: types.FrameMaterials(frame),
			}, queue)

			types.PublishInteractions(&processor.Processor, interactions)
			return nil
		}
}

func pointerToViewport(pointer input.Pos, viewport gfx.Viewport) input.Pos {
	if viewport.WindowWidth <= 0 || viewport.WindowHeight <= 0 {
		return input.Pos{}
	}
	return input.Pos{
		X: pointer.X * float64(viewport.Width/viewport.WindowWidth),
		Y: pointer.Y * float64(viewport.Height/viewport.WindowHeight),
	}
}

var mouseButtons = [...]input.Key{
	input.KeyMouseLeft,
	input.KeyMouseRight,
	input.KeyMouseMiddle,
	input.KeyMouse4,
	input.KeyMouse5,
}
