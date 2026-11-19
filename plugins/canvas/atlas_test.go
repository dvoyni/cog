package canvas_test

import (
	"fmt"
	"image"
	"image/color"
	"reflect"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// redPath, greenPath, bluePath and stripesPath are the paths makeAtlasFiles
// puts its images at.
const (
	redPath     = "red.png"
	greenPath   = "green.png"
	bluePath    = "blue.png"
	stripesPath = "stripes.png"
)

// makeAtlasFiles answers the quadrants' files, with four images of opaque
// texels beside them: red, green and blue, each solid, of 4×4, 4×4 and
// 32×32 texels, at redPath, greenPath and bluePath, and a row of 4 texels,
// red, green, blue and white, at stripesPath.
func makeAtlasFiles(t testing.TB) fstest.MapFS {
	t.Helper()
	files := makeQuadrantsFiles(t)
	addSolidFile(t, files, redPath, 4, 4, color.NRGBA{R: 255, A: 255})
	addSolidFile(t, files, greenPath, 4, 4, color.NRGBA{G: 255, A: 255})
	addSolidFile(t, files, bluePath, 32, 32, color.NRGBA{B: 255, A: 255})
	stripes := image.NewNRGBA(image.Rect(0, 0, 4, 1))
	for x, stripe := range []color.NRGBA{{R: 255, A: 255}, {G: 255, A: 255}, {B: 255, A: 255}, {R: 255, G: 255, B: 255, A: 255}} {
		stripes.SetNRGBA(x, 0, stripe)
	}
	addImageFile(t, files, stripesPath, stripes)
	return files
}

// findTextureLabels answers the label of each texture frames made or loaded.
func findTextureLabels(frames ...render.Inspection) map[render.Texture]string {
	labels := map[render.Texture]string{}
	for _, frame := range frames {
		for _, asset := range frame.MadeAssets {
			labels[asset.Texture] = asset.Label
		}
	}
	return labels
}

// atlasPacking is a cell a packing pass filled: the atlas it filled, its
// page, and the label of the source it drew from.
type atlasPacking struct {
	atlas  render.Texture
	layer  uint32
	source string
}

// findPackings answers the cells the packing passes of frame's canvas lists
// filled, pass after pass, each source labelled as labels has it.
func findPackings(t *testing.T, frame render.Inspection, labels map[render.Texture]string) []atlasPacking {
	t.Helper()
	var packings []atlasPacking
	for _, list := range findCanvasLists(frame) {
		for _, pass := range list.Passes {
			if pass.Label != "canvas pack" {
				continue
			}
			target := pass.ColorTargets[0]
			for _, draw := range pass.Draws {
				source := findParameter(t, draw.DrawParameters, "canvas_atlas").TextureView.Texture
				packings = append(packings, atlasPacking{atlas: target.Texture, layer: target.Layer, source: labels[source]})
			}
		}
	}
	return packings
}

// findBatchAtlases answers the label, as labels has it, of the atlas of each
// batch of frame's first camera.
func findBatchAtlases(frame canvas.Inspection, labels map[render.Texture]string) []string {
	var atlases []string
	for _, inspectedBatch := range frame.Cameras[0].Batches {
		atlases = append(atlases, labels[inspectedBatch.Atlas])
	}
	return atlases
}

// findRenderFrames answers render's inspection of each of frames.
func findRenderFrames(frames []canvas.Inspection) []render.Inspection {
	var renderFrames []render.Inspection
	for _, frame := range frames {
		renderFrames = append(renderFrames, frame.Render)
	}
	return renderFrames
}

// countReports answers how many of the reports h has checked so far are of
// kind.
func countReports(h *canvastest.Harness, kind canvas.ReportKind) int {
	count := 0
	for _, report := range h.Reports() {
		if report.Kind == kind {
			count++
		}
	}
	return count
}

// recordWhiteFill records a white fill of 20 units square about the
// origin into list.
func recordWhiteFill(list canvas.DrawList) {
	list.DrawShape(canvas.Shape{Rect: m.Rect{X: -10, Y: -10, Width: 20, Height: 20}, Tint: m.White})
}

// makeSpriteRecord answers a record of a sprite of each of images, in their
// order.
func makeSpriteRecord(images ...canvas.Image) func(list canvas.DrawList) {
	return func(list canvas.DrawList) {
		for order, image := range images {
			list.DrawSprite(canvas.Sprite{Image: image, Order: int32(order)})
		}
	}
}

func TestSpritesOfDifferentImagesAndTheShapesBesideThemDrawFromTheSpriteAtlasAsOneBatch(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &fileMounter{files: makeAtlasFiles(t)})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	red := canvas.ImageFromFile(redPath)
	blue := canvas.ImageFromFile(bluePath)
	linearRed := canvas.ImageFromFile(redPath, canvas.ImageOptions{ColorSpace: canvas.ColorSpaceLinear})
	fill := m.Rect{X: -4, Y: -4, Width: 8, Height: 8}
	h.Record(func(list canvas.DrawList) {
		list.DrawShape(canvas.Shape{Rect: fill, Tint: m.White, Order: 1})
		// A sprite of the zero image draws the white texel, as shapes do,
		// which no filter changes.
		list.DrawSprite(canvas.Sprite{Size: m.Vec2{X: 8, Y: 8}, Order: 2, Filter: canvas.FilterNearest})
		list.DrawSprites([]canvas.Sprite{{Image: red, Order: 3}, {Image: blue, Order: 4}})
		list.DrawShape(canvas.Shape{Rect: fill, Tint: m.White, Order: 5})
		list.DrawSprite(canvas.Sprite{Image: red, Order: 6, Filter: canvas.FilterNearest})
		list.DrawSprite(canvas.Sprite{Image: linearRed, Order: 7})
	})

	h.Step()

	frame := h.Frames()[0]
	labels := findTextureLabels(frame.Render)
	type batchShape struct {
		atlas         string
		filter        canvas.Filter
		orders        []int32
		instanceCount uint32
	}
	var got []batchShape
	for _, inspectedBatch := range frame.Cameras[0].Batches {
		shape := batchShape{atlas: labels[inspectedBatch.Atlas], filter: inspectedBatch.Filter, instanceCount: inspectedBatch.InstanceCount}
		for _, entry := range inspectedBatch.Entries {
			shape.orders = append(shape.orders, entry.Order)
		}
		got = append(got, shape)
	}
	want := []batchShape{
		{atlas: "canvas sprites sRGB", filter: canvas.FilterLinear, orders: []int32{1, 2, 3, 4, 5}, instanceCount: 5},
		{atlas: "canvas sprites sRGB", filter: canvas.FilterNearest, orders: []int32{6}, instanceCount: 1},
		{atlas: "canvas sprites linear", filter: canvas.FilterLinear, orders: []int32{7}, instanceCount: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the entries batched as %+v, want %+v: in sort order, the shapes and the sprites of one atlas and filter together, broken where the atlas or the filter changes", got, want)
	}
	var textures []render.Texture
	var samplers []render.SamplerDescriptor
	for _, draw := range findPass(t, findCanvasLists(frame.Render)[0], "canvas camera").Draws {
		textures = append(textures, findParameter(t, draw.DrawParameters, "canvas_atlas").TextureView.Texture)
		samplers = append(samplers, findParameter(t, draw.DrawParameters, "canvas_sampler").Sampler)
	}
	batches := frame.Cameras[0].Batches
	if len(batches) != 3 || !slices.Equal(textures, []render.Texture{batches[0].Atlas, batches[1].Atlas, batches[2].Atlas}) {
		t.Errorf("the batches drew %v, want each batch's atlas", textures)
	}
	// The inspection spells a nearest filter, its default, zero.
	linear := render.SamplerDescriptor{MagFilter: render.FilterModeLinear, MinFilter: render.FilterModeLinear}
	if want := []render.SamplerDescriptor{linear, {}, linear}; !slices.Equal(samplers, want) {
		t.Errorf("the batches read their atlases through %+v, want %+v, each batch's filter", samplers, want)
	}
}

func TestAnImageIsPackedAtItsFirstDrawBeforeTheCamerasPassItsSourceReleasedAfter(t *testing.T) {
	h := canvastest.NewHarness(t)
	describer := &textureDescriber{}
	h.Spawn(nil, nil, &fileMounter{files: makeAtlasFiles(t)}, describer)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	red := canvas.ImageFromFile(redPath)
	blue := canvas.ImageFromFile(bluePath)
	// An image differing from red in its pivot alone shares its region.
	pivotedRed := canvas.ImageFromFile(redPath, canvas.ImageOptions{Pivot: canvas.PivotTopLeft})
	record := func(list canvas.DrawList) {
		list.DrawSprite(canvas.Sprite{Image: red})
		list.DrawSprite(canvas.Sprite{Image: blue, Order: 1})
		list.DrawSprite(canvas.Sprite{Image: pivotedRed, Order: 2})
	}
	h.Record(record)

	h.Step()

	frame := h.Frames()[0].Render
	lists := findCanvasLists(frame)
	if len(lists) != 1 || len(lists[0].Passes) != 2 || lists[0].Passes[0].Label != "canvas pack" || lists[0].Passes[1].Label != "canvas camera" {
		t.Fatalf("the first frame drawing two images began %+v, want one list of a pass packing them, then the camera's pass", lists)
	}
	pack := lists[0].Passes[0]
	camera := lists[0].Passes[1]
	if len(camera.Draws) != 1 {
		t.Fatalf("the camera's pass drew %d batches, want the sprites as one", len(camera.Draws))
	}
	atlas := findParameter(t, camera.Draws[0].DrawParameters, "canvas_atlas").TextureView.Texture
	if target := pack.ColorTargets[0]; len(pack.ColorTargets) != 1 || target.Texture != atlas || target.Layer != 0 || target.Clear {
		t.Errorf("the packing pass drew into %+v, want the atlas the camera draws from, its first page, kept", pack.ColorTargets)
	}
	labels := findTextureLabels(frame)
	var sources []render.Texture
	var sourceLabels []string
	for _, draw := range pack.Draws {
		source := findParameter(t, draw.DrawParameters, "canvas_atlas").TextureView.Texture
		sources = append(sources, source)
		sourceLabels = append(sourceLabels, labels[source])
	}
	if want := []string{redPath, bluePath}; !slices.Equal(sourceLabels, want) {
		t.Errorf("the packing pass drew from %v, want a draw of each image's file, %v, the pivoted red sharing red's", sourceLabels, want)
	}
	describer.describe(sources...)
	h.Record(record)
	h.Step()
	if lists := findCanvasLists(h.Frames()[1].Render); len(lists) != 1 || len(lists[0].Passes) != 1 {
		t.Errorf("the second frame drawing the images began %+v, want the camera's pass alone, packing nothing again", lists)
	}
	h.Record(record)
	h.Step()
	described := describer.describedTextures()
	if len(described) != 2 {
		t.Fatalf("the describer read %d sources, want the 2 packed", len(described))
	}
	for index, descriptor := range described {
		if descriptor != (render.TextureDescriptor{}) {
			t.Errorf("the source of %s is still held, as %+v, after its packing", sourceLabels[index], descriptor)
		}
	}
}

func TestAnImageAsLargeAsAPageTakesAnAtlasOfItsOwnExactlyItsSize(t *testing.T) {
	// The blue image of 32 texels is a page's side, larger than the default
	// OwnTextureSize of half a page, so it takes an atlas of its own, of no
	// gutter, no larger than a page.
	h := canvastest.NewHarness(t)
	describer := &textureDescriber{}
	h.Spawn(nil, &canvas.Config{AtlasPageSize: 32}, &fileMounter{files: makeAtlasFiles(t)}, describer)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	blue := canvas.ImageFromFile(bluePath)
	record := func(list canvas.DrawList) { list.DrawSprite(canvas.Sprite{Image: blue}) }
	h.Record(record)

	h.Step()

	frame := h.Frames()[0].Render
	lists := findCanvasLists(frame)
	if len(lists) != 1 || len(lists[0].Passes) != 2 || lists[0].Passes[0].Label != "canvas pack" {
		t.Fatalf("the first frame drawing the image began %+v, want one list of a pass packing it, then the camera's pass", lists)
	}
	camera := lists[0].Passes[1]
	if len(camera.Draws) != 1 {
		t.Fatalf("the camera's pass drew %d batches, want the sprite's", len(camera.Draws))
	}
	atlas := findParameter(t, camera.Draws[0].DrawParameters, "canvas_atlas").TextureView.Texture
	if label := findTextureLabels(frame)[atlas]; label != bluePath {
		t.Errorf("the sprite drew from %q, want an atlas of its own, labelled %q", label, bluePath)
	}
	if target := lists[0].Passes[0].ColorTargets[0]; target.Texture != atlas {
		t.Errorf("the packing pass drew into %+v, want the image's own atlas", target)
	}
	describer.describe(atlas)
	h.Record(record)
	h.Step()
	described := describer.describedTextures()
	if len(described) != 1 || described[0].Width != 32 || described[0].Height != 32 || described[0].LayerCount > 1 {
		t.Errorf("the image's own atlas is %+v, want one page of 32 by 32 texels, its size", described)
	}
}

func TestAPackedImageDrawsItsOwnTexelsAndLinearFilteringNeverReachesItsNeighbourAcrossTheGutter(t *testing.T) {
	// The blue image is packed below the white texel, and the stripes beside
	// the white texel, each drawn larger than its size, the blue first, so a
	// cell filled through another's quad, of another size, misplaces the
	// stripes. Read at their nearest
	// texel, the stripes show each of their own texels where it lies.
	// Linearly filtered, probed half a unit inside its edges, each image
	// reads only its own texels and its gutter, continuing them, never its
	// neighbour or the empty page.
	stripes := canvas.ImageFromFile(stripesPath)
	blue := canvas.ImageFromFile(bluePath)
	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), nil, func(list canvas.DrawList) {
		list.DrawSprite(canvas.Sprite{Image: blue, Transform: m.Translation2x3(40, 0), Size: m.Vec2{X: 40, Y: 40}})
		list.DrawSprite(canvas.Sprite{Image: stripes, Transform: m.Translation2x3(-40, -30), Size: m.Vec2{X: 40, Y: 10}})
		list.DrawSprite(canvas.Sprite{Image: stripes, Transform: m.Translation2x3(-40, 30), Size: m.Vec2{X: 40, Y: 10}, Filter: canvas.FilterNearest})
	}, &fileMounter{files: makeAtlasFiles(t)})

	opaqueRed := color.NRGBA{R: 255, A: 255}
	opaqueGreen := color.NRGBA{G: 255, A: 255}
	opaqueBlue := color.NRGBA{B: 255, A: 255}
	picture.expect(t, []probe{
		{"the nearest stripes' first texel", m.Vec2{X: -55, Y: 30}, opaqueRed},
		{"the nearest stripes' second texel", m.Vec2{X: -45, Y: 30}, opaqueGreen},
		{"the nearest stripes' third texel", m.Vec2{X: -35, Y: 30}, opaqueBlue},
		{"the nearest stripes' last texel", m.Vec2{X: -25, Y: 30}, opaqueWhite},
		{"the linear stripes' left edge", m.Vec2{X: -59.5, Y: -30}, opaqueRed},
		{"the linear stripes' right edge", m.Vec2{X: -20.5, Y: -30}, opaqueWhite},
		{"the linear stripes' top edge", m.Vec2{X: -45, Y: -34.5}, opaqueGreen},
		{"the linear stripes' bottom edge", m.Vec2{X: -35, Y: -25.5}, opaqueBlue},
		{"the linear stripes' top left corner", m.Vec2{X: -59.5, Y: -34.5}, opaqueRed},
		{"the blue sprite's left edge", m.Vec2{X: 20.5}, opaqueBlue},
		{"the blue sprite's right edge", m.Vec2{X: 59.5}, opaqueBlue},
		{"the blue sprite's top edge", m.Vec2{X: 40, Y: -19.5}, opaqueBlue},
		{"the blue sprite's bottom left corner", m.Vec2{X: 20.5, Y: 19.5}, opaqueBlue},
	})
}

