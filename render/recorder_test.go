package render_test

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/render"
)

// recorder is the test's plugin, listed after render. On each FrameEvent it
// schedules a task for each of onFrame, after Ready and joined into the out
// dependency, that records with render's Frame. On each
// platform.WindowOpenedEvent, while no frame is recording, it runs
// onWindowOpened in a task the same way, and closes windowRecorded once it
// has.
type recorder struct {
	onFrame        []func(frame render.Frame)
	onWindowOpened func(frame render.Frame)
	windowRecorded chan struct{}
}

func (*recorder) Name() kernel.PluginName { return "recorder" }

func (*recorder) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{render.PluginName}
}

func (r *recorder) Init(configurator *kernel.Configurator, _ any) error {
	var recordTasks []*kernel.Task[render.FrameEvent, kernel.NoResult]
	for _, record := range r.onFrame {
		recordTasks = append(recordTasks, configurator.RegisterTask1(func(k *kernel.Kernel, event render.FrameEvent, frame render.Frame) kernel.NoResult {
			record(frame)
			return kernel.NoResult{}
		}))
	}
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		out := in
		for _, task := range recordTasks {
			out = ex.Join(out, ex.Schedule(task, event, event.Ready).Handle())
		}
		return event, out
	})
	if r.onWindowOpened != nil {
		r.windowRecorded = make(chan struct{})
		recordWindow := configurator.RegisterTask1(func(k *kernel.Kernel, _ platform.WindowOpenedEvent, frame render.Frame) kernel.NoResult {
			r.onWindowOpened(frame)
			close(r.windowRecorded)
			return kernel.NoResult{}
		})
		configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event platform.WindowOpenedEvent) (platform.WindowOpenedEvent, kernel.JobHandle) {
			return event, ex.Join(in, ex.Schedule(recordWindow, event, in).Handle())
		})
	}
	return nil
}

func (*recorder) Start(*kernel.Kernel) error { return nil }

func (*recorder) Stop(*kernel.Kernel) error { return nil }

func (*recorder) Destroy() error { return nil }
