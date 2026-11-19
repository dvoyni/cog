package canvas

import "github.com/dvoyni/cog/plugins/render"

// imageKey is what ImageFromFile interns: the image's path, its options, and
// whether ImageFromFile was given more sets of options, which it ignored.
type imageKey struct {
	path           string
	options        ImageOptions
	ignoredOptions bool
}

// makeTextureOptions answers the options render loads k's file with: its
// colour space, with no mips.
func (k imageKey) makeTextureOptions() render.TextureOptions {
	colorSpace := render.ColorSpaceSRGB
	if k.options.ColorSpace == ColorSpaceLinear {
		colorSpace = render.ColorSpaceLinear
	}
	return render.TextureOptions{ColorSpace: colorSpace}
}
