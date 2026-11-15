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
	// temporary buffers are ranges of, rounded up to a multiple of 4; zero
	// means 1 MiB.
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
// their names overrides it; subscribes to gfx.DrawEvent, gfx.RenderEvent,
// gfx.DeviceRecoveredEvent, gfx.CapturedEvent and gfx.BufferMappedEvent,
// the Subs kept in the Handlers
// resource; and subscribes to its own private event delivering readbacks. A
// config of another type than *Config is a composition error, and panics.
func (*Plugin) Init(configurator *kernel.Configurator, config any) error {
	pluginConfig := kernel.PluginConfigOrDefault[Config](config)
	configurator.InitResource(&frameState{
		temporaryIdleFrames: cmp.Or(pluginConfig.TemporaryIdleFrames, defaultTemporaryIdleFrames),
		temporaryChunkSize:  (cmp.Or(pluginConfig.TemporaryChunkSize, defaultTemporaryChunkSize) + 3) &^ 3,
		reporter:            reporter{logger: configurator.Logger()},
	})
	configurator.InitResource(&assetTable{reporter: reporter{logger: configurator.Logger()}})
	configurator.BindSocket(platform.ReadMountSocket{FS: makeAssetFS(), Priority: math.MinInt, Prefix: assetPrefix})
	beginFrameTask := configurator.RegisterTask1(beginFrame)
	writeLoadsTask := configurator.RegisterTask4(writeLoads)
	generateMipsTask := configurator.RegisterTask4(generateMips)
	checkClearsTask := configurator.RegisterTask1(checkClears)
	endFrameTask := configurator.RegisterTask3(endFrame)
	rewriteAssetsTask := configurator.RegisterTask4(rewriteAssets)
	findReadbackTask := configurator.RegisterTask1(findReadback)
	releaseReadbackTask := configurator.RegisterTask2(releaseReadback)
	configurator.InitResource(Handlers{
		RecordFrame: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.DrawEvent) (gfx.DrawEvent, kernel.JobHandle) {
			return recordFrame(ex, in, event, beginFrameTask, writeLoadsTask, generateMipsTask, checkClearsTask)
		}),
		EndFrame: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.RenderEvent) (gfx.RenderEvent, kernel.JobHandle) {
			return event, ex.Schedule(endFrameTask, kernel.NoArg{}, in).Handle()
		}),
		ReloadAssets: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.DeviceRecoveredEvent) (gfx.DeviceRecoveredEvent, kernel.JobHandle) {
			return reloadAssets(ex, in, event, rewriteAssetsTask)
		}),
		DeliverCapture: configurator.Subscribe(deliverCapture),
		DeliverReadback: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.BufferMappedEvent) (gfx.BufferMappedEvent, kernel.JobHandle) {
			return event, ex.Schedule(findReadbackTask, event, in).Handle()
		}),
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event readbackDeliveryEvent) (readbackDeliveryEvent, kernel.JobHandle) {
		return publishReadback(ex, in, event, releaseReadbackTask)
	})
	return nil
}

// Start does nothing.
func (*Plugin) Start(*kernel.Kernel) error { return nil }

// Stop does nothing.
func (*Plugin) Stop(*kernel.Kernel) error { return nil }

// Destroy does nothing.
func (*Plugin) Destroy() error { return nil }
