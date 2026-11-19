package canvas_test

import (
	"image/color"
	"slices"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// levelRedPath is a path under the prefix levelPrefix, which the unload
// tests put a red image at.
const (
	levelPrefix  = "level1/"
	levelRedPath = levelPrefix + "red.png"
)

// findLoadedTextures answers the textures frame made or loaded labelled
// label.
func findLoadedTextures(frame render.Inspection, label string) []render.Texture {
	var textures []render.Texture
	for _, asset := range frame.MadeAssets {
		if asset.Texture != (render.Texture{}) && asset.Label == label {
			textures = append(textures, asset.Texture)
		}
	}
	return textures
}

func TestAPreloadedImageIsLoadedBeforeItsFirstDrawWhichPacksItsSource(t *testing.T) {
	h := canvastest.NewHarness(t)
	describer := &textureDescriber{}
	red := canvas.ImageFromFile(redPath)
	tool := &inspectionTool{duringFrame: func(frameIndex int, _ metronome.Beat, assets canvas.Assets, _ render.Frame) {
		if frameIndex == 0 {
			assets.Preload(red)
			assets.Preload(red)
		}
	}}
	h.Spawn(nil, nil, &fileMounter{files: makeAtlasFiles(t)}, describer, tool)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Record(makeSpriteRecord(red))
	h.Step()

	frames := h.Frames()
	preloaded := findLoadedTextures(frames[0].Render, redPath)
	if len(preloaded) != 1 {
		t.Fatalf("the frame preloading red twice loaded %v of it, want one texture", preloaded)
	}
	if loaded := findLoadedTextures(frames[1].Render, redPath); len(loaded) != 0 {
		t.Errorf("the frame first drawing red loaded %v of it again, want the preloaded texture alone", loaded)
	}
	labels := findTextureLabels(findRenderFrames(frames)...)
	packings := findPackings(t, frames[1].Render, labels)
	if len(packings) != 1 || packings[0].source != redPath {
		t.Fatalf("the frame first drawing red packed %+v, want red", packings)
	}
	pack := findPass(t, findCanvasLists(frames[1].Render)[0], "canvas pack")
	if source := findParameter(t, pack.Draws[0].DrawParameters, "canvas_atlas").TextureView.Texture; source != preloaded[0] {
		t.Errorf("red was packed from %v, want the texture preloaded, %v", source, preloaded[0])
	}
	describer.describe(preloaded[0])
	h.Step()
	h.Step()
	if described := describer.describedTextures(); len(described) != 1 || described[0] != (render.TextureDescriptor{}) {
		t.Errorf("the preloaded texture is %+v once packed, want it released, as no acquire holds it", described)
	}
}

func TestUnloadingAPrefixFreesItsImagesRegionsAndALaterDrawLoadsThemAgain(t *testing.T) {
	// A page of 16 texels holds the white texel's cell and two cells of 8,
	// so at a ceiling of one page, the level's red, unloaded by its prefix
	// in a frame drawing it beside red, is loaded again and packed back
	// into the region it freed, red left packed; neither is evicted, as the
	// frame draws both.
	h := canvastest.NewHarness(t)
	files := makeAtlasFiles(t)
	addSolidFile(t, files, levelRedPath, 4, 4, color.NRGBA{R: 255, A: 255})
	tool := &inspectionTool{duringFrame: func(frameIndex int, _ metronome.Beat, assets canvas.Assets, _ render.Frame) {
		if frameIndex == 1 {
			assets.Unload(levelPrefix)
		}
	}}
	h.Spawn(nil, &canvas.Config{AtlasPageSize: 16, AtlasLayerCeiling: 1}, &fileMounter{files: files}, tool)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	record := makeSpriteRecord(canvas.ImageFromFile(levelRedPath), canvas.ImageFromFile(redPath))
	for range 3 {
		h.Record(record)
		h.Step()
	}

	frames := h.Frames()
	labels := findTextureLabels(findRenderFrames(frames)...)
	for index, want := range [][]string{{levelRedPath, redPath}, {levelRedPath}, nil} {
		var packed []string
		for _, packing := range findPackings(t, frames[index].Render, labels) {
			packed = append(packed, packing.source)
		}
		if !slices.Equal(packed, want) {
			t.Errorf("frame %d packed %v, want %v", index, packed, want)
		}
		if atlases := findBatchAtlases(frames[index], labels); !slices.Equal(atlases, []string{"canvas sprites sRGB"}) {
			t.Errorf("frame %d drew from %v, want the sprite atlas alone", index, atlases)
		}
	}
	if loaded := findLoadedTextures(frames[1].Render, levelRedPath); len(loaded) != 1 {
		t.Errorf("the frame unloading the level's red and drawing it loaded %v of it, want its file loaded again", loaded)
	}
}

func TestUnloadingAnImageDrawnFromATextureOfItsOwnReleasesTheTexture(t *testing.T) {
	// The blue image of 32 texels is larger than half a page of 32, so it is
	// drawn from a texture of its own.
	h := canvastest.NewHarness(t)
	describer := &textureDescriber{}
	tool := &inspectionTool{duringFrame: func(frameIndex int, _ metronome.Beat, assets canvas.Assets, _ render.Frame) {
		if frameIndex == 1 {
			assets.Unload(bluePath)
		}
	}}
	h.Spawn(nil, &canvas.Config{AtlasPageSize: 32}, &fileMounter{files: makeAtlasFiles(t)}, describer, tool)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Record(makeSpriteRecord(canvas.ImageFromFile(bluePath)))
	h.Step()
	own := h.Frames()[0].Cameras[0].Batches[0].Atlas
	describer.describe(own)

	h.Step()
	h.Step()

	if described := describer.describedTextures(); len(described) != 1 || described[0] != (render.TextureDescriptor{}) {
		t.Errorf("the image's own texture is %+v once unloaded, want it released", described)
	}
}

func TestImageSizeAnswersAnImagesSizeFromItsHeaderBeforeAnyDrawAndFalseForAMissingFile(t *testing.T) {
	h := canvastest.NewHarness(t)
	type sizeAnswer struct {
		size m.Vec2
		read bool
	}
	images := []canvas.Image{canvas.ImageFromFile(redPath), canvas.ImageFromFile(bluePath), {}, canvas.ImageFromFile("missing.png")}
	var answers [][]sizeAnswer
	tool := &inspectionTool{duringFrame: func(frameIndex int, _ metronome.Beat, assets canvas.Assets, _ render.Frame) {
		var frameAnswers []sizeAnswer
		for _, image := range images {
			size, read := assets.ImageSize(image)
			frameAnswers = append(frameAnswers, sizeAnswer{size, read})
		}
		answers = append(answers, frameAnswers)
	}}
	h.Spawn(nil, nil, &fileMounter{files: makeAtlasFiles(t)}, tool)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.ExpectReport(canvas.ReportKindImageHeaderUnread)

	h.Step()
	h.Step()

	want := []sizeAnswer{{m.Vec2{X: 4, Y: 4}, true}, {m.Vec2{X: 32, Y: 32}, true}, {m.Vec2{X: 1, Y: 1}, true}, {}}
	if len(answers) != 2 || !slices.Equal(answers[0], want) || !slices.Equal(answers[1], want) {
		t.Errorf("ImageSize answered %+v in two frames, want %+v in each: red's and blue's headers, the white texel's one texel, and false for the missing file", answers, want)
	}
	if loaded := slices.Concat(findLoadedTextures(h.Frames()[0].Render, redPath), findLoadedTextures(h.Frames()[0].Render, bluePath)); len(loaded) != 0 {
		t.Errorf("ImageSize loaded %v, want no image loaded", loaded)
	}
	if unreadCount := countReports(h, canvas.ReportKindImageHeaderUnread); unreadCount != 1 {
		t.Errorf("the missing file was reported %d times, want once", unreadCount)
	}
}
