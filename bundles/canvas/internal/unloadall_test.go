package internal

import (
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"golang.org/x/image/font/gofont/goregular"
)

// UnloadAll is the one verb canvas gains from the migration onto assets, and it
// is memory at a level boundary rather than a developer loop. These are the four
// properties it is built for: it empties all five caches, it frees the one
// sprite UnloadSprite cannot name, it is what makes a terminally refused sprite
// packable again, and the residual it leaves for a game that unloads per path
// instead.

// levelConfig is a budget for exactly one texture array of two 16-texel pages.
// The white texel and two sprites padded to 14x14 fill both pages, so a third
// sprite needs an array the budget cannot pay for.
var levelConfig = canvas.Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}

// levelRig draws two level-one sprites, until levelOne is cleared, and a
// level-two sprite in every frame. It is the streaming shape: what the game
// records changes at the boundary, and what is resident is what decides whether
// the level-two sprite fits.
func levelRig(t *testing.T) (k kernel.Executioner, levelOne *atomic.Bool, filesystem *testFS, errs *[]error, backend *testBackend) {
	t.Helper()
	filesystem = &testFS{FS: fstest.MapFS{
		"level1a.png": &fstest.MapFile{Data: pngBytes(t, 10, 10)},
		"level1b.png": &fstest.MapFile{Data: pngBytes(t, 10, 10)},
		"level2.png":  &fstest.MapFile{Data: pngBytes(t, 10, 10)},
	}}
	levelOne = &atomic.Bool{}
	levelOne.Store(true)
	transform := canvas.SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}
	k, errs, backend = testKernelCapturing(t, filesystem, levelConfig, func(write *canvas.OpQueue) {
		if levelOne.Load() {
			write.Sprite(0, "level1a.png", transform, nil)
			write.Sprite(0, "level1b.png", transform, nil)
		}
		write.Sprite(0, "level2.png", transform, nil)
	})
	return k, levelOne, filesystem, errs, backend
}

// TestUnloadAllFreesEveryCache is the whole of what the verb is: five FreeAll
// calls, one per tier, so nothing canvas read stays resident.
//
// It is counted in storage opens rather than in table sizes, because a cache has
// no probe - Get is its only read and it loads on a miss - so the only way to ask
// whether an entry survived is to ask for it again and watch the file. Five reads
// build the state: the sprite's full decode, the tiled sprite's header and its
// decode, the font file the source tier parses, and the header the measurement
// tier reads. Ten after the boundary is each of them read a second time.
//
// The tiled sprite costs two because it is an atlas entry now: its header is read
// to decide whether its padded rectangle fits a page, and its pixels are read to
// pack it. Both are cached, in two different tiers.
//
// The font halves are not separable by a count, and do not need to be: the face
// tier reaches the bytes by re-entering the source cache, so a surviving face
// opens nothing and a surviving source serves a re-baked face without opening
// either. Only both being freed reads the file again.
func TestUnloadAllFreesEveryCache(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{
		"sprite.png": &fstest.MapFile{Data: pngBytes(t, 6, 4)},
		"tile.png":   &fstest.MapFile{Data: pngBytes(t, 6, 4)},
		testFontPath: &fstest.MapFile{Data: goregular.TTF},
	}}
	k, _, _ := testKernel(t, filesystem, canvas.Config{}, func(write *canvas.OpQueue) {
		write.Sprite(0, "sprite.png", canvas.SpriteTransform{Size: m.Vec2{X: 8, Y: 8}}, nil)
		write.Sprite(0, "tile.png", canvas.SpriteTransform{Size: m.Vec2{X: 8, Y: 8}, TileX: true}, nil)
		write.Text(0, testFontPath, "Ag", canvas.TextDraw{
			Position: m.Vec2{X: 4, Y: 20}, Size: 16, Color: m.Color{R: 1, G: 1, B: 1, A: 1},
		})
	})
	runFrame(k)
	probeLookup(k, func(la canvas.LookupAccess) { _ = la.SpriteSize("sprite.png") })
	if filesystem.opens != 5 {
		t.Fatalf("opens filling the caches = %d, want the sprite, the tiled sprite's header and decode, the font and the header",
			filesystem.opens)
	}
	runFrame(k)
	probeLookup(k, func(la canvas.LookupAccess) { _ = la.SpriteSize("sprite.png") })
	if filesystem.opens != 5 {
		t.Fatalf("opens on a second frame = %d, want every tier to answer from its table", filesystem.opens)
	}

	probeLookupDevice(k, func(la canvas.LookupDeviceAccess) { la.UnloadAll() })

	runFrame(k)
	probeLookup(k, func(la canvas.LookupAccess) { _ = la.SpriteSize("sprite.png") })
	if filesystem.opens != 10 {
		t.Fatalf("opens after UnloadAll = %d, want all five caches emptied and every file read again",
			filesystem.opens)
	}
}

