package canvas_test

import (
	"image"
	"image/color"
	"math"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// panelPath is the path addPanelFile puts its panel at.
const panelPath = "panel.png"

// opaqueMagenta is a panel's left and right edges' colour.
var opaqueMagenta = color.NRGBA{R: 255, B: 255, A: 255}

// addPanelFile adds to files an opaque panel of 12 texels, cut by insets of
// 4 into nine cells of 4: its corners red at the top left, yellow at the
// top right, cyan at the bottom left and white at the bottom right, its top
// and bottom edges green, its left and right edges magenta, and its centre
// blue.
func addPanelFile(t testing.TB, files fstest.MapFS) {
	t.Helper()
	cells := [3][3]color.NRGBA{
		{opaqueRed, opaqueGreen, opaqueYellow},
		{opaqueMagenta, opaqueBlue, opaqueMagenta},
		{opaqueCyan, opaqueGreen, opaqueWhite},
	}
	panel := image.NewNRGBA(image.Rect(0, 0, 12, 12))
	for y := range 12 {
		for x := range 12 {
			panel.SetNRGBA(x, y, cells[y/4][x/4])
		}
	}
	addImageFile(t, files, panelPath, panel)
}

// panelInsets are the insets addPanelFile's panel is cut by.
var panelInsets = canvas.Insets{Left: 4, Top: 4, Right: 4, Bottom: 4}

func TestASlicedPanelKeepsItsCornersAtAnySizeTurnedAndSqueezedAndAHollowOneLeavesItsCentreEmpty(t *testing.T) {
	// The panel is drawn sliced at its nearest texel, its corners 8 units
	// at a TexelScale of 2: 40 units square, 120 wide, 60 by 40 turned a
	// quarter, hollow, and squeezed to 10, where its corners, 16 across,
	// shrink to 5 each. At a TexelScale of zero, meaning 1, its corners are
	// 4 units. Insets of 10 and 6 across its 12 texels shrink to 7.5 and
	// 4.5, so its centre, past 15 units, is where they meet, in its top
	// edge's green, where insets of 10 would show its top right corner's
	// yellow. Mirrored by its transform's scale or by a negative size, 30
	// units square, its top left corner lies at its right. Each is probed
	// in its own space, through its transform, a unit past its corners'
	// edges, where a stretched panel would still show its corners.
	files := fstest.MapFS{}
	addPanelFile(t, files)
	panel := canvas.ImageFromFile(panelPath, canvas.ImageOptions{Slice: panelInsets})
	hollowPanel := canvas.ImageFromFile(panelPath, canvas.ImageOptions{Slice: panelInsets, SliceHollow: true})
	meetingInsetsPanel := canvas.ImageFromFile(panelPath, canvas.ImageOptions{Slice: canvas.Insets{Left: 10, Top: 4, Right: 6, Bottom: 4}})
	panels := []struct {
		name       string
		image      canvas.Image
		transform  m.Mat2x3
		size       m.Vec2
		texelScale float32
		probes     []probe
	}{
		{"the square panel", panel, m.Translation2x3(-110, -60), m.Vec2{X: 40, Y: 40}, 2, []probe{
			{"top left corner", m.Vec2{X: -16, Y: -16}, opaqueRed},
			{"top edge past the corner", m.Vec2{X: -11, Y: -16}, opaqueGreen},
			{"left edge past the corner", m.Vec2{X: -16, Y: -11}, opaqueMagenta},
			{"centre", m.Vec2{}, opaqueBlue},
			{"bottom right corner", m.Vec2{X: 16, Y: 16}, opaqueWhite},
		}},
		{"the wide panel", panel, m.Translation2x3(40, -60), m.Vec2{X: 120, Y: 40}, 2, []probe{
			{"top left corner", m.Vec2{X: -56, Y: -16}, opaqueRed},
			{"top edge past the left corner", m.Vec2{X: -51, Y: -16}, opaqueGreen},
			{"top edge past the right corner", m.Vec2{X: 51, Y: -16}, opaqueGreen},
			{"top right corner", m.Vec2{X: 56, Y: -16}, opaqueYellow},
			{"left edge", m.Vec2{X: -56}, opaqueMagenta},
			{"centre", m.Vec2{}, opaqueBlue},
			{"bottom left corner", m.Vec2{X: -56, Y: 16}, opaqueCyan},
			{"bottom right corner", m.Vec2{X: 56, Y: 16}, opaqueWhite},
		}},
		{"the turned panel", panel, m.Translation2x3(-100, 50).Mul(m.Rotation2x3(math.Pi / 2)), m.Vec2{X: 60, Y: 40}, 2, []probe{
			{"top left corner", m.Vec2{X: -26, Y: -16}, opaqueRed},
			{"top edge past the corner", m.Vec2{X: -21, Y: -16}, opaqueGreen},
			{"top right corner", m.Vec2{X: 26, Y: -16}, opaqueYellow},
			{"centre", m.Vec2{}, opaqueBlue},
		}},
		{"the panel at a TexelScale of zero", panel, m.Translation2x3(-30, 50), m.Vec2{X: 40, Y: 40}, 0, []probe{
			{"top left corner", m.Vec2{X: -18, Y: -18}, opaqueRed},
			{"top edge past the corner", m.Vec2{X: -15, Y: -18}, opaqueGreen},
		}},
		{"the squeezed panel", panel, m.Translation2x3(20, 50), m.Vec2{X: 10, Y: 10}, 2, []probe{
			{"top left corner", m.Vec2{X: -3, Y: -3}, opaqueRed},
			{"top right corner", m.Vec2{X: 2, Y: -3}, opaqueYellow},
			{"bottom left corner", m.Vec2{X: -3, Y: 2}, opaqueCyan},
			{"bottom right corner", m.Vec2{X: 2, Y: 2}, opaqueWhite},
		}},
		{"the hollow panel", hollowPanel, m.Translation2x3(100, 50), m.Vec2{X: 40, Y: 40}, 2, []probe{
			{"top left corner", m.Vec2{X: -16, Y: -16}, opaqueRed},
			{"top edge", m.Vec2{Y: -16}, opaqueGreen},
			{"empty centre", m.Vec2{}, opaqueBlack},
		}},
		{"the panel whose insets meet past its frame", meetingInsetsPanel, m.Translation2x3(60, 100), m.Vec2{X: 40, Y: 30}, 2, []probe{
			{"left corner", m.Vec2{X: -17, Y: -11}, opaqueRed},
			{"centre, where the insets meet", m.Vec2{X: -3, Y: -11}, opaqueGreen},
		}},
		{"the panel mirrored by its transform", panel, m.TRS2x3(m.Vec2{X: -110, Y: 100}, 0, m.Vec2{X: -1, Y: 1}), m.Vec2{X: 30, Y: 30}, 2, []probe{
			{"top left corner", m.Vec2{X: -11, Y: -11}, opaqueRed},
			{"top edge past the corner", m.Vec2{X: -6, Y: -11}, opaqueGreen},
			{"top right corner", m.Vec2{X: 11, Y: -11}, opaqueYellow},
		}},
		{"the panel mirrored by a negative size", panel, m.Translation2x3(-60, 100), m.Vec2{X: -30, Y: 30}, 2, []probe{
			{"top left corner", m.Vec2{X: 11, Y: -11}, opaqueRed},
			{"top edge past the corner", m.Vec2{X: 6, Y: -11}, opaqueGreen},
			{"top right corner", m.Vec2{X: -11, Y: -11}, opaqueYellow},
		}},
	}
	picture := captureHarnessDrawing(t, canvastest.NewHarness(t), nil, func(list canvas.DrawList) {
		for _, drawnPanel := range panels {
			list.DrawSprite(canvas.Sprite{Image: drawnPanel.image, Transform: drawnPanel.transform, Size: drawnPanel.size, DrawMode: canvas.Sliced, TexelScale: drawnPanel.texelScale, Filter: canvas.FilterNearest})
		}
	}, &fileMounter{files: files})

	for _, drawnPanel := range panels {
		for _, ownProbe := range drawnPanel.probes {
			world := drawnPanel.transform.TransformPoint(ownProbe.point)
			if shown := picture.at(world); shown != ownProbe.want {
				t.Errorf("%s's %s, at %v, is %v, want %v", drawnPanel.name, ownProbe.name, world, shown, ownProbe.want)
			}
		}
	}
}

// makeTileTexture answers tilePath's tile as a texture a textureRecorder
// makes: an sRGB one of 4 texels square, red at the top left, green at the
// top right, blue at the bottom left and white at the bottom right.
func makeTileTexture() madeTexture {
	var texels []byte
	for y := range 4 {
		for x := range 4 {
			quadrant := [2][2]color.NRGBA{{opaqueRed, opaqueGreen}, {opaqueBlue, opaqueWhite}}[y/2][x/2]
			texels = append(texels, quadrant.R, quadrant.G, quadrant.B, quadrant.A)
		}
	}
	return madeTexture{descriptor: render.TextureDescriptor{Label: "tile", Width: 4, Height: 4, Format: render.TextureFormatRGBA8UnormSrgb}, texels: texels}
}

func TestATiledSpriteRepeatsItsImageWithoutSeams(t *testing.T) {
	// The tile, wrapping along both axes, is drawn tiled and linearly
	// filtered over 70 by 21 units at a TexelScale of 1.75, so its tiles
	// are 7 units and its seams fall at every offset from the pixels
	// sampled together for their gradients. Packed into the atlas, its
	// gutter wraps; drawn from a texture of its own, or as an sRGB render
	// texture's image, its sampler repeats. Along a row through its tiles'
	// top halves, red then green, each tile shows its red and its green,
	// each seam a mix of green and red, and no pixel reads past level 0 to
	// the mips' average, whose blue shows. Below it, a strip of 4 tiles
	// mirrored by a negative size begins its first tile, red first, at its
	// right, and a strip at a TexelScale of zero, meaning 1, repeats every
	// 4 units.
	files := fstest.MapFS{}
	addSheetsFiles(t, files)
	wrapping := canvas.ImageOptions{Wrap: canvas.WrapBoth}
	const left, top, tileSize, tileCount = float32(-35), float32(-10.5), float32(7), 10
	const mirroredY, mirroredRight, unscaledY = float32(60), 2 * tileSize, float32(90)
	drawTile := func(list canvas.DrawList, image canvas.Image) {
		list.DrawSprite(canvas.Sprite{Image: image, Size: m.Vec2{X: tileSize * tileCount, Y: 21}, DrawMode: canvas.Tiled, TexelScale: tileSize / 4, Filter: canvas.FilterLinear})
		list.DrawSprite(canvas.Sprite{Image: image, Transform: m.Translation2x3(0, mirroredY), Size: m.Vec2{X: -4 * tileSize, Y: tileSize}, DrawMode: canvas.Tiled, TexelScale: tileSize / 4, Filter: canvas.FilterLinear})
		list.DrawSprite(canvas.Sprite{Image: image, Transform: m.Translation2x3(0, unscaledY), Size: m.Vec2{X: 16, Y: 4}, DrawMode: canvas.Tiled, Filter: canvas.FilterLinear})
	}
	tileTexture := &textureRecorder{
		made: []madeTexture{makeTileTexture()},
		record: func(list canvas.DrawList, _ canvas.Assets, textures []render.Texture) {
			drawTile(list, canvas.ImageFromTexture(textures[0], wrapping))
		},
	}
	for _, test := range []struct {
		name   string
		config *canvas.Config
		record func(list canvas.DrawList)
		plugin kernel.Plugin
	}{
		{"packed into the atlas", nil, func(list canvas.DrawList) { drawTile(list, canvas.ImageFromFile(tilePath, wrapping)) }, &fileMounter{files: files}},
		{"drawn from a texture of its own", &canvas.Config{OwnTextureSize: 2}, func(list canvas.DrawList) { drawTile(list, canvas.ImageFromFile(tilePath, wrapping)) }, &fileMounter{files: files}},
		{"of a render texture", nil, func(canvas.DrawList) {}, tileTexture},
	} {
		t.Run(test.name, func(t *testing.T) {
			picture := captureHarnessDrawing(t, canvastest.NewHarness(t), test.config, test.record, test.plugin)

			// The row lies a texel's width down, between the centres of
			// the tile's first two rows.
			row := top + tileSize/4
			for tile := range tileCount {
				tileLeft := left + tileSize*float32(tile)
				picture.expect(t, []probe{
					{"a tile's red", m.Vec2{X: tileLeft + tileSize/4, Y: row}, opaqueRed},
					{"a tile's green", m.Vec2{X: tileLeft + 3*tileSize/4, Y: row}, opaqueGreen},
				})
				if tile > 0 {
					if seam := (m.Vec2{X: tileLeft, Y: row}); !isMixOf(picture.at(seam), opaqueGreen, opaqueRed) {
						t.Errorf("the seam at %v is %v, want green mixed with red", seam, picture.at(seam))
					}
				}
			}
			mirroredRow := mirroredY - tileSize/4
			picture.expect(t, []probe{
				{"the mirrored strip's first red", m.Vec2{X: mirroredRight - tileSize/4, Y: mirroredRow}, opaqueRed},
				{"the mirrored strip's first green", m.Vec2{X: mirroredRight - 3*tileSize/4, Y: mirroredRow}, opaqueGreen},
				{"the unscaled strip's first red", m.Vec2{X: -7, Y: unscaledY - 1}, opaqueRed},
				{"the unscaled strip's first green", m.Vec2{X: -5, Y: unscaledY - 1}, opaqueGreen},
				{"the unscaled strip's second red", m.Vec2{X: -3, Y: unscaledY - 1}, opaqueRed},
			})
			from := picture.pixel(m.Vec2{X: left + 1, Y: row})
			to := picture.pixel(m.Vec2{X: left + tileSize*tileCount - 1, Y: row})
			for x := from.X; x <= to.X; x++ {
				if shown := picture.picture.NRGBAAt(x, from.Y); shown.B >= 85 {
					t.Errorf("the pixel at %d, %d is %v, which shows the tile's blue, read at a coarser mip level", x, from.Y, shown)
				}
			}
		})
	}
}

func TestImagesOfOneRenderTextureWrappingApartAreBatchedApartTheirSamplersRepeatingAlongTheirAxes(t *testing.T) {
	// Images of one render texture, plain, wrapping along x and wrapping
	// along y, are drawn one after another from the same view, and each
	// breaks a batch, since its sampler repeats along the axes it wraps
	// along and clamps along the others, as a texture of its own has no
	// gutter.
	h := canvastest.NewHarness(t)
	recorder := &textureRecorder{
		made: []madeTexture{makeTileTexture()},
		record: func(list canvas.DrawList, _ canvas.Assets, textures []render.Texture) {
			for order, wrap := range []canvas.Wrap{canvas.WrapNone, canvas.WrapX, canvas.WrapY} {
				list.DrawSprite(canvas.Sprite{Image: canvas.ImageFromTexture(textures[0], canvas.ImageOptions{Wrap: wrap}), DrawMode: canvas.Tiled, Filter: canvas.FilterLinear, Order: int32(order)})
			}
		},
	}
	h.Spawn(nil, nil, recorder)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()

	frame := h.Frames()[0]
	if batches := frame.Cameras[0].Batches; len(batches) != 3 {
		t.Fatalf("the camera drew the batches %+v, want one for each image", batches)
	}
	var samplers []render.SamplerDescriptor
	for _, draw := range findPass(t, findCanvasLists(frame.Render)[0], "canvas camera").Draws {
		sampler := findParameter(t, draw.DrawParameters, "canvas_sampler").Sampler
		samplers = append(samplers, render.SamplerDescriptor{AddressModeU: sampler.AddressModeU, AddressModeV: sampler.AddressModeV})
	}
	want := []render.SamplerDescriptor{
		{},
		{AddressModeU: render.AddressModeRepeat},
		{AddressModeV: render.AddressModeRepeat},
	}
	if !reflect.DeepEqual(samplers, want) {
		t.Errorf("the images were sampled with the address modes %+v, want %+v", samplers, want)
	}
}
