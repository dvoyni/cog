package shader

import "strings"

// directiveKind is one of the preprocessor's seven keywords, or none.
//
// The set is closed: both sigils are reserved at the start of a line, so a
// keyword not in it is an error rather than text handed to the backend.
type directiveKind int

const (
	directiveNone directiveKind = iota
	directiveInclude
	directiveDefine
	directiveConst
	directiveIf
	directiveElif
	directiveElse
	directiveEndif
)

// directiveKeywords spells each kind, indexed by it.
var directiveKeywords = [...]string{
	directiveNone:    "?",
	directiveInclude: "include",
	directiveDefine:  "define",
	directiveConst:   "const",
	directiveIf:      "if",
	directiveElif:    "elif",
	directiveElse:    "else",
	directiveEndif:   "endif",
}

// directiveKinds is directiveKeywords the other way round: the kind each
// keyword names.
var directiveKinds = func() map[string]directiveKind {
	kinds := make(map[string]directiveKind, len(directiveKeywords))
	for kind := directiveInclude; int(kind) < len(directiveKeywords); kind++ {
		kinds[directiveKeywords[kind]] = kind
	}
	return kinds
}()

// String answers the kind as written, with its sigil: "#include".
func (k directiveKind) String() string {
	if k <= directiveNone || int(k) >= len(directiveKeywords) {
		k = directiveNone
	}
	return "#" + directiveKeywords[k]
}

// directive is one line's parsed preprocessor directive.
type directive struct {
	kind     directiveKind
	keyword  string // as written, for an unknown word
	argument string // everything after the keyword, trimmed
	known    bool
	sigil    bool // a line-start # or //#, whether or not the keyword is known
}

// parseDirective reads a line-start directive from a line whose comments are
// already blanked, which is what makes `/* c */ #if X` a live directive.
//
// Both sigils mean the same, and both are reserved there. Away from the start
// of a line "//" opens an ordinary comment whatever follows, so
// `let x = 1; //#todo` is a comment; at its start a sigil not followed at once
// by a known keyword is an error naming the word, because `#` is not a WGSL
// token and a directive reaching the backend dies in its lexer, in text
// nobody wrote.
func parseDirective(line string) directive {
	rest := strings.TrimLeft(line, " \t")
	switch {
	case strings.HasPrefix(rest, "//#"):
		rest = rest[3:]
	case strings.HasPrefix(rest, "#"):
		rest = rest[1:]
	default:
		return directive{}
	}
	end := 0
	for end < len(rest) && isKeywordByte(rest[end]) {
		end++
	}
	keyword := rest[:end]
	kind, known := directiveKinds[keyword]
	return directive{
		kind:     kind,
		keyword:  keyword,
		argument: strings.TrimSpace(rest[end:]),
		known:    known,
		sigil:    true,
	}
}

// describeUnknownDirective names the word a reserved sigil was followed by.
func describeUnknownDirective(keyword string) string {
	if keyword == "" {
		return "unknown directive: a keyword must follow the sigil immediately, with no space"
	}
	return "unknown directive `#" + keyword + "`"
}

// blankComments replaces every comment in a source with spaces, and answers
// the source's lines without their terminators.
//
// Comments are blanked rather than removed so every line and column after
// them stays where it was. One pass, left to right, the first opener winning:
// `/* … */` is always a comment, around a line-start `#if` too, which makes a
// block comment the reliable way to disable a region. Block comments nest, as
// WGSL's do.
//
// `//` opens a comment unless it is `//#` at the line's first non-whitespace,
// measured on the text blanked so far. WGSL has no string literals, so the
// scan is exact without a lexer.
func blankComments(text string) []string {
	lines := splitLines(text)
	depth := 0
	for index, line := range lines {
		blanked := []byte(line)
		for at := 0; at < len(blanked); {
			opens := at+1 < len(blanked) && blanked[at] == '/' && blanked[at+1] == '*'
			if depth > 0 {
				closes := at+1 < len(blanked) && blanked[at] == '*' && blanked[at+1] == '/'
				switch {
				case closes:
					depth--
					blankBytes(blanked[at : at+2])
					at += 2
				case opens:
					depth++
					blankBytes(blanked[at : at+2])
					at += 2
				default:
					blankBytes(blanked[at : at+1])
					at++
				}
				continue
			}
			if opens {
				depth++
				blankBytes(blanked[at : at+2])
				at += 2
				continue
			}
			if at+1 < len(blanked) && blanked[at] == '/' && blanked[at+1] == '/' {
				if !isDirectiveSigil(blanked, at) {
					blankBytes(blanked[at:])
				}
				break
			}
			at++
		}
		lines[index] = string(blanked)
	}
	return lines
}

// blankLine answers a line as the spaces it took up, which is what a consumed
// directive and a skipped branch leave behind, so every line after it keeps
// its number.
func blankLine(line string) string {
	blanked := []byte(line)
	blankBytes(blanked)
	return string(blanked)
}

// splitLines splits a source into lines, dropping each terminator and any \r
// before it, so a CRLF source preprocesses as an LF one does.
func splitLines(text string) []string {
	lines := strings.Split(text, "\n")
	if count := len(lines); count > 1 && lines[count-1] == "" {
		lines = lines[:count-1]
	}
	for index, line := range lines {
		lines[index] = strings.TrimSuffix(line, "\r")
	}
	return lines
}

// isDirectiveSigil reports whether the "//" at index opens a directive rather
// than a comment: it is "//#", and the line's first non-whitespace.
func isDirectiveSigil(line []byte, index int) bool {
	if index+2 >= len(line) || line[index+2] != '#' {
		return false
	}
	for _, character := range line[:index] {
		if character != ' ' && character != '\t' {
			return false
		}
	}
	return true
}

// blankBytes turns every byte but a tab into a space.
func blankBytes(text []byte) {
	for index := range text {
		if text[index] != '\t' {
			text[index] = ' '
		}
	}
}

func isKeywordByte(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
}

func isNameByte(character byte) bool {
	return isKeywordByte(character) || character >= '0' && character <= '9' || character == '_'
}

// isName reports whether text is a non-empty run of name bytes.
func isName(text string) bool {
	if text == "" {
		return false
	}
	for index := range len(text) {
		if !isNameByte(text[index]) {
			return false
		}
	}
	return true
}
