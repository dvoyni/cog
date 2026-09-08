package gfx

import "strings"

// directiveKind is one of the preprocessor's seven keywords, or none.
//
// The set is closed and complete: both sigils are reserved at line start, so a
// keyword the preprocessor did not recognise is an error rather than text
// handed to the backend, and the table below is what "recognised" means.
type directiveKind int

const (
	dirNone directiveKind = iota
	dirInclude
	dirDefine
	dirConst
	dirIf
	dirElif
	dirElse
	dirEndif
)

var directiveKeywords = map[string]directiveKind{
	"include": dirInclude,
	"define":  dirDefine,
	"const":   dirConst,
	"if":      dirIf,
	"elif":    dirElif,
	"else":    dirElse,
	"endif":   dirEndif,
}

func (k directiveKind) String() string {
	for keyword, kind := range directiveKeywords {
		if kind == k {
			return "#" + keyword
		}
	}
	return "#?"
}

// directiveMisspellings maps the C, GLSL and naga_oil habits onto the spelling
// this language uses. A fixed table with no fuzzy matching and no edit
// distance: it is the cheapest diagnostic on the list, and #ifdef is what a C
// habit actually reaches for.
var directiveMisspellings = map[string]string{
	"ifdef":  "this language spells it `#if NAME`",
	"ifndef": "this language spells it `#if !NAME`",
	"elseif": "this language spells it `#elif`",
	"end":    "this language spells it `#endif`",
	"undef":  "this language has no equivalent",
	"import": "this language spells it `#include`",
}

// directive is one line's parsed preprocessor directive.
type directive struct {
	kind    directiveKind
	keyword string // as written, for an unknown word
	arg     string // everything after the keyword, whitespace-trimmed
	known   bool
	sigil   bool // a line-start # or //#, whether or not the keyword is known
}

// parseDirective recognises a line-start directive on an already comment-blanked
// line.
//
// Both sigils mean the same thing, and both are reserved here. Away from line
// start "//" opens an ordinary comment whatever follows it, so `let x = 1; //#todo`
// is a comment; at line start a sigil not followed immediately by a known
// keyword is an error naming the word, because `#` is untokenizable in WGSL and
// a directive that survives into the backend dies in the lexer with no recovery,
// in text nobody wrote.
//
// The line is expected to have had its comments blanked already, which is what
// makes `/* c */ #if X` a live directive: position is measured after blanking.
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
	kind, known := directiveKeywords[keyword]
	return directive{
		kind:    kind,
		keyword: keyword,
		arg:     strings.TrimSpace(rest[end:]),
		known:   known,
		sigil:   true,
	}
}

func isKeywordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// unknownDirectiveMessage names the word a reserved sigil was followed by, and
// the spelling this language uses when the word is a known habit from another
// preprocessor.
func unknownDirectiveMessage(keyword string) string {
	if keyword == "" {
		return "unknown directive: a keyword must follow the sigil immediately, with no space"
	}
	message := "unknown directive `#" + keyword + "`"
	if advice, ok := directiveMisspellings[keyword]; ok {
		return message + "; " + advice
	}
	return message
}

// blankComments replaces every comment in a source with spaces, line by line,
// and returns the source's lines without their terminators.
//
// Comments are blanked rather than deleted so that every downstream line number
// and column stays accurate: a WGSL error in the surviving text lands on a real
// line. One left-to-right pass, and the first opener wins - `/* … */` is always
// a comment and is never unwrapped, including around a line-start `#if`, which
// makes a block comment the reliable way to disable a region wholesale. Block
// comments nest, as WGSL §2.3 says they do, which is what lets a wholesale
// disable contain comments of its own.
//
// `//` opens a comment unless it is `//#` at the first non-whitespace of the
// line, measured against the text blanked so far. WGSL has no string literals at
// all, so this scan is exact and needs no lexer - the difference from C, where
// "/*" inside a string defeats a naive scanner.
func blankComments(text string) []string {
	lines := splitLines(text)
	depth := 0
	for i, line := range lines {
		out := []byte(line)
		for j := 0; j < len(out); {
			if depth > 0 {
				if j+1 < len(out) && out[j] == '*' && out[j+1] == '/' {
					depth--
					blankBytes(out[j : j+2])
					j += 2
					continue
				}
				if j+1 < len(out) && out[j] == '/' && out[j+1] == '*' {
					depth++
					blankBytes(out[j : j+2])
					j += 2
					continue
				}
				blankBytes(out[j : j+1])
				j++
				continue
			}
			if j+1 < len(out) && out[j] == '/' && out[j+1] == '*' {
				depth++
				blankBytes(out[j : j+2])
				j += 2
				continue
			}
			if j+1 < len(out) && out[j] == '/' && out[j+1] == '/' {
				if isDirectiveSigil(out, j) {
					break
				}
				blankBytes(out[j:])
				break
			}
			j++
		}
		lines[i] = string(out)
	}
	return lines
}

// isDirectiveSigil reports whether the "//" at j opens a directive rather than a
// comment: it must be "//#", and it must be the line's first non-whitespace.
func isDirectiveSigil(line []byte, j int) bool {
	if j+2 >= len(line) || line[j+2] != '#' {
		return false
	}
	for _, b := range line[:j] {
		if b != ' ' && b != '\t' {
			return false
		}
	}
	return true
}

func blankBytes(b []byte) {
	for i := range b {
		if b[i] != '\t' {
			b[i] = ' '
		}
	}
}

// blankLine renders one line as the spaces it occupied, which is what a consumed
// directive and a skipped conditional branch leave behind. The line survives so
// that every line after it keeps its number.
func blankLine(line string) string {
	out := []byte(line)
	blankBytes(out)
	return string(out)
}

// splitLines splits a source into lines, dropping the terminator and any \r
// before it, so that a CRLF source parses and flattens exactly like an LF one.
func splitLines(text string) []string {
	lines := strings.Split(text, "\n")
	if n := len(lines); n > 1 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	return lines
}
