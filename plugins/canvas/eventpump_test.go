package canvas_test

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// eventPump is a test plugin pumping platform's events in each frame, as an
// application's beats do: a window on Wayland gets a framebuffer only once a
// pump has read the compositor's frame callback for its last present, and
// rendertest's harness pumps only to open a window.
type eventPump struct {
	testPlugin
}

func (eventPump) Name() kernel.PluginName { return "event pump" }

func (eventPump) Init(configurator *kernel.Configurator, _ any) error {
	tasks, _ := configurator.Resource[platform.Tasks]()
	processEvents := tasks.ProcessEvents
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(processEvents, kernel.NoArg{}, event.Ready).Handle())
	})
	return nil
}
