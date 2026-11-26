package canvas

import (
	"cmp"
	"fmt"
	"math"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/canvas/font"
	"github.com/dvoyni/cog/plugins/metronome"
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
	// AtlasPageSize is the side of a sprite atlas's pages, and the glyph
	// atlas's, in texels; zero means 2048. An image whose cell, the image rounded up to the atlas grid
	// MipLevels sets and its gutter around it, no page holds is drawn from
	// a texture of its own. The side is a multiple of the atlas grid holding the
	// white texel's cell, 12 at the default 3 levels and 5 at one; a side
	// that is not is raised to the next that is, reported once as
	// ReportKindAtlasPageSizeRaised.
	AtlasPageSize uint32
	// AtlasLayerCeiling is the most pages a sprite atlas grows to, doubling
	// them each time an image fits on none, and the glyph atlas each time a
	// glyph does; zero means 256, WebGPU's default limit on a texture's
	// layers. At its ceiling, the images drawn
	// least lately are evicted to make room, never one drawn this frame,
	// and an image they cannot make room for is drawn from a texture of its
	// own, reported once as ReportKindAtlasFull; the glyph atlas evicts its
	// glyphs alike, and a glyph they cannot make room for draws nothing that
	// frame.
	AtlasLayerCeiling uint32
	// OwnTextureSize is the side, in texels, past which an image is drawn
	// from a texture of its own, exactly its size, rather than packed into
	// a sprite atlas, on either axis; zero means half of AtlasPageSize.
	OwnTextureSize uint32
	// DoNotGenerateMipMaps makes a sprite atlas's pages, and an image's own
	// texture, of one mip level, for a pixel-perfect game, whose sprites
	// are never minified: their cells then reach a gutter of 2 texels past
	// their images, on no atlas grid.
	DoNotGenerateMipMaps bool
	// MipLevels is how many mip levels a sprite atlas's pages have unless
	// DoNotGenerateMipMaps is set, each page's regenerated where an image
	// was packed into it; zero means 3. It fixes the atlas grid each cell's
	// image is rounded up to and the gutter its cell reaches past that,
	// both 2^(MipLevels−1) texels, 4 at the default, so a sprite
	// minified to 1/2^(MipLevels−1) of its size still reads only its own
	// texels. An image's own texture has as many levels, or as many as its
	// size has. More than 12, whose smallest page would pass WebGPU's
	// default limit of 8192 texels on a texture's side, are lowered to 12,
	// reported once as ReportKindMipLevelsLowered.
	MipLevels uint32
	// Language is the current language at start, a tag such as "en", which
	// picks each font set's fonts; WriteFontLibrary.SetLanguage changes it.
	Language string
	// TextSize is the em, in world units, of a run of text whose Size is
	// zero; zero means 16.
	TextSize float32
	// LayoutIdleFrames is how many frames a run's layout may go unused,
	// neither drawn nor measured, before the layout cache frees it; zero
	// means 2.
	LayoutIdleFrames int
	// GlyphFieldEm is the em, in field texels, a font's glyphs are made
	// distance fields at when its font.Font.FieldEm is zero; zero means 48.
	// GlyphFieldRange is how far a field reaches across a glyph's edge, half
	// outside it and half inside, in field texels, when its font's
	// font.Font.FieldRange is zero; zero means 16, so an outline reaches 8
	// field texels at most.
	GlyphFieldEm    int
	GlyphFieldRange int
	// DefaultFontSet describes the default font set, the zero FontSet's,
	// made as canvas starts, its fonts read through platform's FS. Its zero
	// is canvas's built-in JetBrains Mono alone, which the default set is
	// until then.
	DefaultFontSet font.SetDescriptor
}

// Plugin is canvas: it installs Handlers, Targets, FontLibrary and its private state,
// binds its built-in assets, publishes FrameEvent inside each
// render.FrameEvent, draws the frame's entries once they are recorded, and
// then copies the targets drawn into Targets; and starts afresh on
// render.RecoveredEvent.
type Plugin struct{}

// NewPlugin returns canvas.
func NewPlugin() *Plugin { return &Plugin{} }

// Name returns PluginName.
func (*Plugin) Name() kernel.PluginName { return PluginName }

// Dependencies names render, which canvas draws through, platform, and
// metronome, whose Clock times canvas's jobs; canvas is listed after them,
// and a plugin that records into canvas after canvas.
func (*Plugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{render.PluginName, platform.PluginName, metronome.PluginName}
}

