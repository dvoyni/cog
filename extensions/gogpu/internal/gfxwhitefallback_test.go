package internal

import (
	"testing"

	"github.com/dvoyni/cog/slots/gfx"
	"github.com/gogpu/wgpu"
)

// The white fallback is not a courtesy to a forgotten parameter: a texture that
// has not finished loading, or failed to, resolves to the same unknown id, so
// white is what an unresolved texture renders as. That has to hold at whichever
// dimension the binding declares, because a view bound where the layout says
// another dimension is refused outright - which is not white, it is nothing.
func TestAnUnresolvedTextureTakesTheWhiteOfItsBindingsDimension(t *testing.T) {
	b := newGfxBackend()
	flat, pages := &wgpu.TextureView{}, &wgpu.TextureView{}
	b.white = &gfxbTexture{view: flat}
	b.whiteArray = pages
	shader := newGfxbShader("canvas.wgsl", nil, gfx.ShaderLayout{Resources: []gfx.ShaderResource{
		{Name: "flatTexture", Group: 1, Binding: 1, TextureView: gfx.TextureView2D},
		{Name: "canvasTexture", Group: 1, Binding: 2, TextureView: gfx.TextureView2DArray},
	}})
	pass := &gfxRenderPass{backend: b, shader: shader}

	pass.SetTexture(0, 1, 1)
	pass.SetTexture(0, 1, 2)

	if len(b.acc[1]) != 2 {
		t.Fatalf("pending entries = %d, want 2", len(b.acc[1]))
	}
	if got := b.acc[1][0].native.TextureView; got != flat {
		t.Errorf("texture_2d binding took %p, want the 2D white %p", got, flat)
	}
	if got := b.acc[1][1].native.TextureView; got != pages {
		t.Errorf("texture_2d_array binding took %p, want the array white %p", got, pages)
	}
}

// A resolved texture is bound as it is: the dimension it was created at is the
// texture's own, and choosing a white for it would mean replacing a texture the
// caller supplied.
func TestAResolvedTextureIgnoresTheWhites(t *testing.T) {
	b := newGfxBackend()
	b.white = &gfxbTexture{view: &wgpu.TextureView{}}
	b.whiteArray = &wgpu.TextureView{}
	baked := &wgpu.TextureView{}
	b.bakedTextures[7] = &gfxbTexture{view: baked}
	shader := newGfxbShader("canvas.wgsl", nil, gfx.ShaderLayout{Resources: []gfx.ShaderResource{
		{Name: "canvasTexture", Group: 1, Binding: 2, TextureView: gfx.TextureView2DArray},
	}})
	pass := &gfxRenderPass{backend: b, shader: shader}

	pass.SetTexture(7, 1, 2)

	if got := b.acc[1][0].native.TextureView; got != baked {
		t.Errorf("resolved texture bound %p, want the baked view %p", got, baked)
	}
}
