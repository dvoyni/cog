package canvas_test

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// artPath is the path addArtFiles puts a picture of 16 texels square at,
// transparent but for a block of opaque red 6 wide and 4 tall from 7, 3, and
// artSheetPath a sheet of two such frames side by side, the first holding
// a block of opaque blue from 1, 1, the second the red block.
const (
	artPath      = "art.png"
	artSheetPath = "artsheet.png"
)

// artBlock is the red block's rectangle in its frame.
var artBlock = image.Rect(7, 3, 13, 7)

// addArtFiles adds artPath and artSheetPath to files.
func addArtFiles(t testing.TB, files fstest.MapFS) {
	t.Helper()
	art := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	sheet := image.NewNRGBA(image.Rect(0, 0, 32, 16))
	for y := artBlock.Min.Y; y < artBlock.Max.Y; y++ {
		for x := artBlock.Min.X; x < artBlock.Max.X; x++ {
			art.SetNRGBA(x, y, opaqueRed)
			sheet.SetNRGBA(16+x, y, opaqueRed)
			sheet.SetNRGBA(x-6, y-2, opaqueBlue)
		}
	}
	addImageFile(t, files, artPath, art)
	addImageFile(t, files, artSheetPath, sheet)
}

func TestATrimmedSpriteStaysWhereItWasEachFrameOfASheetTrimmedApartAndEqualConvertersShareTheirTexels(t *testing.T) {
	// The art and the second frame of its sheet are drawn plain and
	// trimmed to the red block, 2 units a texel, about two pivots, so the
	// block of each trimmed sprite, if it were placed at its frame's top
	// left, would lie 7 texels left and 3 up. Each shows red at its block's
	// corner texels, and black at the texels just outside them. The art's
	// two trimmed images, of one converter, share their texels, converted
	// once, and the sheet's frame is converted on its own, so its first
	// frame's blue is not part of its bounds.
	files := fstest.MapFS{}
	addArtFiles(t, files)
	convertedCount := &atomic.Int32{}
	trim := trimmer{convertedCount: convertedCount}
	bottomLeft := canvas.ImageOptions{Pivot: canvas.PivotBottomLeft}
	trimmedBottomLeft := canvas.ImageOptions{Pivot: canvas.PivotBottomLeft, Convert: trim}
	offCentre := canvas.Pivot{Fraction: m.Vec2{X: 0.25}, Texels: m.Vec2{Y: -3}}
	sheet := canvas.ImageOptions{Sheet: canvas.Grid{Columns: 2}, Pivot: offCentre}
	trimmedSheet := canvas.ImageOptions{Sheet: canvas.Grid{Columns: 2}, Pivot: offCentre, Convert: trim}
	size := m.Vec2{X: 32, Y: 32}
	sprites := []placedSprite{
		{"the plain art", canvas.ImageFromFile(artPath, bottomLeft), 0, m.Translation2x3(-150, -20), size, canvas.PivotBottomLeft},
		{"the trimmed art", canvas.ImageFromFile(artPath, trimmedBottomLeft), 0, m.Translation2x3(-110, -20), size, canvas.PivotBottomLeft},
		{"the trimmed art about its centre", canvas.ImageFromFile(artPath, canvas.ImageOptions{Convert: trim}), 0, m.Translation2x3(-50, -40), size, canvas.PivotCenter},
		{"the plain sheet's second frame", canvas.ImageFromFile(artSheetPath, sheet), 1, m.Translation2x3(10, -40), size, offCentre},
		{"the trimmed sheet's second frame", canvas.ImageFromFile(artSheetPath, trimmedSheet), 1, m.Translation2x3(60, -40), size, offCentre},
		{"the trimmed sheet's second frame, turned a quarter", canvas.ImageFromFile(artSheetPath, trimmedSheet), 1, m.Translation2x3(120, 60).Mul(m.Rotation2x3(math.Pi / 2)), size, offCentre},
	}

	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), nil, func(list canvas.CommandList) {
		for _, sprite := range sprites {
			sprite.draw(list)
		}
	}, &fileMounter{files: files})

	frameSize := m.Vec2{X: 16, Y: 16}
	for _, sprite := range sprites {
		inside := []image.Point{artBlock.Min, {X: artBlock.Max.X - 1, Y: artBlock.Min.Y}, {X: artBlock.Min.X, Y: artBlock.Max.Y - 1}, artBlock.Max.Sub(image.Point{X: 1, Y: 1})}
		outside := []image.Point{artBlock.Min.Sub(image.Point{X: 1}), artBlock.Min.Sub(image.Point{Y: 1}), artBlock.Max, {X: artBlock.Max.X, Y: artBlock.Min.Y}}
		for _, texel := range inside {
			if shown := picture.at(sprite.findFramePoint(frameSize, texel.X, texel.Y)); shown != opaqueRed {
				t.Errorf("%s's texel %v is %v, want the block's red", sprite.name, texel, shown)
			}
		}
		for _, texel := range outside {
			if shown := picture.at(sprite.findFramePoint(frameSize, texel.X, texel.Y)); shown != opaqueBlack {
				t.Errorf("%s's texel %v is %v, want black, outside the block", sprite.name, texel, shown)
			}
		}
	}
	if converted := convertedCount.Load(); converted != 2 {
		t.Errorf("the trimmer converted %d frames, want 2: the art once for both its images, and the sheet's second frame", converted)
	}
}

