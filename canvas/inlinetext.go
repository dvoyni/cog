package canvas

import (
	"math"
	"strings"
	"unicode"
)

// inlineSegment is one piece of a parsed text line: either a literal text run or
// an inline icon referenced by ${path}.
type inlineSegment struct {
	icon bool
	// text is the literal run (with escapes resolved) or the icon path.
	text string
}

// parseInlineText splits text into lines of literal/icon segments. It recognizes
// ${path} icon tokens and backslash escaping: "\${" yields a literal "${" and
// "\\" yields a literal "\". Unterminated "${" (no closing brace before end of
// line) stays literal; an empty "${}" token is reported by callers and omitted.
// Newlines split lines so measurement and drawing stay in lockstep.
func parseInlineText(text string) [][]inlineSegment {
	var lines [][]inlineSegment
	var current []inlineSegment
	var literal strings.Builder

	flushLiteral := func() {
		if literal.Len() > 0 {
			current = append(current, inlineSegment{text: literal.String()})
			literal.Reset()
		}
	}
	endLine := func() {
		flushLiteral()
		lines = append(lines, current)
		current = nil
	}

	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == '\\' && i+1 < len(text):
			literal.WriteByte(text[i+1])
			i += 2
		case c == '\n':
			endLine()
			i++
		case c == '$' && i+1 < len(text) && text[i+1] == '{':
			closeIndex := -1
			for j := i + 2; j < len(text); j++ {
				if text[j] == '\n' {
					break
				}
				if text[j] == '}' {
					closeIndex = j
					break
				}
			}
			if closeIndex < 0 {
				// Unterminated token: keep the "${" literal and continue scanning.
				literal.WriteByte(c)
				i++
				continue
			}
			flushLiteral()
			current = append(current, inlineSegment{icon: true, text: text[i+2 : closeIndex]})
			i = closeIndex + 1
		default:
			literal.WriteByte(c)
			i++
		}
	}
	endLine()
	return lines
}

func wrapInlineText(lines [][]inlineSegment, width float32, measure func([]inlineSegment) float32) [][]inlineSegment {
	if !validWrapWidth(width) {
		return lines
	}
	wrapped := make([][]inlineSegment, 0, len(lines))
	for _, source := range lines {
		words := inlineWords(source)
		if len(words) == 0 {
			wrapped = append(wrapped, nil)
			continue
		}
		var line []inlineSegment
		for _, word := range words {
			candidate := appendInlineWord(cloneInlineSegments(line), word, len(line) > 0)
			if measure(candidate) <= width {
				line = candidate
				continue
			}
			if len(line) > 0 {
				wrapped = append(wrapped, line)
				line = nil
			}
			if measure(word) <= width {
				line = appendInlineSegments(line, word)
				continue
			}
			for _, unit := range inlineUnits(word) {
				candidate = appendInlineSegments(cloneInlineSegments(line), unit)
				if len(line) > 0 && measure(candidate) > width {
					wrapped = append(wrapped, line)
					line = nil
				}
				line = appendInlineSegments(line, unit)
			}
		}
		wrapped = append(wrapped, line)
	}
	return wrapped
}

func inlineWords(segments []inlineSegment) [][]inlineSegment {
	var words [][]inlineSegment
	var word []inlineSegment
	for _, segment := range segments {
		if segment.icon {
			word = appendInlineSegments(word, []inlineSegment{segment})
			continue
		}
		start := 0
		for index, character := range segment.text {
			if !unicode.IsSpace(character) {
				continue
			}
			word = appendInlineText(word, segment.text[start:index])
			if len(word) > 0 {
				words = append(words, word)
				word = nil
			}
			start = index + len(string(character))
		}
		word = appendInlineText(word, segment.text[start:])
	}
	if len(word) > 0 {
		words = append(words, word)
	}
	return words
}

func inlineUnits(segments []inlineSegment) [][]inlineSegment {
	units := make([][]inlineSegment, 0, len(segments))
	for _, segment := range segments {
		if segment.icon {
			units = append(units, []inlineSegment{segment})
			continue
		}
		for _, character := range segment.text {
			units = append(units, []inlineSegment{{text: string(character)}})
		}
	}
	return units
}

func appendInlineWord(line, word []inlineSegment, space bool) []inlineSegment {
	if space {
		line = appendInlineText(line, " ")
	}
	return appendInlineSegments(line, word)
}

func appendInlineSegments(dst, source []inlineSegment) []inlineSegment {
	for _, segment := range source {
		if segment.icon {
			dst = append(dst, segment)
			continue
		}
		dst = appendInlineText(dst, segment.text)
	}
	return dst
}

func appendInlineText(segments []inlineSegment, text string) []inlineSegment {
	if text == "" {
		return segments
	}
	if len(segments) > 0 && !segments[len(segments)-1].icon {
		segments[len(segments)-1].text += text
		return segments
	}
	return append(segments, inlineSegment{text: text})
}

func cloneInlineSegments(segments []inlineSegment) []inlineSegment {
	return append([]inlineSegment(nil), segments...)
}

func validWrapWidth(width float32) bool {
	return width > 0 && !math.IsNaN(float64(width)) && !math.IsInf(float64(width), 0)
}
