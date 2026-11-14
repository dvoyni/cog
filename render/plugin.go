package render

import (
	"cmp"
	"math"

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
// they are zero, and the asset table, each with the kernel's logger for
// reports; binds render's built-in assets as a read mount under the prefix
// cog.render, at the lowest priority, so any other mount holding one of
// their names overrides it; and subscribes to gfx.DrawEvent and
// gfx.RenderEvent, the Subs kept in the Handlers resource. A config of another type than *Config is a
// composition error, and panics.
func (*Plugin) Init(configurator *kernel.Configurator, config any) error {
	pluginConfig := kernel.PluginConfigOrDefault[Config](config)
	configurator.InitResource(&frameState{
		temporaryIdleFrames: cmp.Or(pluginConfig.TemporaryIdleFrames, defaultTemporaryIdleFrames),
		temporaryChunkSize:  cmp.Or(pluginConfig.TemporaryChunkSize, defaultTemporaryChunkSize),
		reporter:            reporter{logger: configurator.Logger()},
	})
	configurator.InitResource(&assetTable{reporter: reporter{logger: configurator.Logger()}})
	configurator.BindSocket(platform.ReadMountSocket{FS: makeAssetFS(), Priority: math.MinInt, Prefix: assetPrefix})
	beginFrameTask := configurator.RegisterTask1(beginFrame)
	writeLoadsTask := configurator.RegisterTask4(writeLoads)
	generateMipsTask := configurator.RegisterTask4(generateMips)
	endFrameTask := configurator.RegisterTask3(endFrame)
	configurator.InitResource(Handlers{
		RecordFrame: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.DrawEvent) (gfx.DrawEvent, kernel.JobHandle) {
			return recordFrame(ex, in, event, beginFrameTask, writeLoadsTask, generateMipsTask)
		}),
		EndFrame: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.RenderEvent) (gfx.RenderEvent, kernel.JobHandle) {
			return event, ex.Schedule(endFrameTask, kernel.NoArg{}, in).Handle()
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

// recordFrame is Handlers.RecordFrame: it schedules writeLoadsTask for the
// loads pending since the last frame, to run beside the recording; after
// gfx's acquire, it schedules beginFrameTask, which keeps the draw event's
// frame as the one being recorded, and publishes FrameEvent inside
// gfx.DrawEvent, with the draw event's beat and timing signals and that job
// as its Ready; and after the nested publication's out dependency and the
// first writeLoadsTask, it schedules writeLoadsTask again, for the loads
// acquired while the frame recorded, and after that generateMipsTask, for
// the mips of every texture the two wrote. It returns that job joined with
// beginFrameTask's, so gfx's flush waits for every recording task the users
// joined into the out dependency, for every load's body and for its mips.
func recordFrame(ex *kernel.Executor, in kernel.JobHandle, event gfx.DrawEvent, beginFrameTask *kernel.Task[gfx.Frame, kernel.NoResult], writeLoadsTask *kernel.Task[kernel.NoArg, kernel.NoResult], generateMipsTask *kernel.Task[gfx.Frame, kernel.NoResult]) (gfx.DrawEvent, kernel.JobHandle) {
	pendingWritten := ex.Schedule(writeLoadsTask, kernel.NoArg{}).Handle()
	beginFrameJob := ex.Schedule(beginFrameTask, event.Frame, event.Ready).Handle()
	_, out := ex.Publish(in, FrameEvent{
		Beat:        event.Beat,
		Ready:       beginFrameJob,
		AcquireWait: event.AcquireWait,
		GPULatency:  event.GPULatency,
	})
	recordedWritten := ex.Schedule(writeLoadsTask, kernel.NoArg{}, out, pendingWritten).Handle()
	mipsGenerated := ex.Schedule(generateMipsTask, event.Frame, recordedWritten).Handle()
	return event, ex.Join(mipsGenerated, beginFrameJob)
}

// beginFrame keeps frame as the one being recorded, for Frame's calls.
func beginFrame(k *kernel.Kernel, frame gfx.Frame, state kernel.Read[*frameState]) kernel.NoResult {
	state.Get().setRecordingFrame(frame)
	return kernel.NoResult{}
}

// writeLoads decodes the body of each texture loaded and not yet written, one
// after another, and writes it, so gfx's next submit carries it, and requests
// the mips of those loaded with Mipmaps.
func writeLoads(k *kernel.Kernel, _ kernel.NoArg, state kernel.Read[*frameState], table kernel.Read[*assetTable], device kernel.Read[gfx.Device], files kernel.Read[platform.FS]) kernel.NoResult {
	state.Get().mips.request(table.Get().loadedTextures.writePending(table.Get(), device.Get(), files.Get())...)
	return kernel.NoResult{}
}

// generateMips generates, in render's own command list of frame, the mips of
// every texture requested since it last ran, so gfx's submit carries them
// before every user's list.
func generateMips(k *kernel.Kernel, frame gfx.Frame, state kernel.Read[*frameState], table kernel.Read[*assetTable], device kernel.Read[gfx.Device], files kernel.Read[platform.FS]) kernel.NoResult {
	state.Get().mips.generate(table.Get(), device.Get(), files.Get(), frame)
	return kernel.NoResult{}
}

// endFrame ends the frame gfx has presented: it frees the loaded textures
// whose last acquire was released, releases the bind groups its mips drew
// with, takes back the temporary textures held, and frees those gone unused
// for Config's TemporaryIdleFrames frames.
func endFrame(k *kernel.Kernel, _ kernel.NoArg, state kernel.Read[*frameState], table kernel.Read[*assetTable], device kernel.Read[gfx.Device]) kernel.NoResult {
	frameState := state.Get()
	table.Get().loadedTextures.freeReleased(table.Get(), device.Get())
	frameState.mips.endFrame(device.Get())
	frameState.temporaryTextures.endFrame(frameState.temporaryIdleFrames, table.Get(), device.Get())
	return kernel.NoResult{}
}
