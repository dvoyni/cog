package internal

import (
	"math"
	"strings"
	"unicode"
)

// InlineSegment is one piece of a parsed text line: either a literal text run or
// an inline icon referenced by ${path}.
type InlineSegment struct {
	Icon bool
	// Text is the literal run (with escapes resolved) or the icon path.
	Text string
}

// ParseInlineText splits text into lines of literal/icon segments. It recognizes
// ${path} icon tokens and backslash escaping: "\${" yields a literal "${" and
// "\\" yields a literal "\". Unterminated "${" (no closing brace before end of
// line) stays literal; an empty "${}" token is reported by callers and omitted.
// Newlines split lines so measurement and drawing stay in lockstep.
func ParseInlineText(text string) [][]InlineSegment {
	var lines [][]InlineSegment
	var current []InlineSegment
	var literal strings.Builder

	flushLiteral := func() {
		if literal.Len() > 0 {
			current = append(current, InlineSegment{Text: literal.String()})
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
			current = append(current, InlineSegment{Icon: true, Text: text[i+2 : closeIndex]})
			i = closeIndex + 1
		default:
			literal.WriteByte(c)
			i++
		}
	}
	endLine()
	return lines
}

func WrapInlineText(lines [][]InlineSegment, width float32, measure func([]InlineSegment) float32) [][]InlineSegment {
	if !ValidWrapWidth(width) {
		return lines
	}
	wrapped := make([][]InlineSegment, 0, len(lines))
	for _, source := range lines {
		words := inlineWords(source)
		if len(words) == 0 {
			wrapped = append(wrapped, nil)
			continue
		}
		var line []InlineSegment
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

func inlineWords(segments []InlineSegment) [][]InlineSegment {
	var words [][]InlineSegment
	var word []InlineSegment
	for _, segment := range segments {
		if segment.Icon {
			word = appendInlineSegments(word, []InlineSegment{segment})
			continue
		}
		start := 0
		for index, character := range segment.Text {
			if !unicode.IsSpace(character) {
				continue
			}
			word = appendInlineText(word, segment.Text[start:index])
			if len(word) > 0 {
				words = append(words, word)
				word = nil
			}
			start = index + len(string(character))
		}
		word = appendInlineText(word, segment.Text[start:])
	}
	if len(word) > 0 {
		words = append(words, word)
	}
	return words
}

func inlineUnits(segments []InlineSegment) [][]InlineSegment {
	units := make([][]InlineSegment, 0, len(segments))
	for _, segment := range segments {
		if segment.Icon {
			units = append(units, []InlineSegment{segment})
			continue
		}
		for _, character := range segment.Text {
			units = append(units, []InlineSegment{{Text: string(character)}})
		}
	}
	return units
}

func appendInlineWord(line, word []InlineSegment, space bool) []InlineSegment {
	if space {
		line = appendInlineText(line, " ")
	}
	return appendInlineSegments(line, word)
}

func appendInlineSegments(dst, source []InlineSegment) []InlineSegment {
	for _, segment := range source {
		if segment.Icon {
			dst = append(dst, segment)
			continue
		}
		dst = appendInlineText(dst, segment.Text)
	}
	return dst
}

func appendInlineText(segments []InlineSegment, text string) []InlineSegment {
	if text == "" {
		return segments
	}
	if len(segments) > 0 && !segments[len(segments)-1].Icon {
		segments[len(segments)-1].Text += text
		return segments
	}
	return append(segments, InlineSegment{Text: text})
}

func cloneInlineSegments(segments []InlineSegment) []InlineSegment {
	return append([]InlineSegment(nil), segments...)
}

func ValidWrapWidth(width float32) bool {
	return width > 0 && !math.IsNaN(float64(width)) && !math.IsInf(float64(width), 0)
}
