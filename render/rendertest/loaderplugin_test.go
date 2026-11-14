package rendertest_test

import (
	"io/fs"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
	"github.com/dvoyni/cog/render"
)

// loaderPlugin is the test's plugin, listed after render: it binds files as a
// read mount, and on each beat runs duringFrame in each of
// recordingTaskCount recording tasks, zero meaning one, each of which then
// begins and ends an empty command list so the frame is submitted, and
// afterFrame in a task after the out of render's handler of gfx.RenderEvent,
// once the frame has ended. Each is given the index of the frame, counted
// from zero, and skipped when nil.
type loaderPlugin struct {
	files              fs.FS
	recordingTaskCount int
	duringFrame        func(frameIndex int, inventory render.Inventory, frame render.Frame)
	afterFrame         func(frameIndex int, inventory render.Inventory)
	// recordedCount is how many frames the handler of render.FrameEvent has
	// scheduled duringFrame for, and endedCount how many the handler of
	// gfx.RenderEvent has scheduled afterFrame for.
	recordedCount, endedCount int
}

func (*loaderPlugin) Name() kernel.PluginName { return "loader plugin" }

func (*loaderPlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{render.PluginName}
}

func (p *loaderPlugin) Init(configurator *kernel.Configurator, _ any) error {
	configurator.BindSocket(platform.ReadMountSocket{FS: p.files})
	var recordTasks []*kernel.Task[int, kernel.NoResult]
	for index := range max(p.recordingTaskCount, 1) {
		recordTasks = append(recordTasks, configurator.RegisterTask2(func(k *kernel.Kernel, frameIndex int, inventory render.Inventory, frame render.Frame) kernel.NoResult {
			if p.duringFrame != nil {
				p.duringFrame(frameIndex, inventory, frame)
			}
			list := frame.BeginCommandList("loader", render.Order(index+1))
			list.End()
			return kernel.NoResult{}
		}))
	}
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		out := in
		for _, task := range recordTasks {
			out = ex.Join(out, ex.Schedule(task, p.recordedCount, event.Ready).Handle())
		}
		p.recordedCount++
		return event, out
	})
	if p.afterFrame != nil {
		end := configurator.RegisterTask1(func(k *kernel.Kernel, frameIndex int, inventory render.Inventory) kernel.NoResult {
			p.afterFrame(frameIndex, inventory)
			return kernel.NoResult{}
		})
		configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.RenderEvent) (gfx.RenderEvent, kernel.JobHandle) {
			frameIndex := p.endedCount
			p.endedCount++
			return event, ex.Schedule(end, frameIndex, in).Handle()
		})
	}
	return nil
}

func (*loaderPlugin) Start(*kernel.Kernel) error { return nil }

func (*loaderPlugin) Stop(*kernel.Kernel) error { return nil }

func (*loaderPlugin) Destroy() error { return nil }
