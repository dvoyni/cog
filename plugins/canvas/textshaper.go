package canvas

import (
	"cmp"
	"fmt"
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

// iconCharacter is the character an icon is shaped as, the object
// replacement character, drawn by no font of the run's set: the shaper
// gives it the icon's advance, and a name no icon is registered under the
// default font's missing glyph.
const iconCharacter = '\uFFFC'

// iconBreakCharacter is the character an icon is to the line wrapper: a
// letter, so a line breaks around an icon as around a letter, never between
// it and a letter beside it, where Unicode's line breaking (UAX #14) breaks
// around the object replacement character.
const iconBreakCharacter = 'A'

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
	// spanKey is the scratch a spanned run's layout's key is encoded into
	// as it is looked up, and markupSpans the scratch a run's markup is
	// read into.
	spanKey     []byte
	markupSpans []Span
	// The run being laid out: its set and its language's tag, which pick
	// the font of each character, the cap height of the font measuring its
	// language at its em, its em in world units over its shaping em, its
	// lines' height, the width they wrap at and its tracking, both in
	// shaping units, and its language as the shaper takes it, with the
	// layout so far.
	set         *fontSetSlot
	languageTag string
	capHeight   float32
	scale       float32
	lineHeight  float32
	maxWidth    fixed.Int26_6
	tracking    fixed.Int26_6
	language    language.Language
	layout      TextLayout
	// runes are the run's text, its spans' end to end, each icon's
	// iconCharacter; breakRunes the same, each icon's iconBreakCharacter,
	// which the line wrapper reads; runeSpans the span each rune is of;
	// outputs a paragraph's runs shaped; and graphemeStarts the rune each
	// of the text's grapheme clusters starts at.
	runes          []rune
	breakRunes     []rune
	runeSpans      []uint32
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
	if character == iconCharacter {
		return s.findFace(&s.set.defaultFont)
	}
	return s.findFace(s.set.findCharacterFont(s.languageTag, character))
}

