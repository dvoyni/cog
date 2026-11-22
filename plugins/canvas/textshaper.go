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
	// faces are a face of each font the shaper shaped, and the font, by its
	// parsed font, which go-text's shaper keeps its own state of by as well,
	// and which each loaded font has of its own.
	faces map[*font.Font]fontFace
	// The run being laid out: its set and its language's tag, which pick
	// the font of each character, its em in world units over its shaping
	// em, its lines' height, the width they wrap at and its tracking, both
	// in shaping units, and its language as the shaper takes it, with the
	// layout so far.
	set         *fontSetSlot
	languageTag string
	scale       float32
	lineHeight  float32
	maxWidth    fixed.Int26_6
	tracking    fixed.Int26_6
	language    language.Language
	layout      TextLayout
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
	// lineOfCluster the line each cluster position lies on, and lineOfGlyph
	// the line each glyph lies on, which align the positions and the glyphs
	// once the box's width is known, by each line's lineOffsets.
	lineRightToLeft []bool
	lineOfCluster   []int
	lineOfGlyph     []int
	lineOffsets     []float32
}

// ResolveFace answers, as go-text's shaping.Fontmap, the shaper's face of
// the font drawing character in the run's set and language.
func (s *textShaper) ResolveFace(character rune) *font.Face {
	return s.findFace(s.set.findCharacterFont(s.languageTag, character))
}

// layOut answers the layout of input, its text laid out in its set at its
// em, in world units, shaped in its language: each character drawn by the
// first of the language's fonts that has it, else by the default font,
// and its lines measured by the font measuring the language, as
// FontLibrary.Metrics reads.
func (s *textShaper) layOut(input layoutInput) TextLayout {
	em := input.em
	s.set = input.set
	s.languageTag = input.language
	metrics := input.set.findLanguageFont(input.language).findMetrics(em)
	s.scale = em / (shapingEm * 64)
	s.lineHeight = metrics.LineHeight * cmp.Or(input.lineHeight, 1)
	s.maxWidth = math.MaxInt32
	if input.wrapWidth > 0 {
		s.maxWidth = fixed.Int26_6(input.wrapWidth / s.scale)
	}
	s.tracking = fixed.Int26_6(input.tracking * shapingEm * 64)
	s.language = language.NewLanguage(input.language)
	s.layout = TextLayout{baseline: (s.lineHeight-metrics.Ascent-metrics.Descent)/2 + metrics.Ascent, em: em, glyphs: make([]placedGlyph, 0, len(input.text))}
	s.runes = s.runes[:0]
	for _, character := range input.text {
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
	s.lineOfGlyph = s.lineOfGlyph[:0]
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
	s.alignLines(input)
	layout := s.layout
	s.layout = TextLayout{}
	s.set = nil
	return layout
}

// findFace answers the shaper's face of textFont, made at its variations
// the first time.
func (s *textShaper) findFace(textFont *loadedFont) *font.Face {
	shapedFace, cached := s.faces[textFont.font]
	if !cached {
		shapedFace = fontFace{face: font.NewFace(textFont.font), font: textFont}
		shapedFace.face.SetCoords(textFont.coordinates)
		if s.faces == nil {
			s.faces = map[*font.Font]fontFace{}
		}
		s.faces[textFont.font] = shapedFace
	}
	return shapedFace.face
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
	// The segmenter splits the paragraph into runs of one face, each the
	// face of the font drawing its characters, as ResolveFace answers.
	input := shaping.Input{Text: runes, RunEnd: len(runes), Direction: direction, Size: fixed.I(shapingEm), Language: s.language}
	s.outputs = s.outputs[:0]
	for _, run := range s.splitter.Split(input, s) {
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
// with each of its glyphs, and measures it.
func (s *textShaper) placeLine(line shaping.Line, start int) {
	s.runOfVisualIndex = slices.Grow(s.runOfVisualIndex[:0], len(line))[:len(line)]
	for index, run := range line {
		s.runOfVisualIndex[run.VisualIndex] = index
	}
	var x fixed.Int26_6
	for _, runIndex := range s.runOfVisualIndex {
		run := line[runIndex]
		rightToLeft := run.Direction.Progression() == di.TowardTopLeft
		runFont := s.faces[run.Face.Font].font
		for glyph := 0; glyph < len(run.Glyphs); {
			cluster := run.Glyphs[glyph]
			glyphCount := max(cluster.GlyphCount, 1)
			var advance fixed.Int26_6
			for _, clusterGlyph := range run.Glyphs[glyph : glyph+glyphCount] {
				s.placeGlyph(runFont, clusterGlyph, x+advance)
				advance += clusterGlyph.Advance
			}
			s.placeClusters(cluster, start, x, advance, rightToLeft)
			x += advance
			glyph += glyphCount
		}
	}
	s.layout.lineWidths = append(s.layout.lineWidths, float32(x)*s.scale)
}

// placeGlyph places glyph, of glyphFont, on the line being placed, its pen
// at x, in shaping units, moved by its offsets, which point up as go-text's
// do.
func (s *textShaper) placeGlyph(glyphFont *loadedFont, glyph shaping.Glyph, x fixed.Int26_6) {
	line := len(s.layout.lineWidths)
	pen := m.Vec2{X: float32(x+glyph.XOffset) * s.scale, Y: s.layout.baseline + float32(line)*s.lineHeight - float32(glyph.YOffset)*s.scale}
	s.layout.glyphs = append(s.layout.glyphs, placedGlyph{font: glyphFont, id: glyph.GlyphID, pen: pen})
	s.lineOfGlyph = append(s.lineOfGlyph, line)
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

// alignLines sizes the layout's box, input's wrap width or its widest
// line, by its lines' height, and moves each cluster position and each
// glyph by its line's alignment in it.
func (s *textShaper) alignLines(input layoutInput) {
	width := input.wrapWidth
	if width <= 0 {
		width = slices.Max(s.layout.lineWidths)
	}
	s.layout.size = m.Vec2{X: width, Y: float32(len(s.layout.lineWidths)) * s.lineHeight}
	s.lineOffsets = s.lineOffsets[:0]
	for line, lineWidth := range s.layout.lineWidths {
		room := width - lineWidth
		var offset float32
		switch {
		case input.align == TextAlignCenter:
			offset = room / 2
		case (input.align == TextAlignEnd) != s.lineRightToLeft[line]:
			offset = room
		}
		s.lineOffsets = append(s.lineOffsets, offset)
	}
	for cluster, line := range s.lineOfCluster {
		s.layout.clusterPositions[cluster].X += s.lineOffsets[line]
	}
	for glyph, line := range s.lineOfGlyph {
		s.layout.glyphs[glyph].pen.X += s.lineOffsets[line]
	}
}

// fontFace is a face the shaper made of a font, and the font.
type fontFace struct {
	face *font.Face
	font *loadedFont
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
