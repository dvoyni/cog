package canvas

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas/atlas"
	"github.com/dvoyni/cog/plugins/render"
)

// drawnGlyph is a glyph's field as canvas draws it, once packed at its first
// draw: its region in the glyph atlas, in texture coordinates, its origin
// then its size, and its page; its field's rectangle about the pen, in em;
// and its spread, in field texels. packed is set once its field is packed;
// a glyph the atlas had no room for is not, and draws nothing, triedFrame
// the number of the frame that last tried to pack it, which tries once, so
// it is packed again at the next frame drawing it.
type drawnGlyph struct {
	region     m.Rect
	page       uint32
	bounds     m.Rect
	spread     float32
	packed     bool
	triedFrame uint64
}

// makeDrawnGlyph answers field, packed at region of glyphAtlas, as drawn.
func makeDrawnGlyph(glyphAtlas *atlas.Atlas, region render.TextureRegion, field glyphField) drawnGlyph {
	return drawnGlyph{
		region: glyphAtlas.TextureRect(region),
		page:   region.Layer,
		bounds: field.bounds,
		spread: field.spread,
		packed: true,
	}
}
