package canvas_test

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/canvas"
)

// orderedRecorder is a test plugin registering two recording tasks off
// canvas.FrameEvent: late, registered first, and early, registered second,
// which runs first, since late is scheduled after it. Each draws two fills
// of its order, at layers 0 and 1.
type orderedRecorder struct {
	testPlugin
	name       kernel.PluginName
	lateOrder  int32
	earlyOrder int32
}

func (r *orderedRecorder) Name() kernel.PluginName { return r.name }

func (r *orderedRecorder) Init(configurator *kernel.Configurator, _ any) error {
	late := configurator.RegisterTask1(drawTwoFills)
	early := configurator.RegisterTask1(drawTwoFills)
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event canvas.FrameEvent) (canvas.FrameEvent, kernel.JobHandle) {
		earlyJob := ex.Schedule(early, r.earlyOrder, event.Ready).Handle()
		lateJob := ex.Schedule(late, r.lateOrder, earlyJob).Handle()
		return event, ex.Join(in, lateJob)
	})
	return nil
}

// drawTwoFills draws two fills of order, at layers 0 and 1.
func drawTwoFills(k *kernel.Kernel, order int32, list canvas.DrawList) kernel.NoResult {
	list.DrawShape(canvas.Shape{Order: order})
	list.DrawShape(canvas.Shape{Order: order, Layer: 1})
	return kernel.NoResult{}
}
