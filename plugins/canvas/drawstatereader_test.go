package canvas_test

import (
	"slices"
	"sync"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/render"
)

// drawStateReader is a test plugin whose task, hung off render.FrameEvent
// after canvas's handler, so after canvas's drawing, copies the shapes the
// frame merged out of canvas's private draw state, for the test to read once
// a Step returns.
type drawStateReader struct {
	testPlugin
	// mutex guards shapes, the shapes the last frame merged.
	mutex  sync.Mutex
	shapes []canvas.Shape
}

func (*drawStateReader) Name() kernel.PluginName { return "draw state reader" }

func (r *drawStateReader) Init(configurator *kernel.Configurator, _ any) error {
	copyShapes := configurator.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, state kernel.Read[*canvas.DrawState]) kernel.NoResult {
		r.mutex.Lock()
		defer r.mutex.Unlock()
		r.shapes = slices.Clone(state.Get().MergedShapes())
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		return event, ex.Schedule(copyShapes, kernel.NoArg{}, in).Handle()
	})
	return nil
}

// mergedShapes answers the shapes the last frame merged.
func (r *drawStateReader) mergedShapes() []canvas.Shape {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.shapes
}
