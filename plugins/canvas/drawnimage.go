package canvas

import (
	"fmt"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/render"
)

// drawnImage is an image as canvas draws it, once placed at its first draw,
// and of no size until then: where it is packed, or, for an image of a
// render texture, the view of it its entries draw, packed nowhere; its
// region there in texture coordinates, its origin then its size, and its
// page; its size in texels; the bounds of the frame its region was
// converted from, in texels from the region's top left, which place it,
// zero when the region holds the frame itself; the options of the image
// drawing it, which
// place it, slice it and wrap it; whether its entries decode its colour
// from sRGB as they sample it, as an image of a texture whose format
// encodes nothing decodes it; and whether it was found at its first draw
// to draw nothing, its rectangle outside its file or texture, or its
// texture one render does not hold, so it is never packed and its entries
// draw nothing.
type drawnImage struct {
	packed       *packedImage
	view         render.TextureView
	region       m.Rect
	page         uint32
	size         m.Vec2
	frameBounds  m.Rect
	options      ImageOptions
	decode       bool
	drawsNothing bool
}

// makeDrawnImage answers packed as drawn by an image of options.
func makeDrawnImage(packed *packedImage, options ImageOptions) drawnImage {
	pageWidth := float32(packed.atlas.width)
	pageHeight := float32(packed.atlas.height)
	region := packed.region
	return drawnImage{
		packed:      packed,
		region:      m.Rect{X: float32(region.X) / pageWidth, Y: float32(region.Y) / pageHeight, Width: float32(region.Width) / pageWidth, Height: float32(region.Height) / pageHeight},
		page:        region.Layer,
		size:        m.Vec2{X: float32(region.Width), Y: float32(region.Height)},
		frameBounds: packed.frameBounds,
		options:     options,
	}
}

// placeRegion answers the rectangle d's region spans when its frame spans
// size from the origin: the frame itself, or a converted image's region,
// placed within the frame by the frame's bounds.
func (d *drawnImage) placeRegion(size m.Vec2) m.Rect {
	frame := d.frameBounds
	if frame == (m.Rect{}) {
		frame = m.Rect{Width: d.size.X, Height: d.size.Y}
	}
	scale := size.Div(frame.Size())
	origin := frame.Min().Negate().Mul(scale)
	regionSize := d.size.Mul(scale)
	return m.Rect{X: origin.X, Y: origin.Y, Width: regionSize.X, Height: regionSize.Y}
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
// level 0 and layer 0 alone, page 0 of it, with key's options, its colour
// decoded from sRGB where key marks it so and the texture's format does not
// decode it. A texture render does not hold, and a frame whose rectangle
// does not lie within the texture, draw nothing, each reported
// once for the texture.
func placeTextureImage(inventory render.Inventory, reporter *reporter, drawn *drawnImage, key imageKey, frame uint32) {
	descriptor, held := inventory.TextureDescriptor(key.texture)
	if !held {
		refusal := Report{Kind: ReportKindImageTextureRefused, Err: fmt.Errorf("canvas: an image of texture %v, which render does not hold, draws nothing", key.texture)}
		reporter.reportOnce(imageTextureRefusedLogKey+key.findSubject(), refusal)
		drawn.drawsNothing = true
		return
	}
	frameRect, withinTexture := key.options.findFrameRect(int(descriptor.Width), int(descriptor.Height), frame)
	if !withinTexture {
		reportFrameOutOfBounds(reporter, key, frame, "texture", int(descriptor.Width), int(descriptor.Height))
		drawn.drawsNothing = true
		return
	}
	texels := frameRect.Float()
	width := float32(descriptor.Width)
	height := float32(descriptor.Height)
	*drawn = drawnImage{
		view:    render.TextureView{Texture: key.texture, MipLevelCount: 1, LayerCount: 1, Dimension: render.TextureViewDimension2DArray},
		region:  m.Rect{X: texels.X / width, Y: texels.Y / height, Width: texels.Width / width, Height: texels.Height / height},
		size:    frameRect.Size().Float(),
		options: key.options,
		decode:  key.options.ColorSpace == ColorSpaceSrgb && descriptor.Format != render.TextureFormatRGBA8UnormSrgb && descriptor.Format != render.TextureFormatBGRA8UnormSrgb,
	}
}

// packedImage is an image packed into an atlas, which the images differing
// from it only in options that change no pixel share: the atlas, a sprite
// atlas or one of the image's own, its region there, its page as the
// region's layer; the bounds of the frame a converter's output there was
// converted from, in texels from the region's top left, zero for a region
// holding its frame; and the number of the frame that last drew it, which
// no atlas evicts it in.
type packedImage struct {
	atlas          *atlas
	region         render.TextureRegion
	frameBounds    m.Rect
	lastDrawnFrame uint64
}
