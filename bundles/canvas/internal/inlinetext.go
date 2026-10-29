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
	// Language is the language a $[language]{...} span set the run in, which
	// picks the fallback font its characters are looked up in first. It is
	// empty outside a span, and on an icon.
	Language string
}

// ParseInlineText splits text into lines of literal/icon segments. It recognizes
// ${path} icon tokens, $[language]{text} spans and backslash escaping: "\${"
// yields a literal "${" and "\\" yields a literal "\". Unterminated "${" (no
// closing brace before end of line) stays literal; an empty "${}" token is
// reported by callers and omitted. Newlines split lines so measurement and
// drawing stay in lockstep.
//
// A span's text is literal up to its first unescaped "}": it holds no icons and
// no nested span, and "\}" puts a brace inside one. A span that is malformed -
// an empty or unterminated language, no "{" straight after the "]", no closing
// brace before end of line - stays literal, as an unterminated icon does.
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
		case c == '$' && i+1 < len(text) && text[i+1] == '[':
			language, content, next, ok := parseLanguageSpan(text, i)
			if !ok {
				literal.WriteByte(c)
				i++
				continue
			}
			flushLiteral()
			if content != "" {
				current = append(current, InlineSegment{Text: content, Language: language})
			}
			i = next
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

// parseLanguageSpan reads the $[language]{text} span starting at text[start],
// returning its language, its text with escapes resolved, and the index just
// past it. ok is false when the span is malformed.
func parseLanguageSpan(text string, start int) (language, content string, next int, ok bool) {
	i := start + 2
	for i < len(text) && text[i] != ']' {
		if !languageByte(text[i]) {
			return "", "", 0, false
		}
		i++
	}
	if i == start+2 || i+1 >= len(text) || text[i+1] != '{' {
		return "", "", 0, false
	}
	language = text[start+2 : i]
	var body strings.Builder
	for j := i + 2; j < len(text); j++ {
		switch c := text[j]; {
		case c == '\\' && j+1 < len(text) && text[j+1] != '\n':
			body.WriteByte(text[j+1])
			j++
		case c == '}':
			return language, body.String(), j + 1, true
		case c == '\n':
			return "", "", 0, false
		default:
			body.WriteByte(c)
		}
	}
	return "", "", 0, false
}

// languageByte reports whether c may appear in a span's language name, which is
// a tag such as "ja" or "zh-Hans".
func languageByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
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
			word = appendInlineText(word, segment.Text[start:index], segment.Language)
			if len(word) > 0 {
				words = append(words, word)
				word = nil
			}
			start = index + len(string(character))
		}
		word = appendInlineText(word, segment.Text[start:], segment.Language)
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
			units = append(units, []InlineSegment{{Text: string(character), Language: segment.Language}})
		}
	}
	return units
}

func appendInlineWord(line, word []InlineSegment, space bool) []InlineSegment {
	if space {
		line = appendInlineText(line, " ", "")
	}
	return appendInlineSegments(line, word)
}

func appendInlineSegments(dst, source []InlineSegment) []InlineSegment {
	for _, segment := range source {
		if segment.Icon {
			dst = append(dst, segment)
			continue
		}
		dst = appendInlineText(dst, segment.Text, segment.Language)
	}
	return dst
}

// appendInlineText appends a literal run, joining it onto the last segment when
// that is a run in the same language, so a line holds one segment per stretch
// of text that draws alike.
func appendInlineText(segments []InlineSegment, text, language string) []InlineSegment {
	if text == "" {
		return segments
	}
	if last := len(segments) - 1; last >= 0 && !segments[last].Icon && segments[last].Language == language {
		segments[last].Text += text
		return segments
	}
	return append(segments, InlineSegment{Text: text, Language: language})
}

func cloneInlineSegments(segments []InlineSegment) []InlineSegment {
	return append([]InlineSegment(nil), segments...)
}

func ValidWrapWidth(width float32) bool {
	return width > 0 && !math.IsNaN(float64(width)) && !math.IsInf(float64(width), 0)
}
