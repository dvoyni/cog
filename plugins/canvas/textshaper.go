package canvas

import (
	"cmp"
	"fmt"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas/font"
)

// iconCharacter is the rune an icon is among a run's runes, the object
// replacement character, though the font package lays an object out
// whatever rune it holds.
const iconCharacter = '￼'

// textShaper lays runs of text out: it holds the font package's shaper,
// which is not safe for concurrent use, and the scratch of the run it lays
// out, so each layout takes one of the FontLibrary resource's for itself.
type textShaper struct {
	shaper font.Shaper
	// spanKey is the scratch a spanned run's layout's key is encoded into
	// as it is looked up, and markupSpans the scratch a run's markup is
	// read into.
	spanKey     []byte
	markupSpans []Span
	// runes are the run's text, its spans' end to end, each icon
	// iconCharacter; runeSpans the span each rune is of; and objects each
	// icon's object.
	runes     []rune
	runeSpans []uint32
	objects   []font.Object
}

// layOut answers the layout of input, its text laid out in its set at its
// em, in world units, shaped in its language: each character drawn by the
// first of the language's fonts that has it, else by the default font,
// and its lines measured by the font measuring the language, as
// FontLibrary.Metrics reads. A spanned input's icons are those registered
// in icons under their names; a name none is registered under is reported
// once, as is, once for the set, a character no font of it has.
func (s *textShaper) layOut(input layoutInput, icons map[string]registeredIcon) TextLayout {
	measuringFont := input.set.fonts.LanguageFont(input.language)
	layout := TextLayout{em: input.em, fontFilter: FilterLinear}
	if measuringFont.Font().BitmapEm > 0 {
		layout.fontFilter = FilterNearest
	}
	s.runes = s.runes[:0]
	s.runeSpans = s.runeSpans[:0]
	s.objects = s.objects[:0]
	if input.spanned {
		capHeight := measuringFont.Metrics(input.em).CapHeight
		for span := range decodeSpans(input.text) {
			spanIndex := uint32(len(layout.spanIcons))
			var icon *placedIcon
			if span.icon {
				icon = placeIcon(span.content, icons, capHeight, input.set.reporter)
				object := font.Object{Rune: len(s.runes)}
				if icon != nil {
					object.Width = icon.size.X
					object.Raise = icon.offset * input.em
				}
				s.objects = append(s.objects, object)
				s.appendRune(iconCharacter, spanIndex)
			} else {
				for _, character := range span.content {
					s.appendRune(character, spanIndex)
				}
			}
			layout.spanIcons = append(layout.spanIcons, icon)
		}
	} else {
		for _, character := range input.text {
			s.appendRune(character, 0)
		}
	}
	layout.layout = s.shaper.LayOut(font.Input{
		Set:        input.set.fonts,
		Runes:      s.runes,
		Objects:    s.objects,
		Language:   input.language,
		Em:         input.em,
		WrapWidth:  input.wrapWidth,
		LineHeight: input.lineHeight,
		Tracking:   input.tracking,
		Align:      font.Align(input.align),
	})
	if missing := layout.layout.MissingRunes; len(missing) > 0 {
		input.set.reportCharacterMissing(input.language, s.runes[missing[0]])
	}
	// The layout's glyphs are kept with their spans, leaving its own.
	layout.glyphs = make([]placedGlyph, len(layout.layout.Glyphs))
	for index, glyph := range layout.layout.Glyphs {
		layout.glyphs[index] = placedGlyph{font: glyph.Font, id: glyph.ID, span: s.runeSpans[glyph.Rune], pen: glyph.Pen}
	}
	layout.layout.Glyphs = nil
	return layout
}

// appendRune appends character to the run's runes, of the span at
// spanIndex.
func (s *textShaper) appendRune(character rune, spanIndex uint32) {
	s.runes = append(s.runes, character)
	s.runeSpans = append(s.runeSpans, spanIndex)
}

// placeIcon answers the icon registered in icons under name, placed at an
// em whose font measuring the run's language has a cap height of
// capHeight: its Scale, zero meaning 1, times that cap height tall, its
// width by its frame's aspect, raised by its Offset, in em. A name none is
// registered under answers nil, reported once for the name through
// reporter.
func placeIcon(name string, icons map[string]registeredIcon, capHeight float32, reporter *reporter) *placedIcon {
	registered, found := icons[name]
	if !found {
		missing := Report{Kind: ReportKindIconNotRegistered, Err: fmt.Errorf("canvas: a run of text names icon %q, which no icon is registered under, so it is drawn as the default font's missing-glyph box", name)}
		reporter.reportOnce(iconNotRegisteredLogKey+name, missing)
		return nil
	}
	icon := registered.descriptor
	height := capHeight * cmp.Or(icon.Scale, 1)
	size := m.Vec2{X: height * registered.frameSize.X / registered.frameSize.Y, Y: height}
	return &placedIcon{image: icon.Image, frame: icon.Frame, size: size, offset: icon.Offset, tinted: icon.Tinted}
}
