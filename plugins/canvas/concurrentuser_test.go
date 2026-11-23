package canvas_test

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/render"
)

// recordFunc is what a task of concurrentUser's records calls in each
// frame: with its draw list, Assets and the frame's index, from 0.
type recordFunc = func(list canvas.DrawList, assets canvas.Assets, frame int)

// concurrentUser is a test plugin whose tasks use canvas together in each
// frame, each called with the frame's index, from 0, and none ordered
// after another:
//   - each of records in a task of its own, so with a draw list of its own,
//     with Assets, and each of reads in a task of its own with FontLibrary's
//     reading side, off canvas.FrameEvent after its Ready;
//   - each of lateRecords in a task of its own with a draw list, and each
//     of whileDrawing in a task of its own with Assets, off render.FrameEvent
//     after its Ready, so they run while canvas opens, sorts and draws the
//     frame.
type concurrentUser struct {
	testPlugin
	records      []recordFunc
	reads        []func(fonts canvas.FontLibrary, frame int)
	lateRecords  []func(list canvas.DrawList, frame int)
	whileDrawing []func(assets canvas.Assets, frame int)
	// recordedFrameCount and drawnFrameCount are the frames whose
	// canvas.FrameEvent and render.FrameEvent it has handled, each handled
	// once a frame in turn.
	recordedFrameCount int
	drawnFrameCount    int
}

func (*concurrentUser) Name() kernel.PluginName { return "concurrent user" }

func (u *concurrentUser) Init(configurator *kernel.Configurator, _ any) error {
	var atFrame, whileFrameDraws []*kernel.Task[int, kernel.NoResult]
	for _, record := range u.records {
		atFrame = append(atFrame, configurator.RegisterTask2(func(k *kernel.Kernel, frame int, list canvas.DrawList, assets canvas.Assets) kernel.NoResult {
			record(list, assets, frame)
			return kernel.NoResult{}
		}))
	}
	for _, read := range u.reads {
		atFrame = append(atFrame, configurator.RegisterTask1(func(k *kernel.Kernel, frame int, fonts kernel.Read[canvas.FontLibrary]) kernel.NoResult {
			read(fonts.Get(), frame)
			return kernel.NoResult{}
		}))
	}
	for _, record := range u.lateRecords {
		whileFrameDraws = append(whileFrameDraws, configurator.RegisterTask1(func(k *kernel.Kernel, frame int, list canvas.DrawList) kernel.NoResult {
			record(list, frame)
			return kernel.NoResult{}
		}))
	}
	for _, use := range u.whileDrawing {
		whileFrameDraws = append(whileFrameDraws, configurator.RegisterTask1(func(k *kernel.Kernel, frame int, assets canvas.Assets) kernel.NoResult {
			use(assets, frame)
			return kernel.NoResult{}
		}))
	}
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event canvas.FrameEvent) (canvas.FrameEvent, kernel.JobHandle) {
		frame := u.recordedFrameCount
		u.recordedFrameCount++
		out := in
		for _, task := range atFrame {
			out = ex.Join(out, ex.Schedule(task, frame, event.Ready).Handle())
		}
		return event, out
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		frame := u.drawnFrameCount
		u.drawnFrameCount++
		out := in
		for _, task := range whileFrameDraws {
			out = ex.Join(out, ex.Schedule(task, frame, event.Ready).Handle())
		}
		return event, out
	})
	return nil
}
