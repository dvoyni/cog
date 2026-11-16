package render

import (
	"cmp"
	"fmt"

	"github.com/dvoyni/cog/plugins/gfx"
)

// TextureView is part of a texture, or the texture seen another way, bound
// as a parameter: MipLevelCount mip levels from BaseMipLevel, and LayerCount
// layers from BaseLayer, seen with Dimension through Aspect. A zero mip
// level count means every level from the base; a zero layer count means one
// layer for a 2D view, six for a cube and every layer from the base for an
// array, as WebGPU's defaults have it. A zero Dimension or Aspect means the
// binding's: a depth binding over a depth-stencil texture sees its depth
// aspect, and an unsigned-integer one its stencil aspect. The zero view of a
// texture is the whole texture, as its binding sees it.
type TextureView struct {
	Texture                     Texture
	BaseMipLevel, MipLevelCount uint32
	BaseLayer, LayerCount       uint32
	Dimension                   TextureViewDimension
	Aspect                      TextureAspect
}

// makeInspectedTextureView answers the view of texture descriptor, gfx's,
// describes, its counts, dimension and aspect filled in, as an inspection
// writes it down.
func makeInspectedTextureView(texture Texture, descriptor gfx.TextureViewDescriptor) TextureView {
	return TextureView{
		Texture:       texture,
		BaseMipLevel:  descriptor.BaseMipLevel,
		MipLevelCount: descriptor.MipLevelCount,
		BaseLayer:     descriptor.BaseArrayLayer,
		LayerCount:    descriptor.ArrayLayerCount,
		Dimension:     TextureViewDimension(descriptor.Dimension),
		Aspect:        TextureAspect(descriptor.Aspect),
	}
}

