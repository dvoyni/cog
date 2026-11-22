package canvas

import (
	"image"
	"math"

	"github.com/dvoyni/cog/libs/m"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"golang.org/x/image/vector"
)

// glyphField is a glyph made a distance field, or, for a bitmap font, its
// coverage: the field's rectangle about the pen on the baseline, in em, y
// pointing down; its size and texels, one byte each, row by row, a field's
// ½ on the glyph's edge, falling to 0 spread texels outside it and rising
// to 1 spread texels inside it, as DistanceField's alpha does, and a
// bitmap's how much of each texel the glyph covers; and its spread, half
// its font's field range, or bitmapSpread for a bitmap. A glyph of no
// outline, such as a space's, has no texels, and draws nothing.
type glyphField struct {
	bounds        m.Rect
	width, height int
	spread        float32
	texels        []byte
}

// makeGlyphField answers glyph id of face made a distance field of fieldEm
// texels to the em, reaching fieldRange texels across its edge: its outline
// rasterized anti-aliased by x/image's vector rasterizer at face's
// variations, then measured by canvas's one distance transform, as
// DistanceField's image is, the field reaching half the range past the
// outline's bounds on every side. A glyph of no outline answers no texels,
// as does one only a bitmap or colour layers draw, which falls back to its
// plain outline where it has one.
func makeGlyphField(face *font.Face, id font.GID, fieldEm, fieldRange int) glyphField {
	field := rasterizeGlyph(face, id, fieldEm, int(math.Ceil(float64(fieldRange)/2)))
	field.spread = float32(fieldRange) / 2
	if field.texels == nil {
		return field
	}
	covered := make([]float32, len(field.texels))
	for index, alpha := range field.texels {
		covered[index] = float32(alpha) / 255
	}
	distances := measureEdgeDistances(covered, field.width, field.height, field.spread)
	field.texels = make([]byte, len(distances))
	for index, distance := range distances {
		field.texels[index] = uint8(255*m.Clamp01(0.5-distance/(2*field.spread)) + 0.5)
	}
	return field
}

// makeGlyphBitmap answers glyph id of face rasterized anti-aliased at
// pixelSize texels to the em, a bitmap font's glyph: its coverage over the
// whole texels its outline's bounds span. A glyph of no outline answers no
// texels.
func makeGlyphBitmap(face *font.Face, id font.GID, pixelSize int) glyphField {
	field := rasterizeGlyph(face, id, pixelSize, 0)
	field.spread = bitmapSpread
	return field
}

// bitmapSpread is a bitmap glyph's spread, which it has none of, and its
// instance data's width: canvas.wgsl's canvas_glyph_is_bitmap tells a
// bitmap glyph by it.
const bitmapSpread = -1

// rasterizeGlyph answers glyph id of face rasterized anti-aliased by
// x/image's vector rasterizer, at face's variations, at em texels to the
// em, over the whole texels its outline's bounds span and padding more on
// every side: the glyph's bounds and size, and its texels its coverage, a
// byte a texel, row by row, none for a glyph of no outline.
func rasterizeGlyph(face *font.Face, id font.GID, em, padding int) glyphField {
	var field glyphField
	outline, hasOutline := face.GlyphDataOutline(id)
	if !hasOutline || len(outline.Segments) == 0 {
		return field
	}
	// The outline's bounds, in texels, y pointing down.
	scale := float32(em) / float32(face.Upem())
	outlineMinimum := m.Vec2{X: math.MaxFloat32, Y: math.MaxFloat32}
	outlineMaximum := m.Vec2{X: -math.MaxFloat32, Y: -math.MaxFloat32}
	for index := range outline.Segments {
		for _, point := range outline.Segments[index].ArgsSlice() {
			placed := m.Vec2{X: point.X * scale, Y: -point.Y * scale}
			outlineMinimum = m.Vec2{X: min(outlineMinimum.X, placed.X), Y: min(outlineMinimum.Y, placed.Y)}
			outlineMaximum = m.Vec2{X: max(outlineMaximum.X, placed.X), Y: max(outlineMaximum.Y, placed.Y)}
		}
	}
	left := int(math.Floor(float64(outlineMinimum.X))) - padding
	top := int(math.Floor(float64(outlineMinimum.Y))) - padding
	field.width = int(math.Ceil(float64(outlineMaximum.X))) + padding - left
	field.height = int(math.Ceil(float64(outlineMaximum.Y))) + padding - top
	rasterizer := vector.NewRasterizer(field.width, field.height)
	// place answers point, of the outline, in the field's texels.
	place := func(point opentype.SegmentPoint) (float32, float32) {
		return point.X*scale - float32(left), -point.Y*scale - float32(top)
	}
	for index, segment := range outline.Segments {
		switch segment.Op {
		case opentype.SegmentOpMoveTo:
			if index > 0 {
				rasterizer.ClosePath()
			}
			rasterizer.MoveTo(place(segment.Args[0]))
		case opentype.SegmentOpLineTo:
			rasterizer.LineTo(place(segment.Args[0]))
		case opentype.SegmentOpQuadTo:
			controlX, controlY := place(segment.Args[0])
			toX, toY := place(segment.Args[1])
			rasterizer.QuadTo(controlX, controlY, toX, toY)
		case opentype.SegmentOpCubeTo:
			firstX, firstY := place(segment.Args[0])
			secondX, secondY := place(segment.Args[1])
			toX, toY := place(segment.Args[2])
			rasterizer.CubeTo(firstX, firstY, secondX, secondY, toX, toY)
		}
	}
	rasterizer.ClosePath()
	coverage := image.NewAlpha(image.Rect(0, 0, field.width, field.height))
	rasterizer.Draw(coverage, coverage.Bounds(), image.Opaque, image.Point{})
	texelsPerEm := float32(em)
	field.bounds = m.Rect{X: float32(left) / texelsPerEm, Y: float32(top) / texelsPerEm, Width: float32(field.width) / texelsPerEm, Height: float32(field.height) / texelsPerEm}
	field.texels = coverage.Pix
	return field
}
