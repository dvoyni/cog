package internal

import (
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/libs/m"
)

// A sprite that is not there used to be re-opened and fully re-decoded every
// frame, forever, and reported nowhere: the atlas cleared its failed set at the
// top of every frame so that a file which had since appeared would be tried
// again. Nothing in the tree makes a file appear, so all that bought was the
// re-decode.
//
// The entry is the once now: whatever the load produced is what the cache holds,
// so the file is opened one time, the failure is said one time, and later frames
// ask the table rather than the disk.
func TestAMissingSpriteIsOpenedOnceReportedOnceAndNotReopened(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{}}
	config := Config{AtlasSize: 32, LayersPerArray: 2, MaxAtlasBytes: 32 * 32 * 4 * 2}
	k, errs, _ := testKernelCapturing(t, filesystem, config, func(write *OpQueue) {
		write.Sprite(0, "gone.png", SpriteTransform{Size: m.Vec2{X: 8, Y: 8}}, nil)
	})
	runFrame(k)
	runFrame(k)
	runFrame(k)

	if filesystem.opens != 1 {
		t.Fatalf("opens across three frames = %d, want the one that failed", filesystem.opens)
	}
	if len(*errs) != 1 {
		t.Fatalf("reported errors = %d, want one per episode: %v", len(*errs), *errs)
	}
}

// The value of a sprite that did not load is the zero entry, which draws
// nothing. A magenta placeholder was refused: a sprite's on-screen size comes
// from the transform or from its own pixels, so a stand-in would be visible
// exactly when the draw named a size and invisible when the draw trusted the
// file. The loudness belongs in the report.
func TestAMissingSpriteIsSkippedRatherThanSubstituted(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{}}
	config := Config{AtlasSize: 32, LayersPerArray: 2, MaxAtlasBytes: 32 * 32 * 4 * 2}
	k, _, backend := testKernelCapturing(t, filesystem, config, func(write *OpQueue) {
		write.Sprite(0, "gone.png", SpriteTransform{Size: m.Vec2{X: 8, Y: 8}}, nil)
		write.Sprite(0, "gone-tiled.png", SpriteTransform{
			Size: m.Vec2{X: 8, Y: 8}, TileX: true,
		}, nil)
	})
	runFrame(k)

	if instances := spriteInstances(backend); len(instances) != 0 {
		t.Fatalf("instance buffers = %d, want nothing drawn for two missing sprites", len(instances))
	}
	if backend.draws != 0 {
		t.Fatalf("draws = %d, want nothing drawn for two missing sprites", backend.draws)
	}
	if len(backend.updates) != 1 {
		t.Fatalf("uploads = %d, want the reserved white texel and no placeholder", len(backend.updates))
	}
}

// The white texel is reserved at the top of the frame rather than lazily beside
// the first sprite that needs one, because the atlas batch is keyed on the
// texture: a texel that landed in a second array would split every fill away
// from every sprite it draws with. A frame that records nothing at all still
// reserves it, which is what "before any layer's ops" means.
func TestTheWhiteTexelIsReservedBeforeAnyLayersOps(t *testing.T) {
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	k, _, backend := testKernel(t, fstest.MapFS{}, config, func(*OpQueue) {})
	runFrame(k)

	if len(backend.updates) != 1 {
		t.Fatalf("uploads on an empty frame = %d, want the reserved white texel", len(backend.updates))
	}
	update := backend.updates[0]
	if update.region.Width != 1 || update.region.Height != 1 || len(update.pixels) != 4 {
		t.Fatalf("reserved upload = %+v of %d bytes, want one texel", update.region, len(update.pixels))
	}
	for i, value := range update.pixels {
		if value != 255 {
			t.Fatalf("reserved texel byte %d = %d, want opaque white", i, value)
		}
	}
}