// Init installs the draw state, with the kernel's logger for reports, which
// also go into the inspection pending, stopwatches of metronome's Clock
// timing its jobs, the
// filter of entries naming none and the layers config regroups; the
// renderer, its mip levels lowered to the most and reported when config's
// are more, and its page side raised to the smallest on its atlas grid and
// reported when config's is not; the window list, Targets, and FontLibrary, its
// current language config's and its default set JetBrains Mono until Start
// makes it of config's; binds
// canvas's built-in assets as a read
// mount under the prefix cog.canvas, at the lowest priority, so any other
// mount holding one of their names overrides it; subscribes to
// render.FrameEvent, render.RecoveredEvent, platform.WindowOpenedEvent,
// platform.WindowClosedEvent and render.InspectedEvent, the Subs kept in the
// Handlers resource; and
// subscribes to its own private events delivering inspections, starting and
// reloading font files. A config of
// another type than *Config is a composition error, and panics.
func (*Plugin) Init(configurator *kernel.Configurator, config any) error {
	canvasConfig := kernel.PluginConfigOrDefault[Config](config)
	canvasInspector := &inspector{}
	canvasReporter := &reporter{logger: configurator.Logger(), inspector: canvasInspector}
	clock, _ := configurator.Resource[metronome.Clock]()
	configurator.InitResource(&drawState{
		reporter:      canvasReporter,
		inspector:     canvasInspector,
		defaultFilter: cmp.Or(canvasConfig.DefaultFilter, FilterLinear),
		sorter:        makeEntrySorter(canvasConfig.RegroupedLayers),
		sortStopwatch: clock.NewStopwatch(SortStopwatchName),
		drawStopwatch: clock.NewStopwatch(DrawStopwatchName),
	})
	mipLevelCount := cmp.Or(canvasConfig.MipLevels, 3)
	if canvasConfig.DoNotGenerateMipMaps {
		mipLevelCount = 1
	}
	if mipLevelCount > largestAtlasMipLevelCount {
		loweredReport := Report{Kind: ReportKindMipLevelsLowered, Err: fmt.Errorf("canvas: Config.MipLevels %d is more than %d, the most whose smallest page fits WebGPU's default limit on a texture's side, so pages are made of %d", mipLevelCount, largestAtlasMipLevelCount, largestAtlasMipLevelCount)}
		configurator.Logger().LogErr(loweredReport)
		mipLevelCount = largestAtlasMipLevelCount
	}
	// A page is a multiple of the atlas grid holding the white texel's cell.
	atlasPageSize := cmp.Or(canvasConfig.AtlasPageSize, 2048)
	atlasGrid, gutter := findSpriteAtlasGrid(mipLevelCount)
	whiteCellSide := atlasGrid + 2*gutter
	if fitted := max(roundUpToMultiple(atlasPageSize, atlasGrid), whiteCellSide); fitted != atlasPageSize {
		raisedReport := Report{Kind: ReportKindAtlasPageSizeRaised, Err: fmt.Errorf("canvas: Config.AtlasPageSize %d is below %d, the white texel's cell, or no multiple of %d, the atlas grid its cells sit on, so pages are made %d texels a side", atlasPageSize, whiteCellSide, atlasGrid, fitted)}
		configurator.Logger().LogErr(raisedReport)
		atlasPageSize = fitted
	}
	configurator.InitResource(newRenderer(atlasPageSize, cmp.Or(canvasConfig.AtlasLayerCeiling, 256), cmp.Or(canvasConfig.OwnTextureSize, atlasPageSize/2), mipLevelCount))
	configurator.InitResource(&windowList{})
	configurator.InitResource[Targets](&drawnTargets{})
	configurator.InitResource[FontLibrary](newFontLibrary(canvasReporter, canvasConfig))
	configurator.BindSocket(platform.ReadMountSocket{FS: makeAssetFS(), Priority: math.MinInt, Prefix: assetPrefix})
	configurator.BindSocket(platform.ReadMountSocket{FS: font.BuiltInFiles(), Priority: math.MinInt, Prefix: assetPrefix})
	openFrameTask := configurator.RegisterTask1(openFrame)
	sortEntriesTask := configurator.RegisterTask1(sortEntries)
	drawTask := configurator.RegisterTask8(draw)
	copyTargetsTask := configurator.RegisterTask2(copyTargets)
	startAfreshTask := configurator.RegisterTask2(startAfresh)
	addWindowTask := configurator.RegisterTask1(addWindow)
	removeWindowTask := configurator.RegisterTask1(removeWindow)
	matchInspectionTask := configurator.RegisterTask2(matchInspection)
	dropInspectionTask := configurator.RegisterTask1(dropInspection)
	makeDefaultFontSetTask := configurator.RegisterTask2(makeDefaultFontSet)
	reloadChangedFontsTask := configurator.RegisterTask2(reloadChangedFonts)
	configurator.InitResource(Handlers{
		RecordFrame: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.FrameEvent) (render.FrameEvent, kernel.JobHandle) {
			return recordFrame(ex, in, event, openFrameTask, sortEntriesTask, copyTargetsTask, drawTask)
		}),
		Recover: configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event render.RecoveredEvent) (render.RecoveredEvent, kernel.JobHandle) {
			return startAfreshOnRecovery(ex, in, event, startAfreshTask)
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
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event startEvent) (startEvent, kernel.JobHandle) {
		return makeDefaultFontSetOnStart(ex, in, event, makeDefaultFontSetTask)
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event fontReloadEvent) (fontReloadEvent, kernel.JobHandle) {
		return reloadChangedFontsOnRequest(ex, in, event, reloadChangedFontsTask)
	})
	return nil
}

// Start makes the default font set of Config.DefaultFontSet, in a job
// under FontLibrary's write lock, once every mount is bound, so a task reading
// FontLibrary that is ready after it waits for it.
func (*Plugin) Start(k *kernel.Kernel) error {
	k.Publish(startEvent{})
	return nil
}

// Stop does nothing.
func (*Plugin) Stop(*kernel.Kernel) error { return nil }

// Destroy does nothing.
func (*Plugin) Destroy() error { return nil }