// fieldArtPath is the path of a picture of 16 texels square, transparent
// but for an opaque white rectangle from its left edge, fieldArt.
const fieldArtPath = "fieldart.png"

// fieldArt is fieldArtPath's rectangle.
var fieldArt = image.Rect(0, 3, 10, 12)

func TestADistanceFieldOfASpriteLinesUpWithItAtAnyPivotAndSize(t *testing.T) {
	// The art's field, of a spread of 4, is drawn about four pivots, at its
	// natural size, at twice it, stretched and mirrored, its texels read at
	// their nearest, white over black, so each pixel's grey is its alpha.
	// At each texel probed, in the frame's own texels, the grey is the
	// field's alpha for the texel's distance to the rectangle's edge, ½ on
	// it: just inside and just outside each edge, on a corner's diagonal,
	// deep inside, too far outside to show, and 2 texels left of the
	// frame, where the field grown past it reaches.
	files := fstest.MapFS{}
	addImageFile(t, files, fieldArtPath, newRectArt(fieldArt))
	field := canvas.DistanceField{Spread: 4}
	makeField := func(pivot canvas.Pivot) canvas.Image {
		return canvas.ImageFromFile(fieldArtPath, canvas.ImageOptions{Pivot: pivot, Convert: field})
	}
	offCorner := canvas.Pivot{Fraction: m.Vec2{X: 0.5, Y: 0.5}, Texels: m.Vec2{X: -3, Y: -2}}
	sprites := []placedSprite{
		{"the field about its centre", makeField(canvas.PivotCenter), 0, m.Translation2x3(-110, -70), m.Vec2{}, canvas.PivotCenter},
		{"the field twice its size about its top left", makeField(canvas.PivotTopLeft), 0, m.Translation2x3(-60, -100), m.Vec2{X: 32}, canvas.PivotTopLeft},
		{"the field stretched about a point off its corner", makeField(offCorner), 0, m.Translation2x3(60, -20), m.Vec2{X: 24, Y: 40}, offCorner},
		{"the field mirrored about its bottom", makeField(canvas.PivotBottom), 0, m.TRS2x3(m.Vec2{X: -60, Y: 90}, 0, m.Vec2{X: -1, Y: 1}), m.Vec2{X: 32, Y: 32}, canvas.PivotBottom},
	}

	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), nil, func(list canvas.CommandList) {
		for _, sprite := range sprites {
			sprite.draw(list)
		}
	}, &fileMounter{files: files})

	frameSize := m.Vec2{X: 16, Y: 16}
	probed := []image.Point{
		{X: -2, Y: 7}, {X: -1, Y: 7}, {X: 0, Y: 7}, {X: 9, Y: 7}, {X: 10, Y: 7}, {X: 12, Y: 7},
		{X: 5, Y: 2}, {X: 5, Y: 3}, {X: 5, Y: 14}, {X: 11, Y: 13}, {X: 13, Y: 15}, {X: 5, Y: 7},
	}
	for _, sprite := range sprites {
		for _, texel := range probed {
			distance := findRectDistance(float64(texel.X)+0.5, float64(texel.Y)+0.5, fieldArt)
			want := findFieldAlpha(distance, 4)
			shown := picture.at(sprite.findFramePoint(frameSize, texel.X, texel.Y))
			grey := int(shown.R)
			if findMagnitude(grey-want) > 2 || shown.G != shown.R || shown.B != shown.R {
				t.Errorf("%s's texel %v is %v, want the grey %d, of the distance %.2f, within 2", sprite.name, texel, shown, want, distance)
			}
		}
	}
}

