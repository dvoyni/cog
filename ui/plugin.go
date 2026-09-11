package ui

import (
	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/input"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/storage"
)

const Name kernel.PluginName = "ui"

// UpdateEventHandler identifies the UI plugin's per-tick processing subscription.
type UpdateEventHandler kernel.Subscription[app.UpdateEvent]

type Plugin struct {
	// snapshots is ui's one layout-snapshot slot, plugin-owned and
	// self-synchronizing. See snapshotState for why it is not a kernel
	// resource.
	snapshots snapshotState
}

func New() *Plugin { return &Plugin{} }

func (*Plugin) Name() kernel.PluginName { return Name }

func (*Plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{input.Name, gfx.Name, canvas.Name}
}

func (p *Plugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.InitResource(&Frame{})
	registrar.InitResource(&Interactions{})
	registrar.InitResource(&processor{})
	p.registerCommands(registrar)
	registrar.Subscribe[UpdateEventHandler](processUpdate).
		After[input.UpdateEventHandler]().
		Before[canvas.UpdateEventHandler]()
	registrar.Subscribe[SnapshotArmUpdateEventHandler](p.armSnapshotOnUpdate).First()
	registrar.Subscribe[SnapshotUpdateEventHandler](p.snapshotOnUpdate).
		After[UpdateEventHandler]()
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
func (p *Plugin) Stop(kernel.Executioner) error {
	p.snapshots.abandon()
	return nil
}

// armSnapshotOnUpdate admits a waiting snapshot to the tick that has just
// begun. It declares no resources: the snapshot slot carries its own lock.
func (p *Plugin) armSnapshotOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	return nil, func(kernel.Kernel, app.UpdateEvent) error {
		p.snapshots.beginTick()
		return nil
	}
}

// snapshotOnUpdate renders the tick's resolved tree for whoever armed a
// snapshot, from a subscriber ordered after ui's own processing.
//
// That is the only window in which the tree can be read. processor.nodes
// keeps its geometry until the next flatten, but layoutNode.element points
// into the app's borrowed child storage, which Frame.clear releases at the end
// of processUpdate - so afterwards the id, the visual and userData are
// unreadable while the numbers beside them still look right.
//
// A Read conflicts only with a writer, and processUpdate's write is ordered
// ahead of this by the link above.
func (p *Plugin) snapshotOnUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var processorResource kernel.Read[*processor]
	return func(access kernel.ResourceAccess) {
			processorResource = access.GetRead[*processor]()
		}, func(_ kernel.Kernel, event app.UpdateEvent) error {
			p.snapshots.record(processorResource.Get(), event.Tick)
			return nil
		}
}

func processUpdate() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	var frameResource kernel.Write[*Frame]
	var interactionsResource kernel.Write[*Interactions]
	var processorResource kernel.Write[*processor]
	var inputResource kernel.Read[*input.State]
	var viewportResource kernel.Read[*app.Viewport]
	var queueResource kernel.Write[*canvas.OpQueue]
	var lookupResource kernel.Write[*canvas.Lookup]
	var filesystem kernel.Read[storage.FileSystem]
	return func(access kernel.ResourceAccess) {
			frameResource = access.GetWrite[*Frame]()
			interactionsResource = access.GetWrite[*Interactions]()
			processorResource = access.GetWrite[*processor]()
			inputResource = access.GetRead[*input.State]()
			viewportResource = access.GetRead[*app.Viewport]()
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
			defer frame.clear()

			pointer := pointerToViewport(inputState.Pointer(), *viewport)
			var eventBuffer [10]pointerEvent
			events := eventBuffer[:0]
			for button, key := range mouseButtons {
				if inputState.JustPressed(key) {
					events = append(events, pointerEvent{X: float32(pointer.X), Y: float32(pointer.Y), Button: button, Kind: pointerEventDown})
				}
			}
			for button, key := range mouseButtons {
				if inputState.JustReleased(key) {
					events = append(events, pointerEvent{X: float32(pointer.X), Y: float32(pointer.Y), Button: button, Kind: pointerEventUp})
				}
			}

			processor.process(access, frame.roots, frame.layers, globalState{
				Screen: Rect{Width: viewport.Width, Height: viewport.Height},
				Pointer: pointerState{
					X:      float32(pointer.X),
					Y:      float32(pointer.Y),
					Events: events,
				},
				Materials: frame.materials,
			}, queue)

			interactions.values, processor.interactions = processor.interactions, interactions.values
			return nil
		}
}

func pointerToViewport(pointer input.Pos, viewport app.Viewport) input.Pos {
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
