package canvas

import (
	"image"
	"math"

	"github.com/dvoyni/cog/libs/m"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"golang.org/x/image/vector"
)

// glyphField is a glyph made a distance field: the field's rectangle about
// the pen on the baseline, in em, y pointing down; its size and texels, one
// byte each, row by row, ½ on the glyph's edge, falling to 0 spread texels
// outside it and rising to 1 spread texels inside it, as DistanceField's
// alpha does; and its spread, half its font's field range. A glyph of no
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
	field := glyphField{spread: float32(fieldRange) / 2}
	outline, hasOutline := face.GlyphDataOutline(id)
	if !hasOutline || len(outline.Segments) == 0 {
		return field
	}
	// The outline's bounds, in field texels, y pointing down.
	scale := float32(fieldEm) / float32(face.Upem())
	outlineMinimum := m.Vec2{X: math.MaxFloat32, Y: math.MaxFloat32}
	outlineMaximum := m.Vec2{X: -math.MaxFloat32, Y: -math.MaxFloat32}
	for index := range outline.Segments {
		for _, point := range outline.Segments[index].ArgsSlice() {
			placed := m.Vec2{X: point.X * scale, Y: -point.Y * scale}
			outlineMinimum = m.Vec2{X: min(outlineMinimum.X, placed.X), Y: min(outlineMinimum.Y, placed.Y)}
			outlineMaximum = m.Vec2{X: max(outlineMaximum.X, placed.X), Y: max(outlineMaximum.Y, placed.Y)}
		}
	}
	padding := int(math.Ceil(float64(field.spread)))
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
	covered := make([]float32, len(coverage.Pix))
	for index, alpha := range coverage.Pix {
		covered[index] = float32(alpha) / 255
	}
	distances := measureEdgeDistances(covered, field.width, field.height, field.spread)
	field.texels = make([]byte, len(distances))
	for index, distance := range distances {
		field.texels[index] = uint8(255*m.Clamp01(0.5-distance/(2*field.spread)) + 0.5)
	}
	em := float32(fieldEm)
	field.bounds = m.Rect{X: float32(left) / em, Y: float32(top) / em, Width: float32(field.width) / em, Height: float32(field.height) / em}
	return field
}
