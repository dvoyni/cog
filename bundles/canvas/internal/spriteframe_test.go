package internal

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/kernel"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// A Frame says which texels a sprite draws, so it says how many: the natural
// size of a framed sprite is the frame's extent, not the whole sheet's.
//
// The sheet is the reason. A tile cut out of an atlas page at Scale 1 promises
// one source texel per world unit, and sizing it from the page instead stretched
// a 4x4 tile over 16x16 - a texel-per-world ratio of 4 where Scale 1 said 1. The
// sub-rect was sampled and the whole source's size was drawn, so the two halves
// of the same transform disagreed.
//
// The rejected reading is that a Frame is an animation window over a sprite of
// fixed natural size. SpriteFrame is insets rather than a rect, so a uniform
// sheet already gives every frame of a flipbook the same extent: the stable size
// that reading exists to protect is one this reading hands over for free, and
// where the frames are not uniform it draws all of them at the sheet's size,
// which is no authoring intent at all.
func TestAFramedSpriteTakesItsNaturalSizeFromTheFrame(t *testing.T) {
	config := canvas.Config{AtlasSize: 64, LayersPerArray: 2, MaxAtlasBytes: 64 * 64 * 4 * 2}
	files := fstest.MapFS{"sheet.png": &fstest.MapFile{Data: pngBytes(t, 16, 16)}}
	k, _, backend := testKernel(t, files, config, func(write *canvas.OpQueue) {
		write.Sprite(0, "sheet.png", canvas.SpriteTransform{
			Scale: 1,
			Frame: canvas.SpriteFrame{Right: 12, Bottom: 12},
		}, nil)
	})
	runFrame(k)

	instances := spriteInstances(backend)
	if len(instances) != 1 {
		t.Fatalf("instance buffers = %d, want the one batch the sprite draws in", len(instances))
	}
	record := instanceAt(instances[0], 0)
	if x, y := floatAt(record, 8), floatAt(record, 12); x != 4 || y != 4 {
		t.Errorf("drawn size = %vx%v, want the frame's 4x4 at Scale 1, not the sheet's 16x16", x, y)
	}
}

// Size names the destination outright, so a frame never overrides it. Sizing
// from the frame changes what an unset size means and nothing else.
func TestAnExplicitSizeStillWinsOverAFrame(t *testing.T) {
	config := canvas.Config{AtlasSize: 64, LayersPerArray: 2, MaxAtlasBytes: 64 * 64 * 4 * 2}
	files := fstest.MapFS{"sheet.png": &fstest.MapFile{Data: pngBytes(t, 16, 16)}}
	k, _, backend := testKernel(t, files, config, func(write *canvas.OpQueue) {
		write.Sprite(0, "sheet.png", canvas.SpriteTransform{
			Size:  m.Vec2{X: 20, Y: 10},
			Frame: canvas.SpriteFrame{Right: 12, Bottom: 12},
		}, nil)
	})
	runFrame(k)

	instances := spriteInstances(backend)
	if len(instances) != 1 {
		t.Fatalf("instance buffers = %d, want one", len(instances))
	}
	record := instanceAt(instances[0], 0)
	if x, y := floatAt(record, 8), floatAt(record, 12); x != 20 || y != 10 {
		t.Errorf("drawn size = %vx%v, want the explicit 20x10", x, y)
	}
}

// One axis set derives the other from the source's aspect, and the source a
// frame selects is the frame. A 12x4 window of a square sheet is 3:1, so a
// height of 2 is a width of 6 - where the whole sheet's 1:1 would have said 2.
func TestASingleAxisSizeDerivesTheOtherFromTheFramesAspect(t *testing.T) {
	config := canvas.Config{AtlasSize: 64, LayersPerArray: 2, MaxAtlasBytes: 64 * 64 * 4 * 2}
	files := fstest.MapFS{"sheet.png": &fstest.MapFile{Data: pngBytes(t, 16, 16)}}
	k, _, backend := testKernel(t, files, config, func(write *canvas.OpQueue) {
		write.Sprite(0, "sheet.png", canvas.SpriteTransform{
			Size:  m.Vec2{Y: 2},
			Frame: canvas.SpriteFrame{Right: 4, Bottom: 12},
		}, nil)
	})
	runFrame(k)

	instances := spriteInstances(backend)
	if len(instances) != 1 {
		t.Fatalf("instance buffers = %d, want one", len(instances))
	}
	record := instanceAt(instances[0], 0)
	if x, y := floatAt(record, 8), floatAt(record, 12); x != 6 || y != 2 {
		t.Errorf("drawn size = %vx%v, want 6x2: the frame's 12x4 aspect, not the sheet's 1:1", x, y)
	}
}

