package gfx

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/storage"
)

// sources builds a FileSystem over inline .wgsl sources, which is the whole
// fixture apparatus the preprocessor needs: FlattenShader is exported precisely
// so that every rule below is falsifiable with no backend at all.
func sources(files map[string]string) storage.FileSystem {
	return storage.NewFileSystem("test", sourceFS(files))
}

func sourceFS(files map[string]string) fstest.MapFS {
	mapped := fstest.MapFS{}
	for name, text := range files {
		mapped[name] = &fstest.MapFile{Data: []byte(text)}
	}
	return mapped
}

func flatten(t *testing.T, files map[string]string, descr ShaderDescr) (string, ShaderSourceMap) {
	t.Helper()
	text, sourceMap, err := FlattenShader(sources(files), descr)
	if err != nil {
		t.Fatalf("FlattenShader: %v", err)
	}
	return text, sourceMap
}

func flattenErr(t *testing.T, files map[string]string, descr ShaderDescr) ErrShaderSource {
	t.Helper()
	_, _, err := FlattenShader(sources(files), descr)
	if err == nil {
		t.Fatal("FlattenShader succeeded, want an error")
	}
	var refused ErrShaderSource
	if !errors.As(err, &refused) {
		t.Fatalf("FlattenShader: %v, want an ErrShaderSource", err)
	}
	return refused
}

func lineCount(text string) int {
	return len(strings.Split(strings.TrimSuffix(text, "\n"), "\n"))
}

// ----- #135: comments, sigils and diagnostics -----

// The flatten is line-preserving, which is the property the source map rests
// on: comments blank to spaces rather than being deleted, so every downstream
// line number stays accurate.
func TestFlattenPreservesLinesAndBlanksComments(t *testing.T) {
	source := "fn a() {} // trailing\n/* whole line */\nfn b() {} /* mid */ let x = 1;\n"
	text, sourceMap := flatten(t, map[string]string{"s.wgsl": source}, ShaderWithResource("s.wgsl"))

	if got, want := lineCount(text), lineCount(source); got != want {
		t.Fatalf("flattened to %d lines, want %d", got, want)
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if lines[0] != "fn a() {}            " {
		t.Errorf("line 1 = %q, want the comment blanked to spaces", lines[0])
	}
	if strings.TrimSpace(lines[1]) != "" {
		t.Errorf("line 2 = %q, want a whole-line comment blanked", lines[1])
	}
	if lines[2] != "fn b() {}           let x = 1;" {
		t.Errorf("line 3 = %q, want the block comment blanked in place", lines[2])
	}
	if len(sourceMap.Segments) != 1 {
		t.Fatalf("a single source produced %d segments, want 1", len(sourceMap.Segments))
	}
	want := ShaderSegment{OutputStart: 1, Length: 3, Source: "s.wgsl", SourceStart: 1}
	if sourceMap.Segments[0] != want {
		t.Errorf("segment = %+v, want %+v", sourceMap.Segments[0], want)
	}
}

// A block comment is always a comment and is never unwrapped, which is what
// makes it the reliable way to disable a region wholesale - directives inside
// one included.
func TestBlockCommentDisablesARegionWholesale(t *testing.T) {
	source := "/*\n#if X\n#pragma once\n*/\nfn a() {}\n"
	text, _ := flatten(t, map[string]string{"s.wgsl": source}, ShaderWithResource("s.wgsl"))
	if strings.Contains(text, "pragma") {
		t.Fatalf("a commented-out directive survived: %q", text)
	}
}

// Position is measured after comment blanking, so a directive behind a block
// comment is live. The alternative - measuring on the original text - would let
// that line pass through into the WGSL lexer, which is the failure this
// mechanism exists to remove.
func TestPositionIsMeasuredAfterBlanking(t *testing.T) {
	files := map[string]string{"s.wgsl": "/* c */ #if X\nfn cut() {}\n#endif\nfn keep() {}\n"}
	text, _ := flatten(t, files, ShaderWithResource("s.wgsl"))
	if strings.Contains(text, "fn cut") {
		t.Fatalf("the directive behind a block comment was not recognised: %q", text)
	}
	if !strings.Contains(text, "fn keep") {
		t.Fatalf("the live text was cut: %q", text)
	}
}

// Both sigils are the same directive. The //# form is what keeps a source valid
// WGSL, so wgsl-analyzer, formatters and highlighters keep working; accepting
// both lets the author decide per line.
func TestBothSigilsAreOneDirective(t *testing.T) {
	for _, sigil := range []string{"#", "//#"} {
		files := map[string]string{"s.wgsl": "  " + sigil + "if X\nfn cut() {}\n" + sigil + "endif\n"}
		text, _ := flatten(t, files, ShaderWithResource("s.wgsl"))
		if strings.Contains(text, "fn cut") {
			t.Errorf("%q was not recognised as a directive: %q", sigil, text)
		}
	}
}

// A line-start sigil followed by an unknown word fails loudly rather than
// passing through to become a WGSL lexer error in text nobody wrote.
func TestUnknownDirectiveNamesTheWord(t *testing.T) {
	tests := map[string]string{
		"#pragma once":     "#pragma",
		"#includ ./x.wgsl": "#includ",
		"//#TODO":          "#TODO",
		"# include x.wgsl": "no space",
	}
	for source, want := range tests {
		err := flattenErr(t, map[string]string{"s.wgsl": source + "\n"}, ShaderWithResource("s.wgsl"))
		if !strings.Contains(err.Message, want) {
			t.Errorf("%q: message %q does not name %q", source, err.Message, want)
		}
		if len(err.At) != 1 || err.At[0] != (ShaderLocation{Source: "s.wgsl", Line: 1}) {
			t.Errorf("%q: At = %v, want one location at s.wgsl:1", source, err.At)
		}
	}
}

// The named-misspelling table is fixed and six entries long: no fuzzy matching,
// no edit distance. It is the cheapest diagnostic on the list, and #ifdef is
// what a C habit actually reaches for.
func TestNamedMisspellingsSayTheSpelling(t *testing.T) {
	tests := map[string]string{
		"#ifdef X":       "`#if NAME`",
		"#ifndef X":      "`#if !NAME`",
		"#elseif X":      "`#elif`",
		"#end":           "`#endif`",
		"#undef X":       "no equivalent",
		"#import x.wgsl": "`#include`",
	}
	for source, want := range tests {
		err := flattenErr(t, map[string]string{"s.wgsl": source + "\n"}, ShaderWithResource("s.wgsl"))
		if !strings.Contains(err.Message, want) {
			t.Errorf("%q: message %q does not offer %q", source, err.Message, want)
		}
	}
}

// What an editor's comment-toggle produces is inert, not an error: a directive
// commented out is a directive the author disabled.
func TestCommentedOutDirectivesAreInert(t *testing.T) {
	files := map[string]string{"s.wgsl": "// #if X\n// //#if X\nlet x = 1; //#todo\nfn a() {}\n"}
	text, _ := flatten(t, files, ShaderWithResource("s.wgsl"))
	if !strings.Contains(text, "fn a()") || !strings.Contains(text, "let x = 1;") {
		t.Fatalf("an inert line was treated as a directive: %q", text)
	}
}

// Nothing can recover from a preprocessor error, so the error reads as one
// printed block: the default kernel handler is log.Printf("kernel: %v", err).
func TestErrShaderSourceReadsAsOneBlock(t *testing.T) {
	err := ErrShaderSource{
		Shader:  "scene.wgsl [SKIN]",
		At:      []ShaderLocation{{Source: "a.wgsl", Line: 4}, {Source: "b.wgsl", Line: 9}},
		Message: "#const \"N\" is declared twice",
	}
	got := err.Error()
	if strings.Contains(got, "\n") {
		t.Fatalf("Error() spans lines: %q", got)
	}
	for _, want := range []string{"scene.wgsl [SKIN]", "a.wgsl:4", "b.wgsl:9", "declared twice"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, missing %q", got, want)
		}
	}
}

