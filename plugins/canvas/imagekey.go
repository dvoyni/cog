package canvas

import "github.com/dvoyni/cog/plugins/render"

// imageKey is what ImageFromFile interns: the image's path, its options, and
// whether ImageFromFile was given more sets of options, which it ignored.
type imageKey struct {
	path           string
	options        ImageOptions
	ignoredOptions bool
}

// makePackedKey answers k with only the options that change the image's
// pixels, its colour space, so images differing only in their pivot share
// what is packed.
func (k imageKey) makePackedKey() imageKey {
	return imageKey{path: k.path, options: ImageOptions{ColorSpace: k.options.ColorSpace}}
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
