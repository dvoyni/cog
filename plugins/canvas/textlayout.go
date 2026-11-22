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
	// lineWidths are each line's width, in order.
	lineWidths []float32
	// clusterPositions are where each grapheme cluster of the run's text
	// starts, then where the text ends.
	clusterPositions []m.Vec2
	// glyphs are the run's glyphs, line by line, each line's left to right,
	// but for those of no field once laid out for drawing.
	glyphs []placedGlyph
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

// placedGlyph is one glyph a layout draws: the font of the run's set that
// draws it and its id in that font, the index of its field in the
// FontLibrary's glyph table once laid out for drawing, and its pen on its
// line's baseline, in world units from the box's top left.
type placedGlyph struct {
	font  *loadedFont
	id    font.GID
	field uint32
	pen   m.Vec2
}
