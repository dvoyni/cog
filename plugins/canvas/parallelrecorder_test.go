package canvas_test

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/canvas"
)

// parallelShapeCount is how many fills each of parallelRecorder's jobs draws.
const parallelShapeCount = 20000

// parallelRecorder is a test plugin whose two recording tasks run together
// off canvas.FrameEvent, the first scheduled twice, so two of its jobs record
// into its command list together: each job draws parallelShapeCount fills.
type parallelRecorder struct {
	testPlugin
}

func (*parallelRecorder) Name() kernel.PluginName { return "parallel recorder" }

func (*parallelRecorder) Init(configurator *kernel.Configurator, _ any) error {
	twice := configurator.RegisterTask1(drawManyFills)
	once := configurator.RegisterTask1(drawManyFills)
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event canvas.FrameEvent) (canvas.FrameEvent, kernel.JobHandle) {
		first := ex.Schedule(twice, 1, event.Ready).Handle()
		second := ex.Schedule(twice, 2, event.Ready).Handle()
		third := ex.Schedule(once, 3, event.Ready).Handle()
		return event, ex.Join(in, first, second, third)
	})
	return nil
}

// drawManyFills draws parallelShapeCount fills of order.
func drawManyFills(k *kernel.Kernel, order int32, list canvas.CommandList) kernel.NoResult {
	for range parallelShapeCount {
		list.DrawShape(canvas.Shape{Order: order})
	}
	return kernel.NoResult{}
}
