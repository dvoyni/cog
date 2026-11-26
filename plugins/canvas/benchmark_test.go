package canvas_test

import (
	"image"
	"image/color"
	"os"
	"strings"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/canvas/font"
	"github.com/dvoyni/cog/plugins/platform/filesystems"
	"github.com/dvoyni/cog/plugins/render/rendertest"
)

// benchmarkFrames steps h gateWarmFrameCount frames, and then, for each of
// b's iterations, untimedFrameCount frames untimed and one timed,
// reporting the time of each of the timed frame's useCount uses, named by
// unit: its entries, or the first uses the untimed frames made way for.
func benchmarkFrames(b *testing.B, h *rendertest.Harness, untimedFrameCount, useCount int, unit string) {
	b.ReportAllocs()
	for range gateWarmFrameCount {
		h.Step()
	}
	for b.Loop() {
		if untimedFrameCount > 0 {
			b.StopTimer()
			for range untimedFrameCount {
				h.Step()
			}
			b.StartTimer()
		}
		h.Step()
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*useCount), "ns/"+unit)
}

// BenchmarkSprites benchmarks a frame of sprites alone, drawn on canvas by
// gateRecordings and by hand on render by a handDrawnUser, with no canvas
// spawned, on gfx's recording adapter recording nothing, reporting the time
// per sprite: at 1,000, 10,000 and 100,000 sprites, canvas's split among
// 1, 4 and 16 recording tasks, each frame drawn through 1, 2 and 4
// cameras, and with one material and a switch every 16 sprites.
func BenchmarkSprites(b *testing.B) {
	for _, spriteCount := range []int{1000, 10000, 100000} {
		for _, scenario := range []gateScenario{
			{taskCount: 1, cameraCount: 1},
			{taskCount: 4, cameraCount: 1},
			{taskCount: 16, cameraCount: 1},
			{taskCount: 1, cameraCount: 2},
			{taskCount: 1, cameraCount: 4},
			{taskCount: 1, cameraCount: 1, switchPeriod: 16},
		} {
			scenario.spriteCount = spriteCount
			scenario.spritesOnly = true
			b.Run("canvas/"+scenario.String(), func(b *testing.B) {
				h := canvastest.NewHarness(b)
				h.RecordNothing()
				spawnGateHarness(b, h, makeGateRecords(scenario)...)
				benchmarkFrames(b, h.Harness, 0, spriteCount, "sprite")
			})
			if scenario.taskCount != 1 {
				continue
			}
			b.Run("render/"+scenario.String(), func(b *testing.B) {
				h := rendertest.NewHarness(b)
				h.RecordNothing()
				user := &handDrawnUser{files: makeHandDrawnFiles(b), sprites: makeGateRecordings(scenario)[0].sprites, switchPeriod: scenario.switchPeriod, passes: makeHandDrawnPasses(gateWindowSize, scenario.cameraCount)}
				h.Spawn(nil, user)
				user.window = h.OpenWindow(gateWindowSize)
				benchmarkFrames(b, h, 0, spriteCount, "sprite")
			})
		}
	}
}

// benchmarkEntryCount is how many entries a frame of BenchmarkEntries
// records, each kind's in its own frame.
const benchmarkEntryCount = 10000

// BenchmarkEntries benchmarks a frame of benchmarkEntryCount entries of one
// kind, spread over the window, recorded by one task through the default
// camera, on gfx's recording adapter recording nothing, reporting the time
// per entry: fills, each a call; sprites, each a call; runs of text, laid
// out from the layout cache; meshes of one file in bulk; and per-frame
// triangles, each call's copied into the frame's buffers.
func BenchmarkEntries(b *testing.B) {
	solid := canvas.ImageFromFile(gateSolidPath)
	tree := canvas.MeshFileFromFile(gateTreePath)
	triangles := makeSquareTriangles(4)
	var positions []m.Vec2
	var transforms []m.Mat2x3
	for index := range benchmarkEntryCount {
		position := m.Vec2{X: float32(index%300 - 150), Y: float32(index/300%220 - 110)}
		positions = append(positions, position)
		transforms = append(transforms, m.Translation2x3(position.X, position.Y))
	}
	for _, kind := range []struct {
		name string
		// makeRecord answers the kind's record, made in its own benchmark.
		makeRecord func() recordFunc
	}{
		{"fills", func() recordFunc {
			return func(list canvas.CommandList, _ canvas.Assets, _ int) {
				for _, position := range positions {
					list.DrawShape(canvas.Shape{Rect: m.Rect{X: position.X, Y: position.Y, Width: 4, Height: 4}, Tint: m.White})
				}
			}
		}},
		{"sprites", func() recordFunc {
			return func(list canvas.CommandList, _ canvas.Assets, _ int) {
				for _, transform := range transforms {
					list.DrawSprite(canvas.Sprite{Image: solid, Transform: transform})
				}
			}
		}},
		{"texts", func() recordFunc {
			return func(list canvas.CommandList, _ canvas.Assets, _ int) {
				for _, transform := range transforms {
					list.DrawText(canvas.Text{String: "Score 1200", Size: 8, Transform: transform})
				}
			}
		}},
		{"meshes", func() recordFunc {
			var meshes []canvas.Mesh
			for _, transform := range transforms {
				meshes = append(meshes, canvas.Mesh{File: tree, Transform: transform})
			}
			return func(list canvas.CommandList, _ canvas.Assets, _ int) {
				list.DrawMeshes(meshes)
			}
		}},
		{"triangles", func() recordFunc {
			return func(list canvas.CommandList, _ canvas.Assets, _ int) {
				for _, transform := range transforms {
					list.DrawTriangles(canvas.DrawTrianglesOptions{Transform: transform}, triangles)
				}
			}
		}},
	} {
		b.Run(kind.name, func(b *testing.B) {
			h := canvastest.NewHarness(b)
			h.RecordNothing()
			spawnGateHarness(b, h, kind.makeRecord())
			benchmarkFrames(b, h.Harness, 0, benchmarkEntryCount, "entry")
		})
	}
}

