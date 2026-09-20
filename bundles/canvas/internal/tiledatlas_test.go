package internal

import (
	"testing"
	"testing/fstest"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// The claim this whole change exists for. A tiled sprite and an untiled one over
// the same file used to be two textures in two batchers with no rule that could
// merge them; sharing an atlas page they are two instances of one draw.
func TestATiledAndAnUntiledSpriteOverOnePathMerge(t *testing.T) {
	filesystem := fstest.MapFS{"edge.png": &fstest.MapFile{Data: pngBytes(t, 4, 4)}}
	config := canvas.Config{AtlasSize: 64, LayersPerArray: 2, MaxAtlasBytes: 64 * 64 * 4 * 2}
	k, _, backend := testKernel(t, filesystem, config, func(write *canvas.OpQueue) {
		write.Sprite(0, "edge.png", canvas.SpriteTransform{Size: m.Vec2{X: 12, Y: 4}, TileX: true}, nil)
		write.Sprite(0, "edge.png", canvas.SpriteTransform{Position: m.Vec2{Y: 8}, Size: m.Vec2{X: 4, Y: 4}}, nil)
	})
	runFrame(k)
	if backend.draws != 1 {
		t.Fatalf("draws = %d, want the tiled and the untiled sprite in one atlas batch", backend.draws)
	}
}

// The consumer, in the shape ui emits it: four untiled corners and four tiled
// edges over atlas-fitting images. Every cell is one instance of one draw, where
// each tiled edge used to be a standalone texture that split the batch and
// flushed the atlas run it sat between.
func TestANineSliceWithTiledEdgesIsOneDraw(t *testing.T) {
	filesystem := fstest.MapFS{
		"corner.png": &fstest.MapFile{Data: pngBytes(t, 4, 4)},
		"side.png":   &fstest.MapFile{Data: pngBytes(t, 4, 4)},
	}
	config := canvas.Config{AtlasSize: 64, LayersPerArray: 2, MaxAtlasBytes: 64 * 64 * 4 * 2}
	k, _, backend := testKernel(t, filesystem, config, func(write *canvas.OpQueue) {
		corner := func(x, y float32) {
			write.Sprite(0, "corner.png", canvas.SpriteTransform{
				Position: m.Vec2{X: x, Y: y}, Size: m.Vec2{X: 4, Y: 4},
			}, nil)
		}
		corner(0, 0)
		corner(36, 0)
		corner(0, 36)
		corner(36, 36)
		write.Sprite(0, "side.png", canvas.SpriteTransform{
			Position: m.Vec2{X: 4}, Size: m.Vec2{X: 32, Y: 4}, TileX: true,
		}, nil)
		write.Sprite(0, "side.png", canvas.SpriteTransform{
			Position: m.Vec2{X: 4, Y: 36}, Size: m.Vec2{X: 32, Y: 4}, TileX: true,
		}, nil)
		write.Sprite(0, "side.png", canvas.SpriteTransform{
			Position: m.Vec2{Y: 4}, Size: m.Vec2{X: 4, Y: 32}, TileY: true,
		}, nil)
		write.Sprite(0, "side.png", canvas.SpriteTransform{
			Position: m.Vec2{X: 36, Y: 4}, Size: m.Vec2{X: 4, Y: 32}, TileY: true,
		}, nil)
	})
	runFrame(k)
	if backend.draws != 1 {
		t.Fatalf("draws = %d, want eight border cells in one atlas batch", backend.draws)
	}
}

// A label beside that border is the second draw and only the second: glyphs are
// packed by a packer of their own, so they address a different texture array and
// no rule could ever merge them with sprites.
func TestANineSliceWithTiledEdgesAndALabelAreTwoDraws(t *testing.T) {
	filesystem := fstest.MapFS{
		"side.png":   &fstest.MapFile{Data: pngBytes(t, 4, 4)},
		testFontPath: &fstest.MapFile{Data: goregular.TTF},
	}
	config := canvas.Config{AtlasSize: 128, LayersPerArray: 2, MaxAtlasBytes: 128 * 128 * 4 * 2}
	k, _, backend := testKernel(t, filesystem, config, func(write *canvas.OpQueue) {
		write.Sprite(0, "side.png", canvas.SpriteTransform{
			Position: m.Vec2{X: 4}, Size: m.Vec2{X: 32, Y: 4}, TileX: true,
		}, nil)
		write.Sprite(0, "side.png", canvas.SpriteTransform{
			Position: m.Vec2{X: 4, Y: 36}, Size: m.Vec2{X: 32, Y: 4}, TileX: true,
		}, nil)
		write.Text(0, testFontPath, "Ag", canvas.TextDraw{
			Position: m.Vec2{X: 8, Y: 24}, Size: 16, Color: m.Color{R: 1, G: 1, B: 1, A: 1},
		})
	})
	runFrame(k)
	if backend.draws != 2 {
		t.Fatalf("draws = %d, want the border in one draw and the label in another", backend.draws)
	}
}

// A repeat count of zero would collapse the sampled rectangle to its top-left
// corner, because the wrap multiplies the quad coordinate by it before taking a
// fractional part - so every sprite, glyph and fill would paint in one texel's
// colour. One is therefore the value an untiled instance carries, and it is
// asserted rather than assumed because nothing else in a frame would show it.
func TestAnUntiledInstanceRepeatsOnce(t *testing.T) {
	filesystem := fstest.MapFS{
		"sprite.png": &fstest.MapFile{Data: pngBytes(t, 4, 4)},
		testFontPath: &fstest.MapFile{Data: goregular.TTF},
	}
	config := canvas.Config{AtlasSize: 128, LayersPerArray: 2, MaxAtlasBytes: 128 * 128 * 4 * 2}
	k, _, backend := testKernel(t, filesystem, config, func(write *canvas.OpQueue) {
		write.Sprite(0, "sprite.png", canvas.SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, nil)
		write.FillRect(0, m.Rect{Width: 8, Height: 8}, canvas.ShapeDraw{Color: m.Color{R: 1, G: 1, B: 1, A: 1}})
		write.Text(0, testFontPath, "Ag", canvas.TextDraw{
			Position: m.Vec2{X: 4, Y: 24}, Size: 16, Color: m.Color{R: 1, G: 1, B: 1, A: 1},
		})
	})
	runFrame(k)
	buffers := spriteInstances(backend)
	if len(buffers) == 0 {
		t.Fatal("no sprite instance buffers were uploaded")
	}
	seen := 0
	for _, buffer := range buffers {
		for i := 0; i < len(buffer)/testInstanceSize; i++ {
			instance := instanceAt(buffer, i)
			// misc@64 is atlasLayer, repeatX, repeatY.
			if rx, ry := floatAt(instance, 68), floatAt(instance, 72); rx != 1 || ry != 1 {
				t.Errorf("instance %d repeats = (%v,%v), want (1,1)", i, rx, ry)
			}
			seen++
		}
	}
	if seen < 3 {
		t.Fatalf("instances = %d, want at least the sprite, the fill and two glyphs", seen)
	}
}

// An image whose padded rectangle will not fit an atlas page keeps the standalone
// repeat texture it always had, and says nothing while doing it. The route is
// taken from the header, so the packer is never asked and never refuses - which
// matters because its refusals are terminal and would have reported an error
// before anything could fall back.
func TestATiledSpriteTooLargeForTheAtlasStaysStandalone(t *testing.T) {
	filesystem := fstest.MapFS{"wide.png": &fstest.MapFile{Data: pngBytes(t, 64, 8)}}
	config := canvas.Config{AtlasSize: 32, LayersPerArray: 2, MaxAtlasBytes: 32 * 32 * 4 * 2}
	k, errs, backend := testKernelCapturing(t, filesystem, config, func(write *canvas.OpQueue) {
		write.Sprite(0, "wide.png", canvas.SpriteTransform{Size: m.Vec2{X: 128, Y: 8}, TileX: true}, nil)
	})
	runFrame(k)
	if len(*errs) != 0 {
		t.Fatalf("reported errors = %v, want none: an oversized tiled image has a path of its own", *errs)
	}
	if backend.draws != 1 {
		t.Fatalf("draws = %d, want the standalone quad", backend.draws)
	}
}

// One image drawn both ways is two entries, because the gutter is baked and the
// two draws want different gutters. Two decodes and one header is what that costs
// at the file, and the two entries land at different places in the page.
func TestOnePathDrawnBothWaysPacksTwice(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{"edge.png": &fstest.MapFile{Data: pngBytes(t, 4, 4)}}}
	config := canvas.Config{AtlasSize: 64, LayersPerArray: 2, MaxAtlasBytes: 64 * 64 * 4 * 2}
	k, _, backend := testKernel(t, filesystem, config, func(write *canvas.OpQueue) {
		write.Sprite(0, "edge.png", canvas.SpriteTransform{Size: m.Vec2{X: 12, Y: 4}, TileX: true}, nil)
		write.Sprite(0, "edge.png", canvas.SpriteTransform{Position: m.Vec2{Y: 8}, Size: m.Vec2{X: 4, Y: 4}}, nil)
	})
	runFrame(k)
	if filesystem.opens != 3 {
		t.Fatalf("opens = %d, want the header and one decode per gutter fill", filesystem.opens)
	}
	buffer := spriteInstances(backend)[0]
	tiled, plain := instanceAt(buffer, 0), instanceAt(buffer, 1)
	if floatAt(tiled, 32) == floatAt(plain, 32) && floatAt(tiled, 36) == floatAt(plain, 36) {
		t.Fatal("both draws sampled one entry, want a wrap-filled entry and an extruded one")
	}
}

