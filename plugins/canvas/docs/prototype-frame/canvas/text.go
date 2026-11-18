package canvas

import (
	"github.com/dvoyni/cog/libs/m"
	"prototype.invalid/canvasframe/mx"
)

// Text is an entry that draws a run of text: one entry, its glyphs drawn in
// order, with the sprite's sort key.
type Text struct {
	// String is the run's text, read as markup when Markup is set: \i:name is
	// an icon, \c:color{text} a tinted span, \\ a backslash. Spans, when
	// given, are the run instead.
	String string
	Markup bool
	Spans  []Span
	// Font is the run's font set, zero meaning the default set; Size its em
	// in world units, zero meaning Config.TextSize.
	Font FontSet
	Size float32
	// Language overrides the current language for this run alone, such as a
	// name in another language.
	Language  string
	Transform mx.Mat2x3
	// Pivot is a point of the layout's box, zero meaning its centre, its
	// Texels in em. A label over a unit takes PivotBottom, a UI PivotTopLeft.
	Pivot Pivot
	// Tint is the fill's colour, zero meaning white.
	Tint m.Color
	// Material is zero for canvas's TextMaterial.
	Material Material
	Clip     m.Rect
	Filter   Filter
	// WrapWidth wraps lines on UAX #14 breaks, zero meaning no wrapping.
	WrapWidth float32
	Align     Align
	// LineHeight multiplies the font's line height, zero meaning 1; Tracking
	// is letter spacing in em.
	LineHeight, Tracking float32
	Layer, Order         int32
}

// Span is a part of a run with a tint of its own, zero meaning the run's, or
// an icon registered by name. A run's spans are shaped and wrapped together.
type Span struct {
	Text string
	Tint m.Color
	Icon string
}

// Align is how a run's lines align, right-to-left text aligning from its
// right.
type Align uint8

const (
	AlignStart Align = iota
	AlignCenter
	AlignEnd
)

// Layout is text shaped into glyphs, broken into lines and measured.
type Layout struct {
	size     m.Vec2
	lines    []float32
	baseline float32
	clusters []m.Vec2
}

// Size answers the layout's box: the wrap width, or its widest line without
// wrapping, by its lines' height.
func (l Layout) Size() m.Vec2 { return l.size }

// LineCount answers how many lines it has.
func (l Layout) LineCount() int { return len(l.lines) }

// Baseline answers its first line's baseline, from the box's top.
func (l Layout) Baseline() float32 { return l.baseline }

// LineWidth answers line's width.
func (l Layout) LineWidth(line int) float32 { return l.lines[line] }

// ClusterPosition answers where the grapheme cluster cluster starts, for a
// caret or a selection.
func (l Layout) ClusterPosition(cluster int) m.Vec2 { return l.clusters[cluster] }

// DrawText records text, laid out through canvas's cache, so a label drawn
// every frame is shaped once.
func (l DrawList) DrawText(text Text) { l.recordText(text, nil) }

// DrawTextWith records text with its material's argument: for TextMaterial,
// the Profile drawn under the fill, such as an Outline.
func (l DrawList) DrawTextWith[T any](text Text, argument T) { l.recordText(text, argument) }