// A supply with no legal spelling is the one error class with no source
// location at all, and an empty At is the encoding - Shader already carries the
// supply, so no sentinel source name is invented.
func TestMalformedSupplyIsReportedAtFlattenWithNoLocation(t *testing.T) {
	files := map[string]string{"s.wgsl": "fn a() {}\n"}
	for _, descr := range []ShaderDescr{
		ShaderWithResource("s.wgsl", ShaderDefine("")),
		ShaderWithResource("s.wgsl", ShaderConst("A", "x\ny")),
	} {
		err := flattenErr(t, files, descr)
		if len(err.At) != 0 {
			t.Errorf("At = %v, want empty", err.At)
		}
		if err.Shader == "" {
			t.Error("Shader is empty, so the message cannot name the supply")
		}
	}
}

// ----- #136: include resolution and the segment table -----

func TestIncludeResolvesBothPathForms(t *testing.T) {
	files := map[string]string{
		"builtin/scene/scene.wgsl": "//#include ./pbr.wgsl\n//#include builtin/lib/frame.wgsl\nfn main() {}\n",
		"builtin/scene/pbr.wgsl":   "fn pbr() {}\n",
		"builtin/lib/frame.wgsl":   "fn frame() {}\n",
	}
	text, sourceMap := flatten(t, files, ShaderWithResource("builtin/scene/scene.wgsl"))
	for _, want := range []string{"fn pbr()", "fn frame()", "fn main()"} {
		if !strings.Contains(text, want) {
			t.Errorf("flattened text is missing %q:\n%s", want, text)
		}
	}
	// The consumed #include line blanks and the spliced source keeps 1:1
	// correspondence with its own file, so the module is exactly the sum of its
	// sources - minus nothing and plus nothing.
	if got, want := lineCount(text), 5; got != want {
		t.Errorf("flattened to %d lines, want %d", got, want)
	}
	wantSegments := []ShaderSegment{
		{OutputStart: 1, Length: 1, Source: "builtin/scene/scene.wgsl", SourceStart: 1},
		{OutputStart: 2, Length: 1, Source: "builtin/scene/pbr.wgsl", SourceStart: 1,
			IncludedFrom: ShaderLocation{Source: "builtin/scene/scene.wgsl", Line: 1}},
		{OutputStart: 3, Length: 1, Source: "builtin/scene/scene.wgsl", SourceStart: 2},
		{OutputStart: 4, Length: 1, Source: "builtin/lib/frame.wgsl", SourceStart: 1,
			IncludedFrom: ShaderLocation{Source: "builtin/scene/scene.wgsl", Line: 2}},
		{OutputStart: 5, Length: 1, Source: "builtin/scene/scene.wgsl", SourceStart: 3},
	}
	if len(sourceMap.Segments) != len(wantSegments) {
		t.Fatalf("segments = %+v, want %+v", sourceMap.Segments, wantSegments)
	}
	for i, want := range wantSegments {
		if sourceMap.Segments[i] != want {
			t.Errorf("segment %d = %+v, want %+v", i, sourceMap.Segments[i], want)
		}
	}
}

