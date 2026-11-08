package shader

import (
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/dvoyni/cog/gfx"
)

// hoistedKeywords are WGSL's directives that must precede every declaration
// in a module, which splicing breaks. The preprocessor repairs it by hoisting
// them to the top of the module. Forbidding them in an included source needs
// two rules and still misses a root including A including B where B needs
// f16; hoisting needs none, and keeps an included source valid WGSL alone.
//
// This is the one place the preprocessor reads WGSL, and it is bounded: three
// keywords, at the start of a line, in a source's prologue only, the line
// opaque but for a diagnostic's rule name.
var hoistedKeywords = []string{"enable", "requires", "diagnostic"}

// flattener splices one shader's root source and its includes into the one
// source a backend compiles. It is v1's preprocessor, ported.
type flattener struct {
	files fs.FS
	root  string
	label string

	// malformed is the message for the first define given with no legal
	// spelling, and empty otherwise.
	malformed string
	// givenFlags are the defines given without a value, givenValues those
	// with one, by name.
	givenFlags  map[string]bool
	givenValues map[string]string

	// lines and conditionals run in step: one entry per output line, the
	// second the conditional directive that line was, if any.
	lines        []string
	conditionals []conditionalLine

	// included holds every source spliced so far by its cleaned path, which
	// makes a diamond one copy and a shared source's #const one declaration.
	// stack is the sources open now, for finding cycles, and chain the
	// #include lines that opened them, which an error in one names.
	included map[string]bool
	stack    []string
	chain    []gfx.ShaderSourceLocation

	// The declarations are collected from every source before any branch is
	// chosen, so a #define holds for its whole shader, wherever it stands.
	defines map[string]gfx.ShaderSourceLocation
	consts  map[string]gfx.ShaderSourceLocation

	enables     map[string]bool
	requires    map[string]bool
	diagnostics map[string]hoistedDiagnostic
}

// conditionalLine is the conditional directive one output line was, if any.
type conditionalLine struct {
	kind      directiveKind
	condition condition
}

// scannedLine is one line's directive, with what it holds.
type scannedLine struct {
	kind      directiveKind
	argument  string
	condition condition
}

// hoistedDiagnostic is one WGSL diagnostic directive, kept by its rule name so
// two naming one rule with different severities are found.
type hoistedDiagnostic struct {
	text string // spaces normalised, for comparing and emitting
	at   gfx.ShaderSourceLocation
}

func newFlattener(files fs.FS, root string, defines []gfx.ShaderDefine) *flattener {
	label, kept, malformed := labelShader(root, defines)
	flattener := &flattener{
		files:       files,
		root:        root,
		label:       label,
		malformed:   malformed,
		givenFlags:  map[string]bool{},
		givenValues: map[string]string{},
		included:    map[string]bool{},
		defines:     map[string]gfx.ShaderSourceLocation{},
		consts:      map[string]gfx.ShaderSourceLocation{},
		enables:     map[string]bool{},
		requires:    map[string]bool{},
		diagnostics: map[string]hoistedDiagnostic{},
	}
	for _, define := range kept {
		if define.Value == "" {
			flattener.givenFlags[define.Name] = true
			continue
		}
		flattener.givenValues[define.Name] = define.Value
	}
	return flattener
}

