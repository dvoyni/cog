package canvastest

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/canvas"
)

// inspectionRequestEvent has the harness's plugin ask canvas for an
// inspection, between two beats, so the next beat's frame is inspected.
type inspectionRequestEvent struct{}

// harnessPlugin is the harness's own plugin, listed right after canvas: its
// recording task, hung off canvas.FrameEvent after Ready, calls the records
// Harness.Record queued for the frame with its draw list; it asks canvas for
// the inspection Step asks for; and it keeps each inspection canvas
// delivers, for Frames.
type harnessPlugin struct {
	harness *Harness
}

// Name answers the harness plugin's name.
func (harnessPlugin) Name() kernel.PluginName { return "canvastest" }

// Dependencies names canvas.
func (harnessPlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{canvas.PluginName}
}

// Init registers the recording task, the task asking for an inspection and
// the task reading one; and subscribes to canvas.FrameEvent, to schedule the
// recording task after Ready, joined into the out dependency, to
// inspectionRequestEvent, and to canvas.InspectedEvent. The tasks' bodies
// stay closures, since they hand the test's records the draw list and the
// test the inspections.
func (p harnessPlugin) Init(configurator *kernel.Configurator, _ any) error {
	record := configurator.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, list canvas.DrawList) kernel.NoResult {
		for _, queued := range p.harness.takeRecords() {
			queued(list)
		}
		return kernel.NoResult{}
	})
	requestInspection := configurator.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, assets canvas.Assets) kernel.NoResult {
		assets.RequestInspection()
		return kernel.NoResult{}
	})
	readInspection := configurator.RegisterTask1(func(k *kernel.Kernel, event canvas.InspectedEvent, assets canvas.Assets) kernel.NoResult {
		if p.harness.inspectingNothing {
			// A test's own plugin asked for it, and reads it.
			return kernel.NoResult{}
		}
		inspection, err := assets.ReadInspection(event)
		if err != nil {
			p.harness.t.Errorf("canvastest: %v", err)
			return kernel.NoResult{}
		}
		select {
		case p.harness.deliveredInspections <- inspection:
		default:
			p.harness.t.Errorf("canvastest: canvas delivered the inspection of beat %d, which no Step waits for", event.Beat.Tick)
		}
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event canvas.FrameEvent) (canvas.FrameEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(record, kernel.NoArg{}, event.Ready).Handle())
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event inspectionRequestEvent) (inspectionRequestEvent, kernel.JobHandle) {
		return event, ex.Schedule(requestInspection, kernel.NoArg{}, in).Handle()
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event canvas.InspectedEvent) (canvas.InspectedEvent, kernel.JobHandle) {
		return event, ex.Schedule(readInspection, event, in).Handle()
	})
	return nil
}

// Start hands the harness the kernel, which it publishes its requests for an
// inspection into.
func (p harnessPlugin) Start(k *kernel.Kernel) error {
	p.harness.kernel = k
	return nil
}

// Stop does nothing.
func (harnessPlugin) Stop(*kernel.Kernel) error { return nil }

// Destroy does nothing.
func (harnessPlugin) Destroy() error { return nil }