// The path is read unquoted to end of line with whitespace trimmed, which
// composes with comment blanking. A path can never contain "//", because
// fs.ValidPath forbids empty elements, so the two rules cannot collide.
func TestIncludePathReadsToEndOfLine(t *testing.T) {
	files := map[string]string{
		"a/root.wgsl": "#include ./pbr.wgsl // shared\n",
		"a/pbr.wgsl":  "fn pbr() {}\n",
	}
	text, _ := flatten(t, files, ShaderWithResource("a/root.wgsl"))
	if !strings.Contains(text, "fn pbr()") {
		t.Fatalf("a trailing comment defeated the path: %q", text)
	}
}

// ".." climbs and normalises away before Open, so storage never receives a name
// containing a "." or ".." element - which it could not open, because
// fs.ValidPath forbids them outright.
func TestIncludeNormalisesBeforeOpen(t *testing.T) {
	files := map[string]string{
		"a/b/root.wgsl":  "#include ../lib/pbr.wgsl\n",
		"a/lib/pbr.wgsl": "fn pbr() {}\n",
	}
	text, sourceMap := flatten(t, files, ShaderWithResource("a/b/root.wgsl"))
	if !strings.Contains(text, "fn pbr()") {
		t.Fatalf("a climbing path did not resolve: %q", text)
	}
	if got := sourceMap.Segments[1].Source; got != "a/lib/pbr.wgsl" {
		t.Errorf("segment names %q, want the normalised name", got)
	}
}

// The only boundary is the storage root, and a path climbing above it is an
// error rather than a silent clamp.
func TestIncludeAboveTheRootIsAnError(t *testing.T) {
	files := map[string]string{"root.wgsl": "#include ../outside.wgsl\n"}
	err := flattenErr(t, files, ShaderWithResource("root.wgsl"))
	if !strings.Contains(err.Message, "climbs above") {
		t.Errorf("message = %q, want it to name the root boundary", err.Message)
	}
}

func TestEmptyIncludePathIsAnError(t *testing.T) {
	err := flattenErr(t, map[string]string{"root.wgsl": "#include\n"}, ShaderWithResource("root.wgsl"))
	if len(err.At) != 1 {
		t.Fatalf("At = %v, want one location", err.At)
	}
	if !strings.Contains(err.Message, "empty path") {
		t.Errorf("message = %q", err.Message)
	}
}

// Include-once is load-bearing rather than an optimisation: WGSL rejects
// duplicate top-level declarations, so C semantics would turn every diamond into
// a compile error the author hand-guards.
func TestDiamondIncludeSplicesOneCopy(t *testing.T) {
	files := map[string]string{
		"root.wgsl":   "#include ./a.wgsl\n#include ./b.wgsl\n",
		"a.wgsl":      "#include ./common.wgsl\nfn a() {}\n",
		"b.wgsl":      "#include ./common.wgsl\nfn b() {}\n",
		"common.wgsl": "fn common() {}\n",
	}
	text, _ := flatten(t, files, ShaderWithResource("root.wgsl"))
	if got := strings.Count(text, "fn common()"); got != 1 {
		t.Fatalf("the shared source was spliced %d times, want 1:\n%s", got, text)
	}
	if got, want := lineCount(text), 7; got != want {
		t.Errorf("flattened to %d lines, want %d - the sum of the four sources", got, want)
	}
}