// run answers the flattened source, or the first refusal.
func (f *flattener) run() (string, error) {
	if f.malformed != "" {
		return "", f.refuse(f.malformed)
	}
	text, err := fs.ReadFile(f.files, f.root)
	if err != nil {
		return "", f.refuse(fmt.Sprintf("%q not found", f.root))
	}
	if err := f.emitSource(f.root, string(text), gfx.ShaderSourceLocation{}); err != nil {
		return "", err
	}
	if err := f.checkGivenKinds(); err != nil {
		return "", err
	}
	f.selectBranches()
	f.prependHoisted()

	var builder strings.Builder
	for _, line := range f.lines {
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	return builder.String(), nil
}

// emitSource splices one source into the output: it checks the source's
// conditional structure, hoists WGSL's directives from its prologue, collects
// its declarations, and splices its includes.
func (f *flattener) emitSource(name, text string, from gfx.ShaderSourceLocation) error {
	f.included[name] = true
	f.stack = append(f.stack, name)
	defer func() { f.stack = f.stack[:len(f.stack)-1] }()

	lines := blankComments(text)
	scanned, err := f.scanSource(name, lines)
	if err != nil {
		return err
	}
	hoisted, err := f.hoistPrologue(name, lines, scanned)
	if err != nil {
		return err
	}

	for index, line := range lines {
		at := gfx.ShaderSourceLocation{Source: name, Line: index + 1}
		if hoisted[index] {
			f.appendLine(blankLine(line), conditionalLine{})
			continue
		}
		switch scanned[index].kind {
		case directiveInclude:
			f.appendLine(blankLine(line), conditionalLine{})
			if err := f.include(scanned[index].argument, name, at); err != nil {
				return err
			}
		case directiveDefine:
			if err := f.declareDefine(scanned[index].argument, at); err != nil {
				return err
			}
			f.appendLine(blankLine(line), conditionalLine{})
		case directiveConst:
			declared, err := f.declareConst(line, scanned[index].argument, at)
			if err != nil {
				return err
			}
			f.appendLine(declared, conditionalLine{})
		case directiveIf, directiveElif, directiveElse, directiveEndif:
			f.appendLine(blankLine(line), conditionalLine{kind: scanned[index].kind, condition: scanned[index].condition})
		default:
			f.appendLine(line, conditionalLine{})
		}
	}
	return nil
}

// scanSource checks one source's directive structure before anything is
// emitted from it. Every conditional opens and closes in one source, as an
// #include cannot sit inside one, so this is the whole structural check, and
// selectBranches only evaluates.
func (f *flattener) scanSource(name string, lines []string) ([]scannedLine, error) {
	type openConditional struct {
		ifAt   gfx.ShaderSourceLocation
		elseAt gfx.ShaderSourceLocation
	}
	scanned := make([]scannedLine, len(lines))
	var open []openConditional
	for index, line := range lines {
		at := gfx.ShaderSourceLocation{Source: name, Line: index + 1}
		parsed := parseDirective(line)
		if !parsed.sigil {
			continue
		}
		// The reservation is lexical, so an unknown keyword is an error in a
		// skipped branch too.
		if !parsed.known {
			return nil, f.refuse(describeUnknownDirective(parsed.keyword), at)
		}
		scanned[index] = scannedLine{kind: parsed.kind, argument: parsed.argument}
		switch parsed.kind {
		case directiveInclude, directiveDefine, directiveConst:
			// Conditionals gate WGSL text and nothing else, so a declaration is
			// never conditionally present and which sources are included
			// depends on the root alone, not on the defines given.
			if len(open) > 0 {
				return nil, f.refuse(fmt.Sprintf("%s may not appear inside a conditional branch", parsed.kind),
					at, open[len(open)-1].ifAt)
			}
		case directiveIf:
			parsedCondition, err := parseCondition(parsed.argument)
			if err != nil {
				return nil, f.refuse(err.Error(), at)
			}
			scanned[index].condition = parsedCondition
			open = append(open, openConditional{ifAt: at})
		case directiveElif, directiveElse:
			if len(open) == 0 {
				return nil, f.refuse(fmt.Sprintf("%s with no open #if", parsed.kind), at)
			}
			innermost := &open[len(open)-1]
			if innermost.elseAt != (gfx.ShaderSourceLocation{}) {
				return nil, f.refuse(fmt.Sprintf("%s after #else", parsed.kind), at, innermost.elseAt)
			}
			if parsed.kind == directiveElse {
				innermost.elseAt = at
				break
			}
			parsedCondition, err := parseCondition(parsed.argument)
			if err != nil {
				return nil, f.refuse(err.Error(), at)
			}
			scanned[index].condition = parsedCondition
		case directiveEndif:
			if len(open) == 0 {
				return nil, f.refuse("#endif with no open #if", at)
			}
			open = open[:len(open)-1]
		}
	}
	if len(open) > 0 {
		// The opening line is named too: an unclosed #if reported only at the
		// end of its source is the least useful message there is.
		return nil, f.refuse("#if is never closed",
			gfx.ShaderSourceLocation{Source: name, Line: len(lines)}, open[len(open)-1].ifAt)
	}
	return scanned, nil
}

// include resolves one #include and splices the source it names.
func (f *flattener) include(argument, includer string, at gfx.ShaderSourceLocation) error {
	if argument == "" {
		return f.refuse("#include with an empty path", at)
	}
	name, err := f.resolveInclude(argument, includer, at)
	if err != nil {
		return err
	}
	// A cycle is an error: it is always a mistake, and going on would leave
	// out whichever half the cycle cut.
	if slices.Contains(f.stack, name) {
		return f.refuseWithChain(fmt.Sprintf("#include cycle through %q", name), at)
	}
	// A source already spliced is not spliced again. WGSL rejects duplicate
	// declarations, so splicing it twice would make every diamond a compile
	// error, and once is what makes a shared source's #const one declaration.
	if f.included[name] {
		return nil
	}
	text, err := fs.ReadFile(f.files, name)
	if err != nil {
		return f.refuseWithChain(fmt.Sprintf("#include %q not found", name), at)
	}
	f.chain = append(f.chain, at)
	defer func() { f.chain = f.chain[:len(f.chain)-1] }()
	return f.emitSource(name, string(text), at)
}

// resolveInclude turns a written path into the clean path to open. One
// starting "./" or "../" is relative to the including source's directory;
// anything else is a path in the filesystem, used as it is.
func (f *flattener) resolveInclude(argument, includer string, at gfx.ShaderSourceLocation) (string, error) {
	if !strings.HasPrefix(argument, "./") && !strings.HasPrefix(argument, "../") {
		return argument, nil
	}
	// fs.ValidPath forbids "." and ".." elements, so the path is cleaned here
	// and Open only ever gets a clean one.
	directory := path.Dir(includer)
	if directory == "." {
		directory = ""
	}
	name := path.Join(directory, argument)
	// The filesystem's root is the only boundary, and climbing above it is an
	// error rather than a silent clamp.
	if name == ".." || strings.HasPrefix(name, "../") {
		return "", f.refuse(fmt.Sprintf("#include %q climbs above the filesystem's root", argument), at)
	}
	return name, nil
}

// declareDefine records a flag. A define never carries a value, so text after
// its name is an error rather than a value silently dropped.
func (f *flattener) declareDefine(argument string, at gfx.ShaderSourceLocation) error {
	if argument == "" {
		return f.refuse("#define with no name", at)
	}
	if !isName(argument) {
		return f.refuse(fmt.Sprintf("#define %q carries trailing text; a define never carries a value", argument), at)
	}
	if other, ok := f.consts[argument]; ok {
		return f.refuse(fmt.Sprintf("%q is declared as both a define and a const", argument), at, other)
	}
	// A duplicate #define is silent: a define has no value, so defining it
	// again changes nothing.
	if _, ok := f.defines[argument]; !ok {
		f.defines[argument] = at
	}
	return nil
}

// declareConst records a named value and answers the WGSL const it becomes,
// on the directive's own line, so the line count holds.
func (f *flattener) declareConst(line, argument string, at gfx.ShaderSourceLocation) (string, error) {
	name, value, ok := strings.Cut(argument, "=")
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)
	if !ok {
		return "", f.refuse("#const needs a value: write `#const NAME=VALUE`", at)
	}
	if !isName(name) {
		return "", f.refuse(fmt.Sprintf("#const has no usable name in %q", argument), at)
	}
	// A #const is declared exactly once across the sources, referenced or
	// not. No source overrides another's: two sources wanting one const hoist
	// it into a source both include.
	if other, ok := f.consts[name]; ok {
		return "", f.refuse(fmt.Sprintf("#const %q is declared twice", name), at, other)
	}
	if other, ok := f.defines[name]; ok {
		return "", f.refuse(fmt.Sprintf("%q is declared as both a define and a const", name), at, other)
	}
	f.consts[name] = at
	// The defines given configure a declaration and never introduce one, so
	// a source's #const is its default, overridden here.
	if given, ok := f.givenValues[name]; ok {
		value = given
	}
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	return indent + "const " + name + " = " + value + ";", nil
}

