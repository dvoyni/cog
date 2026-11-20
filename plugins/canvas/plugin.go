package canvas

import (
	"cmp"
	"fmt"
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
	// DefaultFilter is the filter of a sprite whose Filter is zero; zero
	// means FilterLinear.
	DefaultFilter Filter
	// AtlasPageSize is the side of a sprite atlas's pages, in texels; zero
	// means 2048. An image whose cell, the image rounded up to the grid
	// MipLevels sets and its gutter around it, no page holds is drawn from
	// a texture of its own. The side is a multiple of the grid holding the
	// white texel's cell, 12 at the default 3 levels and 5 at one; a side
	// that is not is raised to the next that is, reported once as
	// ReportKindAtlasPageSizeRaised.
	AtlasPageSize uint32
	// AtlasLayerCeiling is the most pages a sprite atlas grows to, doubling
	// them each time an image fits on none; zero means 256, WebGPU's
	// default limit on a texture's layers. At its ceiling, the images drawn
	// least lately are evicted to make room, never one drawn this frame,
	// and an image they cannot make room for is drawn from a texture of its
	// own, reported once as ReportKindAtlasFull.
	AtlasLayerCeiling uint32
	// OwnTextureSize is the side, in texels, past which an image is drawn
	// from a texture of its own, exactly its size, rather than packed into
	// a sprite atlas, on either axis; zero means half of AtlasPageSize.
	OwnTextureSize uint32
	// DoNotGenerateMipMaps makes a sprite atlas's pages, and an image's own
	// texture, of one mip level, for a pixel-perfect game, whose sprites
	// are never minified: their cells then reach a gutter of 2 texels past
	// their images, on no grid.
	DoNotGenerateMipMaps bool
	// MipLevels is how many mip levels a sprite atlas's pages have unless
	// DoNotGenerateMipMaps is set, each page's regenerated where an image
	// was packed into it; zero means 3. It fixes the grid each cell's
	// image is rounded up to and the gutter its cell reaches past that,
	// both 2^(MipLevels−1) texels, 4 at the default, so a sprite
	// minified to 1/2^(MipLevels−1) of its size still reads only its own
	// texels. An image's own texture has as many levels, or as many as its
	// size has. More than 12, whose smallest page would pass WebGPU's
	// default limit of 8192 texels on a texture's side, are lowered to 12,
	// reported once as ReportKindMipLevelsLowered.
	MipLevels uint32
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

// Init installs the draw state, with the kernel's logger for reports, the
// filter of entries naming none and the layers config regroups; the
// renderer, its mip levels lowered to the most and reported when config's
// are more, and its page side raised to the smallest on its grid and
// reported when config's is not; the window list and Targets; binds
// canvas's built-in assets as a read
// mount under the prefix cog.canvas, at the lowest priority, so any other
// mount holding one of their names overrides it; subscribes to
// render.FrameEvent, platform.WindowOpenedEvent, platform.WindowClosedEvent
// and render.InspectedEvent, the Subs kept in the Handlers resource; and
// subscribes to its own private event delivering inspections. A config of
// another type than *Config is a composition error, and panics.
func (*Plugin) Init(configurator *kernel.Configurator, config any) error {
	canvasConfig := kernel.PluginConfigOrDefault[Config](config)
	configurator.InitResource(&drawState{logger: configurator.Logger(), defaultFilter: cmp.Or(canvasConfig.DefaultFilter, FilterLinear), sorter: makeEntrySorter(canvasConfig.RegroupedLayers)})
	mipLevelCount := cmp.Or(canvasConfig.MipLevels, 3)
	if canvasConfig.DoNotGenerateMipMaps {
		mipLevelCount = 1
	}
	if mipLevelCount > largestAtlasMipLevelCount {
		loweredReport := Report{Kind: ReportKindMipLevelsLowered, Err: fmt.Errorf("canvas: Config.MipLevels %d is more than %d, the most whose smallest page fits WebGPU's default limit on a texture's side, so pages are made of %d", mipLevelCount, largestAtlasMipLevelCount, largestAtlasMipLevelCount)}
		configurator.Logger().LogErr(loweredReport)
		mipLevelCount = largestAtlasMipLevelCount
	}
	// A page is a multiple of the grid holding the white texel's cell.
	atlasPageSize := cmp.Or(canvasConfig.AtlasPageSize, 2048)
	grid, gutter := findAtlasGrid(mipLevelCount)
	whiteCellSide := grid + 2*gutter
	if fitted := max(roundUpToMultiple(atlasPageSize, grid), whiteCellSide); fitted != atlasPageSize {
		raisedReport := Report{Kind: ReportKindAtlasPageSizeRaised, Err: fmt.Errorf("canvas: Config.AtlasPageSize %d is below %d, the white texel's cell, or no multiple of %d, the grid its cells sit on, so pages are made %d texels a side", atlasPageSize, whiteCellSide, grid, fitted)}
		configurator.Logger().LogErr(raisedReport)
		atlasPageSize = fitted
	}
	configurator.InitResource(newRenderer(atlasPageSize, cmp.Or(canvasConfig.AtlasLayerCeiling, 256), cmp.Or(canvasConfig.OwnTextureSize, atlasPageSize/2), mipLevelCount))
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