// A cycle terminates on its own under include-once and is still an error: it is
// always an authoring mistake, and proceeding silently yields a module missing
// whichever half the cycle cut.
func TestCycleIsAnErrorCarryingTheChain(t *testing.T) {
	files := map[string]string{
		"root.wgsl": "#include ./a.wgsl\n",
		"a.wgsl":    "#include ./b.wgsl\n",
		"b.wgsl":    "#include ./a.wgsl\n",
	}
	err := flattenErr(t, files, ShaderWithResource("root.wgsl"))
	want := []ShaderLocation{
		{Source: "root.wgsl", Line: 1}, {Source: "a.wgsl", Line: 1}, {Source: "b.wgsl", Line: 1},
	}
	if len(err.At) != len(want) {
		t.Fatalf("At = %v, want the chain %v", err.At, want)
	}
	for i := range want {
		if err.At[i] != want[i] {
			t.Errorf("At[%d] = %v, want %v", i, err.At[i], want[i])
		}
	}
}

func TestMissingIncludeCarriesTheChain(t *testing.T) {
	files := map[string]string{
		"root.wgsl": "#include ./a.wgsl\n",
		"a.wgsl":    "#include ./gone.wgsl\n",
	}
	err := flattenErr(t, files, ShaderWithResource("root.wgsl"))
	if len(err.At) != 2 || err.At[1] != (ShaderLocation{Source: "a.wgsl", Line: 1}) {
		t.Fatalf("At = %v, want the chain ending at the failing #include", err.At)
	}
}

// Inline text carries source, not a path, so it has no directory. It is
// deliberately not treated as sitting at the storage root: either would make the
// same snippet resolve differently depending on whether it was pasted or loaded,
// a difference invisible in a diff.
func TestRelativeIncludeInInlineTextIsAnError(t *testing.T) {
	files := map[string]string{"lib/pbr.wgsl": "fn pbr() {}\n"}
	err := flattenErr(t, files, ShaderWithText("#include ./pbr.wgsl\n"))
	if !strings.Contains(err.Message, "#include") {
		t.Errorf("message = %q, want it to name the directive", err.Message)
	}
	text, _ := flatten(t, files, ShaderWithText("#include lib/pbr.wgsl\n"))
	if !strings.Contains(text, "fn pbr()") {
		t.Fatalf("an absolute include from inline text did not resolve: %q", text)
	}
}

// An included path resolves through the full mount overlay, exactly like the
// root source. A game that mounts its own copy of one included source replaces
// that source inside cog's module and keeps the rest - cog's customization
// mechanism working as designed. Pinning to the includer's mount would need a
// mount-scoped Open that FileSystem does not expose, and the flattener asks for
// no such thing: every source, root or included, is one Open on the FileSystem
// it was handed.
func TestIncludesResolveThroughTheMountOverlay(t *testing.T) {
	base := sourceFS(map[string]string{
		"builtin/root.wgsl": "#include ./a.wgsl\n#include ./b.wgsl\n",
		"builtin/a.wgsl":    "fn stock_a() {}\n",
		"builtin/b.wgsl":    "fn stock_b() {}\n",
	})
	override := sourceFS(map[string]string{"builtin/a.wgsl": "fn game_a() {}\n"})
	filesystem := storage.NewFileSystem("overlay", overlayFS{high: override, low: base})

	text, _, err := FlattenShader(filesystem, ShaderWithResource("builtin/root.wgsl"))
	if err != nil {
		t.Fatalf("FlattenShader: %v", err)
	}
	if !strings.Contains(text, "fn game_a()") || strings.Contains(text, "fn stock_a()") {
		t.Errorf("the higher-priority source did not replace the included one:\n%s", text)
	}
	if !strings.Contains(text, "fn stock_b()") {
		t.Errorf("the override reached a source it does not carry:\n%s", text)
	}
}

// overlayFS resolves per file by descending priority, which is what
// storage.FileSystem does across its mounts.
type overlayFS struct{ high, low fs.FS }

func (o overlayFS) Open(name string) (fs.File, error) {
	if file, err := o.high.Open(name); err == nil {
		return file, nil
	}
	return o.low.Open(name)
}

// ----- #137: #define and #const -----

// Collection completes before selection, so position is irrelevant in both
// directions: a #define at the bottom of a source governs an #if at its top,
// and there is no sequential pass in which "before" would mean anything.
func TestDeclarationsAreCollectedRegardlessOfPosition(t *testing.T) {
	files := map[string]string{
		"root.wgsl": "//#if LATE\nfn kept() {}\n//#endif\n//#include ./tail.wgsl\n",
		"tail.wgsl": "//#define LATE\n",
	}
	text, _ := flatten(t, files, ShaderWithResource("root.wgsl"))
	if !strings.Contains(text, "fn kept()") {
		t.Fatalf("a define declared below the conditional that reads it did not count:\n%s", text)
	}
}

func TestDefineTakesABareName(t *testing.T) {
	err := flattenErr(t, map[string]string{"s.wgsl": "#define A 1\n"}, ShaderWithResource("s.wgsl"))
	if !strings.Contains(err.Message, "never carries a value") {
		t.Errorf("message = %q", err.Message)
	}
	if len(err.At) != 1 {
		t.Errorf("At = %v, want one location", err.At)
	}
}

