package render_test

import (
	"slices"
	"sync"
	"time"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/render"
)

// frameWatcher is the test's plugin, listed after render. It keeps every
// FrameEvent and every gfx.DrawEvent, and for each FrameEvent schedules a
// recording task after Ready, joined into the out dependency, which takes
// its time and then takes the frame's encoder from gfx, an error once gfx
// has flushed the frame. Its Init keeps what render made of its Config.
type frameWatcher struct {
	// mutex guards frameEvents, drawEvents and encoderErrors, what each
	// recording task's encoder answered.
	mutex         sync.Mutex
	frameEvents   []render.FrameEvent
	drawEvents    []gfx.DrawEvent
	encoderErrors []error
	// temporaryIdleFrames and temporaryChunkSize are what render's frame
	// state held at Init.
	temporaryIdleFrames int
	temporaryChunkSize  uint64
}

func (*frameWatcher) Name() kernel.PluginName { return "frame watcher" }

func (*frameWatcher) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{render.PluginName}
}

func (w *frameWatcher) Init(configurator *kernel.Configurator, _ any) error {
	state, _ := configurator.Resource[*render.FrameState]()
	w.temporaryIdleFrames = state.TemporaryIdleFrames()
	w.temporaryChunkSize = state.TemporaryChunkSize()
	record := configurator.RegisterTask1(func(k *kernel.Kernel, event render.FrameEvent, device kernel.Read[gfx.Device]) kernel.NoResult {
		time.Sleep(10 * time.Millisecond)
		w.mutex.Lock()
		drawIndex := slices.IndexFunc(w.drawEvents, func(drawEvent gfx.DrawEvent) bool { return drawEvent.Tick == event.Tick })
		frame := w.drawEvents[drawIndex].Frame
		w.mutex.Unlock()
		_, err := device.Get().Encoder(frame, 0, "")
		w.mutex.Lock()
		w.encoderErrors = append(w.encoderErrors, err)
		w.mutex.Unlock()
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
		w.mutex.Lock()
		w.frameEvents = append(w.frameEvents, event)
		w.mutex.Unlock()
		return event, ex.Join(in, ex.Schedule(record, event, event.Ready).Handle())
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.DrawEvent) (gfx.DrawEvent, kernel.JobHandle) {
		w.mutex.Lock()
		w.drawEvents = append(w.drawEvents, event)
		w.mutex.Unlock()
		return event, in
	})
	return nil
}

func (*frameWatcher) Start(*kernel.Kernel) error { return nil }

func (*frameWatcher) Stop(*kernel.Kernel) error { return nil }

func (*frameWatcher) Destroy() error { return nil }

// events answers every FrameEvent and gfx.DrawEvent kept so far.
func (w *frameWatcher) events() ([]render.FrameEvent, []gfx.DrawEvent) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return slices.Clone(w.frameEvents), slices.Clone(w.drawEvents)
}

// encoderAnswers answers what each recording task's encoder answered so far.
func (w *frameWatcher) encoderAnswers() []error {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return slices.Clone(w.encoderErrors)
}