// checkGivenKinds refuses a define given as the other kind than the sources
// declare it: a name both a define and a const makes "what is FOO"
// unanswerable at a glance. A value for a name no source declares is ignored,
// which keeps one set of defines usable across a family of shaders.
func (f *flattener) checkGivenKinds() error {
	for _, name := range slices.Sorted(maps.Keys(f.givenValues)) {
		if at, ok := f.defines[name]; ok {
			return f.refuse(fmt.Sprintf("%q is given a value but declared as a define", name), gfx.ShaderSourceLocation{}, at)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(f.givenFlags)) {
		if at, ok := f.consts[name]; ok {
			return f.refuse(fmt.Sprintf("%q is given as a flag but declared as a const", name), gfx.ShaderSourceLocation{}, at)
		}
	}
	return nil
}

// selectBranches evaluates the conditionals over the whole output, blanking
// the branches that lose. It runs once every source is in, which is what
// makes a #define hold for its whole shader.
func (f *flattener) selectBranches() {
	defines := make(map[string]bool, len(f.defines)+len(f.givenFlags))
	for name := range f.defines {
		defines[name] = true
	}
	for name := range f.givenFlags {
		defines[name] = true
	}

	type openConditional struct {
		taken bool // a branch of this conditional has already won
		live  bool // the text around this conditional is kept
	}
	var open []openConditional
	live := true
	for index := range f.lines {
		switch f.conditionals[index].kind {
		case directiveIf:
			won := live && f.conditionals[index].condition.evaluate(defines)
			open = append(open, openConditional{taken: won, live: live})
			live = won
		case directiveElif:
			innermost := &open[len(open)-1]
			won := innermost.live && !innermost.taken && f.conditionals[index].condition.evaluate(defines)
			innermost.taken = innermost.taken || won
			live = won
		case directiveElse:
			innermost := &open[len(open)-1]
			live = innermost.live && !innermost.taken
			innermost.taken = true
		case directiveEndif:
			live = open[len(open)-1].live
			open = open[:len(open)-1]
		default:
			// A skipped line is blanked rather than removed, so a WGSL error
			// in the lines kept still lands on a real line.
			if !live {
				f.lines[index] = blankLine(f.lines[index])
			}
		}
	}
}

// hoistPrologue collects one source's WGSL directives and reports the lines
// they took up, which emitSource blanks.
//
// They are recognised only in the source's prologue: the run of blank lines,
// comments and preprocessor directives at its top, ending at its first line of
// WGSL. That is WGSL's own placement rule, so it needs no grammar. One after
// WGSL in its own source was already invalid WGSL, and is left to the
// backend.
func (f *flattener) hoistPrologue(name string, lines []string, scanned []scannedLine) (map[int]bool, error) {
	hoisted := map[int]bool{}
	for index := 0; index < len(lines); {
		// A preprocessor directive, a #const included, does not end the
		// prologue: it is not WGSL yet when the prologue is read.
		if strings.TrimSpace(lines[index]) == "" || scanned[index].kind != directiveNone {
			index++
			continue
		}
		keyword, rest := splitLeadingWord(lines[index])
		if !slices.Contains(hoistedKeywords, keyword) {
			return hoisted, nil
		}
		body, end, ok := readToSemicolon(rest, lines, index)
		if !ok {
			return hoisted, nil
		}
		at := gfx.ShaderSourceLocation{Source: name, Line: index + 1}
		if err := f.collectHoisted(keyword, body, at); err != nil {
			return nil, err
		}
		for line := index; line <= end; line++ {
			hoisted[line] = true
		}
		index = end + 1
	}
	return hoisted, nil
}

// collectHoisted keeps one WGSL directive for the top of the module.
func (f *flattener) collectHoisted(keyword, body string, at gfx.ShaderSourceLocation) error {
	switch keyword {
	case "enable", "requires":
		// Each takes a list, and the names are unioned across the sources,
		// which leaves no question of whether WGSL allows one repeated.
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
	text := "diagnostic" + strings.Join(strings.Fields(body), " ") + ";"
	rule := readDiagnosticRule(body)
	// Two diagnostics naming one rule with different severities are an error
	// WGSL makes, and splicing manufactures: apart, the two sources do not
	// conflict. Identical lines are kept once, so two that differ by the same
	// rule differ in severity.
	if existing, ok := f.diagnostics[rule]; ok {
		if existing.text != text {
			return f.refuse(fmt.Sprintf("two diagnostic directives name the rule %q with different severities", rule),
				at, existing.at)
		}
		return nil
	}
	f.diagnostics[rule] = hoistedDiagnostic{text: text, at: at}
	return nil
}

// prependHoisted puts the hoisted directives at the very top of the module,
// above every source. A shader with none gains no lines.
func (f *flattener) prependHoisted() {
	var hoisted []string
	if len(f.enables) > 0 {
		hoisted = append(hoisted, "enable "+strings.Join(slices.Sorted(maps.Keys(f.enables)), ", ")+";")
	}
	if len(f.requires) > 0 {
		hoisted = append(hoisted, "requires "+strings.Join(slices.Sorted(maps.Keys(f.requires)), ", ")+";")
	}
	for _, rule := range slices.Sorted(maps.Keys(f.diagnostics)) {
		hoisted = append(hoisted, f.diagnostics[rule].text)
	}
	f.lines = slices.Insert(f.lines, 0, hoisted...)
}

func (f *flattener) appendLine(text string, conditional conditionalLine) {
	f.lines = append(f.lines, text)
	f.conditionals = append(f.conditionals, conditional)
}

func (f *flattener) refuse(message string, at ...gfx.ShaderSourceLocation) error {
	return gfx.ErrShaderSource{Shader: f.label, Message: message, At: at}
}

// refuseWithChain refuses with the chain of #include lines down to the
// failing one at.
func (f *flattener) refuseWithChain(message string, at gfx.ShaderSourceLocation) error {
	return gfx.ErrShaderSource{Shader: f.label, Message: message, At: append(slices.Clone(f.chain), at)}
}

// splitLeadingWord splits a line's first bare word from the rest. An
// identifier only starting with a keyword, enableThing, is a word of its own
// and matches none, and an @diagnostic attribute starts with "@".
func splitLeadingWord(line string) (word, rest string) {
	trimmed := strings.TrimLeft(line, " \t")
	end := 0
	for end < len(trimmed) && isNameByte(trimmed[end]) {
		end++
	}
	return trimmed[:end], trimmed[end:]
}

// readToSemicolon reads a hoisted directive's body from rest, on through later
// lines to its ";". It declines a line with anything after the ";", leaving
// it to the backend rather than swallowing the code after it.
func readToSemicolon(rest string, lines []string, start int) (body string, end int, ok bool) {
	var builder strings.Builder
	for index := start; index < len(lines); index++ {
		text := rest
		if index > start {
			text = lines[index]
		}
		semicolon := strings.IndexByte(text, ';')
		if semicolon < 0 {
			builder.WriteString(text)
			builder.WriteByte(' ')
			continue
		}
		if strings.TrimSpace(text[semicolon+1:]) != "" {
			return "", 0, false
		}
		builder.WriteString(text[:semicolon])
		return strings.TrimSpace(builder.String()), index, true
	}
	return "", 0, false
}

// readDiagnosticRule reads the rule name from a diagnostic directive's
// arguments, the one place the preprocessor reads inside a WGSL directive.
func readDiagnosticRule(body string) string {
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
