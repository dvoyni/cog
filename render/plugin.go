package render

import (
	"cmp"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/platform"
)

// PluginName is render's plugin name.
const PluginName kernel.PluginName = "render"

// Config is render's part of the kernel's config, under PluginName: how long
// render keeps what it pools, and how much it takes at once.
type Config struct {
	// TemporaryIdleFrames is how many frames something pooled may go unused
	// before render frees it; zero means 10.
	TemporaryIdleFrames int
	// TemporaryChunkSize is the size in bytes of each chunk of the arena
	// temporary buffers are ranges of; zero means 1 MiB.
	TemporaryChunkSize uint64
}

const (
	// defaultTemporaryIdleFrames is Config.TemporaryIdleFrames when it is
	// zero.
	defaultTemporaryIdleFrames = 10
	// defaultTemporaryChunkSize is Config.TemporaryChunkSize when it is zero:
	// 1 MiB.
	defaultTemporaryChunkSize uint64 = 1 << 20
)

// Plugin is render: it installs Handlers, the state of the frames in flight
// and the asset table, and publishes FrameEvent inside each gfx.DrawEvent.
type Plugin struct{}

// NewPlugin returns render.
func NewPlugin() *Plugin { return &Plugin{} }

// Name returns PluginName.
func (*Plugin) Name() kernel.PluginName { return PluginName }

// Dependencies names gfx, which render draws through, and platform; render is
// listed after both, so its handlers on gfx's events run after gfx's own.
func (*Plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{gfx.PluginName, platform.PluginName}
}

// Init installs the frame state, with Config's counts or their defaults where
// they are zero and the kernel's logger for reports, and the asset table, and subscribes to gfx.DrawEvent, the
// Sub kept in the Handlers resource. A config of another type than *Config
// is a composition error, and panics.
func (*Plugin) Init(configurator *kernel.Configurator, config any) error {
	pluginConfig := kernel.PluginConfigOrDefault[Config](config)
	configurator.InitResource(&frameState{
		temporaryIdleFrames: cmp.Or(pluginConfig.TemporaryIdleFrames, defaultTemporaryIdleFrames),
		temporaryChunkSize:  cmp.Or(pluginConfig.TemporaryChunkSize, defaultTemporaryChunkSize),
		logger:              configurator.Logger(),
	})
	configurator.InitResource(&assetTable{})
	beginFrameTask := configurator.RegisterTask1(beginFrame)
	configurator.InitResource(Handlers{
		RecordFrame: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.DrawEvent) (gfx.DrawEvent, kernel.JobHandle) {
			return recordFrame(ex, in, event, beginFrameTask)
		}),
	})
	return nil
}

// Start does nothing.
func (*Plugin) Start(*kernel.Kernel) error { return nil }

// Stop does nothing.
func (*Plugin) Stop(*kernel.Kernel) error { return nil }

// Destroy does nothing.
func (*Plugin) Destroy() error { return nil }

// recordFrame is Handlers.RecordFrame: after gfx's acquire, it schedules
// beginFrameTask, which keeps the draw event's frame as the one being
// recorded, and publishes FrameEvent inside gfx.DrawEvent, with the draw
// event's beat and timing signals and that job as its Ready. It returns the
// nested publication's out dependency joined with that job, so gfx's flush
// waits for every recording task the users joined into it.
func recordFrame(ex *kernel.Executor, in kernel.JobHandle, event gfx.DrawEvent, beginFrameTask *kernel.Task[gfx.Frame, kernel.NoResult]) (gfx.DrawEvent, kernel.JobHandle) {
	beginFrameJob := ex.Schedule(beginFrameTask, event.Frame, event.Ready).Handle()
	_, out := ex.Publish(in, FrameEvent{
		Beat:        event.Beat,
		Ready:       beginFrameJob,
		AcquireWait: event.AcquireWait,
		GPULatency:  event.GPULatency,
	})
	return event, ex.Join(out, beginFrameJob)
}

// beginFrame keeps frame as the one being recorded, for Frame's calls.
func beginFrame(k *kernel.Kernel, frame gfx.Frame, state kernel.Read[*frameState]) kernel.NoResult {
	state.Get().setRecordingFrame(frame)
	return kernel.NoResult{}
}
