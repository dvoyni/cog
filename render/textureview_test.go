package render

import (
	"testing"

	"github.com/dvoyni/cog/gfx"
)

func TestAViewFillsItsCountsDimensionAndAspectFromItsTextureAndBindingAndIsRefusedWhereItDoesNotFit(t *testing.T) {
	texture := func(format gfx.TextureFormat, size, layers, mips, samples uint32) gfx.TextureDescriptor {
		usage := TextureFormat(format).makeTextureUsage(samples)
		return gfx.TextureDescriptor{Size: gfx.Extent3D{Width: size, Height: size, DepthOrArrayLayers: layers}, MipLevelCount: mips, SampleCount: samples, Format: format, Usage: usage}
	}
	sampled := func(sampleType gfx.TextureSampleType, dimension gfx.TextureViewDimension) gfx.ShaderBinding {
		return gfx.ShaderBinding{Texture: gfx.TextureBindingLayout{SampleType: sampleType, ViewDimension: dimension}}
	}
	storage := gfx.ShaderBinding{StorageTexture: gfx.StorageTextureBindingLayout{Access: gfx.StorageTextureAccessWriteOnly, Format: gfx.TextureFormatRGBA8Unorm, ViewDimension: gfx.TextureViewDimension2D}}
	colour := texture(gfx.TextureFormatRGBA8Unorm, 4, 6, 3, 1)
	floatCube := sampled(gfx.TextureSampleTypeFloat, gfx.TextureViewDimensionCube)
	tests := []struct {
		name       string
		view       TextureView
		descriptor gfx.TextureDescriptor
		binding    gfx.ShaderBinding
		want       gfx.TextureViewDescriptor
		wantErr    bool
	}{
		{name: "a whole texture to a 2D binding sees its first layer and every level", descriptor: colour, binding: sampled(gfx.TextureSampleTypeFloat, gfx.TextureViewDimension2D),
			want: gfx.TextureViewDescriptor{Dimension: gfx.TextureViewDimension2D, Aspect: gfx.TextureAspectAll, MipLevelCount: 3, ArrayLayerCount: 1}},
		{name: "a cube binding sees six layers", descriptor: colour, binding: floatCube,
			want: gfx.TextureViewDescriptor{Dimension: gfx.TextureViewDimensionCube, Aspect: gfx.TextureAspectAll, MipLevelCount: 3, ArrayLayerCount: 6}},
		{name: "an array sees the rest of the layers from its base", view: TextureView{BaseLayer: 2, BaseMipLevel: 1}, descriptor: colour, binding: sampled(gfx.TextureSampleTypeFloat, gfx.TextureViewDimension2DArray),
			want: gfx.TextureViewDescriptor{Dimension: gfx.TextureViewDimension2DArray, Aspect: gfx.TextureAspectAll, BaseMipLevel: 1, MipLevelCount: 2, BaseArrayLayer: 2, ArrayLayerCount: 4}},
		{name: "a depth binding sees a depth-stencil texture's depth", descriptor: texture(gfx.TextureFormatDepth24PlusStencil8, 4, 1, 1, 1), binding: sampled(gfx.TextureSampleTypeDepth, gfx.TextureViewDimension2D),
			want: gfx.TextureViewDescriptor{Dimension: gfx.TextureViewDimension2D, Aspect: gfx.TextureAspectDepthOnly, MipLevelCount: 1, ArrayLayerCount: 1}},
		{name: "an unsigned binding sees its stencil", descriptor: texture(gfx.TextureFormatDepth24PlusStencil8, 4, 1, 1, 1), binding: sampled(gfx.TextureSampleTypeUint, gfx.TextureViewDimension2D),
			want: gfx.TextureViewDescriptor{Dimension: gfx.TextureViewDimension2D, Aspect: gfx.TextureAspectStencilOnly, MipLevelCount: 1, ArrayLayerCount: 1}},
		{name: "an unfilterable binding takes a filterable format", descriptor: colour, binding: sampled(gfx.TextureSampleTypeUnfilterableFloat, gfx.TextureViewDimension2D),
			want: gfx.TextureViewDescriptor{Dimension: gfx.TextureViewDimension2D, Aspect: gfx.TextureAspectAll, MipLevelCount: 3, ArrayLayerCount: 1}},
		{name: "a storage binding given one level sees it", view: TextureView{MipLevelCount: 1}, descriptor: colour, binding: storage,
			want: gfx.TextureViewDescriptor{Dimension: gfx.TextureViewDimension2D, Aspect: gfx.TextureAspectAll, MipLevelCount: 1, ArrayLayerCount: 1}},
		{name: "a storage binding given a view of every level", descriptor: colour, binding: storage, wantErr: true},
		{name: "a storage binding of another format", descriptor: texture(gfx.TextureFormatRGBA8Uint, 4, 1, 1, 1), view: TextureView{MipLevelCount: 1}, binding: storage, wantErr: true},
		{name: "another dimension than the binding's", view: TextureView{Dimension: TextureViewDimension2DArray}, descriptor: colour, binding: sampled(gfx.TextureSampleTypeFloat, gfx.TextureViewDimension2D), wantErr: true},
		{name: "a cube of faces not square", descriptor: gfx.TextureDescriptor{Size: gfx.Extent3D{Width: 4, Height: 2, DepthOrArrayLayers: 6}, MipLevelCount: 1, SampleCount: 1, Format: gfx.TextureFormatRGBA8Unorm}, binding: floatCube, wantErr: true},
		{name: "a cube of too few layers", descriptor: texture(gfx.TextureFormatRGBA8Unorm, 4, 3, 1, 1), binding: floatCube, wantErr: true},
		{name: "a 3D binding", descriptor: colour, binding: sampled(gfx.TextureSampleTypeFloat, gfx.TextureViewDimension3D), wantErr: true},
		{name: "a mip level the texture lacks", view: TextureView{BaseMipLevel: 3}, descriptor: colour, binding: sampled(gfx.TextureSampleTypeFloat, gfx.TextureViewDimension2D), wantErr: true},
		{name: "more layers than the texture has", view: TextureView{BaseLayer: 4, LayerCount: 3}, descriptor: colour, binding: sampled(gfx.TextureSampleTypeFloat, gfx.TextureViewDimension2DArray), wantErr: true},
		{name: "a float texture to an unsigned binding", descriptor: colour, binding: sampled(gfx.TextureSampleTypeUint, gfx.TextureViewDimension2D), wantErr: true},
		{name: "a 32-bit float texture to a filtering binding", descriptor: texture(gfx.TextureFormatRGBA32Float, 4, 1, 1, 1), binding: sampled(gfx.TextureSampleTypeFloat, gfx.TextureViewDimension2D), wantErr: true},
		{name: "a multisampled texture to a single-sampled binding", descriptor: texture(gfx.TextureFormatRGBA8Unorm, 4, 1, 1, 4), binding: sampled(gfx.TextureSampleTypeFloat, gfx.TextureViewDimension2D), wantErr: true},
		{name: "a depth-stencil texture seen through both aspects", view: TextureView{Aspect: TextureAspectAll}, descriptor: texture(gfx.TextureFormatDepth24PlusStencil8, 4, 1, 1, 1), binding: sampled(gfx.TextureSampleTypeDepth, gfx.TextureViewDimension2D), wantErr: true},
		{name: "a colour texture's depth aspect", view: TextureView{Aspect: TextureAspectDepthOnly}, descriptor: colour, binding: sampled(gfx.TextureSampleTypeFloat, gfx.TextureViewDimension2D), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.view.makeViewDescriptor(test.descriptor, test.binding)
			switch {
			case test.wantErr && err == nil:
				t.Errorf("answered %+v, want it refused", got)
			case !test.wantErr && err != nil:
				t.Errorf("refused it: %v", err)
			case !test.wantErr && got != test.want:
				t.Errorf("answered %+v, want %+v", got, test.want)
			}
		})
	}
}
