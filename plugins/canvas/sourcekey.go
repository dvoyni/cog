package canvas

import "github.com/dvoyni/cog/plugins/render"

// sourceKey is a file images are drawn from, as render loads it: its path
// and its colour space. The images of every frame and rectangle of one
// file share it, so render decodes it once.
type sourceKey struct {
	path       string
	colorSpace ColorSpace
}

// makeTextureOptions answers the options render loads k's file with: its
// colour space, with no mips.
func (k sourceKey) makeTextureOptions() render.TextureOptions {
	colorSpace := render.ColorSpaceSRGB
	if k.colorSpace == ColorSpaceLinear {
		colorSpace = render.ColorSpaceLinear
	}
	return render.TextureOptions{ColorSpace: colorSpace}
}
