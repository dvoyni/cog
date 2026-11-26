package canvas_test

import (
	"image/color"
	"slices"
	"sync"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

func TestAMinimapCamerasTextureDrawnAsASpriteShowsTheWorldAsDrawnDirectly(t *testing.T) {
	// The world, on layer 0, is a green fill left of the origin and a
	// translucent orange one right of it, 16 units across. The minimap
	// camera draws it into a texture of 16 texels square, a texel a unit,
	// cleared to transparent. The window's camera, over black, draws every
	// layer: the world directly, and, on layer 1, the texture as a sprite of
	// its 16 texels centred 40 units right, and an image of its right half
	// alone, 8 by 16 texels, centred 30 units below that. Its translucent
	// orange, encoded and premultiplied in the texture, shows over black as
	// it does drawn directly only once decoded as it is sampled.
	recorder := &textureRecorder{
		made: []madeTexture{{descriptor: minimapDescriptor}},
		record: func(list canvas.CommandList, _ canvas.Assets, textures []render.Texture) {
			minimap := textures[0]
			list.SetCamera(canvas.Camera{Texture: minimap, Layers: canvas.MakeLayer(0), Clear: true})
			list.SetCamera(canvas.Camera{Clear: true, ClearColor: m.Color{A: 1}})
			list.DrawShape(canvas.Shape{Rect: m.Rect{X: -8, Y: -8, Width: 8, Height: 16}, Tint: green})
			list.DrawShape(canvas.Shape{Rect: m.Rect{Y: -8, Width: 8, Height: 16}, Tint: translucentOrange})
			list.DrawSprite(canvas.Sprite{Image: canvas.ImageFromTexture(minimap), Transform: m.Translation2x3(40, 0), Filter: canvas.FilterNearest, Layer: 1})
			rightHalf := canvas.ImageFromTexture(minimap, canvas.ImageOptions{SourceRect: m.Recti{X: 8, Width: 8, Height: 16}})
			list.DrawSprite(canvas.Sprite{Image: rightHalf, Transform: m.Translation2x3(40, 30), Filter: canvas.FilterNearest, Layer: 1})
		},
	}

	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), nil, func(canvas.CommandList) {}, recorder)

	picture.expect(t, []probe{
		{"the world's green, drawn directly", m.Vec2{X: -4}, greenShown},
		{"the world's translucent orange, drawn directly", m.Vec2{X: 4}, translucentOverBlack},
		{"the minimap's green", m.Vec2{X: 36}, greenShown},
		{"the minimap's translucent orange", m.Vec2{X: 44}, translucentOverBlack},
		{"past the minimap", m.Vec2{X: 52}, opaqueBlack},
		{"the right half's left", m.Vec2{X: 37, Y: 30}, translucentOverBlack},
		{"the right half's right", m.Vec2{X: 43, Y: 30}, translucentOverBlack},
		{"left of the right half", m.Vec2{X: 35, Y: 30}, opaqueBlack},
	})
}

