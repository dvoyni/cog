package canvas

import (
	"cmp"
	"math"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/render"
)

// laidOutText is a run of text recorded, and its layout, made as it was
// recorded, its glyphs' fields found.
type laidOutText struct {
	text   Text
	layout TextLayout
}

// batchKey answers what the run shares with the entries it batches with:
// its material, TextMaterial when it names none, the glyph atlas seen
// through texture, its filter, its layout's font's when it names none, and
// its clip.
func (t *laidOutText) batchKey(texture render.TextureView) batchKey {
	return batchKey{material: cmp.Or(t.text.Material, TextMaterial), texture: texture, filter: cmp.Or(t.text.Filter, t.layout.fontFilter), clip: t.text.Clip}
}

// findTransform answers the transform from the run's own space, its
// layout's box from its top left, y pointing down, into the world: its
// pivot, a point of the box, its Texels in em, moved onto the origin, then
// its Transform, a zero one read as the identity.
func (t *laidOutText) findTransform() m.Mat2x3 {
	pivot := t.text.Pivot
	pivotPoint := pivot.Fraction.AddS(0.5).Mul(t.layout.size).Add(pivot.Texels.MulS(t.layout.em))
	toPivot := m.Translation2x3(-pivotPoint.X, -pivotPoint.Y)
	if t.text.Transform == (m.Mat2x3{}) {
		return toPivot
	}
	return t.text.Transform.Mul(toPivot)
}

// findBounds answers the world rectangle the run draws within, each glyph
// and icon as atlases placed it, and whether it draws any glyph or icon:
// the bounding box, in the world, of the rectangles its glyphs draw their
// fields over and its icons their regions.
func (t *laidOutText) findBounds(atlases *atlasSet) (m.Rect, bool) {
	inkMinimum := m.Vec2{X: math.MaxFloat32, Y: math.MaxFloat32}
	inkMaximum := m.Vec2{X: -math.MaxFloat32, Y: -math.MaxFloat32}
	drawsGlyphs := false
	for _, glyph := range t.layout.glyphs {
		var glyphRect m.Rect
		if icon := t.layout.findIcon(glyph); icon != nil {
			image := atlases.placed(icon.image, icon.frame)
			// An icon drawn from a texture of its own, not the sprite atlas
			// the text material samples, draws nothing.
			if !image.inPlainAtlas() {
				continue
			}
			glyphRect = image.placeRegion(icon.size).Translate(m.Vec2{X: glyph.pen.X, Y: glyph.pen.Y - icon.size.Y})
		} else {
			drawn := atlases.placedGlyph(glyph.field)
			if !drawn.packed {
				continue
			}
			glyphRect = t.findGlyphRect(glyph, drawn)
		}
		inkMinimum = m.Vec2{X: min(inkMinimum.X, glyphRect.X), Y: min(inkMinimum.Y, glyphRect.Y)}
		inkMaximum = m.Vec2{X: max(inkMaximum.X, glyphRect.X+glyphRect.Width), Y: max(inkMaximum.Y, glyphRect.Y+glyphRect.Height)}
		drawsGlyphs = true
	}
	if !drawsGlyphs {
		return m.Rect{}, false
	}
	return t.findTransform().TransformRect(m.Rect{X: inkMinimum.X, Y: inkMinimum.Y, Width: inkMaximum.X - inkMinimum.X, Height: inkMaximum.Y - inkMinimum.Y}), true
}

// appendInstances appends to instances the instance data of the run's
// glyphs packed and of its icons packed into the sRGB sprite atlas, each as
// atlases placed it, and answers them. A glyph is a
// quad over the rectangle its field spans, mapped by the run's transform,
// over its region of the glyph atlas, its width its field's spread, its
// tiles its pen as a point of the quad, and its span's tint, else the
// run's, its zero white, premultiplied. An icon is a quad over its region
// of the sprite atlas, its width zero, in its span's tint where it is
// tinted, and otherwise in white of the run's alpha.
func (t *laidOutText) appendInstances(instances []instanceData, atlases *atlasSet) []instanceData {
	runTint := cmp.Or(t.text.Tint, m.White)
	transform := t.findTransform()
	for _, glyph := range t.layout.glyphs {
		tint := runTint
		if int(glyph.span) < len(t.text.Spans) {
			tint = cmp.Or(t.text.Spans[glyph.span].Tint, runTint)
		}
		if icon := t.layout.findIcon(glyph); icon != nil {
			image := atlases.placed(icon.image, icon.frame)
			if !image.inPlainAtlas() {
				continue
			}
			if !icon.tinted {
				tint = m.Color{R: 1, G: 1, B: 1, A: runTint.A}
			}
			// Its pen is its bottom left, and its frame spans its size.
			iconRect := image.placeRegion(icon.size).Translate(m.Vec2{X: glyph.pen.X, Y: glyph.pen.Y - icon.size.Y})
			instances = append(instances, instanceData{
				tint:      tint.Premultiply(),
				region:    image.region,
				crop:      m.Box2{Max: m.Vec2{X: 1, Y: 1}},
				transform: transform.Mul(m.Mat2x3{iconRect.Width, 0, 0, iconRect.Height, iconRect.X, iconRect.Y}),
				page:      image.page,
				size:      iconRect.Size(),
			})
			continue
		}
		drawn := atlases.placedGlyph(glyph.field)
		if !drawn.packed {
			continue
		}
		glyphRect := t.findGlyphRect(glyph, drawn)
		instances = append(instances, instanceData{
			tint:      tint.Premultiply(),
			region:    drawn.region,
			crop:      m.Box2{Max: m.Vec2{X: 1, Y: 1}},
			transform: transform.Mul(m.Mat2x3{glyphRect.Width, 0, 0, glyphRect.Height, glyphRect.X, glyphRect.Y}),
			page:      drawn.packedRegion.Layer,
			size:      glyphRect.Size(),
			width:     drawn.spread,
			tiles:     m.Vec2{X: -drawn.bounds.X / drawn.bounds.Width, Y: -drawn.bounds.Y / drawn.bounds.Height},
		})
	}
	return instances
}

// findGlyphRect answers the rectangle in the run's own space that glyph,
// its field as drawn, draws its field over: the field's bounds, in em,
// scaled by the run's em, about the glyph's pen.
func (t *laidOutText) findGlyphRect(glyph placedGlyph, drawn *drawnGlyph) m.Rect {
	em := t.layout.em
	bounds := drawn.bounds
	return m.Rect{X: glyph.pen.X + bounds.X*em, Y: glyph.pen.Y + bounds.Y*em, Width: bounds.Width * em, Height: bounds.Height * em}
}
