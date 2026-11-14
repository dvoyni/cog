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

// Plugin is render: it installs Handlers and the state of the frames in
// flight, and publishes FrameEvent inside each gfx.DrawEvent.
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
// they are zero, and subscribes to gfx.DrawEvent, the Sub kept in the
// Handlers resource. A config of another type than *Config is a composition
// error, and panics.
func (*Plugin) Init(configurator *kernel.Configurator, config any) error {
	pluginConfig := kernel.PluginConfigOrDefault[Config](config)
	configurator.InitResource(&frameState{
		temporaryIdleFrames: cmp.Or(pluginConfig.TemporaryIdleFrames, defaultTemporaryIdleFrames),
		temporaryChunkSize:  cmp.Or(pluginConfig.TemporaryChunkSize, defaultTemporaryChunkSize),
	})
	configurator.InitResource(Handlers{
		RecordFrame: configurator.Subscribe(recordFrame),
	})
	return nil
}

// Start does nothing.
func (*Plugin) Start(*kernel.Kernel) error { return nil }

// Stop does nothing.
func (*Plugin) Stop(*kernel.Kernel) error { return nil }

// Destroy does nothing.
func (*Plugin) Destroy() error { return nil }

// recordFrame is Handlers.RecordFrame: it publishes FrameEvent inside
// gfx.DrawEvent, with the draw event's beat, Ready and timing signals, and
// returns the nested publication's out dependency, so gfx's flush waits for
// every recording task the users joined into it.
func recordFrame(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.DrawEvent) (gfx.DrawEvent, kernel.JobHandle) {
	_, out := ex.Publish(in, FrameEvent{
		Beat:        event.Beat,
		Ready:       event.Ready,
		AcquireWait: event.AcquireWait,
		GPULatency:  event.GPULatency,
	})
	return event, out
}