func TestAnImageNoPageHoldsIsDrawnFromANewPageOrATextureOfItsOwn(t *testing.T) {
	// A page of 8 texels holds the white texel's cell of 5 and the rows
	// below it, too few for the quadrants' cell of 8, so the atlas grows a
	// second page holding them, which their instance names; past an
	// OwnTextureSize of 2, they are drawn from a texture of their own
	// instead. Either way they are read at their nearest texel beside a
	// white fill of the first page.
	for _, test := range []struct {
		name   string
		config canvas.Config
	}{
		{"from a new page", canvas.Config{AtlasPageSize: 8}},
		{"from a texture of its own", canvas.Config{AtlasPageSize: 8, OwnTextureSize: 2}},
	} {
		t.Run(test.name, func(t *testing.T) {
			quadrants := canvas.ImageFromFile(quadrantsPath)
			picture := captureHarnessDrawing(t, canvastest.NewHarness(t), &test.config, func(list canvas.DrawList) {
				list.DrawSprite(canvas.Sprite{Image: quadrants, Size: m.Vec2{X: 40, Y: 40}, Filter: canvas.FilterNearest})
				list.DrawShape(canvas.Shape{Rect: m.Rect{X: 40, Y: -10, Width: 20, Height: 20}, Tint: m.White})
			}, &fileMounter{files: makeQuadrantsFiles(t)})

			picture.expect(t, []probe{
				{"the sprite's top left", m.Vec2{X: -10, Y: -10}, color.NRGBA{R: 255, A: 255}},
				{"the sprite's top right", m.Vec2{X: 10, Y: -10}, color.NRGBA{G: 128, A: 255}},
				{"the sprite's bottom left", m.Vec2{X: -10, Y: 10}, color.NRGBA{B: 255, A: 255}},
				{"the sprite's bottom right", m.Vec2{X: 10, Y: 10}, opaqueWhite},
				{"the white fill beside it", m.Vec2{X: 50}, opaqueWhite},
			})
		})
	}
}

