package gfx

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/dvoyni/cog/storage"
)

// inlineShaderName is what a ShaderWithText root is called in a location and a
// segment. It carries no directory, which is why a relative #include in inline
// text is an error rather than resolving against the storage root: the same
// snippet must not resolve differently depending on whether it was pasted or
// loaded, a difference invisible in a diff.
const inlineShaderName = "gfx.shader"

// FlattenShader resolves one shader's sources into the single WGSL string a
// Backend compiles, and reports where every line of it came from.
//
// It is an exported plain function rather than a command because gfx cannot
// write the result anywhere: the render handler holds kernel.Read[storage.FileSystem],
// and writing needs kernel.Write, which would serialise rendering against every
// reader. Exporting it is a signature decision rather than extra work - the
// translator needs the function regardless - and it pays for itself twice. It is
// the test surface for the whole preprocessor, which otherwise could only be
// exercised through a backend, and what a developer dumps is byte-identical to
// what compiled, because it is the same call.
func FlattenShader(filesystem storage.FileSystem, descr ShaderDescr) (string, ShaderSourceMap, error) {
	flattened, err := flattenShader(filesystem, descr)
	return flattened.text, flattened.sourceMap, err
}

// flattenedShader is what the translator keeps from one flatten: the text a
// backend compiles, the map its errors are read against, and the sources the
// module was built from, which is what eviction scans.
type flattenedShader struct {
	text      string
	sourceMap ShaderSourceMap
	sources   []string
}

// flattenShader is FlattenShader with the include set the translator needs.
func flattenShader(filesystem storage.FileSystem, descr ShaderDescr) (flattenedShader, error) {
	f := &flattener{
		filesystem: filesystem,
		descr:      descr,
		label:      shaderLabel(descr),
		included:   map[string]bool{},
		noted:      map[string]bool{},
		defines:    map[string]ShaderLocation{},
		consts:     map[string]ShaderLocation{},
		enables:    map[string]bool{},
		requires:   map[string]bool{},
		diagnostic: map[string]hoistedDiagnostic{},
	}
	text, sourceMap, err := f.run()
	return flattenedShader{text: text, sourceMap: sourceMap, sources: f.sources}, err
}

type flattener struct {
	filesystem storage.FileSystem
	descr      ShaderDescr
	label      string

	// out and cond are parallel: one entry per flattened line, cond carrying the
	// conditional directive that line was, if any. Phase 2 walks them together.
	out      []string
	cond     []condLine
	segments []ShaderSegment

	// included keys on the resolved absolute path, which is what makes a
	// diamond one copy and a shared source's #const one declaration. sources is
	// the same set in visit order, which the translator evicts by. stack is the
	// sources currently open, for cycle detection; chain is the #include lines
	// that opened them, which is the message when one of those two fails.
	included map[string]bool
	sources  []string
	noted    map[string]bool
	stack    []string
	chain    []ShaderLocation

	// Declarations are collected across the whole tree before anything is
	// selected, which is what makes a #define file-scoped rather than
	// position-scoped and needs no traversal of the include graph.
	defines       map[string]ShaderLocation
	consts        map[string]ShaderLocation
	supplyDefines map[string]bool
	supplyConsts  map[string]string

	enables    map[string]bool
	requires   map[string]bool
	diagnostic map[string]hoistedDiagnostic
}

// hoistedDiagnostic is one WGSL diagnostic directive, kept by rule name so that
// two naming one rule with different severities can be reported.
type hoistedDiagnostic struct {
	text string // normalised, for de-duplication and emission
	at   ShaderLocation
}

// condLine is the conditional directive one flattened line was, if any.
type condLine struct {
	kind directiveKind
	expr condExpr
	at   ShaderLocation
}

