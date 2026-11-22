package canvas

import (
	"cmp"
	"math"
	"slices"

	"github.com/dvoyni/cog/libs/m"
	"github.com/go-text/typesetting/bidi"
	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/segmenter"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
)

// shapingEm is the em, in go-text's units, every run is shaped at, then
// scaled to its own: go-text measures in 26.6 fixed point, which a run's
// own em in world units, however small, would round coarsely.
const shapingEm = 1024

// textShaper lays runs of text out: it holds go-text's shaper, segmenters
// and line wrapper, and a face of each font it shaped, which keep what they
// read and are not safe for concurrent use, and the scratch of the run it
// lays out, so each layout takes one of the FontLibrary resource's for
// itself.
type textShaper struct {
	shaper    shaping.HarfbuzzShaper
	splitter  shaping.Segmenter
	wrapper   shaping.LineWrapper
	paragraph bidi.Paragraph
	segmenter segmenter.Segmenter
	// faces are a face of each font the shaper shaped, by the font, which
	// go-text's shaper keeps its own state of by the font as well.
	faces map[*font.Font]*font.Face
	// The run being laid out: its face, its em in world units over its
	// shaping em, its lines' height, the width they wrap at and its
	// tracking, both in shaping units, and its language, with the layout
	// so far.
	face       *font.Face
	scale      float32
	lineHeight float32
	maxWidth   fixed.Int26_6
	tracking   fixed.Int26_6
	language   language.Language
	layout     TextLayout
	// runes are the run's text, outputs a paragraph's runs shaped, and
	// graphemeStarts the rune each of the text's grapheme clusters starts
	// at.
	runes          []rune
	outputs        []shaping.Output
	graphemeStarts []int
	// runOfVisualIndex is a line's runs in their visual order, left to
	// right, by their index in the line.
	runOfVisualIndex []int
	// lineRightToLeft is whether each line's paragraph runs right to left,
	// and lineOfCluster the line each cluster position lies on, which
	// align the positions once the box's width is known.
	lineRightToLeft []bool
	lineOfCluster   []int
}

// layOut answers text laid out in textFont at em, in world units, shaped
// in languageTag.
func (s *textShaper) layOut(textFont loadedFont, text Text, em float32, languageTag string) TextLayout {
	s.face = s.faces[textFont.font]
	if s.face == nil {
		s.face = font.NewFace(textFont.font)
		s.face.SetCoords(textFont.coordinates)
		if s.faces == nil {
			s.faces = map[*font.Font]*font.Face{}
		}
		s.faces[textFont.font] = s.face
	}
	metrics := textFont.findMetrics(em)
	s.scale = em / (shapingEm * 64)
	s.lineHeight = metrics.LineHeight * cmp.Or(text.LineHeight, 1)
	s.maxWidth = math.MaxInt32
	if text.WrapWidth > 0 {
		s.maxWidth = fixed.Int26_6(text.WrapWidth / s.scale)
	}
	s.tracking = fixed.Int26_6(text.Tracking * shapingEm * 64)
	s.language = language.NewLanguage(languageTag)
	s.layout = TextLayout{baseline: (s.lineHeight-metrics.Ascent-metrics.Descent)/2 + metrics.Ascent}
	s.runes = s.runes[:0]
	for _, character := range text.String {
		s.runes = append(s.runes, character)
	}
	s.segmenter.Init(s.runes)
	s.graphemeStarts = s.graphemeStarts[:0]
	for iterator := s.segmenter.GraphemeIterator(); iterator.Next(); {
		s.graphemeStarts = append(s.graphemeStarts, iterator.Grapheme().Offset)
	}
	// One more position than clusters: where the text ends.
	s.layout.clusterPositions = make([]m.Vec2, len(s.graphemeStarts)+1)
	s.lineOfCluster = slices.Grow(s.lineOfCluster[:0], len(s.graphemeStarts)+1)[:len(s.graphemeStarts)+1]
	s.lineRightToLeft = s.lineRightToLeft[:0]
	// A paragraph ends at each mandatory break of Unicode's line breaking
	// (UAX #14), a newline among them, and is laid out on lines of its own,
	// in a direction of its own; an empty text, or one ending in a break,
	// ends in an empty paragraph.
	paragraphStart := 0
	endsInBreak := true
	for iterator := s.segmenter.LineIterator(); iterator.Next(); {
		line := iterator.Line()
		if line.IsMandatoryBreak {
			end := line.Offset + len(line.Text)
			endsInBreak = s.layOutParagraph(paragraphStart, end)
			paragraphStart = end
		}
	}
	if endsInBreak {
		s.layOutParagraph(len(s.runes), len(s.runes))
	}
	s.placeLineEnd(len(s.graphemeStarts))
	s.alignLines(text)
	layout := s.layout
	s.layout = TextLayout{}
	s.face = nil
	return layout
}

// layOutParagraph lays out the paragraph of the run's runes from start to
// end onto the layout's next lines, and answers whether it ends in a hard
// break, whose clusters are placed where its last line ends.
func (s *textShaper) layOutParagraph(start, end int) bool {
	content := end
	for content > start && isHardBreak(s.runes[content-1]) {
		content--
	}
	if content == start {
		s.layout.lineWidths = append(s.layout.lineWidths, 0)
		s.lineRightToLeft = append(s.lineRightToLeft, false)
	} else {
		s.layOutContent(start, content)
	}
	breakCluster, _ := slices.BinarySearch(s.graphemeStarts, content)
	for ; breakCluster < len(s.graphemeStarts) && s.graphemeStarts[breakCluster] < end; breakCluster++ {
		s.placeLineEnd(breakCluster)
	}
	return content < end
}