func TestAFullAtlasDoublesItsPagesCopiedIntoATextureOfTwiceAsMany(t *testing.T) {
	// A page of 8 texels holds the white texel's cell of 5 and no cell of 8
	// beside it, so red's cell grows the atlas to 2 pages and the
	// quadrants' to 4 in one frame, its one page copied once into the
	// last texture, the one between released uncopied. In the next frame
	// green fills the fourth page and the stripes grow it to 8, its 4
	// pages copied. Each image is packed onto the first page holding it,
	// in a pass of its own.
	h := canvastest.NewHarness(t)
	describer := &textureDescriber{}
	h.Spawn(nil, &canvas.Config{AtlasPageSize: 8}, &fileMounter{files: makeAtlasFiles(t)}, describer)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Record(makeSpriteRecord(canvas.ImageFromFile(redPath), canvas.ImageFromFile(quadrantsPath)))
	h.Step()
	h.Record(makeSpriteRecord(canvas.ImageFromFile(greenPath), canvas.ImageFromFile(stripesPath)))
	h.Step()

	frames := h.Frames()
	labels := findTextureLabels(findRenderFrames(frames)...)
	var atlasTextures []render.Texture
	for texture, label := range labels {
		if label == "canvas sprites sRGB" {
			atlasTextures = append(atlasTextures, texture)
		}
	}
	fourPages := frames[0].Cameras[0].Batches[0].Atlas
	eightPages := frames[1].Cameras[0].Batches[0].Atlas
	type pageCopy struct {
		source, destination           render.Texture
		sourceLayer, destinationLayer uint32
	}
	for index, test := range []struct {
		copies   []pageCopy
		packings []atlasPacking
	}{
		{nil, []atlasPacking{{fourPages, 1, redPath}, {fourPages, 2, quadrantsPath}}},
		{
			[]pageCopy{{fourPages, eightPages, 0, 0}, {fourPages, eightPages, 1, 1}, {fourPages, eightPages, 2, 2}, {fourPages, eightPages, 3, 3}},
			[]atlasPacking{{eightPages, 3, greenPath}, {eightPages, 4, stripesPath}},
		},
	} {
		lists := findCanvasLists(frames[index].Render)
		if len(lists) != 1 {
			t.Fatalf("frame %d began %d canvas lists, want one", index, len(lists))
		}
		var copies []pageCopy
		for _, copied := range lists[0].Copies {
			copies = append(copies, pageCopy{copied.SourceTexture, copied.DestinationTexture, copied.SourceRegion.Layer, copied.DestinationRegion.Layer})
			for _, pass := range lists[0].Passes {
				if pass.Position < copied.Position {
					t.Errorf("frame %d's pass %q is at %d, before a copy, want every pass after the copies", index, pass.Label, pass.Position)
				}
			}
		}
		if index == 0 {
			// The first frame's one copy is from the texture of one page,
			// which no frame drew from.
			if len(copies) != 1 || copies[0].destination != fourPages || copies[0].sourceLayer != 0 || copies[0].destinationLayer != 0 {
				t.Errorf("frame 0 copied %+v, want the one page once into the texture of 4", copies)
			}
		} else if !slices.Equal(copies, test.copies) {
			t.Errorf("frame %d copied %+v, want each page into the same page of the texture replacing its own: %+v", index, copies, test.copies)
		}
		if packings := findPackings(t, frames[index].Render, labels); !slices.Equal(packings, test.packings) {
			t.Errorf("frame %d packed %+v, want %+v", index, packings, test.packings)
		}
		if batches := frames[index].Cameras[0].Batches; len(batches) != 1 {
			t.Errorf("frame %d drew %d batches, want its sprites as one", index, len(batches))
		}
	}
	describer.describe(atlasTextures...)
	h.Step()
	h.Step()
	held := 0
	for index, descriptor := range describer.describedTextures() {
		switch {
		case atlasTextures[index] == eightPages && descriptor.LayerCount != 8:
			t.Errorf("the atlas's last texture is %+v, want 8 pages", descriptor)
		case atlasTextures[index] != eightPages && descriptor != (render.TextureDescriptor{}):
			t.Errorf("a texture the atlas grew out of is still held, as %+v", descriptor)
		}
		if descriptor != (render.TextureDescriptor{}) {
			held++
		}
	}
	if len(atlasTextures) != 4 || held != 1 {
		t.Errorf("the atlas made %d textures, %d still held, want 4 of 1, 2, 4 and 8 pages, the last alone held", len(atlasTextures), held)
	}
}