func TestImagesOfEqualDistanceFieldsDrawFromOneAtlasInOneBatchAndThoseOfAnotherSpreadFromOneOfTheirOwn(t *testing.T) {
	// Two files baked by equal converters, of a spread of 8, are drawn
	// first, then one of a spread of 16: the first two share their
	// converter's atlas and batch, and the third draws from an atlas of its
	// own, none of them the plain sprite atlas.
	files := fstest.MapFS{}
	addArtFiles(t, files)
	addImageFile(t, files, fieldArtPath, newRectArt(fieldArt))
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &fileMounter{files: files})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	art := canvas.ImageFromFile(artPath, canvas.ImageOptions{Convert: canvas.DistanceField{Spread: 8}})
	rectangle := canvas.ImageFromFile(fieldArtPath, canvas.ImageOptions{Convert: canvas.DistanceField{Spread: 8}})
	widerArt := canvas.ImageFromFile(artPath, canvas.ImageOptions{Convert: canvas.DistanceField{Spread: 16}})
	h.Record(makeSpriteRecord(art, rectangle, widerArt))

	h.Step()

	want := []convertedBatch{
		{atlas: fmt.Sprintf("canvas sprites converted by canvas.DistanceField{Spread:8}, of format %d", render.TextureFormatRGBA8UnormSrgb), orders: []int32{0, 1}},
		{atlas: fmt.Sprintf("canvas sprites converted by canvas.DistanceField{Spread:16}, of format %d", render.TextureFormatRGBA8UnormSrgb), orders: []int32{2}},
	}
	if got := readConvertedBatches(h); !reflect.DeepEqual(got, want) {
		t.Errorf("the fields batched as %+v, want %+v: those of equal converters from one atlas in one batch, the other spread's from its own", got, want)
	}
}

func TestATrimmedSpriteBatchesApartFromPlainSpritesAndShapes(t *testing.T) {
	// A shape, the plain art, its trimmed image and the plain art again:
	// the trimmed sprite draws from its trimmer's atlas, breaking the batch
	// the shape and the plain sprites would make of the plain sprite atlas.
	files := fstest.MapFS{}
	addArtFiles(t, files)
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &fileMounter{files: files})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	plain := canvas.ImageFromFile(artPath)
	trimmed := canvas.ImageFromFile(artPath, canvas.ImageOptions{Convert: trimmer{convertedCount: &atomic.Int32{}}})
	h.Record(func(list canvas.CommandList) {
		list.DrawShape(canvas.Shape{Rect: m.Rect{X: -4, Y: -4, Width: 8, Height: 8}, Tint: m.White, Order: 0})
		list.DrawSprite(canvas.Sprite{Image: plain, Order: 1})
		list.DrawSprite(canvas.Sprite{Image: trimmed, Order: 2})
		list.DrawSprite(canvas.Sprite{Image: plain, Order: 3})
	})

	h.Step()

	batches := readConvertedBatches(h)
	if len(batches) != 3 || !slices.Equal(batches[0].orders, []int32{0, 1}) || !slices.Equal(batches[1].orders, []int32{2}) || !slices.Equal(batches[2].orders, []int32{3}) {
		t.Fatalf("the entries batched as %+v, want the shape and the plain sprite, the trimmed sprite, then the plain sprite", batches)
	}
	if batches[0].atlas != "canvas sprites sRGB" || batches[2].atlas != "canvas sprites sRGB" || !strings.HasPrefix(batches[1].atlas, "canvas sprites converted by canvas_test.trimmer{") {
		t.Errorf("the batches drew from %q, %q and %q, want the plain sRGB atlas, the trimmer's and the plain one", batches[0].atlas, batches[1].atlas, batches[2].atlas)
	}
}

