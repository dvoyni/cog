package canvas

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/render"
)

// drawnImage is an image as canvas draws it, once packed at its first draw,
// and of no size until then: the texture of the atlas it is packed into,
// its region there in texture coordinates, its origin then its size, and
// its page; its size in texels; and its pivot.
type drawnImage struct {
	atlas  render.Texture
	region [4]float32
	page   uint32
	size   m.Vec2
	pivot  Pivot
}

// makeDrawnImage answers the image packed into imageAtlas at region, of
// no pivot.
func makeDrawnImage(imageAtlas *atlas, region render.TextureRegion) drawnImage {
	pageWidth := float32(imageAtlas.width)
	pageHeight := float32(imageAtlas.height)
	return drawnImage{
		atlas:  imageAtlas.texture,
		region: [4]float32{float32(region.X) / pageWidth, float32(region.Y) / pageHeight, float32(region.Width) / pageWidth, float32(region.Height) / pageHeight},
		page:   region.Layer,
		size:   m.Vec2{X: float32(region.Width), Y: float32(region.Height)},
	}
}
