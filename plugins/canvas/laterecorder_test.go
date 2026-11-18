package canvas_test

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/render"
)

// lateRecorder is a test plugin whose two recording tasks hang off
// render.FrameEvent after canvas's handler, so they run once canvas's
// drawing has begun: each draws two fills, outside the frame.
type lateRecorder struct {
	testPlugin
}

func (*lateRecorder) Name() kernel.PluginName { return "late recorder" }

func (*lateRecorder) Init(configurator *kernel.Configurator, _ any) error {
	first := configurator.RegisterTask1(drawTwoFills)
	second := configurator.RegisterTask1(drawTwoFills)
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		firstJob := ex.Schedule(first, 1, in).Handle()
		secondJob := ex.Schedule(second, 2, in).Handle()
		return event, ex.Join(firstJob, secondJob)
	})
	return nil
}
