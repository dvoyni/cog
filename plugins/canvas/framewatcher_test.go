package canvas_test

import (
	"slices"
	"sync"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/render"
)

// frameWatcher is a test plugin keeping every render.FrameEvent and every
// canvas.FrameEvent.
type frameWatcher struct {
	testPlugin
	// mutex guards renderEvents and canvasEvents.
	mutex        sync.Mutex
	renderEvents []render.FrameEvent
	canvasEvents []canvas.FrameEvent
}

func (*frameWatcher) Name() kernel.PluginName { return "frame watcher" }

func (w *frameWatcher) Init(configurator *kernel.Configurator, _ any) error {
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		w.mutex.Lock()
		defer w.mutex.Unlock()
		w.renderEvents = append(w.renderEvents, event)
		return event, in
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event canvas.FrameEvent) (canvas.FrameEvent, kernel.JobHandle) {
		w.mutex.Lock()
		defer w.mutex.Unlock()
		w.canvasEvents = append(w.canvasEvents, event)
		return event, in
	})
	return nil
}

// events answers the canvas.FrameEvents and render.FrameEvents kept so far.
func (w *frameWatcher) events() ([]canvas.FrameEvent, []render.FrameEvent) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return slices.Clone(w.canvasEvents), slices.Clone(w.renderEvents)
}