// UnloadSprite names a file, and the texel is named by its bytes, so the one
// sprite it cannot free is the one canvas generates. The empty path is refused
// and reported rather than quietly freeing something, and the texel stays
// resident - the residual this leaves, and what UnloadAll answers.
func TestUnloadSpriteCannotNameTheGeneratedTexel(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{}}
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	k, errs, backend := testKernelCapturing(t, filesystem, config, func(write *OpQueue) {
		write.FillRect(0, m.Rect{Width: 4, Height: 4}, ShapeDraw{Color: m.Color{R: 1, A: 1}})
	})
	runFrame(k)
	probeLookupDevice(k, func(la LookupDeviceAccess) { la.UnloadSprite("") })
	runFrame(k)

	if len(*errs) != 1 {
		t.Fatalf("reported errors = %d, want the refused empty path: %v", len(*errs), *errs)
	}
	if len(backend.releasedTextures) != 0 {
		t.Fatalf("released textures = %v, want the texel's array kept", backend.releasedTextures)
	}
	if len(backend.updates) != 1 {
		t.Fatalf("texel uploads = %d, want the one reservation still standing", len(backend.updates))
	}
}

// The texel is named by its bytes rather than by a sentinel path, and the bytes
// are a const, so every frame's descriptor is one identity and one cache entry.
// Counted through uploads rather than by comparing two descriptors: a blob's
// identity is an address, and two constructor calls compared in one expression
// can share a stack slot and report an equality the heap does not have.
func TestTheWhiteTexelIsOneEntryAcrossFrames(t *testing.T) {
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	k, _, backend := testKernel(t, fstest.MapFS{}, config, func(write *OpQueue) {
		write.FillRect(0, m.Rect{Width: 4, Height: 4}, ShapeDraw{Color: m.Color{R: 1, A: 1}})
	})
	runFrame(k)
	runFrame(k)
	runFrame(k)

	if len(backend.updates) != 1 {
		t.Fatalf("white texel uploads across three frames = %d, want one entry", len(backend.updates))
	}
}

// Validation converges on one rule applied where a path enters. "." used to
// clean to the empty path and draw a silent white quad; it names a directory
// where a file belongs, so it is refused now, said once, and never handed to a
// cache. The empty path keeps meaning the white texel.
func TestADotPathIsInvalidRatherThanASilentWhiteQuad(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{}}
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	k, errs, backend := testKernelCapturing(t, filesystem, config, func(write *OpQueue) {
		write.Sprite(0, ".", SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, nil)
		write.Sprite(0, "../escape.png", SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, nil)
		write.Sprite(0, "", SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, nil)
	})
	runFrame(k)
	runFrame(k)

	if filesystem.opens != 0 {
		t.Fatalf("storage opens = %d, want an invalid path never to reach a cache", filesystem.opens)
	}
	if len(*errs) != 2 {
		t.Fatalf("reported errors = %d, want one per invalid path per episode: %v", len(*errs), *errs)
	}
	instances := spriteInstances(backend)
	if len(instances) != 2 {
		t.Fatalf("instance buffers = %d, want one batch per frame", len(instances))
	}
	if len(instances[0]) != testInstanceSize {
		t.Fatalf("instances in a frame = %d, want the white quad alone and nothing for the two refused paths",
			len(instances[0])/testInstanceSize)
	}
}

// The packer refuses an image larger than a page, and that refusal is terminal:
// the zero entry it produces is cached like any other value, so the sprite is
// read once, said once, and drawn never. A sprite is padded by 2 on each side,
// so the largest that can pack is AtlasSize-4.
func TestASpriteLargerThanAPageIsRefusedOnceAndTerminally(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{"huge.png": &fstest.MapFile{Data: pngBytes(t, 14, 14)}}}
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	k, errs, backend := testKernelCapturing(t, filesystem, config, func(write *OpQueue) {
		write.Sprite(0, "huge.png", SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, nil)
	})
	runFrame(k)
	runFrame(k)

	if filesystem.opens != 1 {
		t.Fatalf("opens = %d, want the refusal cached rather than retried", filesystem.opens)
	}
	if len(*errs) != 1 {
		t.Fatalf("reported errors = %d, want one: %v", len(*errs), *errs)
	}
	if len(backend.updates) != 1 {
		t.Fatalf("uploads = %d, want the white texel and nothing of the refused sprite", len(backend.updates))
	}
}