// Duplicate #define is allowed and silent: a define has no value, so "override"
// collapses to "set", and the set has a defined answer.
func TestDuplicateDefineIsSilent(t *testing.T) {
	files := map[string]string{
		"root.wgsl": "#define A\n#include ./a.wgsl\n//#if A\nfn kept() {}\n//#endif\n",
		"a.wgsl":    "#define A\n",
	}
	text, _ := flatten(t, files, ShaderWithResource("root.wgsl"))
	if !strings.Contains(text, "fn kept()") {
		t.Fatalf("a duplicate define changed the answer:\n%s", text)
	}
}

// A #const is declared exactly once across the flattened tree, whether or not
// the name is ever referenced: deciding "unreferenced" would mean reading WGSL,
// and both declarations are in hand either way.
func TestConstDeclaredTwiceIsAnErrorWithTwoLocations(t *testing.T) {
	files := map[string]string{
		"root.wgsl": "#const N=16\n#include ./a.wgsl\n",
		"a.wgsl":    "\n#const N=4\n",
	}
	err := flattenErr(t, files, ShaderWithResource("root.wgsl"))
	want := []ShaderLocation{{Source: "a.wgsl", Line: 2}, {Source: "root.wgsl", Line: 1}}
	if len(err.At) != 2 || err.At[0] != want[0] || err.At[1] != want[1] {
		t.Fatalf("At = %v, want the offending line first and the line it conflicts with second: %v", err.At, want)
	}
}

func TestMalformedConstIsAnError(t *testing.T) {
	for _, source := range []string{"#const N\n", "#const =16\n"} {
		err := flattenErr(t, map[string]string{"s.wgsl": source}, ShaderWithResource("s.wgsl"))
		if len(err.At) != 1 {
			t.Errorf("%q: At = %v, want one location", source, err.At)
		}
	}
}

// One namespace for both kinds: a name that is both makes "what is FOO"
// unanswerable at a glance, and the supply carries both kinds under one key.
func TestOneNamespaceForDefinesAndConsts(t *testing.T) {
	files := map[string]string{"s.wgsl": "#define N\n#const N=16\n"}
	err := flattenErr(t, files, ShaderWithResource("s.wgsl"))
	if len(err.At) != 2 {
		t.Fatalf("At = %v, want two locations", err.At)
	}
	// The supply is one of the two sides just as readily.
	declared := map[string]string{"s.wgsl": "#const N=16\nfn a() {}\n"}
	err = flattenErr(t, declared, ShaderWithResource("s.wgsl", ShaderDefine("N")))
	if len(err.At) != 2 {
		t.Fatalf("At = %v, want two locations with the supply as one side", err.At)
	}
}

// A #const compiles to a WGSL const injected in place, on the directive's own
// line, so the line count is preserved exactly and the source map needs no
// synthetic prologue. The value is text, read verbatim and never interpreted.
func TestConstInjectsAWgslConstInPlace(t *testing.T) {
	source := "//#const SCENE_MAX_LIGHTS=16\n//#const F0=vec3<f32>(0.04, 0.04, 0.04)\nfn a() {}\n"
	text, _ := flatten(t, map[string]string{"s.wgsl": source}, ShaderWithResource("s.wgsl"))
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if got, want := len(lines), 3; got != want {
		t.Fatalf("flattened to %d lines, want %d", got, want)
	}
	if lines[0] != "const SCENE_MAX_LIGHTS = 16;" {
		t.Errorf("line 1 = %q", lines[0])
	}
	if lines[1] != "const F0 = vec3<f32>(0.04, 0.04, 0.04);" {
		t.Errorf("line 2 = %q, want the value emitted verbatim", lines[1])
	}
}

// Go configures the one declaration; it never introduces a name. So an
// includee's #const is its default, in the Sass "@use ... with" sense, with no
// separate spelling needed to say so.
func TestSupplyOverridesTheDeclarationAndNeverIntroducesAName(t *testing.T) {
	files := map[string]string{"s.wgsl": "//#const N=16\nfn a() {}\n"}
	descr := ShaderWithResource("s.wgsl", ShaderConst("N", "4"), ShaderConst("NOBODY_DECLARES_THIS", "1"))
	text, _ := flatten(t, files, descr)
	if !strings.Contains(text, "const N = 4;") {
		t.Errorf("the supplied value did not override the declaration:\n%s", text)
	}
	if strings.Contains(text, "NOBODY_DECLARES_THIS") {
		t.Errorf("an unmatched supply key was injected:\n%s", text)
	}
}

// ----- #138: conditional compilation -----

