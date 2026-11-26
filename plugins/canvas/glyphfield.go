package canvas

import (
	"math"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas/font"
)

// glyphField is a glyph made a distance field, or, for a bitmap font, its
// coverage: the field's bounds about the pen on the baseline, in em, y
// pointing down, and its size and texels, one byte each, row by row, a
// field's ½ on the glyph's edge, falling to 0 spread texels outside it and
// rising to 1 spread texels inside it, as DistanceField's alpha does, and a
// bitmap's how much of each texel the glyph covers; and its spread, half
// its font's field range, or bitmapSpread for a bitmap. A glyph of no
// outline, such as a space's, has no texels, and draws nothing.
type glyphField struct {
	font.Coverage
	spread float32
}

// makeGlyphField answers glyph id of glyphFont made a distance field of
// fieldEm texels to the em, reaching fieldRange texels across its edge: its
// outline filled by shaper at its font's variations, then measured by
// canvas's one distance transform, as DistanceField's image is, the field
// reaching half the range past the outline's bounds on every side. A glyph
// of no outline answers no texels, as does one only a bitmap or colour
// layers draw, which falls back to its plain outline where it has one.
func makeGlyphField(shaper *font.Shaper, glyphFont *font.Loaded, id font.GlyphID, fieldEm, fieldRange int) glyphField {
	field := glyphField{Coverage: shaper.Rasterize(glyphFont, id, fieldEm, int(math.Ceil(float64(fieldRange)/2))), spread: float32(fieldRange) / 2}
	if field.Texels == nil {
		return field
	}
	covered := make([]float32, len(field.Texels))
	for index, alpha := range field.Texels {
		covered[index] = float32(alpha) / 255
	}
	distances := measureEdgeDistances(covered, field.Width, field.Height, field.spread)
	field.Texels = make([]byte, len(distances))
	for index, distance := range distances {
		field.Texels[index] = uint8(255*m.Clamp01(0.5-distance/(2*field.spread)) + 0.5)
	}
	return field
}

// makeGlyphBitmap answers glyph id of glyphFont filled by shaper at
// pixelSize texels to the em, a bitmap font's glyph: its coverage over the
// whole texels its outline's bounds span. A glyph of no outline answers no
// texels.
func makeGlyphBitmap(shaper *font.Shaper, glyphFont *font.Loaded, id font.GlyphID, pixelSize int) glyphField {
	return glyphField{Coverage: shaper.Rasterize(glyphFont, id, pixelSize, 0), spread: bitmapSpread}
}

// bitmapSpread is a bitmap glyph's spread, which it has none of, and its
// instance data's width: canvas.wgsl's canvas_glyph_is_bitmap tells a
// bitmap glyph by it.
const bitmapSpread = -1