func TestAConvertedImageTooLargeForAPageIsDrawnFromATextureOfItsOwnMadeOfItsTexelsItsMipsMadeBeforeTheCamerasPass(t *testing.T) {
	// The art's field of a spread of 16 is 48 texels square, past a page
	// of 32: it is made a texture of its own, written its 48×48 texels as
	// it is made, its levels 1 and 2 drawn before the camera's pass, beside
	// the white texel's page's.
	files := fstest.MapFS{}
	addArtFiles(t, files)
	h := canvastest.NewHarness(t)
	h.Spawn(nil, &canvas.Config{AtlasPageSize: 32}, &fileMounter{files: files})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	field := canvas.ImageFromFile(artPath, canvas.ImageOptions{Convert: canvas.DistanceField{Spread: 16}})
	h.Record(makeSpriteRecord(field))

	h.Step()

	frame := h.Frames()[0]
	own := findLoadedTextures(frame.Render, artPath)
	if len(own) != 1 {
		t.Fatalf("the frame first drawing the field made %v labelled with its path, want its texture", own)
	}
	if writes := findTextureWrites(frame.Render, own[0]); len(writes) != 1 || writes[0].Region.Width != 48 || writes[0].Region.Height != 48 {
		t.Errorf("the field's texture was written %+v, want its 48×48 texels", writes)
	}
	if batches := frame.Cameras[0].Batches; len(batches) != 1 || batches[0].Texture != own[0] {
		t.Errorf("the camera batched %+v, want the field drawn from its texture", batches)
	}
	want := []passSummary{
		{mipsLabel, artPath, 1, 0, 1}, {mipsLabel, artPath, 2, 0, 1},
		{mipsLabel, spriteAtlasLabel, 1, 0, 1}, {mipsLabel, spriteAtlasLabel, 2, 0, 1},
		{"canvas camera", "", 0, 0, 1},
	}
	if got := findPassSummaries(findCanvasLists(frame.Render)[0], findTextureLabels(frame.Render)); !slices.Equal(got, want) {
		t.Errorf("the frame drew %+v, want %+v: the field's mips, then the white texel's page's, then the camera's pass", got, want)
	}
}

func TestAConverterNotComparableAndOneOfARenderTexturesImageAreRefusedTheirImagesDrawnUnconverted(t *testing.T) {
	// The art, given a recolourer, which is not comparable, and an image of
	// a render texture given a trimmer, are drawn as they are, each
	// reported once.
	files := fstest.MapFS{}
	addArtFiles(t, files)
	recoloured := canvas.ImageFromFile(artPath, canvas.ImageOptions{Convert: recolourer{colours: []color.NRGBA{opaqueGreen}}})
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindConverterRefused)
	ofTexture := &textureRecorder{
		made: []madeTexture{makeTileTexture()},
		record: func(list canvas.CommandList, _ canvas.Assets, textures []render.Texture) {
			list.DrawSprite(canvas.Sprite{Image: recoloured, Transform: m.Translation2x3(-40, 0), Size: m.Vec2{X: 32, Y: 32}, Filter: canvas.FilterNearest})
			trimmed := canvas.ImageFromTexture(textures[0], canvas.ImageOptions{Convert: trimmer{convertedCount: &atomic.Int32{}}})
			list.DrawSprite(canvas.Sprite{Image: trimmed, Transform: m.Translation2x3(40, 0), Size: m.Vec2{X: 8, Y: 8}, Filter: canvas.FilterNearest})
		},
	}

	picture := captureHarnessDrawing(t, h, nil, func(canvas.CommandList) {}, &fileMounter{files: files}, ofTexture)

	unconverted := placedSprite{transform: m.Translation2x3(-40, 0), size: m.Vec2{X: 32, Y: 32}}
	if shown := picture.at(unconverted.findFramePoint(m.Vec2{X: 16, Y: 16}, artBlock.Min.X, artBlock.Min.Y)); shown != opaqueRed {
		t.Errorf("the art given a converter that is not comparable shows %v in its block, want it unconverted, red", shown)
	}
	picture.expect(t, []probe{
		{"the render texture's image's top left", m.Vec2{X: 37, Y: -3}, opaqueRed},
		{"the render texture's image's bottom right", m.Vec2{X: 43, Y: 3}, opaqueWhite},
	})
	if refusedCount := countReports(h, canvas.ReportKindConverterRefused); refusedCount != 2 {
		t.Errorf("the converters were reported refused %d times, want twice, once for each image", refusedCount)
	}
}