// The payoff shape: one root, two supplies, two different declaration sets,
// both flattening to the same line count.
func TestConditionalCutsWhereverItIsAsked(t *testing.T) {
	source := strings.Join([]string{
		"//#if SKIN",
		"@group(0) @binding(2) var<storage, read> poses: array<vec4<f32>>;",
		"//#endif",
		"struct VertexIn {",
		"    @location(0) position: vec3<f32>,",
		"//#if SKIN",
		"    @location(6) joints: vec4<u32>,",
		"//#endif",
		"}",
		"fn deform() -> f32 {",
		"//#if SKIN",
		"    return 1.0;",
		"//#else",
		"    return 0.0;",
		"//#endif",
		"}",
		"",
	}, "\n")
	files := map[string]string{"s.wgsl": source}

	off, _ := flatten(t, files, ShaderWithResource("s.wgsl"))
	on, _ := flatten(t, files, ShaderWithResource("s.wgsl", ShaderDefine("SKIN")))

	if lineCount(off) != lineCount(on) || lineCount(on) != lineCount(source) {
		t.Fatalf("line counts diverged: %d, %d, source %d", lineCount(off), lineCount(on), lineCount(source))
	}
	if strings.Contains(off, "poses") || strings.Contains(off, "joints") {
		t.Errorf("the module-scope binding or the struct field survived with SKIN off:\n%s", off)
	}
	if !strings.Contains(off, "return 0.0;") || strings.Contains(off, "return 1.0;") {
		t.Errorf("the #else branch did not win with SKIN off:\n%s", off)
	}
	if !strings.Contains(on, "poses") || !strings.Contains(on, "joints") || !strings.Contains(on, "return 1.0;") {
		t.Errorf("the SKIN branch did not win with SKIN on:\n%s", on)
	}
}

// There is no precedence between & and |, so mixing them at one level does not
// compile rather than becoming a puzzle.
func TestMixedOperatorsMustBeParenthesised(t *testing.T) {
	err := flattenErr(t, map[string]string{"s.wgsl": "//#if A | B & C\n//#endif\n"}, ShaderWithResource("s.wgsl"))
	if !strings.Contains(err.Message, "parentheses") {
		t.Errorf("message = %q, want it to tell the author to parenthesise", err.Message)
	}
	for _, condition := range []string{"A | (B & C)", "(A | B) & C", "!A & B", "!(A | B)", "A"} {
		files := map[string]string{"s.wgsl": "//#if " + condition + "\nfn a() {}\n//#endif\n"}
		if _, _, err := FlattenShader(sources(files), ShaderWithResource("s.wgsl")); err != nil {
			t.Errorf("%q: %v", condition, err)
		}
	}
}

func TestConditionEvaluation(t *testing.T) {
	tests := []struct {
		condition string
		defines   []string
		want      bool
	}{
		{"A", []string{"A"}, true},
		{"A", nil, false},
		{"!A", nil, true},
		{"A & B", []string{"A"}, false},
		{"A & B", []string{"A", "B"}, true},
		{"A | B", []string{"B"}, true},
		{"SKIN | MORPH", nil, false},
		{"!A & B", []string{"B"}, true},
		{"A | (B & C)", []string{"B", "C"}, true},
		{"(A | B) & C", []string{"A"}, false},
	}
	for _, test := range tests {
		opts := make([]ShaderOption, 0, len(test.defines))
		for _, name := range test.defines {
			opts = append(opts, ShaderDefine(name))
		}
		files := map[string]string{"s.wgsl": "//#if " + test.condition + "\nfn kept() {}\n//#endif\n"}
		text, _ := flatten(t, files, ShaderWithResource("s.wgsl", opts...))
		if got := strings.Contains(text, "fn kept()"); got != test.want {
			t.Errorf("#if %s with %v = %v, want %v", test.condition, test.defines, got, test.want)
		}
	}
}

func TestEmptyConditionIsAnError(t *testing.T) {
	err := flattenErr(t, map[string]string{"s.wgsl": "//#if\n//#endif\n"}, ShaderWithResource("s.wgsl"))
	if len(err.At) != 1 || !strings.Contains(err.Message, "empty condition") {
		t.Errorf("At = %v, message = %q", err.At, err.Message)
	}
}

func TestConditionalsNestWithNoDepthLimit(t *testing.T) {
	var b strings.Builder
	const depth = 64
	for i := 0; i < depth; i++ {
		b.WriteString("//#if A\n")
	}
	b.WriteString("fn deep() {}\n")
	for i := 0; i < depth; i++ {
		b.WriteString("//#endif\n")
	}
	files := map[string]string{"s.wgsl": b.String()}
	on, _ := flatten(t, files, ShaderWithResource("s.wgsl", ShaderDefine("A")))
	if !strings.Contains(on, "fn deep()") {
		t.Error("a deeply nested live branch was cut")
	}
	off, _ := flatten(t, files, ShaderWithResource("s.wgsl"))
	if strings.Contains(off, "fn deep()") {
		t.Error("a deeply nested dead branch survived")
	}
}

