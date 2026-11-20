package canvas

import "github.com/dvoyni/cog/plugins/render"

// Filter is how an entry's image is read between its texels' centres.
type Filter uint8

const (
	// FilterDefault is Config.DefaultFilter.
	FilterDefault Filter = iota
	// FilterLinear blends the nearest texels, for art that scales smoothly.
	FilterLinear
	// FilterNearest reads the nearest texel, for pixel art.
	FilterNearest
)

// makeSamplerDescriptor answers the sampler f reads through, for magnifying
// and minifying alike and between mip levels: linear for FilterLinear, and
// nearest otherwise, as for the white texel's batches, of FilterDefault;
// repeating along the axes of wrap, and clamping at the texture's edge
// along the others.
func (f Filter) makeSamplerDescriptor(wrap Wrap) render.SamplerDescriptor {
	descriptor := render.SamplerDescriptor{MagFilter: render.FilterModeNearest, MinFilter: render.FilterModeNearest}
	if f == FilterLinear {
		descriptor = render.SamplerDescriptor{MagFilter: render.FilterModeLinear, MinFilter: render.FilterModeLinear, MipmapFilter: render.FilterModeLinear}
	}
	if wrap == WrapX || wrap == WrapBoth {
		descriptor.AddressModeU = render.AddressModeRepeat
	}
	if wrap == WrapY || wrap == WrapBoth {
		descriptor.AddressModeV = render.AddressModeRepeat
	}
	return descriptor
}