// The other refusal: every layer of every array full, no tombstoned index to
// reuse, and another array over MaxAtlasBytes. It is contingent on what else is
// resident and it is terminal anyway - a game that fills its configured budget
// learns so in development, and unloading is what gives the slots back.
func TestASpriteOverTheAtlasByteBudgetIsRefusedOnceAndTerminally(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{
		"first.png":  &fstest.MapFile{Data: pngBytes(t, 10, 10)},
		"second.png": &fstest.MapFile{Data: pngBytes(t, 10, 10)},
		"late.png":   &fstest.MapFile{Data: pngBytes(t, 10, 10)},
	}}
	// A budget for exactly one array of two 16-texel pages. The white texel and
	// two padded 14x14 sprites fill both pages, and the third sprite would need
	// an array the budget cannot pay for.
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	k, errs, backend := testKernelCapturing(t, filesystem, config, func(write *OpQueue) {
		write.Sprite(0, "first.png", SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, nil)
		write.Sprite(0, "second.png", SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, nil)
		write.Sprite(0, "late.png", SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, nil)
	})
	runFrame(k)
	runFrame(k)

	if len(*errs) != 1 {
		t.Fatalf("reported errors = %d, want one over-budget refusal per episode: %v", len(*errs), *errs)
	}
	if filesystem.opens != 3 {
		t.Fatalf("opens = %d, want each sprite read once and the refusal cached", filesystem.opens)
	}
	if len(backend.allocations) != 1 {
		t.Fatalf("texture arrays = %d, want the budget to hold exactly one", len(backend.allocations))
	}
}

// SpriteSize answers from the header tier and from nothing else. It used to
// prefer a resident atlas entry, so that layout tracked the pixels actually
// drawn; that read was a non-loading probe of the sprite table, and an asset
// cache has no such read - its one read loads, and from a handler holding no
// resource queue it could not load anyway. So a sprite already packed into the
// atlas is still measured by reading its header.
func TestSpriteSizeAnswersFromTheHeaderTierEvenWhenTheSpriteIsResident(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{"sprite.png": &fstest.MapFile{Data: pngBytes(t, 6, 4)}}}
	config := Config{AtlasSize: 32, LayersPerArray: 2, MaxAtlasBytes: 32 * 32 * 4 * 2}
	k, _, _ := testKernel(t, filesystem, config, func(write *OpQueue) {
		write.Sprite(0, "sprite.png", SpriteTransform{Size: m.Vec2{X: 8, Y: 8}}, nil)
	})
	runFrame(k)
	if filesystem.opens != 1 {
		t.Fatalf("opens after the draw = %d, want the one full decode", filesystem.opens)
	}

	var size m.Vec2
	probeLookup(k, func(la LookupAccess) { size = la.SpriteSize("sprite.png") })
	if size != (m.Vec2{X: 6, Y: 4}) {
		t.Fatalf("size = %+v, want 6x4", size)
	}
	if filesystem.opens != 2 {
		t.Fatalf("opens after the measurement = %d, want the header read the sprite tier cannot answer",
			filesystem.opens)
	}
}

