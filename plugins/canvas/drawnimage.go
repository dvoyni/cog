package canvas

import (
	"fmt"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/render"
)

// drawnImage is an image as canvas draws it, once placed at its first draw,
// and of no size until then: where it is packed, or, for an image of a
// render texture, the view of it its entries draw, packed nowhere; its
// region there in texture coordinates, its origin then its size, and its
// page; its size in texels; its pivot; whether its entries decode its
// colour from sRGB as they sample it, as an image of a texture whose format
// encodes nothing decodes it; and whether it was found at its first draw
// to draw nothing, its rectangle outside its file or texture, or its
// texture one render does not hold, so it is never packed and its entries
// draw nothing.
type drawnImage struct {
	packed       *packedImage
	view         render.TextureView
	region       [4]float32
	page         uint32
	size         m.Vec2
	pivot        Pivot
	decode       bool
	drawsNothing bool
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

// findView answers the view of the texture d's entries draw, placed: its
// own texture's, or the whole texture of the atlas it is packed into, as
// that texture is now, since growing an atlas replaces it.
func (d *drawnImage) findView() render.TextureView {
	if d.packed == nil {
		return d.view
	}
	return render.TextureView{Texture: d.packed.atlas.texture}
}

// placeTextureImage writes into drawn frame of the image of key, of a
// render texture, as drawn: its rectangle of the texture, as render's
// descriptor of it measures it, drawn through a 2D-array view of its mip
// level 0 and layer 0 alone, page 0 of it, with key's pivot, its colour
// decoded from sRGB where key marks it so and the texture's format does not
// decode it. A texture render does not hold, and a frame whose rectangle
// does not lie within the texture, draw nothing, each reported to logger
// once for the texture.
func placeTextureImage(inventory render.Inventory, logger *kernel.Logger, drawn *drawnImage, key imageKey, frame uint32) {
	descriptor, held := inventory.TextureDescriptor(key.texture)
	if !held {
		refusal := Report{Kind: ReportKindImageTextureRefused, Err: fmt.Errorf("canvas: an image of texture %v, which render does not hold, draws nothing", key.texture)}
		logger.LogErrOnce(imageTextureRefusedLogKey+key.findSubject(), refusal)
		drawn.drawsNothing = true
		return
	}
	frameRect, withinTexture := key.options.findFrameRect(int(descriptor.Width), int(descriptor.Height), frame)
	if !withinTexture {
		outOfBoundsReport := Report{Kind: ReportKindFrameOutOfBounds, Err: fmt.Errorf("canvas: frame %d of image %s, of its SourceRect %v cut by its sheet of %d by %d, does not lie within its texture of %d by %d texels, so it draws nothing", frame, key.describe(), key.options.SourceRect, key.options.Sheet.Columns, key.options.Sheet.Rows, descriptor.Width, descriptor.Height)}
		logger.LogErrOnce(frameOutOfBoundsLogKey+key.findSubject(), outOfBoundsReport)
		drawn.drawsNothing = true
		return
	}
	texels := frameRect.Float()
	width := float32(descriptor.Width)
	height := float32(descriptor.Height)
	*drawn = drawnImage{
		view:   render.TextureView{Texture: key.texture, MipLevelCount: 1, LayerCount: 1, Dimension: render.TextureViewDimension2DArray},
		region: [4]float32{texels.X / width, texels.Y / height, texels.Width / width, texels.Height / height},
		size:   frameRect.Size().Float(),
		pivot:  key.options.Pivot,
		decode: key.options.ColorSpace == ColorSpaceSrgb && descriptor.Format != render.TextureFormatRGBA8UnormSrgb && descriptor.Format != render.TextureFormatBGRA8UnormSrgb,
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