// UnloadSprite names a file, and after this change a file may be two entries. It
// frees every fill the path was packed at, or the variant a caller did not happen
// to name would stay resident and unreachable by name.
//
// Three reads rather than two: it frees the measurement tier as well, and the
// route a tiled draw takes is decided from the header, so that is read again too.
func TestUnloadSpriteFreesEveryGutterFill(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{"edge.png": &fstest.MapFile{Data: pngBytes(t, 4, 4)}}}
	config := canvas.Config{AtlasSize: 64, LayersPerArray: 2, MaxAtlasBytes: 64 * 64 * 4 * 2}
	k, _, _ := testKernel(t, filesystem, config, func(write *canvas.OpQueue) {
		write.Sprite(0, "edge.png", canvas.SpriteTransform{Size: m.Vec2{X: 12, Y: 4}, TileX: true}, nil)
		write.Sprite(0, "edge.png", canvas.SpriteTransform{Position: m.Vec2{Y: 8}, Size: m.Vec2{X: 4, Y: 4}}, nil)
	})
	runFrame(k)
	before := filesystem.opens
	probeLookupDevice(k, func(la canvas.LookupDeviceAccess) { la.UnloadSprite("edge.png") })
	runFrame(k)
	if got := filesystem.opens - before; got != 3 {
		t.Fatalf("re-reads after UnloadSprite = %d, want both gutter fills decoded again and the header re-measured", got)
	}
}