// makeViewDescriptor answers the gfx view binding sees of v's texture, which
// gfx made from descriptor, its counts, dimension and aspect filled in. An
// error says why the view does not fit binding:
// another dimension, sample type, format or aspect, several samples where
// it has one or one where it has several, mips or layers the texture lacks,
// or several mip levels for a storage binding.
func (v TextureView) makeViewDescriptor(descriptor gfx.TextureDescriptor, binding gfx.ShaderBinding) (gfx.TextureViewDescriptor, error) {
	storageBinding := binding.StorageTexture.Access != gfx.StorageTextureAccessUndefined
	bindingDimension := binding.Texture.ViewDimension
	if storageBinding {
		bindingDimension = binding.StorageTexture.ViewDimension
	}
	dimension := cmp.Or(gfx.TextureViewDimension(v.Dimension), bindingDimension)
	if dimension != bindingDimension {
		return gfx.TextureViewDescriptor{}, fmt.Errorf("is seen in view dimension %d, where its binding's is %d", dimension, bindingDimension)
	}
	format := TextureFormat(descriptor.Format)
	flags := format.info().flags
	aspect, err := v.Aspect.makeBindingAspect(format, binding)
	if err != nil {
		return gfx.TextureViewDescriptor{}, err
	}
	if v.BaseMipLevel >= descriptor.MipLevelCount {
		return gfx.TextureViewDescriptor{}, fmt.Errorf("starts at mip level %d, of %d", v.BaseMipLevel, descriptor.MipLevelCount)
	}
	mipLevelCount := cmp.Or(v.MipLevelCount, descriptor.MipLevelCount-v.BaseMipLevel)
	layers := descriptor.Size.DepthOrArrayLayers
	if v.BaseLayer >= layers {
		return gfx.TextureViewDescriptor{}, fmt.Errorf("starts at layer %d, of %d", v.BaseLayer, layers)
	}
	layerCount := v.LayerCount
	if layerCount == 0 {
		switch dimension {
		case gfx.TextureViewDimension2D:
			layerCount = 1
		case gfx.TextureViewDimensionCube:
			layerCount = 6
		default:
			layerCount = layers - v.BaseLayer
		}
	}
	switch {
	case mipLevelCount > descriptor.MipLevelCount-v.BaseMipLevel:
		return gfx.TextureViewDescriptor{}, fmt.Errorf("names %d mip levels from %d, of %d", mipLevelCount, v.BaseMipLevel, descriptor.MipLevelCount)
	case layerCount > layers-v.BaseLayer:
		return gfx.TextureViewDescriptor{}, fmt.Errorf("names %d layers from %d, of %d", layerCount, v.BaseLayer, layers)
	case dimension == gfx.TextureViewDimension1D || dimension == gfx.TextureViewDimension3D:
		return gfx.TextureViewDescriptor{}, fmt.Errorf("is seen in view dimension %d, which a 2D texture has no view of", dimension)
	case dimension == gfx.TextureViewDimension2D && layerCount != 1:
		return gfx.TextureViewDescriptor{}, fmt.Errorf("is a 2D view of %d layers, where it has one", layerCount)
	case (dimension == gfx.TextureViewDimensionCube && layerCount != 6) || (dimension == gfx.TextureViewDimensionCubeArray && layerCount%6 != 0):
		return gfx.TextureViewDescriptor{}, fmt.Errorf("is a cube view of %d layers, where it has six for each cube", layerCount)
	case (dimension == gfx.TextureViewDimensionCube || dimension == gfx.TextureViewDimensionCubeArray) && descriptor.Size.Width != descriptor.Size.Height:
		return gfx.TextureViewDescriptor{}, fmt.Errorf("is a cube view of %d×%d texels, where its faces are square", descriptor.Size.Width, descriptor.Size.Height)
	case storageBinding && binding.StorageTexture.Format != descriptor.Format:
		return gfx.TextureViewDescriptor{}, fmt.Errorf("is of format %d, where its storage binding's is %d", descriptor.Format, binding.StorageTexture.Format)
	case storageBinding && (descriptor.Usage&gfx.TextureUsageFlagStorageBinding == 0):
		return gfx.TextureViewDescriptor{}, fmt.Errorf("is of format %d with %d samples, which no storage binding takes", descriptor.Format, descriptor.SampleCount)
	case storageBinding && mipLevelCount != 1:
		return gfx.TextureViewDescriptor{}, fmt.Errorf("names %d mip levels, where its storage binding takes one", mipLevelCount)
	case !storageBinding && binding.Texture.Multisampled != (descriptor.SampleCount > 1):
		return gfx.TextureViewDescriptor{}, fmt.Errorf("has %d samples, where its binding is multisampled: %t", descriptor.SampleCount, binding.Texture.Multisampled)
	}
	if !storageBinding {
		sampleType := format.findSampleType(aspect)
		switch binding.Texture.SampleType {
		case sampleType:
		case gfx.TextureSampleTypeUnfilterableFloat:
			if sampleType != gfx.TextureSampleTypeFloat && sampleType != gfx.TextureSampleTypeDepth {
				return gfx.TextureViewDescriptor{}, fmt.Errorf("is of format %d, sampled as sample type %d, where its binding samples floats", format, sampleType)
			}
		default:
			return gfx.TextureViewDescriptor{}, fmt.Errorf("is of format %d, sampled as sample type %d, where its binding's is %d", format, sampleType, binding.Texture.SampleType)
		}
	}
	if aspect == gfx.TextureAspectAll && flags&textureFormatFlagDepth != 0 && flags&textureFormatFlagStencil != 0 {
		return gfx.TextureViewDescriptor{}, fmt.Errorf("is of format %d, seen through both its aspects, where a binding sees one", format)
	}
	return gfx.TextureViewDescriptor{
		Dimension:       dimension,
		Aspect:          aspect,
		BaseMipLevel:    v.BaseMipLevel,
		MipLevelCount:   mipLevelCount,
		BaseArrayLayer:  v.BaseLayer,
		ArrayLayerCount: layerCount,
	}, nil
}

// TextureViewDimension is how a view presents its texture to a shader, each
// dimension gfx's of the same name; its zero value means the binding's.
type TextureViewDimension uint32

const (
	TextureViewDimension2D        = TextureViewDimension(gfx.TextureViewDimension2D)
	TextureViewDimension2DArray   = TextureViewDimension(gfx.TextureViewDimension2DArray)
	TextureViewDimensionCube      = TextureViewDimension(gfx.TextureViewDimensionCube)
	TextureViewDimensionCubeArray = TextureViewDimension(gfx.TextureViewDimensionCubeArray)
)