// Inside a skipped branch the nesting structure is still tracked so matching
// works, and the sigil reservation still fires: reservation is lexical and does
// not depend on liveness.
func TestSkippedBranchesStillTrackNestingAndReserveSigils(t *testing.T) {
	files := map[string]string{"s.wgsl": "//#if OFF\n//#if OFF\n//#endif\n//#endif\nfn a() {}\n"}
	text, _ := flatten(t, files, ShaderWithResource("s.wgsl"))
	if !strings.Contains(text, "fn a()") {
		t.Fatalf("nesting inside a skipped branch was mismatched:\n%s", text)
	}
	err := flattenErr(t, map[string]string{"s.wgsl": "//#if OFF\n#pragma once\n//#endif\n"}, ShaderWithResource("s.wgsl"))
	if !strings.Contains(err.Message, "#pragma") {
		t.Errorf("message = %q, want the reservation to fire in dead text too", err.Message)
	}
}

func TestConditionalStructureErrors(t *testing.T) {
	tests := map[string]struct {
		source    string
		locations int
	}{
		"#endif with no open #if": {"//#endif\n", 1},
		"#else with no open #if":  {"//#else\n", 1},
		"#elif with no open #if":  {"//#elif A\n", 1},
		"#else after #else":       {"//#if A\n//#else\n//#else\n//#endif\n", 2},
		"#elif after #else":       {"//#if A\n//#else\n//#elif B\n//#endif\n", 2},
		"unclosed #if":            {"//#if A\nfn a() {}\n", 2},
	}
	for name, test := range tests {
		err := flattenErr(t, map[string]string{"s.wgsl": test.source}, ShaderWithResource("s.wgsl"))
		if len(err.At) != test.locations {
			t.Errorf("%s: At = %v, want %d locations", name, err.At, test.locations)
		}
	}
}

// Conditionals gate WGSL text and nothing else. That is what makes declarations
// never conditionally present, makes the include graph a pure function of the
// root, and makes every #if/#endif pair open and close in one source.
func TestDeclarationsMayNotSitInsideAConditional(t *testing.T) {
	for _, directive := range []string{"#include ./a.wgsl", "#define A", "#const N=1"} {
		files := map[string]string{
			"s.wgsl": "//#if X\n" + directive + "\n//#endif\n",
			"a.wgsl": "fn a() {}\n",
		}
		err := flattenErr(t, files, ShaderWithResource("s.wgsl"))
		if len(err.At) != 2 {
			t.Errorf("%s: At = %v, want two locations", directive, err.At)
		}
	}
}

// ----- #139: WGSL §4 directives -----

func TestEnableAndRequiresUnionAcrossSources(t *testing.T) {
	files := map[string]string{
		"root.wgsl": "enable a, b;\nrequires r1;\n#include ./x.wgsl\nfn main() {}\n",
		"x.wgsl":    "enable b, c;\nfn x() {}\n",
	}
	text, sourceMap := flatten(t, files, ShaderWithResource("root.wgsl"))
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if lines[0] != "enable a, b, c;" {
		t.Errorf("line 1 = %q, want one canonical enable naming the union", lines[0])
	}
	if lines[1] != "requires r1;" {
		t.Errorf("line 2 = %q", lines[1])
	}
	// Every hoisted line blanks at its original site, so the source it came from
	// keeps its line count and the module grows by exactly the prologue.
	if got, want := lineCount(text), 6+2; got != want {
		t.Errorf("flattened to %d lines, want %d", got, want)
	}
	first := sourceMap.Segments[0]
	if first.Source != "" || first.Length != 2 || first.OutputStart != 1 {
		t.Errorf("prologue segment = %+v, want two lines with no source correspondence", first)
	}
	if sourceMap.Segments[1].OutputStart != 3 {
		t.Errorf("the segments after the prologue were not shifted: %+v", sourceMap.Segments[1])
	}
}

// A source with no §4 directives produces a zero-line prologue, so the output
// line count equals the sum of its sources'.
func TestNoDirectivesMeansNoPrologue(t *testing.T) {
	files := map[string]string{"s.wgsl": "fn a() {}\n"}
	_, sourceMap := flatten(t, files, ShaderWithResource("s.wgsl"))
	if sourceMap.Segments[0].Source == "" {
		t.Fatalf("a prologue segment was emitted for a source with no §4 directives: %+v", sourceMap.Segments)
	}
}

// Recognition happens only in a source's prologue, which is WGSL's own placement
// rule restated. A directive after real content was already invalid standalone
// WGSL, and the preprocessor does not rescue it.
func TestDirectivesAreRecognisedOnlyInThePrologue(t *testing.T) {
	files := map[string]string{"s.wgsl": "//#const N=1\n\n// a comment\nenable f16;\nfn a() {}\nenable f32;\n"}
	text, _ := flatten(t, files, ShaderWithResource("s.wgsl"))
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if lines[0] != "enable f16;" {
		t.Errorf("line 1 = %q; a #const line must not end the prologue", lines[0])
	}
	if strings.Count(text, "enable f32;") != 1 || !strings.HasSuffix(strings.TrimSuffix(text, "\n"), "enable f32;") {
		t.Errorf("the directive after real content was hoisted rather than left alone:\n%s", text)
	}
}