func TestAnAtlasAtItsCeilingEvictsTheImageDrawnLeastLatelyToPackANewOne(t *testing.T) {
	// A page of 16 texels holds the white texel's cell and two cells of 8,
	// so at a ceiling of one page, green evicts red, drawn before quadrants,
	// and red, drawn again with quadrants, evicts green, never quadrants,
	// drawn less lately but drawn in that frame, after red.
	h := canvastest.NewHarness(t)
	h.Spawn(nil, &canvas.Config{AtlasPageSize: 16, AtlasLayerCeiling: 1}, &fileMounter{files: makeAtlasFiles(t)})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	red := canvas.ImageFromFile(redPath)
	quadrants := canvas.ImageFromFile(quadrantsPath)
	green := canvas.ImageFromFile(greenPath)
	for _, record := range []func(list canvas.DrawList){makeSpriteRecord(red), makeSpriteRecord(quadrants), makeSpriteRecord(green), makeSpriteRecord(red, quadrants)} {
		h.Record(record)
		h.Step()
	}

	frames := h.Frames()
	labels := findTextureLabels(findRenderFrames(frames)...)
	spriteAtlas := frames[0].Cameras[0].Batches[0].Atlas
	for index, want := range [][]string{{redPath}, {quadrantsPath}, {greenPath}, {redPath}} {
		var packed []string
		for _, packing := range findPackings(t, frames[index].Render, labels) {
			packed = append(packed, packing.source)
			if packing.atlas != spriteAtlas || packing.layer != 0 {
				t.Errorf("frame %d packed %s into %v, page %d, want the sprite atlas's one page", index, packing.source, packing.atlas, packing.layer)
			}
		}
		if !slices.Equal(packed, want) {
			t.Errorf("frame %d packed %v, want %v", index, packed, want)
		}
		if atlases := findBatchAtlases(frames[index], labels); !slices.Equal(atlases, []string{"canvas sprites sRGB"}) {
			t.Errorf("frame %d drew from %v, want the sprite atlas alone", index, atlases)
		}
	}
}