// The texture-sourced path means by Size and Scale exactly what the atlas path
// means, which is why spriteSize is the one place the rule lives - and why the
// fault was never only entrySize. textureUV insets by Frame exactly as entryUV
// does, so a framed texture sprite stretched its sub-rect the same way.
func TestAFramedTextureSpriteTakesItsNaturalSizeFromTheFrame(t *testing.T) {
	k, _, backend := testKernelGfx(t, fstest.MapFS{}, targetTestConfig(), func(write *canvas.OpQueue, gfxWrite *gfx.OpQueue) {
		_, texture := gfxWrite.TemporaryTarget(64, 32, gfx.FormatRGBA8Srgb)
		write.SpriteTexture(0, texture, canvas.SpriteTransform{
			Frame: canvas.SpriteFrame{Right: 48, Bottom: 24},
		}, nil)
	})
	runFrame(k)

	positions, uvs := quadVertices(t, backend)
	if positions[0] != (m.Vec2{}) || positions[2] != (m.Vec2{X: 16, Y: 8}) {
		t.Errorf("quad corners = %v and %v, want (0,0) to (16,8): the frame's own 16x8", positions[0], positions[2])
	}
	if uvs[0] != (m.Vec2{}) || uvs[2] != (m.Vec2{X: 0.25, Y: 0.25}) {
		t.Errorf("quad uvs = %v and %v, want the frame's quarter of each axis", uvs[0], uvs[2])
	}
}

// A nine-slice over a framed sprite slices the frame, not the sheet. The insets
// measure into the frame's extent and are offset by it, so the nine parts cover
// the frame's destination and sample only the frame's texels.
//
// They used to do neither: nineSliceParts computed its source columns against
// the whole source and overwrote part.Frame with them, so a caller's frame was
// discarded outright and the nine parts tiled the entire sheet - silently, with
// no report. A nine-sliced panel packed into a UI sheet, which is the normal
// authoring case, was undrawable and said nothing about why.
func TestANineSliceOverAFrameSlicesTheFrame(t *testing.T) {
	config := canvas.Config{AtlasSize: 64, LayersPerArray: 2, MaxAtlasBytes: 64 * 64 * 4 * 2}
	files := fstest.MapFS{"sheet.png": &fstest.MapFile{Data: pngBytes(t, 16, 16)}}
	k, _, backend := testKernel(t, files, config, func(write *canvas.OpQueue) {
		write.Sprite(0, "sheet.png", canvas.SpriteTransform{
			Scale:     1,
			Frame:     canvas.SpriteFrame{Left: 4, Top: 4, Right: 4, Bottom: 4},
			NineSlice: canvas.SpriteFrame{Left: 2, Top: 2, Right: 2, Bottom: 2},
		}, nil)
	})
	runFrame(k)

	instances := spriteInstances(backend)
	if len(instances) != 1 {
		t.Fatalf("instance buffers = %d, want one", len(instances))
	}
	parts := len(instances[0]) / testInstanceSize
	if parts != 9 {
		t.Fatalf("nine-slice parts = %d, want 9", parts)
	}
	// The frame is the source's middle 8x8, so at Scale 1 the nine parts cover
	// 8x8 of destination: the last one starts at (6,6) and is 2x2.
	last := instanceAt(instances[0], 8)
	if x, y := floatAt(last, 0), floatAt(last, 4); x != 6 || y != 6 {
		t.Errorf("last part at (%v,%v), want (6,6): a destination of the frame's 8x8", x, y)
	}
	if x, y := floatAt(last, 8), floatAt(last, 12); x != 2 || y != 2 {
		t.Errorf("last part size = %vx%v, want the 2x2 corner", x, y)
	}
	// Source texels 4..12 of a 16x16 sheet sit at atlas texels 7..15 on a
	// 64-texel page one texel of padding in, so the nine parts must sample
	// 7/64..15/64 and nothing outside it.
	first := instanceAt(instances[0], 0)
	if u := floatAt(first, 32); u != 7.0/64 {
		t.Errorf("first part starts at u = %v, want %v: the frame's left edge", u, 7.0/64)
	}
	if u := floatAt(last, 40); u != 15.0/64 {
		t.Errorf("last part ends at u = %v, want %v: the frame's right edge, not the sheet's", u, 15.0/64)
	}
}

