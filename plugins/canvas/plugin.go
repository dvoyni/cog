package canvas

import (
	"math"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// PluginName is canvas's plugin name.
const PluginName kernel.PluginName = "canvas"

// Config is canvas's part of the kernel's config, under PluginName.
type Config struct {
	// RegroupedLayers are the layers whose entries of equal keys canvas may
	// regroup by batch, for a UI's or particles' entries whose order among
	// themselves does not matter. Entries of equal keys in any other layer
	// are drawn in the order they were recorded.
	RegroupedLayers []int32
}

// Plugin is canvas: it installs Handlers, Targets and its private state,
// binds its built-in assets, publishes FrameEvent inside each
// render.FrameEvent, draws the frame's entries once they are recorded, and
// then copies the targets drawn into Targets.
type Plugin struct{}

// NewPlugin returns canvas.
func NewPlugin() *Plugin { return &Plugin{} }

// Name returns PluginName.
func (*Plugin) Name() kernel.PluginName { return PluginName }

// Dependencies names render, which canvas draws through, and platform; canvas
// is listed after both, and a plugin that records into canvas after canvas.
func (*Plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{render.PluginName, platform.PluginName}
}

// Init installs the draw state, with the kernel's logger for reports and the
// layers config regroups, the renderer, the window list and Targets; binds canvas's built-in assets as a read
// mount under the prefix cog.canvas, at the lowest priority, so any other
// mount holding one of their names overrides it; subscribes to
// render.FrameEvent, platform.WindowOpenedEvent, platform.WindowClosedEvent
// and render.InspectedEvent, the Subs kept in the Handlers resource; and
// subscribes to its own private event delivering inspections. A config of
// another type than *Config is a composition error, and panics.
func (*Plugin) Init(configurator *kernel.Configurator, config any) error {
	canvasConfig := kernel.PluginConfigOrDefault[Config](config)
	configurator.InitResource(&drawState{logger: configurator.Logger(), sorter: makeEntrySorter(canvasConfig.RegroupedLayers)})
	configurator.InitResource(&renderer{})
	configurator.InitResource(&windowList{})
	configurator.InitResource[Targets](&drawnTargets{})
	configurator.BindSocket(platform.ReadMountSocket{FS: makeAssetFS(), Priority: math.MinInt, Prefix: assetPrefix})
	openFrameTask := configurator.RegisterTask1(openFrame)
	sortEntriesTask := configurator.RegisterTask1(sortEntries)
	drawTask := configurator.RegisterTask6(draw)
	copyTargetsTask := configurator.RegisterTask2(copyTargets)
	addWindowTask := configurator.RegisterTask1(addWindow)
	removeWindowTask := configurator.RegisterTask1(removeWindow)
	matchInspectionTask := configurator.RegisterTask2(matchInspection)
	dropInspectionTask := configurator.RegisterTask1(dropInspection)
	configurator.InitResource(Handlers{
		RecordFrame: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
			return recordFrame(ex, in, event, openFrameTask, sortEntriesTask, copyTargetsTask, drawTask)
		}),
		TrackOpenedWindow: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event platform.WindowOpenedEvent) (platform.WindowOpenedEvent, kernel.JobHandle) {
			return trackOpenedWindow(ex, in, event, addWindowTask)
		}),
		ForgetClosedWindow: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event platform.WindowClosedEvent) (platform.WindowClosedEvent, kernel.JobHandle) {
			return forgetClosedWindow(ex, in, event, removeWindowTask)
		}),
		DeliverInspection: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.InspectedEvent) (render.InspectedEvent, kernel.JobHandle) {
			return deliverInspection(ex, in, event, matchInspectionTask)
		}),
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event inspectionDeliveryEvent) (inspectionDeliveryEvent, kernel.JobHandle) {
		return publishInspection(ex, in, event, dropInspectionTask)
	})
	return nil
}

// Start does nothing.
func (*Plugin) Start(*kernel.Kernel) error { return nil }

// Stop does nothing.
func (*Plugin) Stop(*kernel.Kernel) error { return nil }

// Destroy does nothing.
func (*Plugin) Destroy() error { return nil }
