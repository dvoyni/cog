package render_test

import (
	"slices"
	"testing"

	"github.com/dvoyni/cog/render"
	"github.com/dvoyni/cog/render/rendertest"
)

func TestEveryLevelOfEveryLayerOfA2DArrayAndACubeTextureIsGeneratedLevelByLevel(t *testing.T) {
	h := rendertest.NewHarness(t)
	var array, cube render.Texture
	h.Spawn(nil, &recorder{onFrame: []func(render.Frame){func(frame render.Frame) {
		if array == (render.Texture{}) {
			array = frame.AcquireMippedTexture(render.TextureDescriptor{Label: "atlas", Width: 8, Height: 4, LayerCount: 3, MipLevelCount: 4, Format: render.TextureFormatRGBA8UnormSrgb})
			cube = frame.AcquireMippedTexture(render.TextureDescriptor{Label: "sky", Width: 4, Height: 4, LayerCount: 6, MipLevelCount: 3, Format: render.TextureFormatRGBA16Float})
		}
	}}})

	h.Step()
	h.Step()

	var want []render.ColorTarget
	for _, texture := range []struct {
		texture                   render.Texture
		mipLevelCount, layerCount uint32
	}{{array, 4, 3}, {cube, 3, 6}} {
		for mipLevel := uint32(1); mipLevel < texture.mipLevelCount; mipLevel++ {
			for layer := range texture.layerCount {
				want = append(want, render.ColorTarget{Texture: texture.texture, MipLevel: mipLevel, Layer: layer, Clear: true})
			}
		}
	}
	// The second frame records nothing, its textures' mips generated
	// already, so gfx submits nothing for it.
	frames := h.Frames()
	if len(frames) != 1 || len(frames[0].CommandLists) != 1 {
		t.Fatalf("the harness read back %d frames, want 1 of render's own list alone", len(frames))
	}
	var got []render.ColorTarget
	for _, pass := range frames[0].CommandLists[0].Passes {
		got = append(got, pass.ColorTargets...)
	}
	if !slices.Equal(got, want) {
		t.Errorf("render's own list drew into %v, want %v", got, want)
	}
}