// A Frame that does not fit its source draws nothing and says so, once. It used
// to draw nothing and say nothing: entryUV returned false and the batcher
// returned, so the author saw a missing sprite and no reason for it.
//
// Once is the whole difficulty. A frame is resolved per draw rather than cached
// like an atlas entry, so an unkeyed report would fire every frame for as long
// as the sprite is recorded; the key is the path and the frame together, which
// is canvas's own type and so shares a namespace with nothing.
func TestAFrameThatDoesNotFitItsSourceDrawsNothingAndIsReportedOnce(t *testing.T) {
	config := canvas.Config{AtlasSize: 64, LayersPerArray: 2, MaxAtlasBytes: 64 * 64 * 4 * 2}
	files := fstest.MapFS{"sheet.png": &fstest.MapFile{Data: pngBytes(t, 16, 16)}}
	k, errs, backend := testKernelCapturing(t, files, config, func(write *canvas.OpQueue) {
		write.Sprite(0, "sheet.png", canvas.SpriteTransform{
			Scale: 1,
			Frame: canvas.SpriteFrame{Left: 10, Right: 10},
		}, nil)
	})
	runFrame(k)
	runFrame(k)
	runFrame(k)

	if instances := spriteInstances(backend); len(instances) != 0 {
		t.Errorf("instance buffers = %d, want nothing drawn for a frame that does not fit", len(instances))
	}
	if len(*errs) != 1 {
		t.Fatalf("reported errors across three frames = %d, want the one: %v", len(*errs), *errs)
	}
	if message := (*errs)[0].Error(); !strings.Contains(message, "sheet.png") {
		t.Errorf("reported %q, want a message naming the sheet the frame missed", message)
	}
}

// testKernelGfxCapturing is testKernelGfx with testKernelCapturing's error
// handler: a texture-sourced sprite needs gfx's queue to mint its texture, and
// a report test needs the errors collected rather than failing the test.
func testKernelGfxCapturing(t testing.TB, config canvas.Config, record func(*canvas.OpQueue, *gfx.OpQueue)) (kernel.Executioner, *[]error, *testBackend) {
	t.Helper()
	var errs []error
	k, _, backend := testKernelRecorder(t, fstest.MapFS{}, config, recordCanvasPlugin{recordGfx: record}, func(err error) error {
		errs = append(errs, err)
		return nil
	})
	return k, &errs, backend
}

// The texture path reports the same refusal, keyed on the texture rather than a
// path because a texture-sourced sprite has no path of its own to be named by.
//
// The dedupe is asserted within one frame rather than across frames, because a
// TemporaryTarget is a different texture every frame and so is a different
// mistake every frame: once is once per texture, which is the strongest thing
// the key can honestly promise. Three draws of one texture is the case a UI
// actually produces.
func TestAFrameThatDoesNotFitATextureIsReportedOnce(t *testing.T) {
	k, errs, backend := testKernelGfxCapturing(t, targetTestConfig(), func(write *canvas.OpQueue, gfxWrite *gfx.OpQueue) {
		_, texture := gfxWrite.TemporaryTarget(64, 32, gfx.FormatRGBA8Srgb)
		for i := 0; i < 3; i++ {
			write.SpriteTexture(0, texture, canvas.SpriteTransform{
				Position: m.Vec2{X: float32(i) * 8},
				Frame:    canvas.SpriteFrame{Top: 20, Bottom: 20},
			}, nil)
		}
	})
	runFrame(k)

	if backend.draws != 0 {
		t.Errorf("draws = %d, want nothing drawn for a frame taller than its texture", backend.draws)
	}
	if len(*errs) != 1 {
		t.Fatalf("reported errors for three draws of one texture = %d, want the one: %v", len(*errs), *errs)
	}
	if message := (*errs)[0].Error(); !strings.Contains(message, "64x32") {
		t.Errorf("reported %q, want a message naming the texture the frame missed", message)
	}
}