// A failure is cached like any other value, so unloading one hands the packer a
// zero entry to free. It must reclaim nothing: the zero entry's array index is
// zero, which is a live array, and treating it as a slot would return a bogus
// rectangle to the free list and count an occupant off an array that never had
// one - eventually releasing an array still full of sprites.
func TestFreeingASpriteThatNeverPackedReclaimsNothing(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{"sprite.png": &fstest.MapFile{Data: pngBytes(t, 4, 4)}}}
	config := Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	k, _, backend := testKernelCapturing(t, filesystem, config, func(write *OpQueue) {
		write.Sprite(0, "gone.png", SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, nil)
		write.Sprite(0, "also-gone.png", SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, nil)
		write.Sprite(0, "sprite.png", SpriteTransform{Size: m.Vec2{X: 4, Y: 4}}, nil)
	})
	runFrame(k)
	// Two of them, because the array holds two real occupants - the white texel
	// and the sprite - so two bogus reclaims are what would count it empty.
	probeLookupDevice(k, func(la LookupDeviceAccess) {
		la.UnloadSprite("gone.png")
		la.UnloadSprite("also-gone.png")
	})
	runFrame(k)

	if len(backend.releasedTextures) != 0 {
		t.Fatalf("released textures = %v, want the array holding the white texel and the sprite kept",
			backend.releasedTextures)
	}
	if len(backend.allocations) != 1 {
		t.Fatalf("texture arrays = %d, want the one that never emptied", len(backend.allocations))
	}
	if instances := spriteInstances(backend); len(instances) == 0 {
		t.Fatal("the resident sprite stopped drawing after a failed sibling was unloaded")
	}
}

// One missing path is three failures at three different times - layout, draw,
// tiled draw - and each tier says its own once. They are three caches keyed by
// three descriptor types, so none of them can silence another: the kernel's
// report table is keyed by type, and a tier that shared a descriptor type with
// another would share its namespace.
func TestOneMissingPathIsOneReportPerTier(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{}}
	config := Config{AtlasSize: 32, LayersPerArray: 2, MaxAtlasBytes: 32 * 32 * 4 * 2}
	k, errs, _ := testKernelCapturing(t, filesystem, config, func(write *OpQueue) {
		write.Sprite(0, "gone.png", SpriteTransform{Size: m.Vec2{X: 8, Y: 8}}, nil)
		write.Sprite(0, "gone.png", SpriteTransform{Size: m.Vec2{X: 8, Y: 8}, TileX: true}, nil)
	})
	runFrame(k)
	runFrame(k)
	probeLookup(k, func(la LookupAccess) { _ = la.SpriteSize("gone.png") })
	probeLookup(k, func(la LookupAccess) { _ = la.SpriteSize("gone.png") })

	if len(*errs) != 3 {
		t.Fatalf("reported errors = %d, want one each for the draw, the tiled draw and the measurement: %v",
			len(*errs), *errs)
	}
	if filesystem.opens != 3 {
		t.Fatalf("opens = %d, want one failed read per tier and no retry", filesystem.opens)
	}
}

// Unloading a sprite frees it from every tier that may hold it, and asking for
// it again is a reload rather than an error. It is also the one lever a terminal
// failure has: each entry's report-once key goes with it, so a path that failed,
// was fixed and was asked for again can speak.
func TestUnloadingASpriteFreesEveryTierAndLetsItSpeakAgain(t *testing.T) {
	filesystem := &testFS{FS: fstest.MapFS{}}
	config := Config{AtlasSize: 32, LayersPerArray: 2, MaxAtlasBytes: 32 * 32 * 4 * 2}
	k, errs, _ := testKernelCapturing(t, filesystem, config, func(write *OpQueue) {
		write.Sprite(0, "gone.png", SpriteTransform{Size: m.Vec2{X: 8, Y: 8}}, nil)
	})
	runFrame(k)
	probeLookup(k, func(la LookupAccess) { _ = la.SpriteSize("gone.png") })
	// Two reports for one missing file: the draw tier and the measurement tier
	// are two caches keyed by two descriptor types, so each says its own failure
	// once. They are two different failures at two different times.
	if len(*errs) != 2 {
		t.Fatalf("reported errors = %d, want one per tier that tried: %v", len(*errs), *errs)
	}

	probeLookupDevice(k, func(la LookupDeviceAccess) { la.UnloadSprite("gone.png") })
	runFrame(k)
	probeLookup(k, func(la LookupAccess) { _ = la.SpriteSize("gone.png") })
	if len(*errs) != 4 {
		t.Fatalf("reported errors after the unload = %d, want both tiers to speak again: %v", len(*errs), *errs)
	}
	if filesystem.opens != 4 {
		t.Fatalf("opens = %d, want two per episode across two episodes", filesystem.opens)
	}
}
