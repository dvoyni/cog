package render

import (
	"fmt"

	"github.com/dvoyni/cog/plugins/gfx"
)

// TextureAspect is which aspects of a texture a view sees, each aspect gfx's
// of the same name; its zero value means the one its binding reads.
type TextureAspect uint32

const (
	TextureAspectAll         = TextureAspect(gfx.TextureAspectAll)
	TextureAspectStencilOnly = TextureAspect(gfx.TextureAspectStencilOnly)
	TextureAspectDepthOnly   = TextureAspect(gfx.TextureAspectDepthOnly)
)

// makeBindingAspect answers the gfx aspect binding sees of a texture of
// format through a, a zero a the one binding reads: the depth aspect for a
// depth binding, the stencil aspect for an unsigned-integer binding of a
// format with stencil and no colour, and every aspect otherwise. An aspect
// format lacks answers an error.
func (a TextureAspect) makeBindingAspect(format TextureFormat, binding gfx.ShaderBinding) (gfx.TextureAspect, error) {
	flags := format.info().flags
	aspect := gfx.TextureAspect(a)
	if aspect == gfx.TextureAspectUndefined {
		switch {
		case binding.Texture.SampleType == gfx.TextureSampleTypeDepth || (binding.Texture.SampleType == gfx.TextureSampleTypeUnfilterableFloat && flags&textureFormatFlagDepth != 0):
			aspect = gfx.TextureAspectDepthOnly
		case binding.Texture.SampleType == gfx.TextureSampleTypeUint && flags&textureFormatFlagStencil != 0:
			aspect = gfx.TextureAspectStencilOnly
		default:
			aspect = gfx.TextureAspectAll
		}
	}
	switch {
	case aspect == gfx.TextureAspectDepthOnly && flags&textureFormatFlagDepth == 0, aspect == gfx.TextureAspectStencilOnly && flags&textureFormatFlagStencil == 0:
		return gfx.TextureAspectUndefined, fmt.Errorf("is of format %d, which has no aspect %d", format, aspect)
	case aspect > gfx.TextureAspectDepthOnly:
		return gfx.TextureAspectUndefined, fmt.Errorf("is seen through aspect %d, which gfx has none of", aspect)
	}
	return aspect, nil
}