// @diagnostic(...) as a statement or function attribute never matches, because
// it starts with "@".
func TestDiagnosticAttributeIsNeverHoisted(t *testing.T) {
	files := map[string]string{"s.wgsl": "fn a() {\n  @diagnostic(off, derivative_uniformity)\n  let x = 1;\n}\n"}
	text, _ := flatten(t, files, ShaderWithResource("s.wgsl"))
	if !strings.Contains(text, "  @diagnostic(off, derivative_uniformity)") {
		t.Fatalf("a function attribute was hoisted:\n%s", text)
	}
}

func TestDiagnosticLinesDeduplicateByText(t *testing.T) {
	files := map[string]string{
		"root.wgsl": "diagnostic(off,  derivative_uniformity);\n#include ./x.wgsl\nfn main() {}\n",
		"x.wgsl":    "diagnostic(off, derivative_uniformity);\nfn x() {}\n",
	}
	text, _ := flatten(t, files, ShaderWithResource("root.wgsl"))
	if got := strings.Count(text, "diagnostic("); got != 1 {
		t.Fatalf("emitted %d diagnostic lines, want 1:\n%s", got, text)
	}
}

// WGSL makes two diagnostics naming one rule with different severities an error;
// the preprocessor manufactures it, since unflattened the two sources are
// separate modules with no conflict at all.
func TestConflictingDiagnosticSeveritiesIsAnError(t *testing.T) {
	files := map[string]string{
		"root.wgsl": "diagnostic(off, derivative_uniformity);\n#include ./x.wgsl\n",
		"x.wgsl":    "diagnostic(error, derivative_uniformity);\n",
	}
	err := flattenErr(t, files, ShaderWithResource("root.wgsl"))
	if len(err.At) != 2 {
		t.Fatalf("At = %v, want two locations", err.At)
	}
	if !strings.Contains(err.Message, "derivative_uniformity") {
		t.Errorf("message = %q, want it to name the rule", err.Message)
	}
}

// requires is hoisted exactly like the other two even though gogpu/naga does not
// implement it. Rejecting it here would encode one backend's current gap into
// this repo's language spec.
func TestRequiresIsHoistedWithNoSpecialCase(t *testing.T) {
	files := map[string]string{"s.wgsl": "requires readonly_and_readwrite_storage_textures;\nfn a() {}\n"}
	text, _ := flatten(t, files, ShaderWithResource("s.wgsl"))
	if !strings.HasPrefix(text, "requires readonly_and_readwrite_storage_textures;\n") {
		t.Fatalf("requires was not hoisted:\n%s", text)
	}
}

// ----- the shaders in the tree today -----

// The sigil reservation costs no migration: a shader that uses no directive
// flattens to itself, comments blanked and nothing else touched. The canvas
// sources that declare no directive are the standing measurement - real shader
// text this package never designed around - so the mechanism must not disturb a
// source that has not opted in.
func TestAnUnmigratedShaderFlattensUnchanged(t *testing.T) {
	for _, name := range unmigratedShaderNames(t) {
		text := readBuiltinShader(t, name)
		files := map[string]string{name: text}
		flattened, _ := flatten(t, files, ShaderWithResource(name))
		if got, want := lineCount(flattened), lineCount(text); got != want {
			t.Errorf("%s: flattened to %d lines, want %d", name, got, want)
		}
		want := blankedLines(text)
		for i, got := range flattenedLines(flattened) {
			if got != want[i] {
				t.Errorf("%s:%d flattened to %q, want %q", name, i+1, got, want[i])
				break
			}
		}
	}
}

// unmigratedShaderNames lists the .wgsl files that use no directive, read off
// disk rather than through a package's embed so that gfx tests can measure them
// without importing canvas. Canvas's artwork shaders now share their key-colour
// ramp by #include, so the set is filtered rather than assumed: what is left is
// the sources that opted out, and the measurement is only meaningful while at
// least one of them exists.
func unmigratedShaderNames(t *testing.T) []string {
	t.Helper()
	globbed, err := filepath.Glob(filepath.Join("..", "canvas", "builtin", "canvas", "*.wgsl"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	var names []string
	for _, name := range globbed {
		if !strings.Contains(readBuiltinShader(t, name), "//#") {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		t.Fatal("found no unmigrated .wgsl files in the tree")
	}
	return names
}

func readBuiltinShader(t *testing.T, name string) string {
	t.Helper()
	text, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(text)
}

// blankedLines is the source as the lexical layer alone leaves it: comments
// gone, nothing else touched. A shader with no directives must flatten to
// exactly that.
func blankedLines(text string) []string {
	lines := blankComments(text)
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " 	")
	}
	return lines
}

func flattenedLines(text string) []string {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " 	")
	}
	return lines
}
