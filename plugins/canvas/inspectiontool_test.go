package canvas_test

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/render"
)

// inspectionTool is a test plugin standing for a tool attached to canvas's
// inspection, calling each of its funcs that is set with the task's Assets:
// duringFrame from a task off canvas.FrameEvent, with the frame's index,
// from 0, its beat and the task's render.Frame; afterDrawing from a task
// off render.FrameEvent after canvas's handler, so after canvas's drawing,
// with the frame's index; and onInspected from a task off
// canvas.InspectedEvent, with the event.
type inspectionTool struct {
	testPlugin
	duringFrame  func(frameIndex int, beat metronome.Beat, assets canvas.Assets, frame render.Frame)
	afterDrawing func(frameIndex int, assets canvas.Assets)
	onInspected  func(event canvas.InspectedEvent, assets canvas.Assets)
	// frameIndex and drawnFrameIndex are the indexes of the frames
	// duringFrame and afterDrawing are called for next; each frame's job
	// runs after the frame before's.
	frameIndex      int
	drawnFrameIndex int
}

func (*inspectionTool) Name() kernel.PluginName { return "inspection tool" }

func (p *inspectionTool) Init(configurator *kernel.Configurator, _ any) error {
	atFrame := configurator.RegisterTask2(func(k *kernel.Kernel, beat metronome.Beat, assets canvas.Assets, frame render.Frame) kernel.NoResult {
		if p.duringFrame != nil {
			p.duringFrame(p.frameIndex, beat, assets, frame)
		}
		p.frameIndex++
		return kernel.NoResult{}
	})
	afterDrawing := configurator.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, assets canvas.Assets) kernel.NoResult {
		if p.afterDrawing != nil {
			p.afterDrawing(p.drawnFrameIndex, assets)
		}
		p.drawnFrameIndex++
		return kernel.NoResult{}
	})
	inspected := configurator.RegisterTask1(func(k *kernel.Kernel, event canvas.InspectedEvent, assets canvas.Assets) kernel.NoResult {
		if p.onInspected != nil {
			p.onInspected(event, assets)
		}
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event canvas.FrameEvent) (canvas.FrameEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(atFrame, event.Beat, event.Ready).Handle())
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		return event, ex.Schedule(afterDrawing, kernel.NoArg{}, in).Handle()
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event canvas.InspectedEvent) (canvas.InspectedEvent, kernel.JobHandle) {
		return event, ex.Schedule(inspected, event, in).Handle()
	})
	return nil
}
