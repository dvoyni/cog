package font

import (
	"cmp"
	"fmt"
	"image"
	"math"
	"slices"

	"github.com/dvoyni/cog/libs/m"
	"github.com/go-text/typesetting/bidi"
	"github.com/go-text/typesetting/di"
	gotext "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/segmenter"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// GlyphID is a glyph's index in its font.
type GlyphID uint32

// Align is how a layout's lines align in its box. A paragraph's start and
// end follow its direction: a right-to-left paragraph starts at the right.
type Align uint8

const (
	// AlignStart aligns each line to its paragraph's start.
	AlignStart Align = iota
	// AlignCenter centres each line.
	AlignCenter
	// AlignEnd aligns each line to its paragraph's end.
	AlignEnd
)

// Input is a run of text to lay out, in world units: its Runes, drawn in
// Set's fonts for Language at an em of Em, its lines wrapped at WrapWidth,
// zero meaning no wrapping, their height LineHeight times the line height
// of the font measuring Language, zero meaning 1, its letters spaced by
// Tracking, in em, half on each side of a letter and none at a line's ends,
// and its lines aligned by Align.
type Input struct {
	Set   *Set
	Runes []rune
	// Objects are the runes laid out as icons, each among Runes and laid
	// out once, or LayOut panics.
	Objects    []Object
	Language   string
	Em         float32
	WrapWidth  float32
	LineHeight float32
	Tracking   float32
	Align      Align
}

// Object is the rune at index Rune of an Input's Runes laid out as an
// icon, whatever rune it holds: a box Width wide, in world units, which a
// line breaks around as around a letter, its glyph's pen raised by Raise,
// in world units. A zero Width lays it out as the default font's missing
// glyph instead.
type Object struct {
	Rune         int
	Width, Raise float32
}

// Layout is a run laid out, in world units from its box's top left, y
// pointing down: its box's Size, its input's wrap width or its widest line
// without wrapping, by its lines' height; its first line's Baseline from
// the box's top; each line's width in LineWidths, its trailing spaces left
// out; where each grapheme cluster of its runes starts in
// ClusterPositions, then where the runes end; its Glyphs, line by line,
// each line's left to right; and its MissingRunes.
type Layout struct {
	Size     m.Vec2
	Baseline float32
	// LineWidths has a line at least: an empty paragraph's line holds
	// nothing.
	LineWidths []float32
	// ClusterPositions are each on its line's baseline: a cluster's left
	// edge in left-to-right text and its right edge in right-to-left text,
	// a hard break's where its line ends. A ligature's width is shared
	// evenly by the clusters it draws.
	ClusterPositions []m.Vec2
	Glyphs           []Glyph
	// MissingRunes are the index among the input's runes of each rune no
	// font of the set has, in order, nil when every font needed was found:
	// each is drawn as the default font's missing glyph. Neither objects nor
	// the controls, separators, spaces and default-ignorable runes that take
	// the font of the runes beside them are ever among them.
	MissingRunes []int
}

// Glyph is one glyph a layout draws: its font, of the input's set, and its
// ID in it, the index among the input's runes of the first rune of the
// cluster it was shaped of, and its Pen on its line's baseline. An object
// of a non-zero width is a glyph of a nil Font, its Pen its box's bottom
// left.
type Glyph struct {
	Font *Loaded
	ID   GlyphID
	Rune int
	Pen  m.Vec2
}

// Coverage is a glyph's outline filled: its Bounds about its pen on the
// baseline, in em, y pointing down, and its Width and Height in texels,
// with its Texels, a byte each, row by row, how much of each texel the
// glyph covers. A glyph of no outline, such as a space's, has no Texels.
type Coverage struct {
	Bounds        m.Rect
	Width, Height int
	Texels        []byte
}

// shapingEm is the em, in go-text's units, every run is shaped at, then
// scaled to its own: go-text measures in 26.6 fixed point, which a run's
// own em in world units, however small, would round coarsely.
const shapingEm = 1024

// objectCharacter is the character an object is shaped as, the object
// replacement character, which the default font draws: the shaper gives it
// the object's width, and an object of no width the default font's missing
// glyph.
const objectCharacter = '￼'

// objectBreakCharacter is the character an object is to the line wrapper:
// a letter, so a line breaks around an object as around a letter, never
// between it and a letter beside it, where Unicode's line breaking (UAX
// #14) breaks around the object replacement character.
const objectBreakCharacter = 'A'

// Shaper lays runs of text out and fills glyphs' outlines. It holds
// go-text's shaper, segmenters and line wrapper, a face of each font it
// used, which keep what they read, and the scratch of the run it lays out,
// so it is not safe for concurrent use: each job laying text out takes one
// of its own. The zero Shaper is ready.
type Shaper struct {
	shaper    shaping.HarfbuzzShaper
	splitter  shaping.Segmenter
	wrapper   shaping.LineWrapper
	paragraph bidi.Paragraph
	segmenter segmenter.Segmenter
	// faces are a face of each font the shaper used, and the font, by its
	// parsed font, which go-text's shaper keeps its own state of by as well,
	// and which each loaded font has of its own.
	faces map[*gotext.Font]fontFace
	// The run being laid out: its input, the line height and baseline of
	// the font measuring its language, its em in world units over its
	// shaping em, the width its lines wrap at and its tracking, both in
	// shaping units, and its language as the shaper takes it, with the
	// layout so far.
	input      Input
	lineHeight float32
	scale      float32
	maxWidth   fixed.Int26_6
	tracking   fixed.Int26_6
	language   language.Language
	layout     Layout
	// runes are the run's runes, each object's objectCharacter; breakRunes
	// the same, each object's objectBreakCharacter, which the line wrapper
	// reads; runeObjects the index in the input's Objects of the object
	// each rune is, -1 for none; outputs a paragraph's runs shaped; and
	// graphemeStarts the rune each of the runes' grapheme clusters starts
	// at.
	runes          []rune
	breakRunes     []rune
	runeObjects    []int
	outputs        []shaping.Output
	graphemeStarts []int
	// runOfVisualIndex is a line's runs in their visual order, left to
	// right, by their index in the line.
	runOfVisualIndex []int
	// nextRuneToResolve is the index of the rune after the last one
	// ResolveFace found the font of: go-text's segmenter asks of the runes
	// in their order, so the next it asks of is there or after.
	nextRuneToResolve int
	// lineRightToLeft is whether each line's paragraph runs right to left,
	// lineOfCluster the line each cluster position lies on, and lineOfGlyph
	// the line each glyph lies on, which align the positions and the glyphs
	// once the box's width is known, by each line's lineOffsets.
	lineRightToLeft []bool
	lineOfCluster   []int
	lineOfGlyph     []int
	lineOffsets     []float32
}

// LayOut answers input laid out: its runes shaped in their language, each
// drawn by the first of the language's fonts in the set that has it, else
// by the default font's, a rune none has by the default font's missing
// glyph, broken into lines on Unicode's line breaks (UAX #14), each
// paragraph between hard breaks in the direction of its first strong
// character (UAX #9), and measured by the font measuring the language,
// whichever fonts draw its runes, so a fallback glyph never changes a
// line's height. A word too long for its line breaks between grapheme
// clusters. The answer's slices are its own.
func (s *Shaper) LayOut(input Input) Layout {
	s.input = input
	metrics := input.Set.LanguageFont(input.Language).Metrics(input.Em)
	s.scale = input.Em / (shapingEm * 64)
	s.lineHeight = metrics.LineHeight * cmp.Or(input.LineHeight, 1)
	s.maxWidth = math.MaxInt32
	if input.WrapWidth > 0 {
		s.maxWidth = fixed.Int26_6(input.WrapWidth / s.scale)
	}
	s.tracking = fixed.Int26_6(input.Tracking * shapingEm * 64)
	s.language = language.NewLanguage(input.Language)
	s.layout = Layout{Baseline: (s.lineHeight-metrics.Ascent-metrics.Descent)/2 + metrics.Ascent, Glyphs: make([]Glyph, 0, len(input.Runes))}
	s.runes = append(s.runes[:0], input.Runes...)
	s.breakRunes = append(s.breakRunes[:0], input.Runes...)
	s.runeObjects = slices.Grow(s.runeObjects[:0], len(input.Runes))[:len(input.Runes)]
	for index := range s.runeObjects {
		s.runeObjects[index] = -1
	}
	for index, object := range input.Objects {
		if object.Rune < 0 || object.Rune >= len(input.Runes) {
			panic(fmt.Sprintf("font: Input.Objects[%d] lays out rune %d, which is not among the input's %d runes", index, object.Rune, len(input.Runes)))
		}
		if s.runeObjects[object.Rune] >= 0 {
			panic(fmt.Sprintf("font: Input.Objects[%d] lays out rune %d, which Input.Objects[%d] lays out already", index, object.Rune, s.runeObjects[object.Rune]))
		}
		s.runes[object.Rune] = objectCharacter
		s.breakRunes[object.Rune] = objectBreakCharacter
		s.runeObjects[object.Rune] = index
	}
	s.segmenter.Init(s.runes)
	s.graphemeStarts = s.graphemeStarts[:0]
	for iterator := s.segmenter.GraphemeIterator(); iterator.Next(); {
		s.graphemeStarts = append(s.graphemeStarts, iterator.Grapheme().Offset)
	}
	// One more position than clusters: where the runes end.
	s.layout.ClusterPositions = make([]m.Vec2, len(s.graphemeStarts)+1)
	s.lineOfCluster = slices.Grow(s.lineOfCluster[:0], len(s.graphemeStarts)+1)[:len(s.graphemeStarts)+1]
	s.lineRightToLeft = s.lineRightToLeft[:0]
	s.lineOfGlyph = s.lineOfGlyph[:0]
	// A paragraph ends at each mandatory break of Unicode's line breaking
	// (UAX #14), a newline among them, and is laid out on lines of its own,
	// in a direction of its own; empty runes, or runes ending in a break,
	// end in an empty paragraph.
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
	s.alignLines()
	layout := s.layout
	s.layout = Layout{}
	s.input = Input{}
	return layout
}

// Rasterize answers glyph id of glyphFont filled anti-aliased, at its
// variations, at em texels to the em, over the whole texels its outline's
// bounds span and padding more on every side. A glyph only a bitmap or
// colour layers draw answers its plain outline where it has one, and no
// Texels where it has none.
func (s *Shaper) Rasterize(glyphFont *Loaded, id GlyphID, em, padding int) Coverage {
	face := s.findFace(glyphFont)
	var coverage Coverage
	outline, hasOutline := face.GlyphDataOutline(gotext.GID(id))
	if !hasOutline || len(outline.Segments) == 0 {
		return coverage
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
	coverage.Width = int(math.Ceil(float64(outlineMaximum.X))) + padding - left
	coverage.Height = int(math.Ceil(float64(outlineMaximum.Y))) + padding - top
	rasterizer := vector.NewRasterizer(coverage.Width, coverage.Height)
	// place answers point, of the outline, in the coverage's texels.
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
	filled := image.NewAlpha(image.Rect(0, 0, coverage.Width, coverage.Height))
	rasterizer.Draw(filled, filled.Bounds(), image.Opaque, image.Point{})
	texelsPerEm := float32(em)
	coverage.Bounds = m.Rect{X: float32(left) / texelsPerEm, Y: float32(top) / texelsPerEm, Width: float32(coverage.Width) / texelsPerEm, Height: float32(coverage.Height) / texelsPerEm}
	coverage.Texels = filled.Pix
	return coverage
}

// ClearFaces lets go of every face the shaper made, and its scratch, so
// the fonts of a set no longer used can go; a run laid out after makes its
// faces anew.
func (s *Shaper) ClearFaces() { *s = Shaper{} }

// findFace answers the shaper's face of textFont, made at its variations
// the first time.
func (s *Shaper) findFace(textFont *Loaded) *gotext.Face {
	shapedFace, cached := s.faces[textFont.parsed]
	if !cached {
		shapedFace = fontFace{face: gotext.NewFace(textFont.parsed), font: textFont}
		shapedFace.face.SetCoords(textFont.coordinates)
		if s.faces == nil {
			s.faces = map[*gotext.Font]fontFace{}
		}
		s.faces[textFont.parsed] = shapedFace
	}
	return shapedFace.face
}

// layOutParagraph lays out the paragraph of the run's runes from start to
// end onto the layout's next lines, and answers whether it ends in a hard
// break, whose clusters are placed where its last line ends.
func (s *Shaper) layOutParagraph(start, end int) bool {
	content := end
	for content > start && isHardBreak(s.runes[content-1]) {
		content--
	}
	if content == start {
		s.layout.LineWidths = append(s.layout.LineWidths, 0)
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
func (s *Shaper) layOutContent(start, end int) {
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
	s.nextRuneToResolve = start
	for _, run := range s.splitter.Split(input, (*faceResolver)(s)) {
		output := s.shaper.Shape(run)
		s.sizeObjects(&output, start)
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

// sizeObjects gives each glyph of output, shaped of a paragraph whose runes
// start at the run's rune start, that is an object its width as its
// advance and content and its raise as its offset, which points up, and
// each that is an object of no width the default font's missing glyph,
// whose face shaped it.
func (s *Shaper) sizeObjects(output *shaping.Output, start int) {
	sized := false
	for index := range output.Glyphs {
		glyph := &output.Glyphs[index]
		objectIndex := s.runeObjects[start+glyph.ClusterIndex]
		if objectIndex < 0 {
			continue
		}
		object := s.input.Objects[objectIndex]
		var advance fixed.Int26_6
		if object.Width > 0 {
			advance = fixed.Int26_6(object.Width / s.scale)
		} else {
			glyph.GlyphID = 0
			advance = output.FromFontUnit(output.Face.HorizontalAdvance(0))
		}
		// A glyph of no width ending a run is taken for a space.
		glyph.Advance = advance
		glyph.XAdvance = advance
		glyph.Width = advance
		glyph.XOffset = 0
		glyph.YOffset = fixed.Int26_6(object.Raise / s.scale)
		sized = true
	}
	if sized {
		output.RecalculateAll()
	}
}

// placeLine places line, the next line of the paragraph whose runes start
// at the run's rune start, its runs left to right in their visual order,
// with each of its glyphs, and measures it.
func (s *Shaper) placeLine(line shaping.Line, start int) {
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
	s.layout.LineWidths = append(s.layout.LineWidths, float32(x)*s.scale)
}

// placeGlyph places glyph, of glyphFont, shaped of the cluster starting at
// the run's rune character, on the line being placed, its pen at x, in
// shaping units, moved by its offsets, which point up as go-text's do: an
// object of a width is of no font.
func (s *Shaper) placeGlyph(glyphFont *Loaded, glyph shaping.Glyph, x fixed.Int26_6, character int) {
	line := len(s.layout.LineWidths)
	pen := m.Vec2{X: float32(x+glyph.XOffset) * s.scale, Y: s.layout.Baseline + float32(line)*s.lineHeight - float32(glyph.YOffset)*s.scale}
	placed := Glyph{Font: glyphFont, ID: GlyphID(glyph.GlyphID), Rune: character, Pen: pen}
	if objectIndex := s.runeObjects[character]; objectIndex >= 0 && s.input.Objects[objectIndex].Width > 0 {
		placed.Font = nil
		placed.ID = 0
	}
	s.layout.Glyphs = append(s.layout.Glyphs, placed)
	s.lineOfGlyph = append(s.lineOfGlyph, line)
}

// placeClusters places, on the line being placed, the grapheme clusters
// starting in the runes of cluster, a glyph cluster of a paragraph whose
// runes start at the run's rune start, drawn at x, sharing its advance
// evenly, from its right when rightToLeft.
func (s *Shaper) placeClusters(cluster shaping.Glyph, start int, x, advance fixed.Int26_6, rightToLeft bool) {
	line := len(s.layout.LineWidths)
	first, _ := slices.BinarySearch(s.graphemeStarts, start+cluster.ClusterIndex)
	end, _ := slices.BinarySearch(s.graphemeStarts, start+cluster.ClusterIndex+cluster.RuneCount)
	left := float32(x) * s.scale
	width := float32(advance) * s.scale
	for grapheme := first; grapheme < end; grapheme++ {
		offset := width * float32(grapheme-first) / float32(end-first)
		if rightToLeft {
			offset = width - offset
		}
		s.layout.ClusterPositions[grapheme] = m.Vec2{X: left + offset, Y: s.layout.Baseline + float32(line)*s.lineHeight}
		s.lineOfCluster[grapheme] = line
	}
}

// placeLineEnd places cluster, a hard break's or the runes' end, where the
// layout's last line ends.
func (s *Shaper) placeLineEnd(cluster int) {
	line := len(s.layout.LineWidths) - 1
	position := m.Vec2{Y: s.layout.Baseline + float32(line)*s.lineHeight}
	if !s.lineRightToLeft[line] {
		position.X = s.layout.LineWidths[line]
	}
	s.layout.ClusterPositions[cluster] = position
	s.lineOfCluster[cluster] = line
}

// alignLines sizes the layout's box, the input's wrap width or its widest
// line, by its lines' height, and moves each cluster position and each
// glyph by its line's alignment in it.
func (s *Shaper) alignLines() {
	width := s.input.WrapWidth
	if width <= 0 {
		width = slices.Max(s.layout.LineWidths)
	}
	s.layout.Size = m.Vec2{X: width, Y: float32(len(s.layout.LineWidths)) * s.lineHeight}
	s.lineOffsets = s.lineOffsets[:0]
	for line, lineWidth := range s.layout.LineWidths {
		room := width - lineWidth
		var offset float32
		switch {
		case s.input.Align == AlignCenter:
			offset = room / 2
		case (s.input.Align == AlignEnd) != s.lineRightToLeft[line]:
			offset = room
		}
		s.lineOffsets = append(s.lineOffsets, offset)
	}
	for cluster, line := range s.lineOfCluster {
		s.layout.ClusterPositions[cluster].X += s.lineOffsets[line]
	}
	for glyph, line := range s.lineOfGlyph {
		s.layout.Glyphs[glyph].Pen.X += s.lineOffsets[line]
	}
}

// fontFace is a face the shaper made of a font, and the font.
type fontFace struct {
	face *gotext.Face
	font *Loaded
}

// isHardBreak answers whether character forces a line break by Unicode's
// line breaking (UAX #14): a line feed, a carriage return, a next line, a
// line or paragraph separator, a line tabulation or a form feed.
func isHardBreak(character rune) bool {
	switch character {
	case '\n', '\r', '\u0085', ' ', ' ', '\v', '\f':
		return true
	}
	return false
}
