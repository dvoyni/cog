package render

import (
	"cmp"
	"fmt"

	"github.com/dvoyni/cog/gfx"
)

// TextureRegion is the rectangle of one mip level and one layer of a texture
// that WriteTexture writes or a copy copies from or into: the level, the layer, its top-left texel, and its
// size in texels, a width or height of zero meaning the rest of the level
// from X or Y. The zero TextureRegion is the whole of mip level 0 of layer 0.
type TextureRegion struct {
	MipLevel, Layer uint32
	X, Y            uint32
	Width, Height   uint32
}

// findSize answers the width and height of r of a texture gfx made from
// descriptor, a zero width or height filled in as the rest of its level; an
// error when the texture lacks r's mip level or layer, or r reaches past the
// level's edges.
func (r TextureRegion) findSize(descriptor gfx.TextureDescriptor) (uint32, uint32, error) {
	if r.MipLevel >= descriptor.MipLevelCount || r.Layer >= descriptor.Size.DepthOrArrayLayers {
		return 0, 0, fmt.Errorf("names mip level %d and layer %d of texture %q, of %d mip levels and %d layers", r.MipLevel, r.Layer, descriptor.Label, descriptor.MipLevelCount, descriptor.Size.DepthOrArrayLayers)
	}
	levelWidth := max(descriptor.Size.Width>>r.MipLevel, 1)
	levelHeight := max(descriptor.Size.Height>>r.MipLevel, 1)
	width := cmp.Or(r.Width, levelWidth-min(r.X, levelWidth))
	height := cmp.Or(r.Height, levelHeight-min(r.Y, levelHeight))
	if r.X > levelWidth || width > levelWidth-r.X || r.Y > levelHeight || height > levelHeight-r.Y {
		return 0, 0, fmt.Errorf("is %d×%d texels at %d,%d of texture %q at mip level %d, of %d×%d texels", width, height, r.X, r.Y, descriptor.Label, r.MipLevel, levelWidth, levelHeight)
	}
	return width, height, nil
}

// makeImageCopyTexture answers gfx's origin of r in texture, every aspect
// of it.
func makeImageCopyTexture(texture gfx.Texture, r TextureRegion) gfx.ImageCopyTexture {
	return gfx.ImageCopyTexture{Texture: texture, MipLevel: r.MipLevel, Origin: gfx.Origin3D{X: r.X, Y: r.Y, Z: r.Layer}, Aspect: gfx.TextureAspectAll}
}
