package gfx

import (
	"fmt"
	"slices"
	"strings"
)

// WGSL §4 requires enable, requires and diagnostic to precede all declarations
// in a module. Flattening breaks that by construction, so the preprocessor owns
// the repair: it hoists them to the top of the flattened module.
//
// Forbidding them outside the root source lost on rule count. Forbidding needs
// two rules - no directives in an included source, and no #const above a
// directive - and still leaves the transitive case unaddressed: a root includes
// A which includes B, B needs f16, and the root must somehow know. Hoisting
// needs zero rules, and it keeps the //# standalone-validity promise intact,
// which forbidding would have voided for any source needing an extension.
//
// This is the one exception to "no parsing WGSL", and it is bounded: three fixed
// keywords, line-start position, prologue only, and the line body opaque apart
// from a diagnostic line's rule name.
var hoistedKeywords = []string{"enable", "requires", "diagnostic"}

// hoistPrologue collects one source's §4 directives and reports which of its
// lines they occupied, so that emitSource can blank them at their original site.
//
// They are recognised only in the source's prologue - the run of blank lines,
// comments and preprocessor directive lines at the top, ending at the first line
// of real WGSL content. That is WGSL's own placement rule restated, so it needs
// no grammar, and it draws a clean line: a source that is valid standalone WGSL
// keeps its directives in its own prologue by definition. A directive sitting
// after real content in its own source is not recognised, not hoisted, and left
// to the backend - that source was already invalid standalone WGSL, and the
// preprocessor does not rescue it.
func (f *flattener) hoistPrologue(name string, lines []string, scan []lineScan) (map[int]bool, error) {
	hoisted := map[int]bool{}
	for i := 0; i < len(lines); {
		// A #const line does not end the prologue: it is a preprocessor
		// directive, not WGSL content, at the time the prologue is scanned.
		if strings.TrimSpace(lines[i]) == "" || scan[i].kind != dirNone {
			i++
			continue
		}
		keyword, rest := leadingWord(lines[i])
		if !slices.Contains(hoistedKeywords, keyword) {
			return hoisted, nil
		}
		body, end, ok := readToSemicolon(rest, lines, i)
		if !ok {
			return hoisted, nil
		}
		at := ShaderLocation{Source: name, Line: i + 1}
		if err := f.collectDirective(keyword, body, at); err != nil {
			return nil, err
		}
		for line := i; line <= end; line++ {
			hoisted[line] = true
		}
		i = end + 1
	}
	return hoisted, nil
}

// leadingWord splits a line's first bare word from the rest of it. A WGSL
// identifier that merely starts with a keyword - enableThing - yields the whole
// identifier and so matches nothing, and @diagnostic(...) as a statement or
// function attribute never matches because it starts with "@".
func leadingWord(line string) (word, rest string) {
	trimmed := strings.TrimLeft(line, " \t")
	end := 0
	for end < len(trimmed) && isNameByte(trimmed[end]) {
		end++
	}
	return trimmed[:end], trimmed[end:]
}

// readToSemicolon reads a §4 directive's body from rest, continuing onto later
// lines until the terminating ";". It declines the line if nothing follows the
// ";", so a line carrying real code after a directive is left to the backend
// rather than silently swallowed with it.
func readToSemicolon(rest string, lines []string, start int) (body string, end int, ok bool) {
	var b strings.Builder
	for i := start; i < len(lines); i++ {
		text := rest
		if i > start {
			text = lines[i]
		}
		semicolon := strings.IndexByte(text, ';')
		if semicolon < 0 {
			b.WriteString(text)
			b.WriteByte(' ')
			continue
		}
		if strings.TrimSpace(text[semicolon+1:]) != "" {
			return "", 0, false
		}
		b.WriteString(text[:semicolon])
		return strings.TrimSpace(b.String()), i, true
	}
	return "", 0, false
}

func (f *flattener) collectDirective(keyword, body string, at ShaderLocation) error {
	switch keyword {
	case "enable", "requires":
		// enable and requires each take a comma-separated list: split it and
		// union the names across all sources. The union was chosen over
		// text-level de-duplication because whether WGSL permits repeating an
		// enable for one extension was not verified; unioning makes the question
		// moot for one Split, which is cheaper than answering it and cannot rot
		// if the answer changes.
		set := f.enables
		if keyword == "requires" {
			set = f.requires
		}
		for _, item := range strings.Split(body, ",") {
			if item = strings.TrimSpace(item); item != "" {
				set[item] = true
			}
		}
		return nil
	}
	text := "diagnostic" + normaliseSpace(body) + ";"
	rule := diagnosticRule(body)
	// Two diagnostic directives naming one rule with different severities is an
	// error WGSL makes, and one the preprocessor manufactures: unflattened the
	// two sources are separate modules with no conflict at all. Detection needs
	// the rule name only - identical lines de-duplicate, so any two survivors
	// naming one rule differ, and differ only in severity.
	if existing, ok := f.diagnostic[rule]; ok {
		if existing.text != text {
			return f.errAt(fmt.Sprintf("two diagnostic directives name the rule %q with different severities", rule),
				at, existing.at)
		}
		return nil
	}
	f.diagnostic[rule] = hoistedDiagnostic{text: text, at: at}
	return nil
}

// diagnosticRule reads the rule name out of a diagnostic directive's argument
// list. This is the only place the preprocessor reads inside a §4 directive.
func diagnosticRule(body string) string {
	open := strings.IndexByte(body, '(')
	shut := strings.LastIndexByte(body, ')')
	if open < 0 || shut < open {
		return strings.TrimSpace(body)
	}
	parts := strings.Split(body[open+1:shut], ",")
	if len(parts) < 2 {
		return strings.TrimSpace(body[open+1 : shut])
	}
	return strings.TrimSpace(parts[1])
}

func normaliseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// prependPrologue puts the hoisted directives at the very top of the flattened
// module, above everything, injected consts included, and gives them the one
// segment with no source correspondence. A source with no §4 directives produces
// a zero-line prologue, so the output line count equals the sum of its sources'.
func (f *flattener) prependPrologue() {
	var prologue []string
	if len(f.enables) > 0 {
		prologue = append(prologue, "enable "+strings.Join(sortedKeys(f.enables), ", ")+";")
	}
	if len(f.requires) > 0 {
		prologue = append(prologue, "requires "+strings.Join(sortedKeys(f.requires), ", ")+";")
	}
	for _, rule := range sortedKeys(f.diagnostic) {
		prologue = append(prologue, f.diagnostic[rule].text)
	}
	if len(prologue) == 0 {
		return
	}
	for i := range f.segments {
		f.segments[i].OutputStart += len(prologue)
	}
	f.segments = slices.Insert(f.segments, 0, ShaderSegment{OutputStart: 1, Length: len(prologue)})
	f.out = slices.Insert(f.out, 0, prologue...)
	f.cond = slices.Insert(f.cond, 0, make([]condLine, len(prologue))...)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