func TestAnImageOfATextureMarkedSrgbIsDecodedAsSampledWhereItsFormatEncodesNothing(t *testing.T) {
	// Each texture is one opaque texel of green's encoded bytes, drawn
	// marked sRGB and linear as a sprite 20 units square. A Unorm texture
	// marked sRGB is decoded, so it shows green; marked linear, its bytes
	// are read as linear colour, encoded again by the window. An sRGB
	// texture is decoded by its format, however it is marked.
	greenTexel := []byte{greenShown.R, greenShown.G, greenShown.B, greenShown.A}
	red, greenChannel, blue, _ := m.Color{R: float32(greenShown.R) / 255, G: float32(greenShown.G) / 255, B: float32(greenShown.B) / 255, A: 1}.Srgb8()
	encodedTwice := color.NRGBA{R: red, G: greenChannel, B: blue, A: 255}
	recorder := &textureRecorder{
		made: []madeTexture{
			{descriptor: render.TextureDescriptor{Label: "unorm", Width: 1, Height: 1, Format: render.TextureFormatRGBA8Unorm}, texels: greenTexel},
			{descriptor: render.TextureDescriptor{Label: "srgb", Width: 1, Height: 1, Format: render.TextureFormatRGBA8UnormSrgb}, texels: greenTexel},
		},
		record: func(list canvas.CommandList, _ canvas.Assets, textures []render.Texture) {
			for index, texture := range textures {
				for column, colorSpace := range []canvas.ColorSpace{canvas.ColorSpaceSrgb, canvas.ColorSpaceLinear} {
					x := float32(-60 + 40*(2*index+column))
					list.DrawSprite(canvas.Sprite{Image: canvas.ImageFromTexture(texture, canvas.ImageOptions{ColorSpace: colorSpace}), Transform: m.Translation2x3(x, 0), Size: m.Vec2{X: 20, Y: 20}, Filter: canvas.FilterNearest})
				}
			}
		},
	}

	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), nil, func(canvas.CommandList) {}, recorder)

	picture.expect(t, []probe{
		{"the Unorm texture marked sRGB", m.Vec2{X: -60}, greenShown},
		{"the Unorm texture marked linear", m.Vec2{X: -20}, encodedTwice},
		{"the sRGB texture marked sRGB", m.Vec2{X: 20}, greenShown},
		{"the sRGB texture marked linear", m.Vec2{X: 60}, greenShown},
	})
}

func TestAnImageOfATextureDrawsItsFirstLevelAndLayerThroughAViewInABatchOfItsOwnNeverPacked(t *testing.T) {
	h := canvastest.NewHarness(t)
	recorder := &textureRecorder{
		made: []madeTexture{{descriptor: render.TextureDescriptor{Label: "layered", Width: 8, Height: 4, LayerCount: 2, MipLevelCount: 3, Format: render.TextureFormatRGBA8Unorm}}},
		record: func(list canvas.CommandList, _ canvas.Assets, textures []render.Texture) {
			image := canvas.ImageFromTexture(textures[0])
			list.DrawShape(canvas.Shape{Rect: m.Rect{Width: 4, Height: 4}})
			list.DrawSprite(canvas.Sprite{Image: image, Order: 1})
			list.DrawSprite(canvas.Sprite{Image: image, Order: 2})
		},
	}
	h.Spawn(nil, nil, recorder)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()

	frame := h.Frames()[0]
	texture := recorder.madeTextures()[0]
	batches := frame.Cameras[0].Batches
	if len(batches) != 2 || batches[1].Texture != texture || batches[1].InstanceCount != 2 {
		t.Fatalf("the camera drew the batches %+v, want the shape's, then one of both sprites over texture %v", batches, texture)
	}
	list := findCameraLists(frame.Render)[0]
	if passes := findPassSummaries(list, nil); !slices.Equal(passes, []passSummary{{label: "canvas camera", draws: 2}}) {
		t.Errorf("canvas drew the passes %+v, want the camera's alone into the window, of two draws, packing nothing", passes)
	}
	draws := findPass(t, list, "canvas camera").Draws
	view := findParameter(t, draws[len(draws)-1].DrawParameters, "canvas_atlas").TextureView
	if view.Texture != texture || view.BaseMipLevel != 0 || view.MipLevelCount != 1 || view.BaseLayer != 0 || view.LayerCount != 1 || view.Dimension != render.TextureViewDimension2DArray {
		t.Errorf("the sprites were drawn through the view %+v, want a 2D-array view of the texture's mip level 0 and layer 0 alone", view)
	}
}

