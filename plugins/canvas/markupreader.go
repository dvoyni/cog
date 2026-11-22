package canvas

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dvoyni/cog/libs/m"
)

// markupFault is where and why a run's markup does not parse, its problem
// empty when it parses: a value, so a run whose markup does not parse,
// read each frame, makes no error until it is reported.
type markupFault struct {
	offset  int
	problem string
}

// appendMarkupSpans appends to spans the spans markup, a run's String
// whose Markup is set, reads as, a colour's name one registered in colors,
// and answers them: `\i:name` an icon, `\c:colour{text}` a span of text in
// colour, `#rgb`, `#rrggbb`, `#rrggbbaa` or a name, its braces counted, and
// `\\`, `\{` and `\}` a backslash and lone braces. Spans hold markup's own
// substrings, so reading it allocates nothing once spans has room. For
// markup that does not parse, it answers spans as given, with where and
// why it does not.
func appendMarkupSpans(spans []Span, markup string, colors map[string]m.Color) ([]Span, markupFault) {
	reader := markupReader{markup: markup, colors: colors, spans: spans}
	if fault := reader.readSpans(m.Color{}, false); fault.problem != "" {
		return spans, fault
	}
	return reader.spans, markupFault{}
}

// markupReader reads a run's markup into spans, from its offset on.
type markupReader struct {
	markup string
	colors map[string]m.Color
	spans  []Span
	offset int
}

// readSpans reads the markup's spans of text, its icons and its spans
// within, in tint, to its end or, inside a span, to the brace closing it,
// which it reads past, the braces between counted.
func (r *markupReader) readSpans(tint m.Color, insideSpan bool) markupFault {
	openBraceCount := 0
	start := r.offset
	for r.offset < len(r.markup) {
		switch r.markup[r.offset] {
		case '\\':
			r.appendText(start, tint)
			if fault := r.readEscape(tint); fault.problem != "" {
				return fault
			}
			start = r.offset
			continue
		case '{':
			openBraceCount++
		case '}':
			if insideSpan && openBraceCount == 0 {
				r.appendText(start, tint)
				r.offset++
				return markupFault{}
			}
			openBraceCount = max(openBraceCount-1, 0)
		}
		r.offset++
	}
	if insideSpan {
		return markupFault{offset: r.offset, problem: "a span is left open"}
	}
	r.appendText(start, tint)
	return markupFault{}
}

// readEscape reads the escape at the reader's offset, a backslash and what
// follows it, in tint: an icon, a span of a colour, or a backslash or a
// brace written as itself.
func (r *markupReader) readEscape(tint m.Color) markupFault {
	escape := r.offset
	if escape+1 == len(r.markup) {
		return markupFault{offset: escape, problem: "a backslash ends it"}
	}
	switch r.markup[escape+1] {
	case '\\', '{', '}':
		r.spans = append(r.spans, Span{Text: r.markup[escape+1 : escape+2], Tint: tint})
		r.offset = escape + 2
		return markupFault{}
	case 'i', 'c':
		r.offset = escape + 2
		if r.offset == len(r.markup) || r.markup[r.offset] != ':' {
			return markupFault{offset: r.offset, problem: "a colon does not follow \\i or \\c"}
		}
		r.offset++
		if r.markup[escape+1] == 'c' {
			return r.readColorSpan()
		}
		name, fault := r.readName("an icon's")
		if fault.problem == "" {
			r.spans = append(r.spans, Span{Icon: name, Tint: tint})
		}
		return fault
	}
	return markupFault{offset: escape, problem: "a backslash is followed by none of \\, {, }, i: and c:"}
}

// readColorSpan reads a span of a colour after its colon: the colour, its
// opening brace, and the spans within, in the colour.
func (r *markupReader) readColorSpan() markupFault {
	color, fault := r.readColor()
	if fault.problem != "" {
		return fault
	}
	if r.offset == len(r.markup) || r.markup[r.offset] != '{' {
		return markupFault{offset: r.offset, problem: "a colour is not followed by {"}
	}
	r.offset++
	return r.readSpans(color, true)
}

// readName reads a name, of letters, digits, _, - and /, the name of what,
// an icon's or a colour's, says.
func (r *markupReader) readName(what string) (string, markupFault) {
	start := r.offset
	for r.offset < len(r.markup) {
		character, size := utf8.DecodeRuneInString(r.markup[r.offset:])
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != '_' && character != '-' && character != '/' {
			break
		}
		r.offset += size
	}
	if r.offset == start {
		return "", markupFault{offset: start, problem: what + " name is empty"}
	}
	return r.markup[start:r.offset], markupFault{}
}

// readColor reads a span's colour after its colon, `#rgb`, `#rrggbb` or
// `#rrggbbaa`, in sRGB of straight alpha, or a name registered in the
// reader's colours.
func (r *markupReader) readColor() (m.Color, markupFault) {
	if r.offset < len(r.markup) && r.markup[r.offset] == '#' {
		r.offset++
		start := r.offset
		for r.offset < len(r.markup) && strings.IndexByte("0123456789abcdefABCDEF", r.markup[r.offset]) >= 0 {
			r.offset++
		}
		color, parsed := parseHexColor(r.markup[start:r.offset])
		if !parsed {
			return m.Color{}, markupFault{offset: start, problem: "a colour's # is followed by other than 3, 6 or 8 hexadecimal digits"}
		}
		return color, markupFault{}
	}
	start := r.offset
	name, fault := r.readName("a colour's")
	if fault.problem != "" {
		return m.Color{}, fault
	}
	color, registered := r.colors[name]
	if !registered {
		return m.Color{}, markupFault{offset: start, problem: "a colour's name is not registered by WriteFontLibrary.RegisterColor"}
	}
	return color, markupFault{}
}

// appendText appends the markup's text from start to the reader's offset,
// in tint, as a span, unless it is empty.
func (r *markupReader) appendText(start int, tint m.Color) {
	if r.offset > start {
		r.spans = append(r.spans, Span{Text: r.markup[start:r.offset], Tint: tint})
	}
}

// parseHexColor answers digits, 3, 6 or 8 hexadecimal digits of red, green,
// blue and alpha, each one digit or two, alpha opaque unless given, as an
// sRGB colour, and whether they are that.
func parseHexColor(digits string) (m.Color, bool) {
	value, err := strconv.ParseUint(digits, 16, 32)
	if err != nil {
		return m.Color{}, false
	}
	switch len(digits) {
	case 3:
		return m.NewColorSrgb8(uint8(value>>8&0xf)*17, uint8(value>>4&0xf)*17, uint8(value&0xf)*17, 255), true
	case 6:
		return m.NewColorSrgb8(uint8(value>>16), uint8(value>>8), uint8(value), 255), true
	case 8:
		return m.NewColorSrgb8(uint8(value>>24), uint8(value>>16), uint8(value>>8), uint8(value)), true
	}
	return m.Color{}, false
}
