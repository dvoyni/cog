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

type Plugin struct{}

func New() *Plugin { return &Plugin{} }

func (*Plugin) Name() kernel.PluginName { return Name }

func (*Plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{input.Name, gfx.Name, canvas.Name}
}

func (*Plugin) Register(registrar *kernel.Registrar, _ any) error {
	registrar.InitResource(&Frame{})
	registrar.InitResource(&Interactions{})
	registrar.InitResource(&processor{})
	registrar.Subscribe[UpdateEventHandler](processUpdate).
		After[input.UpdateEventHandler]().
		Before[canvas.UpdateEventHandler]()
	return nil
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