func TestAnImageOfATextureRenderDoesNotHoldOrOfAFramePastItDrawsNothingReportedOnce(t *testing.T) {
	h := canvastest.NewHarness(t)
	recorder := &textureRecorder{
		made: []madeTexture{{descriptor: minimapDescriptor}},
		record: func(list canvas.CommandList, _ canvas.Assets, textures []render.Texture) {
			list.DrawSprite(canvas.Sprite{Image: canvas.ImageFromTexture(render.Texture{})})
			list.DrawSprite(canvas.Sprite{Image: canvas.ImageFromTexture(textures[0], canvas.ImageOptions{Sheet: canvas.Grid{Columns: 2}}), Frame: 2})
		},
	}
	h.Spawn(nil, nil, recorder)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.ExpectReport(canvas.ReportKindImageTextureRefused)
	h.ExpectReport(canvas.ReportKindFrameOutOfBounds)

	h.Step()
	h.Step()

	for index, frame := range h.Frames() {
		if len(frame.Cameras) != 1 || len(frame.Cameras[0].Batches) != 0 {
			t.Errorf("frame %d was drawn through %+v, want the default camera drawing no batch", index, frame.Cameras)
		}
	}
	if refusedCount := countReports(h, canvas.ReportKindImageTextureRefused); refusedCount != 1 {
		t.Errorf("the image of a texture render does not hold was reported %d times, want once", refusedCount)
	}
	if outOfBoundsCount := countReports(h, canvas.ReportKindFrameOutOfBounds); outOfBoundsCount != 1 {
		t.Errorf("the frame past the sheet's last was reported %d times, want once", outOfBoundsCount)
	}
}

func TestImageSizeAnswersATexturesSizeOrItsFramesAndFalseForOneRenderDoesNotHold(t *testing.T) {
	h := canvastest.NewHarness(t)
	type sizeAnswer struct {
		size m.Vec2
		read bool
	}
	var mutex sync.Mutex
	var answers []sizeAnswer
	recorder := &textureRecorder{
		made: []madeTexture{{descriptor: render.TextureDescriptor{Label: "minimap", Width: 16, Height: 8, Format: render.TextureFormatRGBA8Unorm}}},
		record: func(_ canvas.CommandList, assets canvas.Assets, textures []render.Texture) {
			mutex.Lock()
			defer mutex.Unlock()
			for _, image := range []canvas.Image{
				canvas.ImageFromTexture(textures[0]),
				canvas.ImageFromTexture(textures[0], canvas.ImageOptions{Sheet: canvas.Grid{Columns: 4}}),
				canvas.ImageFromTexture(textures[0], canvas.ImageOptions{SourceRect: m.Recti{X: 12, Width: 8, Height: 8}}),
				canvas.ImageFromTexture(render.Texture{}),
			} {
				size, read := assets.ImageSize(image)
				answers = append(answers, sizeAnswer{size, read})
			}
		},
	}
	h.Spawn(nil, nil, recorder)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()

	mutex.Lock()
	defer mutex.Unlock()
	want := []sizeAnswer{{m.Vec2{X: 16, Y: 8}, true}, {m.Vec2{X: 4, Y: 8}, true}, {}, {}}
	if !slices.Equal(answers, want) {
		t.Errorf("ImageSize answered %+v, want %+v: the texture's size, a frame's, and false for a rectangle past it and a texture render does not hold", answers, want)
	}
}

func TestPreloadingAnImageOfATextureLoadsNothing(t *testing.T) {
	h := canvastest.NewHarness(t)
	recorder := &textureRecorder{
		made: []madeTexture{{descriptor: minimapDescriptor}},
		record: func(_ canvas.CommandList, assets canvas.Assets, textures []render.Texture) {
			assets.Preload(canvas.ImageFromTexture(textures[0]))
		},
	}
	h.Spawn(nil, nil, recorder)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Step()

	made := findTextureLabels(findRenderFrames(h.Frames())...)
	delete(made, render.Texture{})
	delete(made, recorder.madeTextures()[0])
	if len(made) != 0 {
		t.Errorf("the frames made the textures %v, want none but the recorder's", made)
	}
}