// layOut answers the layout of input, its text laid out in its set at its
// em, in world units, shaped in its language: each character drawn by the
// first of the language's fonts that has it, else by the default font,
// and its lines measured by the font measuring the language, as
// FontLibrary.Metrics reads. A spanned input's icons are those registered
// in icons under their names; a name none is registered under is reported
// once.
func (s *textShaper) layOut(input layoutInput, icons map[string]registeredIcon) TextLayout {
	em := input.em
	s.set = input.set
	s.languageTag = input.language
	metrics := input.set.findLanguageFont(input.language).findMetrics(em)
	s.capHeight = metrics.CapHeight
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
	s.breakRunes = s.breakRunes[:0]
	s.runeSpans = s.runeSpans[:0]
	if input.spanned {
		for span := range decodeSpans(input.text) {
			spanIndex := uint32(len(s.layout.spanIcons))
			var icon *placedIcon
			if span.icon {
				icon = s.placeIcon(span.content, icons)
				s.appendRune(iconCharacter, iconBreakCharacter, spanIndex)
			} else {
				for _, character := range span.content {
					s.appendRune(character, character, spanIndex)
				}
			}
			s.layout.spanIcons = append(s.layout.spanIcons, icon)
		}
	} else {
		for _, character := range input.text {
			s.appendRune(character, character, 0)
		}
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

// appendRune appends character to the run's runes, and breakCharacter to
// the runes the line wrapper reads, of the span at spanIndex.
func (s *textShaper) appendRune(character, breakCharacter rune, spanIndex uint32) {
	s.runes = append(s.runes, character)
	s.breakRunes = append(s.breakRunes, breakCharacter)
	s.runeSpans = append(s.runeSpans, spanIndex)
}

// findRuneIcon answers the icon the run's rune character is, nil for a
// rune of text and for a name no icon is registered under.
func (s *textShaper) findRuneIcon(character int) *placedIcon {
	if s.layout.spanIcons == nil {
		return nil
	}
	return s.layout.spanIcons[s.runeSpans[character]]
}

// placeIcon answers the icon registered in icons under name, placed at the
// run's em: the cap height of the font measuring its language times its
// Scale, zero meaning 1, tall, its width by its frame's aspect, raised by
// its Offset, in em. A name none is registered under answers nil,
// reported once for the name.
func (s *textShaper) placeIcon(name string, icons map[string]registeredIcon) *placedIcon {
	registered, found := icons[name]
	if !found {
		missing := Report{Kind: ReportKindIconNotRegistered, Err: fmt.Errorf("canvas: a run of text names icon %q, which no icon is registered under, so it is drawn as the default font's missing-glyph box", name)}
		s.set.logger.LogErrOnce(iconNotRegisteredLogKey+name, missing)
		return nil
	}
	icon := registered.descriptor
	height := s.capHeight * cmp.Or(icon.Scale, 1)
	size := m.Vec2{X: height * registered.frameSize.X / registered.frameSize.Y, Y: height}
	return &placedIcon{image: icon.Image, frame: icon.Frame, size: size, offset: icon.Offset, tinted: icon.Tinted}
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
		output := s.shaper.Shape(run)
		s.sizeIcons(&output, start)
		s.outputs = append(s.outputs, output)
	}
	if s.tracking != 0 {
		shaping.AddSpacing(s.outputs, runes, 0, s.tracking)
	}
	s.wrapper.Prepare(shaping.WrapConfig{Direction: direction}, s.breakRunes[start:end], shaping.NewSliceIterator(s.outputs))
	for done := false; !done; {
		var line shaping.WrappedLine
		line, done = s.wrapper.WrapNextLineF(s.maxWidth)
		if line.Line != nil {
			s.placeLine(line.Line, start)
			s.lineRightToLeft = append(s.lineRightToLeft, direction == di.DirectionRTL)
		}
	}
}

// sizeIcons gives each glyph of output, shaped of a paragraph whose runes
// start at the run's rune start, that is an icon its icon's width as its
// advance and content and its offset as its own, which point up, and each
// that is an icon of a name no icon is registered under the default font's
// missing glyph, whose face shaped it.
func (s *textShaper) sizeIcons(output *shaping.Output, start int) {
	sized := false
	for index := range output.Glyphs {
		glyph := &output.Glyphs[index]
		character := start + glyph.ClusterIndex
		if s.runes[character] != iconCharacter {
			continue
		}
		var advance, raise fixed.Int26_6
		if icon := s.findRuneIcon(character); icon != nil {
			advance = fixed.Int26_6(icon.size.X / s.scale)
			raise = fixed.Int26_6(icon.offset * shapingEm * 64)
		} else {
			glyph.GlyphID = 0
			advance = output.FromFontUnit(output.Face.HorizontalAdvance(0))
		}
		// A glyph of no width ending a run is taken for a space.
		glyph.Advance = advance
		glyph.XAdvance = advance
		glyph.Width = advance
		glyph.XOffset = 0
		glyph.YOffset = raise
		sized = true
	}
	if sized {
		output.RecalculateAll()
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
				s.placeGlyph(runFont, clusterGlyph, x+advance, start+clusterGlyph.ClusterIndex)
				advance += clusterGlyph.Advance
			}
			s.placeClusters(cluster, start, x, advance, rightToLeft)
			x += advance
			glyph += glyphCount
		}
	}
	s.layout.lineWidths = append(s.layout.lineWidths, float32(x)*s.scale)
}

// placeGlyph places glyph, of glyphFont, shaped of the run's rune
// character, on the line being placed, its pen at x, in shaping units,
// moved by its offsets, which point up as go-text's do: an icon of no
// font.
func (s *textShaper) placeGlyph(glyphFont *loadedFont, glyph shaping.Glyph, x fixed.Int26_6, character int) {
	line := len(s.layout.lineWidths)
	pen := m.Vec2{X: float32(x+glyph.XOffset) * s.scale, Y: s.layout.baseline + float32(line)*s.lineHeight - float32(glyph.YOffset)*s.scale}
	placed := placedGlyph{font: glyphFont, id: glyph.GlyphID, span: s.runeSpans[character], pen: pen}
	if s.findRuneIcon(character) != nil {
		placed.font = nil
		placed.id = 0
	}
	s.layout.glyphs = append(s.layout.glyphs, placed)
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
