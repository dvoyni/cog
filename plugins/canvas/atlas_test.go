package canvas_test

import (
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

// redPath, bluePath and stripesPath are the paths makeAtlasFiles puts its
// images at.
const (
	redPath     = "red.png"
	bluePath    = "blue.png"
	stripesPath = "stripes.png"
)

// makeAtlasFiles answers the quadrants' files, with three images of opaque
// texels beside them: red and blue, each solid, of 4×4 and 32×32 texels, at
// redPath and bluePath, and a row of 4 texels, red, green, blue and white,
// at stripesPath.
func makeAtlasFiles(t testing.TB) fstest.MapFS {
	t.Helper()
	files := makeQuadrantsFiles(t)
	addSolidFile(t, files, redPath, 4, 4, color.NRGBA{R: 255, A: 255})
	addSolidFile(t, files, bluePath, 32, 32, color.NRGBA{B: 255, A: 255})
	stripes := image.NewNRGBA(image.Rect(0, 0, 4, 1))
	for x, stripe := range []color.NRGBA{{R: 255, A: 255}, {G: 255, A: 255}, {B: 255, A: 255}, {R: 255, G: 255, B: 255, A: 255}} {
		stripes.SetNRGBA(x, 0, stripe)
	}
	addImageFile(t, files, stripesPath, stripes)
	return files
}

// findTextureLabels answers the label of each texture frame made or loaded.
func findTextureLabels(frame render.Inspection) map[render.Texture]string {
	labels := map[render.Texture]string{}
	for _, asset := range frame.MadeAssets {
		labels[asset.Texture] = asset.Label
	}
	return labels
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
	// The blue image of 32 texels is a page's side, its cell with a gutter
	// larger, so it takes an atlas of its own, of no gutter, no larger than
	// a page.
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

func TestAnImageNoPageHoldsIsDrawnFromAnAtlasOfItsOwn(t *testing.T) {
	// A page of 8 texels holds the white texel's cell of 5 and the rows
	// below it, too few for the quadrants' cell of 8, so the quadrants are
	// packed into an atlas of their own, read at their nearest texel.
	quadrants := canvas.ImageFromFile(quadrantsPath)
	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), &canvas.Config{AtlasPageSize: 8}, func(list canvas.DrawList) {
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
}