// Filter is a sprite-batch key field, so it still splits a batch after the move -
// the one piece of per-draw sampler state tiling used to carry that survives it.
func TestTiledSpritesDifferingInFilterSplit(t *testing.T) {
	filesystem := fstest.MapFS{"edge.png": &fstest.MapFile{Data: pngBytes(t, 4, 4)}}
	config := canvas.Config{AtlasSize: 64, LayersPerArray: 2, MaxAtlasBytes: 64 * 64 * 4 * 2}
	k, _, backend := testKernel(t, filesystem, config, func(write *canvas.OpQueue) {
		write.Sprite(0, "edge.png", canvas.SpriteTransform{
			Size: m.Vec2{X: 12, Y: 4}, TileX: true, Filter: gfx.FilterNearest,
		}, nil)
		write.Sprite(0, "edge.png", canvas.SpriteTransform{
			Position: m.Vec2{Y: 8}, Size: m.Vec2{X: 12, Y: 4}, TileX: true, Filter: gfx.FilterLinear,
		}, nil)
	})
	runFrame(k)
	if backend.draws != 2 {
		t.Fatalf("draws = %d, want two filters to be two bindings", backend.draws)
	}
}

// A tiled axis has to be told how long it is. The aspect fallback an untiled
// sprite gets would mean repeating until the aspect ratio matched, which is not
// something a caller can mean, so a tiled axis with no Size records nothing.
func TestATiledAxisWithNoSizeDrawsNothing(t *testing.T) {
	filesystem := fstest.MapFS{"edge.png": &fstest.MapFile{Data: pngBytes(t, 4, 4)}}
	config := canvas.Config{AtlasSize: 64, LayersPerArray: 2, MaxAtlasBytes: 64 * 64 * 4 * 2}
	k, _, backend := testKernel(t, filesystem, config, func(write *canvas.OpQueue) {
		write.Sprite(0, "edge.png", canvas.SpriteTransform{TileX: true}, nil)
	})
	runFrame(k)
	if backend.draws != 0 {
		t.Fatalf("draws = %d, want none: a tiled axis with no Size has no length", backend.draws)
	}
}

// A tiled sprite resolves the sprite family now, so it can name a material at
// all - it could not before, because drawTiledSprite resolved the triangles
// family with a nil material and dropped whatever the op named. Naming the same
// one as an untiled sprite therefore merges with it, which is the same rule every
// other sprite obeys.
func TestATiledSpriteNamesTheSameSpriteMaterialAndMerges(t *testing.T) {
	custom := gfx.MaterialWithState(gfx.ShaderWithText("fn tiledSpriteMark() {}"), gfx.StateOverlay2D())
	filesystem := fstest.MapFS{"edge.png": &fstest.MapFile{Data: pngBytes(t, 4, 4)}}
	config := canvas.Config{AtlasSize: 64, LayersPerArray: 2, MaxAtlasBytes: 64 * 64 * 4 * 2}
	k, _, backend := testKernel(t, filesystem, config, func(write *canvas.OpQueue) {
		write.Sprite(0, "edge.png", canvas.SpriteTransform{Size: m.Vec2{X: 12, Y: 4}, TileX: true}, &custom)
		write.Sprite(0, "edge.png", canvas.SpriteTransform{Position: m.Vec2{Y: 8}, Size: m.Vec2{X: 4, Y: 4}}, &custom)
	})
	runFrame(k)
	if backend.draws != 1 {
		t.Fatalf("draws = %d, want one material named by both to be one batch", backend.draws)
	}
}
