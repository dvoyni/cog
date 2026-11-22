package canvas

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/go-text/typesetting/font"
)

// TextLayout is a run of text shaped, broken into lines and measured, which
// FontLibrary.Layout answers, in world units from its box's top left, y
// pointing down. Its box is the run's wrap width, or its widest line
// without wrapping, by its lines' height.
type TextLayout struct {
	size     m.Vec2
	baseline float32
	// em is the run's em, in world units, which its glyphs' fields are
	// scaled by.
	em float32
	// fontFilter is the filter a run of it reads its glyphs with when it
	// names none, its font's: nearest where a bitmap font measures its
	// language, else linear, as a distance field is read.
	fontFilter Filter
	// lineWidths are each line's width, in order.
	lineWidths []float32
	// clusterPositions are where each grapheme cluster of the run's text
	// starts, then where the text ends.
	clusterPositions []m.Vec2
	// glyphs are the run's glyphs, line by line, each line's left to right,
	// but for those of no field once laid out for drawing.
	glyphs []placedGlyph
	// spanIcons are the icon each of the run's spans is, nil for a span of
	// text and for a name no icon is registered under, and none for a run
	// of no spans.
	spanIcons []*placedIcon
}

// Size answers the layout's box.
func (l TextLayout) Size() m.Vec2 { return l.size }

// LineCount answers how many lines the layout has: one at least, an empty
// paragraph's line holding nothing.
func (l TextLayout) LineCount() int { return len(l.lineWidths) }

// Baseline answers the first line's baseline, from the box's top.
func (l TextLayout) Baseline() float32 { return l.baseline }

// LineWidth answers the width of line, counted from zero, its trailing
// spaces left out.
func (l TextLayout) LineWidth(line int) float32 { return l.lineWidths[line] }

// ClusterPosition answers where the grapheme cluster cluster of the run's
// text starts, counted from zero, on its line's baseline: its left edge in
// left-to-right text, its right edge in right-to-left text. One past the
// last cluster answers where the text ends; a hard break's cluster, such as
// a newline's, is where its line ends. A ligature's width is shared evenly
// by the clusters it draws.
func (l TextLayout) ClusterPosition(cluster int) m.Vec2 { return l.clusterPositions[cluster] }

// findIcon answers the icon glyph is, nil for a glyph of a font.
func (l TextLayout) findIcon(glyph placedGlyph) *placedIcon {
	if glyph.font != nil {
		return nil
	}
	return l.spanIcons[glyph.span]
}

// placedGlyph is one glyph a layout draws: the font of the run's set that
// draws it and its id in that font, the index of its field in the
// FontLibrary's glyph table once laid out for drawing, the index of the
// span of the run's Spans it is of, zero for a run of no spans, and its pen
// on its line's baseline, in world units from the box's top left. An icon
// is a glyph of no font, its span's icon, its pen its bottom left.
type placedGlyph struct {
	font  *loadedFont
	id    font.GID
	field uint32
	span  uint32
	pen   m.Vec2
}

// placedIcon is an icon a layout places, as it was registered when the
// layout was made: its image and frame, its size in world units, its
// bottom raised above the baseline by offset, in em, and whether it takes
// its span's tint.
type placedIcon struct {
	image  Image
	frame  uint32
	size   m.Vec2
	offset float32
	tinted bool
}