// layOutContent shapes and wraps the paragraph's runes from start to end,
// none of them a hard break, onto the layout's next lines.
func (s *textShaper) layOutContent(start, end int) {
	runes := s.runes[start:end]
	// go-text's segmenter takes a paragraph's direction rather than finding
	// it, so its bidi pass runs once here, to find it.
	runs := s.paragraph.Segment(runes, bidi.Neutral)
	level := runs.Run(0).Level
	for index := range runs.NumRuns() {
		level = min(level, runs.Run(index).Level)
	}
	direction := di.DirectionLTR
	if level%2 == 1 {
		direction = di.DirectionRTL
	}
	input := shaping.Input{Text: runes, RunEnd: len(runes), Direction: direction, Face: s.face, Size: fixed.I(shapingEm), Language: s.language}
	s.outputs = s.outputs[:0]
	for _, run := range s.splitter.Split(input, singleFaceFontmap{face: s.face}) {
		s.outputs = append(s.outputs, s.shaper.Shape(run))
	}
	if s.tracking != 0 {
		shaping.AddSpacing(s.outputs, runes, 0, s.tracking)
	}
	s.wrapper.Prepare(shaping.WrapConfig{Direction: direction}, runes, shaping.NewSliceIterator(s.outputs))
	for done := false; !done; {
		var line shaping.WrappedLine
		line, done = s.wrapper.WrapNextLineF(s.maxWidth)
		if line.Line != nil {
			s.placeLine(line.Line, start)
			s.lineRightToLeft = append(s.lineRightToLeft, direction == di.DirectionRTL)
		}
	}
}

// placeLine places line, the next line of the paragraph whose runes start
// at the run's rune start, its runs left to right in their visual order,
// and measures it.
func (s *textShaper) placeLine(line shaping.Line, start int) {
	s.runOfVisualIndex = slices.Grow(s.runOfVisualIndex[:0], len(line))[:len(line)]
	for index, run := range line {
		s.runOfVisualIndex[run.VisualIndex] = index
	}
	var x fixed.Int26_6
	for _, runIndex := range s.runOfVisualIndex {
		run := line[runIndex]
		rightToLeft := run.Direction.Progression() == di.TowardTopLeft
		for glyph := 0; glyph < len(run.Glyphs); {
			cluster := run.Glyphs[glyph]
			glyphCount := max(cluster.GlyphCount, 1)
			var advance fixed.Int26_6
			for _, clusterGlyph := range run.Glyphs[glyph : glyph+glyphCount] {
				advance += clusterGlyph.Advance
			}
			s.placeClusters(cluster, start, x, advance, rightToLeft)
			x += advance
			glyph += glyphCount
		}
	}
	s.layout.lineWidths = append(s.layout.lineWidths, float32(x)*s.scale)
}

// placeClusters places, on the line being placed, the grapheme clusters
// starting in the runes of cluster, a glyph cluster of a paragraph whose
// runes start at the run's rune start, drawn at x, sharing its advance
// evenly, from its right when rightToLeft.
func (s *textShaper) placeClusters(cluster shaping.Glyph, start int, x, advance fixed.Int26_6, rightToLeft bool) {
	line := len(s.layout.lineWidths)
	first, _ := slices.BinarySearch(s.graphemeStarts, start+cluster.ClusterIndex)
	end, _ := slices.BinarySearch(s.graphemeStarts, start+cluster.ClusterIndex+cluster.RuneCount)
	left := float32(x) * s.scale
	width := float32(advance) * s.scale
	for grapheme := first; grapheme < end; grapheme++ {
		offset := width * float32(grapheme-first) / float32(end-first)
		if rightToLeft {
			offset = width - offset
		}
		s.layout.clusterPositions[grapheme] = m.Vec2{X: left + offset, Y: s.layout.baseline + float32(line)*s.lineHeight}
		s.lineOfCluster[grapheme] = line
	}
}

// placeLineEnd places cluster, a hard break's or the text's end, where the
// layout's last line ends.
func (s *textShaper) placeLineEnd(cluster int) {
	line := len(s.layout.lineWidths) - 1
	position := m.Vec2{Y: s.layout.baseline + float32(line)*s.lineHeight}
	if !s.lineRightToLeft[line] {
		position.X = s.layout.lineWidths[line]
	}
	s.layout.clusterPositions[cluster] = position
	s.lineOfCluster[cluster] = line
}

// alignLines sizes the layout's box, text's wrap width or its widest line,
// by its lines' height, and moves each cluster position by its line's
// alignment in it.
func (s *textShaper) alignLines(text Text) {
	width := text.WrapWidth
	if width <= 0 {
		width = slices.Max(s.layout.lineWidths)
	}
	s.layout.size = m.Vec2{X: width, Y: float32(len(s.layout.lineWidths)) * s.lineHeight}
	for cluster, line := range s.lineOfCluster {
		room := width - s.layout.lineWidths[line]
		var offset float32
		switch {
		case text.Align == TextAlignCenter:
			offset = room / 2
		case (text.Align == TextAlignEnd) != s.lineRightToLeft[line]:
			offset = room
		}
		s.layout.clusterPositions[cluster].X += offset
	}
}

// isHardBreak answers whether character forces a line break by Unicode's
// line breaking (UAX #14): a line feed, a carriage return, a next line, a
// line or paragraph separator, a line tabulation or a form feed.
func isHardBreak(character rune) bool {
	switch character {
	case '\n', '\r', '\u0085', '\u2028', '\u2029', '\v', '\f':
		return true
	}
	return false
}
