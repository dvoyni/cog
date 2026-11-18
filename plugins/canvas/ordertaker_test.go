package canvas_test

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/render"
)

// orderTaker is a test plugin whose task, hung off canvas.FrameEvent so it
// runs before canvas draws, begins an empty command list "taken" at order in
// each frame, taking that order before any camera's list.
type orderTaker struct {
	testPlugin
	order render.Order
}

func (*orderTaker) Name() kernel.PluginName { return "order taker" }

func (o *orderTaker) Init(configurator *kernel.Configurator, _ any) error {
	take := configurator.RegisterTask1(beginTakenList)
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event canvas.FrameEvent) (canvas.FrameEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(take, o.order, event.Ready).Handle())
	})
	return nil
}

// beginTakenList begins the empty list "taken" at order.
func beginTakenList(k *kernel.Kernel, order render.Order, frame render.Frame) kernel.NoResult {
	list := frame.BeginCommandList("taken", order)
	list.End()
	return kernel.NoResult{}
}