func (f *flattener) run() (string, ShaderSourceMap, error) {
	// A malformed supply surfaces here rather than at construction, because a
	// constructor that panicked would turn a typo in a material declaration into
	// a crash on a path that today cannot fail. It has no source location at
	// all, and an empty At is the encoding: Shader already carries the supply, so
	// no sentinel source name is invented.
	if f.descr.supplyMalformed != "" {
		return "", ShaderSourceMap{}, ErrShaderSource{Shader: f.label, Message: f.descr.supplyMalformed}
	}
	f.readSupply()

	name, text, err := f.rootSource()
	if err != nil {
		return "", ShaderSourceMap{}, err
	}
	if err := f.emitSource(name, text, ShaderLocation{}); err != nil {
		return "", ShaderSourceMap{}, err
	}
	if err := f.checkSupplyKinds(); err != nil {
		return "", ShaderSourceMap{}, err
	}
	if err := f.selectBranches(); err != nil {
		return "", ShaderSourceMap{}, err
	}
	f.prependPrologue()

	var b strings.Builder
	for _, line := range f.out {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String(), ShaderSourceMap{Segments: f.segments}, nil
}

func (f *flattener) readSupply() {
	f.supplyDefines = map[string]bool{}
	f.supplyConsts = map[string]string{}
	for _, entry := range f.descr.supplyEntries() {
		if entry.isConst {
			f.supplyConsts[entry.name] = entry.value
			continue
		}
		f.supplyDefines[entry.name] = true
	}
}

func (f *flattener) rootSource() (name, text string, err error) {
	if f.descr.source != ShaderSourceResource {
		return inlineShaderName, f.descr.textOrPath, nil
	}
	name = f.descr.textOrPath
	// The root is noted before it is opened, so that a module which failed
	// because its root was missing is still evicted when that path is released.
	f.note(name)
	code, ok := loadShaderResource(f.filesystem, name)
	if !ok {
		return "", "", ErrShaderNotFound{Name: name}
	}
	return name, string(code), nil
}

// note records one source as participating in this module. The set is what
// eviction scans, so a source counts from the moment its name resolves, whether
// or not it turns out to exist and whether or not the flatten succeeds.
func (f *flattener) note(name string) {
	if f.noted[name] {
		return
	}
	f.noted[name] = true
	f.sources = append(f.sources, name)
}

func (f *flattener) errAt(message string, at ...ShaderLocation) error {
	return ErrShaderSource{Shader: f.label, Message: message, At: at}
}

// errChain reports the two failures where the include chain is the message. The
// chain is on the stack while flattening, so emitting it costs nothing, and the
// last entry is the failing #include line.
func (f *flattener) errChain(message string, at ShaderLocation) error {
	return ErrShaderSource{Shader: f.label, Message: message, At: append(slices.Clone(f.chain), at)}
}

// emitSource splices one source into the output: it validates the source's
// conditional structure, hoists the WGSL §4 directives out of its prologue,
// collects its declarations, and recurses through its includes.
func (f *flattener) emitSource(name, text string, from ShaderLocation) error {
	f.included[name] = true
	f.stack = append(f.stack, name)
	defer func() { f.stack = f.stack[:len(f.stack)-1] }()

	lines := blankComments(text)
	scan, err := f.scanSource(name, lines)
	if err != nil {
		return err
	}
	hoisted, err := f.hoistPrologue(name, lines, scan)
	if err != nil {
		return err
	}

	for i, line := range lines {
		at := ShaderLocation{Source: name, Line: i + 1}
		if hoisted[i] {
			f.appendLine(blankLine(line), name, i+1, from, condLine{})
			continue
		}
		switch scan[i].kind {
		case dirInclude:
			f.appendLine(blankLine(line), name, i+1, from, condLine{})
			if err := f.include(scan[i].arg, name, at); err != nil {
				return err
			}
		case dirDefine:
			if err := f.declareDefine(scan[i].arg, at); err != nil {
				return err
			}
			f.appendLine(blankLine(line), name, i+1, from, condLine{})
		case dirConst:
			injected, err := f.declareConst(line, scan[i].arg, at)
			if err != nil {
				return err
			}
			f.appendLine(injected, name, i+1, from, condLine{})
		case dirIf, dirElif, dirElse, dirEndif:
			f.appendLine(blankLine(line), name, i+1, from,
				condLine{kind: scan[i].kind, expr: scan[i].expr, at: at})
		default:
			f.appendLine(line, name, i+1, from, condLine{})
		}
	}
	return nil
}

// appendLine adds one output line, extending the segment it continues rather
// than starting a new one. A segment breaks exactly where the output stops
// corresponding 1:1 to a run of one source's lines, which is at an include and
// nowhere else.
func (f *flattener) appendLine(text, source string, sourceLine int, from ShaderLocation, cond condLine) {
	outputLine := len(f.out) + 1
	f.out = append(f.out, text)
	f.cond = append(f.cond, cond)
	if n := len(f.segments); n > 0 {
		last := &f.segments[n-1]
		if last.Source == source && last.IncludedFrom == from &&
			last.OutputStart+last.Length == outputLine && last.SourceStart+last.Length == sourceLine {
			last.Length++
			return
		}
	}
	f.segments = append(f.segments, ShaderSegment{
		OutputStart: outputLine, Length: 1, Source: source, SourceStart: sourceLine, IncludedFrom: from,
	})
}

// lineScan is one line's directive plus the conditional it sits inside.
type lineScan struct {
	kind    directiveKind
	arg     string
	expr    condExpr
	openIf  ShaderLocation // the innermost open #if; zero at depth 0
	nesting int
}

// scanSource validates one source's directive structure before anything is
// emitted from it. Every conditional opens and closes in one source - an
// #include cannot sit inside one - so this is the whole of the structural check,
// and phase 2 only has to evaluate.
func (f *flattener) scanSource(name string, lines []string) ([]lineScan, error) {
	type frame struct {
		openIf  ShaderLocation
		elseAt  ShaderLocation
		hasElse bool
	}
	scan := make([]lineScan, len(lines))
	var open []frame
	for i, line := range lines {
		at := ShaderLocation{Source: name, Line: i + 1}
		d := parseDirective(line)
		if !d.sigil {
			continue
		}
		// The reservation is lexical and does not depend on liveness, so an
		// unknown keyword is an error inside a skipped branch too.
		if !d.known {
			return nil, f.errAt(unknownDirectiveMessage(d.keyword), at)
		}
		scan[i] = lineScan{kind: d.kind, arg: d.arg, nesting: len(open)}
		if len(open) > 0 {
			scan[i].openIf = open[len(open)-1].openIf
		}
		switch d.kind {
		case dirInclude, dirDefine, dirConst:
			// Conditionals gate WGSL text and nothing else. That is what makes
			// declarations never conditionally present and the include graph a
			// pure function of the root, independent of the supply.
			if len(open) > 0 {
				return nil, f.errAt(fmt.Sprintf("%s may not appear inside a conditional branch", d.kind),
					at, open[len(open)-1].openIf)
			}
		case dirIf:
			expr, err := parseCondition(d.arg)
			if err != nil {
				return nil, f.errAt(err.Error(), at)
			}
			scan[i].expr = expr
			open = append(open, frame{openIf: at})
		case dirElif, dirElse:
			if len(open) == 0 {
				return nil, f.errAt(fmt.Sprintf("%s with no open #if", d.kind), at)
			}
			top := &open[len(open)-1]
			if top.hasElse {
				return nil, f.errAt(fmt.Sprintf("%s after #else", d.kind), at, top.elseAt)
			}
			if d.kind == dirElse {
				top.hasElse, top.elseAt = true, at
				break
			}
			expr, err := parseCondition(d.arg)
			if err != nil {
				return nil, f.errAt(err.Error(), at)
			}
			scan[i].expr = expr
		case dirEndif:
			if len(open) == 0 {
				return nil, f.errAt("#endif with no open #if", at)
			}
			open = open[:len(open)-1]
		}
	}
	if len(open) > 0 {
		// The opening line is never optional: an unclosed #if reported only at
		// EOF is the least useful message a preprocessor can emit.
		return nil, f.errAt("#if is never closed",
			ShaderLocation{Source: name, Line: len(lines)}, open[len(open)-1].openIf)
	}
	return scan, nil
}

// include resolves one #include and splices the source it names.
func (f *flattener) include(arg, includer string, at ShaderLocation) error {
	if arg == "" {
		return f.errAt("#include with an empty path", at)
	}
	name, err := f.resolveInclude(arg, includer, at)
	if err != nil {
		return err
	}
	// Noting the resolved name before the load is what lets a module that failed
	// on a missing include be evicted by the path the author then creates.
	f.note(name)
	// A cycle terminates on its own under include-once, and is still an error:
	// it is always an authoring mistake, and proceeding silently yields a module
	// missing whichever half the cycle cut.
	if slices.Contains(f.stack, name) {
		return f.errChain(fmt.Sprintf("#include cycle through %q", name), at)
	}
	// A source already spliced into this module is not spliced again. This is
	// load-bearing rather than an optimisation: WGSL rejects duplicate top-level
	// declarations, so C semantics would turn every diamond into a compile error
	// the author hand-guards, and it is what makes a shared source's #const
	// exactly one declaration under the uniqueness rule.
	if f.included[name] {
		return nil
	}
	code, ok := loadShaderResource(f.filesystem, name)
	if !ok {
		return f.errChain(fmt.Sprintf("#include %q not found", name), at)
	}
	f.chain = append(f.chain, at)
	defer func() { f.chain = f.chain[:len(f.chain)-1] }()
	return f.emitSource(name, string(code), at)
}

// resolveInclude turns a written path into the clean absolute storage name to
// open. The "./" prefix is the marker: a path beginning "./" or "../" is
// relative to the including source's directory, and anything else is an absolute
// storage name used verbatim. There is no leading-"/" form, because storage
// names are unrooted and inventing one would give the root a second spelling.
func (f *flattener) resolveInclude(arg, includer string, at ShaderLocation) (string, error) {
	if !strings.HasPrefix(arg, "./") && !strings.HasPrefix(arg, "../") {
		return arg, nil
	}
	if includer == inlineShaderName {
		return "", f.errAt("a relative #include has no directory to resolve against in a ShaderWithText shader; "+
			"name the source by its absolute storage path", at)
	}
	// fs.ValidPath forbids "." and ".." elements outright, so the preprocessor
	// resolves and normalises the path itself and only ever hands Open a clean
	// absolute name.
	dir := path.Dir(includer)
	if dir == "." {
		dir = ""
	}
	name := path.Join(dir, arg)
	// The only boundary is the storage root, and a path climbing above it is an
	// error rather than a silent clamp.
	if name == ".." || strings.HasPrefix(name, "../") {
		return "", f.errAt(fmt.Sprintf("#include %q climbs above the storage root", arg), at)
	}
	return name, nil
}

// declareDefine records a valueless flag. A define never carries a value, so
// trailing text is an error rather than a value silently dropped.
func (f *flattener) declareDefine(arg string, at ShaderLocation) error {
	if arg == "" {
		return f.errAt("#define with no name", at)
	}
	if !isName(arg) {
		return f.errAt(fmt.Sprintf("#define %q carries trailing text; a define never carries a value", arg), at)
	}
	if other, ok := f.consts[arg]; ok {
		return f.errAt(fmt.Sprintf("%q is declared as both a define and a const", arg), at, other)
	}
	// Duplicate #define is allowed and silent: a define has no value, so
	// "override" collapses to "set", and the set has a defined answer. Contrast
	// consts, where a duplicate leaves no single injection site.
	if _, ok := f.defines[arg]; !ok {
		f.defines[arg] = at
	}
	return nil
}

// declareConst records a named value and returns the WGSL const it compiles to,
// injected on the directive's own line so that the line count is preserved
// exactly and the source map needs no synthetic prologue.
func (f *flattener) declareConst(line, arg string, at ShaderLocation) (string, error) {
	name, value, ok := strings.Cut(arg, "=")
	name, value = strings.TrimSpace(name), strings.TrimSpace(value)
	if !ok {
		return "", f.errAt("#const needs a value: write `#const NAME=VALUE`", at)
	}
	if name == "" || !isName(name) {
		return "", f.errAt(fmt.Sprintf("#const has no usable name in %q", arg), at)
	}
	// A #const is declared exactly once across the flattened tree, whether or not
	// the name is ever referenced. There is no shadowing and no precedence
	// between sources: an includer does not override its includee. The repair for
	// two sources wanting one const is to hoist it into a source both include,
	// which include-once makes exactly one declaration.
	if other, ok := f.consts[name]; ok {
		return "", f.errAt(fmt.Sprintf("#const %q is declared twice", name), at, other)
	}
	if other, ok := f.defines[name]; ok {
		return "", f.errAt(fmt.Sprintf("%q is declared as both a define and a const", name), at, other)
	}
	f.consts[name] = at
	// Go configures the one declaration; it never introduces a name. So an
	// includee's #const is its default, and a supplied value overrides it here,
	// at the declaration's own line - the only line there is to inject at.
	if supplied, ok := f.supplyConsts[name]; ok {
		value = supplied
	}
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	return indent + "const " + name + " = " + value + ";", nil
}

// checkSupplyKinds reports a supply key whose kind disagrees with the tree's
// declaration. A supplied const for a name no source declares is silently
// ignored - it has no line to inject at, and ignoring it keeps one const map
// usable across a family of shaders where only some declare each name - but a
// name that is both a define and a const makes "what is FOO" unanswerable at a
// glance, and the supply carries both kinds under one key string.
func (f *flattener) checkSupplyKinds() error {
	for name := range f.supplyConsts {
		if at, ok := f.defines[name]; ok {
			return f.errAt(fmt.Sprintf("%q is supplied as a const but declared as a define", name),
				ShaderLocation{}, at)
		}
	}
	for name := range f.supplyDefines {
		if at, ok := f.consts[name]; ok {
			return f.errAt(fmt.Sprintf("%q is supplied as a define but declared as a const", name),
				ShaderLocation{}, at)
		}
	}
	return nil
}

// selectBranches evaluates the conditionals over the flattened text, blanking
// the branches that lose. It runs after the whole tree is in hand, which is what
// makes a #define file-scoped: there is no sequential pass in which "before"
// would mean anything.
func (f *flattener) selectBranches() error {
	defines := make(map[string]bool, len(f.defines)+len(f.supplyDefines))
	for name := range f.defines {
		defines[name] = true
	}
	for name := range f.supplyDefines {
		defines[name] = true
	}

	type frame struct {
		taken bool // some branch of this conditional has already won
		live  bool
	}
	var open []frame
	live := true
	for i := range f.out {
		switch f.cond[i].kind {
		case dirIf:
			parent := live
			won := parent && f.cond[i].expr.eval(defines)
			open = append(open, frame{taken: won, live: parent})
			live = won
		case dirElif:
			top := &open[len(open)-1]
			won := top.live && !top.taken && f.cond[i].expr.eval(defines)
			top.taken = top.taken || won
			live = won
		case dirElse:
			top := &open[len(open)-1]
			won := top.live && !top.taken
			top.taken = true
			live = won
		case dirEndif:
			live = open[len(open)-1].live
			open = open[:len(open)-1]
		default:
			// Skipped text is blanked, not deleted, so a WGSL error in the
			// surviving text still lands on a real line.
			if !live {
				f.out[i] = blankLine(f.out[i])
			}
		}
	}
	return nil
}

func isName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isNameByte(s[i]) {
			return false
		}
	}
	return true
}
