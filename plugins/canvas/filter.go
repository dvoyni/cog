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
// and minifying alike: linear for FilterLinear, and nearest otherwise, as
// for the white texel's batches, of FilterDefault.
func (f Filter) makeSamplerDescriptor() render.SamplerDescriptor {
	if f == FilterLinear {
		return render.SamplerDescriptor{MagFilter: render.FilterModeLinear, MinFilter: render.FilterModeLinear}
	}
	return render.SamplerDescriptor{MagFilter: render.FilterModeNearest, MinFilter: render.FilterModeNearest}
}