func TestAConvertedImageWhoseFileFailsToDecodeLoadsThroughRenderWhichDrawsItMagenta(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Harness.ExpectReport(render.ReportKindLoadFailed)
	missing := canvas.ImageFromFile("missing.png", canvas.ImageOptions{Convert: canvas.DistanceField{}})

	picture := captureHarnessDrawing(t, h, nil, func(list canvas.CommandList) {
		list.DrawSprite(canvas.Sprite{Image: missing, Size: m.Vec2{X: 10, Y: 10}})
	}, &fileMounter{files: fstest.MapFS{}})

	picture.expect(t, []probe{{"the missing file's image", m.Vec2{}, color.NRGBA{R: 255, B: 255, A: 255}}})
	if reports := h.Harness.Reports(); len(reports) != 1 || reports[0].Kind != render.ReportKindLoadFailed {
		t.Errorf("the frames drawing a converted image of a missing file reported %v, want render's one report of its load failing", reports)
	}
}

// findConvertedPuts answers the copies frame's command lists recorded from
// a buffer into the atlases of converters: each converted image's texels
// put into its cell.
func findConvertedPuts(frame render.Inspection) []render.InspectedCopy {
	labels := findTextureLabels(frame)
	var puts []render.InspectedCopy
	for _, list := range findCanvasLists(frame) {
		for _, copied := range list.Copies {
			if copied.SourceBuffer.Buffer != (render.Buffer{}) && strings.HasPrefix(labels[copied.DestinationTexture], "canvas sprites converted by ") {
				puts = append(puts, copied)
			}
		}
	}
	return puts
}

func TestAPreloadedConvertedImageIsDecodedInThePreloadingJob(t *testing.T) {
	// The art's trimmed image is preloaded, its file then taken from the
	// FS, and drawn: it is packed from what the preload decoded, its output
	// put into its trimmer's atlas, reporting no failure to load.
	files := fstest.MapFS{}
	addArtFiles(t, files)
	convertedCount := &atomic.Int32{}
	trimmed := canvas.ImageFromFile(artPath, canvas.ImageOptions{Convert: trimmer{convertedCount: convertedCount}})
	h := canvastest.NewHarness(t)
	tool := &inspectionTool{duringFrame: func(frameIndex int, _ metronome.Beat, assets canvas.Assets, _ render.Frame) {
		if frameIndex == 0 {
			assets.Preload(trimmed)
		}
	}}
	h.Spawn(nil, nil, &fileMounter{files: files}, tool)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	delete(files, artPath)
	h.Record(makeSpriteRecord(trimmed))
	h.Step()

	if converted := convertedCount.Load(); converted != 1 {
		t.Errorf("the trimmer converted %d frames, want the preloaded art's", converted)
	}
	if made := findLoadedTextures(h.Frames()[1].Render, artPath); len(made) != 0 {
		t.Errorf("the frame first drawing the art loaded %v of it, want none, its output put into its trimmer's atlas", made)
	}
	if puts := findConvertedPuts(h.Frames()[1].Render); len(puts) != 1 {
		t.Errorf("the frame first drawing the art put %+v into converters' atlases, want its output's cell", puts)
	}
}

func TestTheConvertedFramesOfOneFileFirstDrawnInOneFrameDecodeItOnce(t *testing.T) {
	files := fstest.MapFS{}
	addArtFiles(t, files)
	counting := &countingFS{files: files, openCounts: map[string]int{}}
	sheet := canvas.ImageFromFile(artSheetPath, canvas.ImageOptions{Sheet: canvas.Grid{Columns: 2}, Convert: trimmer{convertedCount: &atomic.Int32{}}})
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &fileMounter{files: counting})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Record(func(list canvas.CommandList) {
		list.DrawSprite(canvas.Sprite{Image: sheet})
		list.DrawSprite(canvas.Sprite{Image: sheet, Frame: 1})
	})
	h.Step()

	if opens := counting.countOpens(artSheetPath); opens != 1 {
		t.Errorf("the sheet's file was opened %d times for its two frames, want once", opens)
	}
}
