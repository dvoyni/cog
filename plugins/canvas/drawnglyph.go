package canvas

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas/atlas"
	"github.com/dvoyni/cog/plugins/render"
)

// drawnGlyph is a glyph's field as canvas draws it, once packed at its first
// draw: where the glyph atlas packed it, packedRegion, the glyph atlas's
// Pack's answer, its layer the page, freed when it is evicted, and region,
// the same in texture coordinates, its origin then its size; its field's rectangle
// about the pen, in em; and its spread, in field texels. packed is set once
// its field is packed; a glyph the atlas had no room for is not, and draws
// nothing. lastDrawnFrame is the number of the frame that last drew it,
// which findGlyphs notes it missed in once, so one the atlas had no room for
// is tried again at the next frame drawing it, and which a full glyph atlas
// evicts by.
type drawnGlyph struct {
	packedRegion   render.TextureRegion
	region         m.Rect
	bounds         m.Rect
	spread         float32
	packed         bool
	lastDrawnFrame uint64
}

// makeDrawnGlyph answers field, packed at region of glyphAtlas in frame, the
// number of the frame drawing it, as drawn.
func makeDrawnGlyph(glyphAtlas *atlas.Atlas, region render.TextureRegion, field glyphField, frame uint64) drawnGlyph {
	return drawnGlyph{
		packedRegion:   region,
		region:         glyphAtlas.TextureRect(region),
		bounds:         field.Bounds,
		spread:         field.spread,
		packed:         true,
		lastDrawnFrame: frame,
	}
}