// BenchmarkImageFirstUse benchmarks the frame of an image's first use, a
// sprite drawing a PNG of 256 texels square, its decode, its conversion,
// and its packing into the atlas: as it is, and converted into a distance
// field. Each frame before it unloads the image, untimed.
func BenchmarkImageFirstUse(b *testing.B) {
	picture := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	for y := range 256 {
		for x := range 256 {
			if (x-128)*(x-128)+(y-128)*(y-128) < 100*100 {
				picture.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
			}
		}
	}
	for _, options := range []struct {
		name    string
		options canvas.ImageOptions
	}{
		{"plain", canvas.ImageOptions{}},
		{"distance field", canvas.ImageOptions{Convert: canvas.DistanceField{}}},
	} {
		b.Run(options.name, func(b *testing.B) {
			files := makeGateFiles(b)
			addImageFile(b, files, "disc.png", picture)
			disc := canvas.ImageFromFile("disc.png", options.options)
			h := canvastest.NewHarness(b)
			h.RecordNothing()
			h.Spawn(nil, nil, &fileMounter{files: files}, &concurrentUser{records: []recordFunc{func(list canvas.CommandList, assets canvas.Assets, frame int) {
				if frame%2 == 0 {
					assets.Unload("disc.png")
					return
				}
				list.DrawSprite(canvas.Sprite{Image: disc})
			}}})
			h.OpenWindow(gateWindowSize)
			benchmarkFrames(b, h.Harness, 1, 1, "image")
		})
	}
}

// newHanziCount is how many new hanzi BenchmarkFrameOfNewHanzi's frames
// draw.
const newHanziCount = 200

// BenchmarkFrameOfNewHanzi benchmarks a frame of newHanziCount hanzi
// canvas holds no glyph of, at a field em of 64, laid out anew and each
// glyph made in the frame, reporting the time per glyph. The first of the
// two frames before it, untimed, puts the font again and reloads it, which
// drops its glyphs and layouts, and the second gives the reload, in a batch
// of its own, its frame to finish in.
func BenchmarkFrameOfNewHanzi(b *testing.B) {
	const fontPath = "hanzi.ttf"
	fontFile, err := os.ReadFile("testdata/fonts/notosanssc-hanzi.ttf")
	if err != nil {
		b.Fatal(err)
	}
	var hanziBuilder strings.Builder
	for index := range newHanziCount {
		hanziBuilder.WriteRune(rune(0x4E00 + index))
	}
	hanzi := hanziBuilder.String()
	memory := filesystems.NewMemoryFS()
	putFile(b, memory, fontPath, fontFile)
	config := &canvas.Config{DefaultFontSet: font.SetDescriptor{Default: font.Font{Path: fontPath, FieldEm: 64}}}
	h := canvastest.NewHarness(b)
	h.RecordNothing()
	h.Spawn(nil, config, &fileMounter{files: memory}, &concurrentUser{records: []recordFunc{func(list canvas.CommandList, assets canvas.Assets, frame int) {
		// After gateWarmFrameCount frames, each frame divisible by 3 is the
		// timed one.
		switch frame % 3 {
		case 1:
			putFile(b, memory, fontPath, fontFile)
			assets.ReloadChanged()
		case 0:
			list.DrawText(canvas.Text{String: hanzi, Size: 12, WrapWidth: 300})
		}
	}}})
	h.OpenWindow(gateWindowSize)
	benchmarkFrames(b, h.Harness, 2, newHanziCount, "glyph")
}