func TestAnAtlasAtItsCeilingWithNothingToEvictDrawsAnImageFromATextureOfItsOwnReportedOnce(t *testing.T) {
	// A page of 16 texels holds the white texel's cell and two cells of 8,
	// so at a ceiling of one page, a frame drawing three images of 4 draws
	// the third from a texture of its own, the atlas full of what it
	// draws.
	h := canvastest.NewHarness(t)
	h.Spawn(nil, &canvas.Config{AtlasPageSize: 16, AtlasLayerCeiling: 1}, &fileMounter{files: makeAtlasFiles(t)})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.ExpectReport(canvas.ReportKindAtlasFull)
	record := makeSpriteRecord(canvas.ImageFromFile(redPath), canvas.ImageFromFile(quadrantsPath), canvas.ImageFromFile(greenPath))
	for range 2 {
		h.Record(record)
		h.Step()
	}

	frame := h.Frames()[0]
	labels := findTextureLabels(frame.Render)
	if atlases, want := findBatchAtlases(frame, labels), []string{"canvas sprites sRGB", greenPath}; !slices.Equal(atlases, want) {
		t.Errorf("the frame drew from %v, want the first two images from the sprite atlas and the third from its own, %v", atlases, want)
	}
	if packings := findPackings(t, h.Frames()[1].Render, labels); len(packings) != 0 {
		t.Errorf("the second frame packed %+v, want nothing, each image kept where it was packed", packings)
	}
	if fullCount := countReports(h, canvas.ReportKindAtlasFull); fullCount != 1 {
		t.Errorf("the fullCount atlas was reported %d times, want once", fullCount)
	}
}