// TestUnloadAllFreesTheGeneratedTexelAndTheNextFrameReservesIt is the residual
// #459 handed this ticket. UnloadSprite names a file and the white texel is
// named by its bytes, so it is the one sprite that verb cannot free; UnloadAll
// names nothing and frees it with the rest.
//
// Freeing it is safe because the reservation is not lazy: flushFrame packs the
// texel before any layer's ops, so the frame after the boundary puts it back
// before it draws anything with it. Both halves are asserted here - the array it
// was alone in is released, and the very next upload is one opaque white texel -
// because freeing it without the reservation would be every fill, line and
// stroke silently vanishing.
func TestUnloadAllFreesTheGeneratedTexelAndTheNextFrameReservesIt(t *testing.T) {
	k, _, backend := testKernel(t, fstest.MapFS{}, levelConfig, func(write *canvas.OpQueue) {
		write.FillRect(0, m.Rect{Width: 4, Height: 4}, canvas.ShapeDraw{Color: m.Color{R: 1, A: 1}})
	})
	runFrame(k)
	if len(backend.updates) != 1 || len(backend.releasedTextures) != 0 {
		t.Fatalf("uploads = %d and releases = %d before the boundary, want the one reservation standing",
			len(backend.updates), len(backend.releasedTextures))
	}

	probeLookupDevice(k, func(la canvas.LookupDeviceAccess) { la.UnloadAll() })
	runFrame(k)

	if len(backend.releasedTextures) != 1 {
		t.Fatalf("released textures = %v, want the array the texel was alone in given back",
			backend.releasedTextures)
	}
	if len(backend.updates) != 2 {
		t.Fatalf("uploads = %d, want the texel freed and reserved again", len(backend.updates))
	}
	reserved := backend.updates[1]
	if reserved.region.Width != 1 || reserved.region.Height != 1 || len(reserved.pixels) != 4 {
		t.Fatalf("first upload after the boundary = %+v of %d bytes, want the reserved texel",
			reserved.region, len(reserved.pixels))
	}
	for i, value := range reserved.pixels {
		if value != 255 {
			t.Fatalf("reserved texel byte %d = %d, want opaque white", i, value)
		}
	}
	if instances := spriteInstances(backend); len(instances) != 2 {
		t.Fatalf("instance buffers = %d, want the fill drawn in the frame after the boundary too",
			len(instances))
	}
}

// TestUnloadAllLetsARefusedSpritePackAtTheNextLevel is the sequence the
// migration introduced and this verb answers.
//
// The budget wall is contingent on what else is resident, and every returned
// value is cached, so level two's sprite finds no room, caches that refusal
// terminally, and would never pack again however empty the atlas later became.
// Freeing level one is a Free, so the lever is being pulled - just not on the
// entry that needs it, and the game would have to name a sprite it has every
// reason to believe was never loaded. UnloadAll frees the cached failure along
// with everything else, so the discarded retry stays discarded and the next
// level packs into an empty atlas.
func TestUnloadAllLetsARefusedSpritePackAtTheNextLevel(t *testing.T) {
	k, levelOne, filesystem, errs, backend := levelRig(t)

	runFrame(k)
	if len(*errs) != 1 {
		t.Fatalf("reported errors = %d, want the one over-budget refusal: %v", len(*errs), *errs)
	}
	if len(backend.updates) != 3 {
		t.Fatalf("uploads = %d, want the texel and level one, and nothing of the refused sprite",
			len(backend.updates))
	}

	levelOne.Store(false)
	probeLookupDevice(k, func(la canvas.LookupDeviceAccess) { la.UnloadAll() })
	runFrame(k)

	if len(backend.updates) != 5 {
		t.Fatalf("uploads after the boundary = %d, want the texel reserved again and the refused sprite packed",
			len(backend.updates))
	}
	if filesystem.opens != 4 {
		t.Fatalf("opens = %d, want the freed failure read again rather than answered from the table",
			filesystem.opens)
	}
	if len(*errs) != 1 {
		t.Fatalf("reported errors after the boundary = %d, want the refusal not to recur: %v", len(*errs), *errs)
	}
}

// TestUnloadingPerPathKeepsTheCachedFailure is the residual, stated rather than
// left to be discovered. Naming level one's sprites gives their slots back - the
// array is not even released, because the white texel still occupies it - but
// level two's entry is untouched, and an entry is the once: the refusal it holds
// is what every later frame reads. Nothing is uploaded and nothing is re-read.
//
// It is narrow and it is visible the moment a developer looks, and UnloadAll is
// what a game streaming levels calls instead.
func TestUnloadingPerPathKeepsTheCachedFailure(t *testing.T) {
	k, levelOne, filesystem, errs, backend := levelRig(t)

	runFrame(k)
	if len(*errs) != 1 || len(backend.updates) != 3 {
		t.Fatalf("errors = %d and uploads = %d before the boundary, want the refusal cached: %v",
			len(*errs), len(backend.updates), *errs)
	}

	levelOne.Store(false)
	probeLookupDevice(k, func(la canvas.LookupDeviceAccess) {
		la.UnloadSprite("level1a.png")
		la.UnloadSprite("level1b.png")
	})
	runFrame(k)

	if len(backend.updates) != 3 {
		t.Fatalf("uploads after a per-path boundary = %d, want the cached refusal to keep the sprite out",
			len(backend.updates))
	}
	if filesystem.opens != 3 {
		t.Fatalf("opens = %d, want the failed entry answered from the table rather than retried",
			filesystem.opens)
	}
	if len(backend.releasedTextures) != 0 {
		t.Fatalf("released textures = %v, want the array the texel still occupies kept",
			backend.releasedTextures)
	}
}