// Nine-slice insets are measured into the frame, so insets that fit the sheet
// and not the frame are refused - and said, because this is the case the new
// composition creates and the one an author has no other way to diagnose. Three
// texels of border on each side of a 4x4 window leave no middle, even though
// they would sit comfortably inside the 16x16 sheet the window is cut from.
func TestNineSliceInsetsThatFitTheSheetButNotTheFrameAreReported(t *testing.T) {
	config := canvas.Config{AtlasSize: 64, LayersPerArray: 2, MaxAtlasBytes: 64 * 64 * 4 * 2}
	files := fstest.MapFS{"sheet.png": &fstest.MapFile{Data: pngBytes(t, 16, 16)}}
	k, errs, backend := testKernelCapturing(t, files, config, func(write *canvas.OpQueue) {
		write.Sprite(0, "sheet.png", canvas.SpriteTransform{
			Scale:     1,
			Frame:     canvas.SpriteFrame{Left: 6, Top: 6, Right: 6, Bottom: 6},
			NineSlice: canvas.SpriteFrame{Left: 3, Top: 3, Right: 3, Bottom: 3},
		}, nil)
	})
	runFrame(k)
	runFrame(k)

	if instances := spriteInstances(backend); len(instances) != 0 {
		t.Errorf("instance buffers = %d, want nothing drawn for insets with no middle", len(instances))
	}
	if len(*errs) != 1 {
		t.Fatalf("reported errors across two frames = %d, want the one: %v", len(*errs), *errs)
	}
	if message := (*errs)[0].Error(); !strings.Contains(message, "4x4") {
		t.Errorf("reported %q, want a message naming the 4x4 the frame selects, not the sheet", message)
	}
}

// A frame that does not fit and a nine-slice over it produce one report, the
// frame's. The insets are measured into the frame's extent, so on a frame that
// does not fit their verdict is derived from a number that means nothing: a
// second report would name an error that disappears when the first is fixed.
func TestABadFrameUnderANineSliceReportsOnlyTheFrame(t *testing.T) {
	config := canvas.Config{AtlasSize: 64, LayersPerArray: 2, MaxAtlasBytes: 64 * 64 * 4 * 2}
	files := fstest.MapFS{"sheet.png": &fstest.MapFile{Data: pngBytes(t, 16, 16)}}
	k, errs, _ := testKernelCapturing(t, files, config, func(write *canvas.OpQueue) {
		write.Sprite(0, "sheet.png", canvas.SpriteTransform{
			Scale:     1,
			Frame:     canvas.SpriteFrame{Left: 10, Right: 10},
			NineSlice: canvas.SpriteFrame{Left: 3, Top: 3, Right: 3, Bottom: 3},
		}, nil)
	})
	runFrame(k)

	if len(*errs) != 1 {
		t.Fatalf("reported errors = %d, want only the frame's: %v", len(*errs), *errs)
	}
	if message := (*errs)[0].Error(); !strings.Contains(message, "sprite frame") {
		t.Errorf("reported %q, want the frame's refusal rather than the nine-slice's", message)
	}
}

// Tiling still ignores Frame. What repeats is the texture and not a window onto
// it, so a tiled axis samples a standalone repeat texture whole - which is why
// drawTiledSprite resolves its own size instead of going through spriteSize,
// and why narrowing spriteSize by the frame had to leave it alone.
//
// The non-tiled axis therefore still falls back to the whole texture's pixel
// size: 16, not the frame's 4. A frame that does not even fit is ignored here
// rather than reported, because nothing on this path reads it.
func TestATiledSpriteStillIgnoresItsFrame(t *testing.T) {
	config := canvas.Config{AtlasSize: 64, LayersPerArray: 2, MaxAtlasBytes: 64 * 64 * 4 * 2}
	files := fstest.MapFS{"sheet.png": &fstest.MapFile{Data: pngBytes(t, 16, 16)}}
	k, errs, backend := testKernelCapturing(t, files, config, func(write *canvas.OpQueue) {
		write.Sprite(0, "sheet.png", canvas.SpriteTransform{
			Scale: 1,
			Size:  m.Vec2{X: 32},
			TileX: true,
			Frame: canvas.SpriteFrame{Right: 12, Bottom: 12},
		}, nil)
	})
	runFrame(k)

	if len(*errs) != 0 {
		t.Fatalf("reported errors = %v, want none: a tiled sprite never reads its frame", *errs)
	}
	positions, _ := quadVertices(t, backend)
	if positions[0] != (m.Vec2{}) || positions[2] != (m.Vec2{X: 32, Y: 16}) {
		t.Errorf("quad corners = %v and %v, want (0,0) to (32,16): the tiled width and the whole texture's height", positions[0], positions[2])
	}
}
