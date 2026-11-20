package canvas

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/render"
)

// drawnImage is an image as canvas draws it, once packed at its first draw,
// and of no size until then: where it is packed, its region there in
// texture coordinates, its origin then its size, and its page; its size in
// texels; its pivot; and whether its rectangle of its file was found at its
// first draw to lie outside the file, so it is never packed and its
// entries draw nothing.
type drawnImage struct {
	packed      *packedImage
	region      [4]float32
	page        uint32
	size        m.Vec2
	pivot       Pivot
	outsideFile bool
}

// makeDrawnImage answers packed as drawn, of no pivot.
func makeDrawnImage(packed *packedImage) drawnImage {
	pageWidth := float32(packed.atlas.width)
	pageHeight := float32(packed.atlas.height)
	region := packed.region
	return drawnImage{
		packed: packed,
		region: [4]float32{float32(region.X) / pageWidth, float32(region.Y) / pageHeight, float32(region.Width) / pageWidth, float32(region.Height) / pageHeight},
		page:   region.Layer,
		size:   m.Vec2{X: float32(region.Width), Y: float32(region.Height)},
	}
}

// packedImage is an image packed into an atlas, which the images differing
// from it only in options that change no pixel share: the atlas, a sprite
// atlas or one of the image's own, its region there, its page as the
// region's layer, and the number of the frame that last drew it, which no
// atlas evicts it in.
type packedImage struct {
	atlas          *atlas
	region         render.TextureRegion
	lastDrawnFrame uint64
}
