package canvastest

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/canvas"
)

// recordPlugin is the harness's own plugin, listed right after canvas: its
// recording task, hung off canvas.FrameEvent after Ready, calls the records
// Harness.Record queued for the frame with its draw list.
type recordPlugin struct {
	harness *Harness
}

// Name answers the harness plugin's name.
func (recordPlugin) Name() kernel.PluginName { return "canvastest" }

// Dependencies names canvas.
func (recordPlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.PluginName}
}

// Init registers the recording task and subscribes to canvas.FrameEvent, to
// schedule it after Ready, joined into the out dependency. The task's body
// stays a closure, since it hands the test's records the draw list.
func (p recordPlugin) Init(configurator *kernel.Configurator, _ any) error {
	record := configurator.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, list canvas.DrawList) kernel.NoResult {
		for _, queued := range p.harness.takeRecords() {
			queued(list)
		}
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event canvas.FrameEvent) (canvas.FrameEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(record, kernel.NoArg{}, event.Ready).Handle())
	})
	return nil
}

// Start does nothing.
func (recordPlugin) Start(*kernel.Kernel) error { return nil }

// Stop does nothing.
func (recordPlugin) Stop(*kernel.Kernel) error { return nil }

// Destroy does nothing.
func (recordPlugin) Destroy() error { return nil }
