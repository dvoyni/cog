package internal

import (
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/gfx"
)

// A texture-sourced quad is a triangle draw in the built-in vertex layout, so it
// belongs in the triangles batcher like every other one. It used to emit
// straight to the queue, which made a nine-slice nine draws for one sprite.
func TestNineSliceOverOneTextureIsOneDraw(t *testing.T) {
	k, _, backend := testKernelGfx(t, fstest.MapFS{}, targetTestConfig(), func(write *canvas.OpQueue, gfxWrite *gfx.OpQueue) {
		_, texture := gfxWrite.TemporaryTarget(64, 32, gfx.FormatRGBA8Srgb)
		write.SpriteTexture(0, texture, canvas.SpriteTransform{
			Size:      m.Vec2{X: 128, Y: 64},
			NineSlice: canvas.SpriteFrame{Left: 8, Top: 8, Right: 8, Bottom: 8},
		}, nil)
	})
	runFrame(k)
	if backend.draws != 1 {
		t.Fatalf("draws = %d, want the nine parts of one nine-slice merged into 1", backend.draws)
	}
}

// The batch is what makes a run of panels, portraits or camera views one draw.
func TestConsecutiveTextureSpritesOverOneTextureMerge(t *testing.T) {
	k, _, backend := testKernelGfx(t, fstest.MapFS{}, targetTestConfig(), func(write *canvas.OpQueue, gfxWrite *gfx.OpQueue) {
		_, texture := gfxWrite.TemporaryTarget(64, 32, gfx.FormatRGBA8Srgb)
		for i := range 3 {
			write.SpriteTexture(0, texture, canvas.SpriteTransform{Position: m.Vec2{X: float32(i * 70)}}, nil)
		}
	})
	runFrame(k)
	if backend.draws != 1 {
		t.Fatalf("draws = %d, want three sprites over one texture in 1 batch", backend.draws)
	}
}

// The texture is bind-group state, so it splits the batch - and it does so with
// no key field of canvas's own, because gfx's parameter fingerprint already
// hashes a texture parameter by identity.
func TestTextureSpritesOverDifferentTexturesSplit(t *testing.T) {
	k, _, backend := testKernelGfx(t, fstest.MapFS{}, targetTestConfig(), func(write *canvas.OpQueue, gfxWrite *gfx.OpQueue) {
		_, first := gfxWrite.TemporaryTarget(64, 32, gfx.FormatRGBA8Srgb)
		_, second := gfxWrite.TemporaryTarget(64, 32, gfx.FormatRGBA8Srgb)
		write.SpriteTexture(0, first, canvas.SpriteTransform{}, nil)
		write.SpriteTexture(0, second, canvas.SpriteTransform{Position: m.Vec2{X: 70}}, nil)
	})
	runFrame(k)
	if backend.draws != 2 {
		t.Fatalf("draws = %d, want 2: two textures cannot share a bind group", backend.draws)
	}
}

// Same reasoning one step down: a tiled quad samples through a repeat sampler
// and a plain one through a clamp, and a sampler is bind-group state too.
func TestATiledAndAPlainTextureSpriteSplit(t *testing.T) {
	k, _, backend := testKernelGfx(t, fstest.MapFS{}, targetTestConfig(), func(write *canvas.OpQueue, gfxWrite *gfx.OpQueue) {
		_, texture := gfxWrite.TemporaryTarget(64, 32, gfx.FormatRGBA8Srgb)
		write.SpriteTexture(0, texture, canvas.SpriteTransform{Size: m.Vec2{X: 128, Y: 32}, TileX: true}, nil)
		write.SpriteTexture(0, texture, canvas.SpriteTransform{Position: m.Vec2{Y: 40}}, nil)
	})
	runFrame(k)
	if backend.draws != 2 {
		t.Fatalf("draws = %d, want 2: the repeat and clamp samplers are different bindings", backend.draws)
	}
}

// A tiled path sprite draws a standalone repeat texture through the same
// textured-triangle path, so it batches under the same rule.
func TestTiledSpritesOverOneStandaloneTextureMerge(t *testing.T) {
	filesystem := fstest.MapFS{"wave.png": &fstest.MapFile{Data: pngBytes(t, 4, 4)}}
	config := canvas.Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	k, _, backend := testKernel(t, filesystem, config, func(write *canvas.OpQueue) {
		write.Sprite(0, "wave.png", canvas.SpriteTransform{Size: m.Vec2{X: 12, Y: 4}, TileX: true}, nil)
		write.Sprite(0, "wave.png", canvas.SpriteTransform{Position: m.Vec2{Y: 8}, Size: m.Vec2{X: 12, Y: 4}, TileX: true}, nil)
	})
	runFrame(k)
	if backend.draws != 1 {
		t.Fatalf("draws = %d, want two tiled quads over one standalone texture in 1 batch", backend.draws)
	}
}

// Recording order is the contract. An atlas sprite between two texture sprites
// cannot join either batch, so it closes the first and the second opens after
// it - three draws, in the order they were recorded.
func TestAnAtlasSpriteBetweenTwoTextureSpritesSplitsThem(t *testing.T) {
	filesystem := fstest.MapFS{"sprite.png": &fstest.MapFile{Data: pngBytes(t, 4, 4)}}
	config := canvas.Config{AtlasSize: 16, LayersPerArray: 2, MaxAtlasBytes: 16 * 16 * 4 * 2}
	k, _, backend := testKernelGfx(t, filesystem, config, func(write *canvas.OpQueue, gfxWrite *gfx.OpQueue) {
		_, texture := gfxWrite.TemporaryTarget(8, 8, gfx.FormatRGBA8Srgb)
		write.SpriteTexture(0, texture, canvas.SpriteTransform{}, nil)
		write.Sprite(0, "sprite.png", canvas.SpriteTransform{Position: m.Vec2{X: 20}}, nil)
		write.SpriteTexture(0, texture, canvas.SpriteTransform{Position: m.Vec2{X: 40}}, nil)
	})
	runFrame(k)
	if backend.draws != 3 {
		t.Fatalf("draws = %d, want 3: the atlas sprite must not be reordered past either texture sprite", backend.draws)
	}
}