func TestAnImageWiderOrTallerThanOwnTextureSizeIsDrawnFromATextureOfItsOwn(t *testing.T) {
	// The blue image of 32 texels is half a page of 64, packed at the
	// default OwnTextureSize, and past an OwnTextureSize of 16.
	for _, test := range []struct {
		name           string
		ownTextureSize uint32
		atlas          string
	}{
		{"at the default of half a page", 0, "canvas sprites sRGB"},
		{"past an OwnTextureSize of 16", 16, bluePath},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := canvastest.NewHarness(t)
			h.Spawn(nil, &canvas.Config{AtlasPageSize: 64, OwnTextureSize: test.ownTextureSize}, &fileMounter{files: makeAtlasFiles(t)})
			h.OpenWindow(platform.Size{Width: 64, Height: 48})
			h.Record(makeSpriteRecord(canvas.ImageFromFile(bluePath)))

			h.Step()

			frame := h.Frames()[0]
			if atlases := findBatchAtlases(frame, findTextureLabels(frame.Render)); !slices.Equal(atlases, []string{test.atlas}) {
				t.Errorf("the image was drawn from %v, want %q", atlases, test.atlas)
			}
		})
	}
}

func TestAnAtlasPageSizeBelowTheWhiteTexelsCellIsRaisedToItReportedOnce(t *testing.T) {
	// A page of 1 or 4 texels cannot hold the white texel's cell of 5, so
	// the sRGB atlas's pages are made 5 texels square, reported once; a
	// page of 5 holds it, and is neither raised nor reported. The second
	// frame lets the describer read the atlas.
	for _, test := range []struct {
		pageSize    uint32
		raisedCount int
	}{
		{1, 1},
		{4, 1},
		{5, 0},
	} {
		t.Run(fmt.Sprintf("a page of %d", test.pageSize), func(t *testing.T) {
			h := canvastest.NewHarness(t)
			describer := &textureDescriber{}
			h.Spawn(nil, &canvas.Config{AtlasPageSize: test.pageSize}, describer)
			h.OpenWindow(platform.Size{Width: 64, Height: 48})
			h.ExpectReport(canvas.ReportKindAtlasPageSizeRaised)
			h.Record(recordWhiteFill)
			h.Step()
			describer.describe(findLoadedTextures(h.Frames()[0].Render, "canvas sprites sRGB")...)
			h.Step()

			descriptors := describer.describedTextures()
			if len(descriptors) != 1 || descriptors[0].Width != 5 || descriptors[0].Height != 5 {
				t.Errorf("the sRGB atlas is %+v, want one page of 5×5 texels", descriptors)
			}
			if raised := countReports(h, canvas.ReportKindAtlasPageSizeRaised); raised != test.raisedCount {
				t.Errorf("the page side raised was reported %d times, want %d", raised, test.raisedCount)
			}
		})
	}
}

func TestShapesDrawWhiteThroughAnAtlasPageSizeBelowTheWhiteTexelsCell(t *testing.T) {
	// The white texel's cell, written into a page raised to its side, is
	// what a white fill samples.
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindAtlasPageSizeRaised)
	picture := captureHarnessDrawing(t, h, &canvas.Config{AtlasPageSize: 1}, recordWhiteFill)

	picture.expect(t, []probe{
		{"the fill's centre", m.Vec2{}, opaqueWhite},
		{"the fill's top left", m.Vec2{X: -9.5, Y: -9.5}, opaqueWhite},
	})
}
