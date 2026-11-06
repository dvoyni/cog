package shader_test

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/gfx/shader"
)

// Ported from v1's preprocessor, _legacy/slots/gfx/internal/shader, with the
// source map left out: Preprocess answers text alone.

// ----- comments, sigils and errors -----

// The preprocessor preserves lines: comments blank to spaces rather than being
// deleted, so every line number a backend reports stays accurate.
func TestPreprocessingPreservesLinesAndBlanksComments(t *testing.T) {
	source := "fn a() {} // trailing\n/* whole line */\nfn b() {} /* mid */ let x = 1;\n"
	lines := preprocessLines(t, map[string]string{"s.wgsl": source}, "s.wgsl")

	if got, want := len(lines), lineCount(source); got != want {
		t.Fatalf("preprocessed to %d lines, want %d", got, want)
	}
	if lines[0] != "fn a() {}            " {
		t.Errorf("line 1 = %q, want the comment blanked to spaces", lines[0])
	}
	if strings.TrimSpace(lines[1]) != "" {
		t.Errorf("line 2 = %q, want a whole-line comment blanked", lines[1])
	}
	if lines[2] != "fn b() {}           let x = 1;" {
		t.Errorf("line 3 = %q, want the block comment blanked in place", lines[2])
	}
}

// A block comment is always a comment and is never unwrapped, which makes it
// the reliable way to disable a region wholesale, directives inside it too.
func TestABlockCommentDisablesARegionWholesale(t *testing.T) {
	text := preprocess(t, map[string]string{"s.wgsl": "/*\n#if X\n#pragma once\n*/\nfn a() {}\n"}, "s.wgsl")
	if strings.Contains(text, "pragma") {
		t.Fatalf("a commented-out directive survived: %q", text)
	}
}

// Position is measured after comments are blanked, so a directive behind a
// block comment is live rather than passed on to the WGSL lexer.
func TestPositionIsMeasuredAfterBlanking(t *testing.T) {
	text := preprocess(t, map[string]string{"s.wgsl": "/* c */ #if X\nfn cut() {}\n#endif\nfn keep() {}\n"}, "s.wgsl")
	if strings.Contains(text, "fn cut") {
		t.Fatalf("the directive behind a block comment was not recognised: %q", text)
	}
	if !strings.Contains(text, "fn keep") {
		t.Fatalf("the live text was cut: %q", text)
	}
}

// Both sigils are one directive. The //# form keeps a source valid WGSL for
// analyzers, formatters and highlighters.
func TestBothSigilsAreOneDirective(t *testing.T) {
	for _, sigil := range []string{"#", "//#"} {
		text := preprocess(t, map[string]string{"s.wgsl": "  " + sigil + "if X\nfn cut() {}\n" + sigil + "endif\n"}, "s.wgsl")
		if strings.Contains(text, "fn cut") {
			t.Errorf("%q was not recognised as a directive: %q", sigil, text)
		}
	}
}

// A line-start sigil followed by an unknown word fails loudly rather than
// becoming a WGSL lexer error in text nobody wrote.
func TestAnUnknownDirectiveNamesTheWord(t *testing.T) {
	tests := map[string]string{
		"#pragma once":     "#pragma",
		"#includ ./x.wgsl": "#includ",
		"//#TODO":          "#TODO",
		"# include x.wgsl": "no space",
	}
	for source, want := range tests {
		err := preprocessErr(t, map[string]string{"s.wgsl": source + "\n"}, "s.wgsl")
		if !strings.Contains(err.Message, want) {
			t.Errorf("%q: message %q does not name %q", source, err.Message, want)
		}
		if len(err.At) != 1 || err.At[0] != (shader.Location{Source: "s.wgsl", Line: 1}) {
			t.Errorf("%q: At = %v, want one location at s.wgsl:1", source, err.At)
		}
	}
}

// What an editor's comment toggle produces is inert: a directive commented out
// is one the author disabled.
func TestCommentedOutDirectivesAreInert(t *testing.T) {
	text := preprocess(t, map[string]string{"s.wgsl": "// #if X\n// //#if X\nlet x = 1; //#todo\nfn a() {}\n"}, "s.wgsl")
	if !strings.Contains(text, "fn a()") || !strings.Contains(text, "let x = 1;") {
		t.Fatalf("an inert line was treated as a directive: %q", text)
	}
}

// Nothing recovers from a preprocessor error, so it reads as one printed line
// naming the shader and every location.
func TestErrShaderSourceReadsAsOneLine(t *testing.T) {
	err := shader.ErrSource{
		Shader:  "scene.wgsl [SKIN]",
		At:      []shader.Location{{Source: "a.wgsl", Line: 4}, {Source: "b.wgsl", Line: 9}},
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

// The error names the shader by its path and its defines, sorted by name, a
// duplicate name resolved last-wins: one path under two sets of defines is two
// shaders.
func TestTheErrorNamesTheShaderWithItsDefines(t *testing.T) {
	err := preprocessErr(t, map[string]string{"s.wgsl": "#pragma once\n"}, "s.wgsl",
		shader.Define{Name: "SKIN"}, shader.Define{Name: "N", Value: "4"}, shader.Define{Name: "N", Value: "8"})
	if got, want := err.Shader, "s.wgsl [N=8 SKIN]"; got != want {
		t.Errorf("Shader = %q, want %q", got, want)
	}
	err = preprocessErr(t, map[string]string{"s.wgsl": "#pragma once\n"}, "s.wgsl")
	if got, want := err.Shader, "s.wgsl"; got != want {
		t.Errorf("Shader = %q, want %q", got, want)
	}
}

// A define with no legal spelling has no source location, and an empty At is
// that: Shader already carries the defines.
func TestAMalformedDefineIsReportedWithNoLocation(t *testing.T) {
	files := map[string]string{"s.wgsl": "fn a() {}\n"}
	for _, define := range []shader.Define{{Name: ""}, {Name: "A", Value: "x\ny"}} {
		err := preprocessErr(t, files, "s.wgsl", define)
		if len(err.At) != 0 {
			t.Errorf("%+v: At = %v, want empty", define, err.At)
		}
		if err.Shader == "" {
			t.Errorf("%+v: Shader is empty, so the message cannot name the defines", define)
		}
	}
}

func TestAMissingRootIsAnError(t *testing.T) {
	err := preprocessErr(t, map[string]string{}, "gone.wgsl")
	if !strings.Contains(err.Message, "not found") {
		t.Errorf("message = %q, want it to say the root is not found", err.Message)
	}
}

// ----- includes -----

// A path beginning "./" or "../" is relative to the including source;
// anything else names a path in the filesystem as it is. The consumed #include
// line blanks, so the output is the sum of its sources' lines.
func TestIncludeResolvesBothPathForms(t *testing.T) {
	files := map[string]string{
		"builtin/model/scene.wgsl": "//#include ./pbr.wgsl\n//#include builtin/lib/frame.wgsl\nfn main() {}\n",
		"builtin/model/pbr.wgsl":   "fn pbr() {}\n",
		"builtin/lib/frame.wgsl":   "fn frame() {}\n",
	}
	lines := preprocessLines(t, files, "builtin/model/scene.wgsl")
	want := []string{"", "fn pbr() {}", "", "fn frame() {}", "fn main() {}"}
	assertTrimmedLines(t, lines, want)
}

// The path is read unquoted to the end of the line, which composes with
// comments: a path can never hold "//", since fs.ValidPath forbids empty
// elements.
func TestAnIncludePathReadsToTheEndOfTheLine(t *testing.T) {
	files := map[string]string{
		"a/root.wgsl": "#include ./pbr.wgsl // shared\n",
		"a/pbr.wgsl":  "fn pbr() {}\n",
	}
	if text := preprocess(t, files, "a/root.wgsl"); !strings.Contains(text, "fn pbr()") {
		t.Fatalf("a trailing comment defeated the path: %q", text)
	}
}

// ".." is resolved before Open, which could not take it: fs.ValidPath forbids
// "." and ".." elements.
func TestAnIncludeIsCleanedBeforeOpen(t *testing.T) {
	files := map[string]string{
		"a/b/root.wgsl":  "#include ../lib/pbr.wgsl\n",
		"a/lib/pbr.wgsl": "fn pbr() {}\n",
	}
	if text := preprocess(t, files, "a/b/root.wgsl"); !strings.Contains(text, "fn pbr()") {
		t.Fatalf("a climbing path did not resolve: %q", text)
	}
}

// The filesystem's root is the only boundary, and a path climbing above it is
// an error rather than a silent clamp.
func TestAnIncludeAboveTheRootIsAnError(t *testing.T) {
	err := preprocessErr(t, map[string]string{"root.wgsl": "#include ../outside.wgsl\n"}, "root.wgsl")
	if !strings.Contains(err.Message, "climbs above") {
		t.Errorf("message = %q, want it to name the root boundary", err.Message)
	}
}

func TestAnEmptyIncludePathIsAnError(t *testing.T) {
	err := preprocessErr(t, map[string]string{"root.wgsl": "#include\n"}, "root.wgsl")
	if len(err.At) != 1 {
		t.Fatalf("At = %v, want one location", err.At)
	}
	if !strings.Contains(err.Message, "empty path") {
		t.Errorf("message = %q", err.Message)
	}
}

// A source is spliced once: WGSL rejects duplicate top-level declarations, so
// splicing it twice would make every diamond a compile error.
func TestADiamondIncludeSplicesOneCopy(t *testing.T) {
	files := map[string]string{
		"root.wgsl":   "#include ./a.wgsl\n#include ./b.wgsl\n",
		"a.wgsl":      "#include ./common.wgsl\nfn a() {}\n",
		"b.wgsl":      "#include ./common.wgsl\nfn b() {}\n",
		"common.wgsl": "fn common() {}\n",
	}
	text := preprocess(t, files, "root.wgsl")
	if got := strings.Count(text, "fn common()"); got != 1 {
		t.Fatalf("the shared source was spliced %d times, want 1:\n%s", got, text)
	}
	if got, want := lineCount(text), 7; got != want {
		t.Errorf("preprocessed to %d lines, want %d, the sum of the four sources", got, want)
	}
}

// A cycle is an error carrying the chain of #include lines: it is always a
// mistake, and going on would leave out whichever half the cycle cut.
func TestACycleIsAnErrorCarryingTheChain(t *testing.T) {
	files := map[string]string{
		"root.wgsl": "#include ./a.wgsl\n",
		"a.wgsl":    "#include ./b.wgsl\n",
		"b.wgsl":    "#include ./a.wgsl\n",
	}
	err := preprocessErr(t, files, "root.wgsl")
	want := []shader.Location{{Source: "root.wgsl", Line: 1}, {Source: "a.wgsl", Line: 1}, {Source: "b.wgsl", Line: 1}}
	assertLocations(t, err.At, want)
}

func TestAMissingIncludeCarriesTheChain(t *testing.T) {
	files := map[string]string{
		"root.wgsl": "#include ./a.wgsl\n",
		"a.wgsl":    "#include ./gone.wgsl\n",
	}
	err := preprocessErr(t, files, "root.wgsl")
	want := []shader.Location{{Source: "root.wgsl", Line: 1}, {Source: "a.wgsl", Line: 1}}
	assertLocations(t, err.At, want)
}

// Every source, the root or an include, is one Open on the filesystem given,
// so a filesystem that layers mounts replaces one included source and keeps
// the rest.
func TestIncludesOpenThroughTheFilesystemGiven(t *testing.T) {
	stock := sourceFiles(map[string]string{
		"builtin/root.wgsl": "#include ./a.wgsl\n#include ./b.wgsl\n",
		"builtin/a.wgsl":    "fn stock_a() {}\n",
		"builtin/b.wgsl":    "fn stock_b() {}\n",
	})
	game := sourceFiles(map[string]string{"builtin/a.wgsl": "fn game_a() {}\n"})
	text, err := shader.Preprocess(layeredFS{high: game, low: stock}, "builtin/root.wgsl", nil)
	if err != nil {
		t.Fatalf("Preprocess: %v", err)
	}
	if !strings.Contains(text, "fn game_a()") || strings.Contains(text, "fn stock_a()") {
		t.Errorf("the higher layer did not replace the included source:\n%s", text)
	}
	if !strings.Contains(text, "fn stock_b()") {
		t.Errorf("the higher layer reached a source it does not hold:\n%s", text)
	}
}

// ----- #define and #const -----

// Every declaration is collected before any branch is chosen, so a #define
// below the #if that reads it counts.
func TestDeclarationsAreCollectedRegardlessOfPosition(t *testing.T) {
	files := map[string]string{
		"root.wgsl": "//#if LATE\nfn kept() {}\n//#endif\n//#include ./tail.wgsl\n",
		"tail.wgsl": "//#define LATE\n",
	}
	if text := preprocess(t, files, "root.wgsl"); !strings.Contains(text, "fn kept()") {
		t.Fatalf("a define declared below the conditional that reads it did not count:\n%s", text)
	}
}

func TestADefineTakesABareName(t *testing.T) {
	err := preprocessErr(t, map[string]string{"s.wgsl": "#define A 1\n"}, "s.wgsl")
	if !strings.Contains(err.Message, "never carries a value") {
		t.Errorf("message = %q", err.Message)
	}
	if len(err.At) != 1 {
		t.Errorf("At = %v, want one location", err.At)
	}
}

// A duplicate #define is silent: a define has no value, so defining it again
// changes nothing.
func TestADuplicateDefineIsSilent(t *testing.T) {
	files := map[string]string{
		"root.wgsl": "#define A\n#include ./a.wgsl\n//#if A\nfn kept() {}\n//#endif\n",
		"a.wgsl":    "#define A\n",
	}
	if text := preprocess(t, files, "root.wgsl"); !strings.Contains(text, "fn kept()") {
		t.Fatalf("a duplicate define changed the answer:\n%s", text)
	}
}

// A #const is declared exactly once across the sources, referenced or not.
func TestAConstDeclaredTwiceIsAnErrorAtBothLines(t *testing.T) {
	files := map[string]string{
		"root.wgsl": "#const N=16\n#include ./a.wgsl\n",
		"a.wgsl":    "\n#const N=4\n",
	}
	err := preprocessErr(t, files, "root.wgsl")
	assertLocations(t, err.At, []shader.Location{{Source: "a.wgsl", Line: 2}, {Source: "root.wgsl", Line: 1}})
}

func TestAMalformedConstIsAnError(t *testing.T) {
	for _, source := range []string{"#const N\n", "#const =16\n"} {
		err := preprocessErr(t, map[string]string{"s.wgsl": source}, "s.wgsl")
		if len(err.At) != 1 {
			t.Errorf("%q: At = %v, want one location", source, err.At)
		}
	}
}

// Defines and consts share one namespace, in the sources and in the defines
// given alike.
func TestDefinesAndConstsShareOneNamespace(t *testing.T) {
	err := preprocessErr(t, map[string]string{"s.wgsl": "#define N\n#const N=16\n"}, "s.wgsl")
	if len(err.At) != 2 {
		t.Fatalf("At = %v, want two locations", err.At)
	}
	declared := map[string]string{"s.wgsl": "#const N=16\nfn a() {}\n"}
	err = preprocessErr(t, declared, "s.wgsl", shader.Define{Name: "N"})
	if len(err.At) != 2 {
		t.Fatalf("At = %v, want two locations, the defines given as one", err.At)
	}
	flagged := map[string]string{"s.wgsl": "#define N\nfn a() {}\n"}
	err = preprocessErr(t, flagged, "s.wgsl", shader.Define{Name: "N", Value: "4"})
	if len(err.At) != 2 {
		t.Fatalf("At = %v, want two locations, the defines given as one", err.At)
	}
}

// A #const compiles to a WGSL const on the directive's own line, so the line
// count holds. The value is text, never interpreted.
func TestAConstBecomesAWGSLConstInPlace(t *testing.T) {
	source := "//#const SCENE_MAX_LIGHTS=16\n//#const F0=vec3<f32>(0.04, 0.04, 0.04)\nfn a() {}\n"
	lines := preprocessLines(t, map[string]string{"s.wgsl": source}, "s.wgsl")
	if got, want := len(lines), 3; got != want {
		t.Fatalf("preprocessed to %d lines, want %d", got, want)
	}
	if lines[0] != "const SCENE_MAX_LIGHTS = 16;" {
		t.Errorf("line 1 = %q", lines[0])
	}
	if lines[1] != "const F0 = vec3<f32>(0.04, 0.04, 0.04);" {
		t.Errorf("line 2 = %q, want the value emitted verbatim", lines[1])
	}
}

// A define given with a value overrides the #const of its name, and introduces
// no name a source does not declare.
func TestAValueOverridesTheConstAndNeverIntroducesAName(t *testing.T) {
	files := map[string]string{"s.wgsl": "//#const N=16\nfn a() {}\n"}
	text := preprocess(t, files, "s.wgsl",
		shader.Define{Name: "N", Value: "4"}, shader.Define{Name: "NOBODY_DECLARES_THIS", Value: "1"})
	if !strings.Contains(text, "const N = 4;") {
		t.Errorf("the value given did not override the declaration:\n%s", text)
	}
	if strings.Contains(text, "NOBODY_DECLARES_THIS") {
		t.Errorf("a value for a name no source declares was injected:\n%s", text)
	}
}

// ----- conditionals -----

// One source under two sets of defines declares two sets of bindings, both in
// as many lines as the source.
func TestAConditionalCutsWhereverItIsAsked(t *testing.T) {
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

	off := preprocess(t, files, "s.wgsl")
	on := preprocess(t, files, "s.wgsl", shader.Define{Name: "SKIN"})

	if lineCount(off) != lineCount(on) || lineCount(on) != lineCount(source) {
		t.Fatalf("line counts diverged: %d, %d, source %d", lineCount(off), lineCount(on), lineCount(source))
	}
	if strings.Contains(off, "poses") || strings.Contains(off, "joints") {
		t.Errorf("the binding or the struct field survived with SKIN off:\n%s", off)
	}
	if !strings.Contains(off, "return 0.0;") || strings.Contains(off, "return 1.0;") {
		t.Errorf("the #else branch did not win with SKIN off:\n%s", off)
	}
	if !strings.Contains(on, "poses") || !strings.Contains(on, "joints") || !strings.Contains(on, "return 1.0;") {
		t.Errorf("the SKIN branch did not win with SKIN on:\n%s", on)
	}
}

// & and | have no precedence between them, so mixing them at one level does
// not parse.
func TestMixedOperatorsMustBeParenthesised(t *testing.T) {
	err := preprocessErr(t, map[string]string{"s.wgsl": "//#if A | B & C\n//#endif\n"}, "s.wgsl")
	if !strings.Contains(err.Message, "parentheses") {
		t.Errorf("message = %q, want it to tell the author to parenthesise", err.Message)
	}
	for _, condition := range []string{"A | (B & C)", "(A | B) & C", "!A & B", "!(A | B)", "A"} {
		files := sourceFiles(map[string]string{"s.wgsl": "//#if " + condition + "\nfn a() {}\n//#endif\n"})
		if _, err := shader.Preprocess(files, "s.wgsl", nil); err != nil {
			t.Errorf("%q: %v", condition, err)
		}
	}
}

func TestConditionsEvaluateOverTheDefines(t *testing.T) {
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
		defines := make([]shader.Define, 0, len(test.defines))
		for _, name := range test.defines {
			defines = append(defines, shader.Define{Name: name})
		}
		files := map[string]string{"s.wgsl": "//#if " + test.condition + "\nfn kept() {}\n//#endif\n"}
		text := preprocess(t, files, "s.wgsl", defines...)
		if got := strings.Contains(text, "fn kept()"); got != test.want {
			t.Errorf("#if %s with %v = %v, want %v", test.condition, test.defines, got, test.want)
		}
	}
}

func TestAnEmptyConditionIsAnError(t *testing.T) {
	err := preprocessErr(t, map[string]string{"s.wgsl": "//#if\n//#endif\n"}, "s.wgsl")
	if len(err.At) != 1 || !strings.Contains(err.Message, "empty condition") {
		t.Errorf("At = %v, message = %q", err.At, err.Message)
	}
}

func TestConditionalsNestWithNoDepthLimit(t *testing.T) {
	var builder strings.Builder
	const depth = 64
	for range depth {
		builder.WriteString("//#if A\n")
	}
	builder.WriteString("fn deep() {}\n")
	for range depth {
		builder.WriteString("//#endif\n")
	}
	files := map[string]string{"s.wgsl": builder.String()}
	if on := preprocess(t, files, "s.wgsl", shader.Define{Name: "A"}); !strings.Contains(on, "fn deep()") {
		t.Error("a deeply nested live branch was cut")
	}
	if off := preprocess(t, files, "s.wgsl"); strings.Contains(off, "fn deep()") {
		t.Error("a deeply nested dead branch survived")
	}
}

// A skipped branch still tracks nesting, so its #endif lines match, and still
// reserves the sigils: the reservation is lexical.
func TestSkippedBranchesStillTrackNestingAndReserveSigils(t *testing.T) {
	text := preprocess(t, map[string]string{"s.wgsl": "//#if OFF\n//#if OFF\n//#endif\n//#endif\nfn a() {}\n"}, "s.wgsl")
	if !strings.Contains(text, "fn a()") {
		t.Fatalf("nesting inside a skipped branch was mismatched:\n%s", text)
	}
	err := preprocessErr(t, map[string]string{"s.wgsl": "//#if OFF\n#pragma once\n//#endif\n"}, "s.wgsl")
	if !strings.Contains(err.Message, "#pragma") {
		t.Errorf("message = %q, want the reservation to hold in a skipped branch too", err.Message)
	}
}

func TestConditionalStructureErrorsNameTheirLines(t *testing.T) {
	tests := map[string]struct {
		source string
		want   []shader.Location
	}{
		"#endif with no open #if": {"//#endif\n", []shader.Location{{Source: "s.wgsl", Line: 1}}},
		"#else with no open #if":  {"//#else\n", []shader.Location{{Source: "s.wgsl", Line: 1}}},
		"#elif with no open #if":  {"//#elif A\n", []shader.Location{{Source: "s.wgsl", Line: 1}}},
		"#else after #else": {"//#if A\n//#else\n//#else\n//#endif\n",
			[]shader.Location{{Source: "s.wgsl", Line: 3}, {Source: "s.wgsl", Line: 2}}},
		"#elif after #else": {"//#if A\n//#else\n//#elif B\n//#endif\n",
			[]shader.Location{{Source: "s.wgsl", Line: 3}, {Source: "s.wgsl", Line: 2}}},
		"unclosed #if": {"//#if A\nfn a() {}\n",
			[]shader.Location{{Source: "s.wgsl", Line: 2}, {Source: "s.wgsl", Line: 1}}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := preprocessErr(t, map[string]string{"s.wgsl": test.source}, "s.wgsl")
			assertLocations(t, err.At, test.want)
		})
	}
}

// Conditionals gate WGSL text and nothing else, so declarations are never
// conditionally present and every #if closes in the source that opened it.
func TestDeclarationsMayNotSitInsideAConditional(t *testing.T) {
	for _, directive := range []string{"#include ./a.wgsl", "#define A", "#const N=1"} {
		files := map[string]string{
			"s.wgsl": "//#if X\n" + directive + "\n//#endif\n",
			"a.wgsl": "fn a() {}\n",
		}
		err := preprocessErr(t, files, "s.wgsl")
		assertLocations(t, err.At, []shader.Location{{Source: "s.wgsl", Line: 2}, {Source: "s.wgsl", Line: 1}})
	}
}

// ----- WGSL's enable, requires and diagnostic directives -----

// WGSL wants these before every declaration, which splicing breaks, so they
// are hoisted to the top: enable and requires as one line each, naming the
// union, and each blanked where it was.
func TestEnableAndRequiresAreHoistedAsTheirUnion(t *testing.T) {
	files := map[string]string{
		"root.wgsl": "enable a, b;\nrequires r1;\n#include ./x.wgsl\nfn main() {}\n",
		"x.wgsl":    "enable b, c;\nfn x() {}\n",
	}
	lines := preprocessLines(t, files, "root.wgsl")
	want := []string{"enable a, b, c;", "requires r1;", "", "", "", "", "fn x() {}", "fn main() {}"}
	assertTrimmedLines(t, lines, want)
}

// A source with none of them gains no lines.
func TestNoDirectivesMeansNoPrologue(t *testing.T) {
	source := "fn a() {}\n"
	if got, want := lineCount(preprocess(t, map[string]string{"s.wgsl": source}, "s.wgsl")), lineCount(source); got != want {
		t.Fatalf("preprocessed to %d lines, want %d", got, want)
	}
}

// They are recognised only in a source's prologue, as WGSL places them: one
// after real content was already invalid WGSL, and is left as it is.
func TestDirectivesAreRecognisedOnlyInThePrologue(t *testing.T) {
	text := preprocess(t, map[string]string{"s.wgsl": "//#const N=1\n\n// a comment\nenable f16;\nfn a() {}\nenable f32;\n"}, "s.wgsl")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if lines[0] != "enable f16;" {
		t.Errorf("line 1 = %q; a #const line must not end the prologue", lines[0])
	}
	if strings.Count(text, "enable f32;") != 1 || !strings.HasSuffix(strings.TrimSuffix(text, "\n"), "enable f32;") {
		t.Errorf("the directive after real content was hoisted rather than left alone:\n%s", text)
	}
}

// @diagnostic(...) as a statement or function attribute starts with "@", and
// is never hoisted.
func TestADiagnosticAttributeIsNeverHoisted(t *testing.T) {
	text := preprocess(t, map[string]string{"s.wgsl": "fn a() {\n  @diagnostic(off, derivative_uniformity)\n  let x = 1;\n}\n"}, "s.wgsl")
	if !strings.Contains(text, "  @diagnostic(off, derivative_uniformity)") {
		t.Fatalf("a function attribute was hoisted:\n%s", text)
	}
}

func TestDiagnosticLinesDeduplicateByText(t *testing.T) {
	files := map[string]string{
		"root.wgsl": "diagnostic(off,  derivative_uniformity);\n#include ./x.wgsl\nfn main() {}\n",
		"x.wgsl":    "diagnostic(off, derivative_uniformity);\nfn x() {}\n",
	}
	if text := preprocess(t, files, "root.wgsl"); strings.Count(text, "diagnostic(") != 1 {
		t.Fatalf("emitted %d diagnostic lines, want 1:\n%s", strings.Count(text, "diagnostic("), text)
	}
}

// Two diagnostics naming one rule with different severities are an error
// WGSL makes and splicing manufactures: apart, the two sources do not conflict.
func TestConflictingDiagnosticSeveritiesAreAnError(t *testing.T) {
	files := map[string]string{
		"root.wgsl": "diagnostic(off, derivative_uniformity);\n#include ./x.wgsl\n",
		"x.wgsl":    "diagnostic(error, derivative_uniformity);\n",
	}
	err := preprocessErr(t, files, "root.wgsl")
	assertLocations(t, err.At, []shader.Location{{Source: "x.wgsl", Line: 1}, {Source: "root.wgsl", Line: 1}})
	if !strings.Contains(err.Message, "derivative_uniformity") {
		t.Errorf("message = %q, want it to name the rule", err.Message)
	}
}

// requires is hoisted like the other two though naga does not implement it:
// rejecting it would write one backend's gap into the language.
func TestRequiresIsHoistedWithNoSpecialCase(t *testing.T) {
	text := preprocess(t, map[string]string{"s.wgsl": "requires readonly_and_readwrite_storage_textures;\nfn a() {}\n"}, "s.wgsl")
	if !strings.HasPrefix(text, "requires readonly_and_readwrite_storage_textures;\n") {
		t.Fatalf("requires was not hoisted:\n%s", text)
	}
}

// preprocess answers the preprocessed text of path in files, failing the test
// on an error.
func preprocess(t *testing.T, files map[string]string, path string, defines ...shader.Define) string {
	t.Helper()
	text, err := shader.Preprocess(sourceFiles(files), path, defines)
	if err != nil {
		t.Fatalf("Preprocess: %v", err)
	}
	return text
}

// preprocessLines answers the preprocessed text of path in files as its lines.
func preprocessLines(t *testing.T, files map[string]string, path string, defines ...shader.Define) []string {
	t.Helper()
	return strings.Split(strings.TrimSuffix(preprocess(t, files, path, defines...), "\n"), "\n")
}

// preprocessErr answers the ErrSource preprocessing path in files fails
// with, failing the test on success or on another error.
func preprocessErr(t *testing.T, files map[string]string, path string, defines ...shader.Define) shader.ErrSource {
	t.Helper()
	_, err := shader.Preprocess(sourceFiles(files), path, defines)
	if err == nil {
		t.Fatal("Preprocess succeeded, want an error")
	}
	var refused shader.ErrSource
	if !errors.As(err, &refused) {
		t.Fatalf("Preprocess: %v, want an ErrSource", err)
	}
	return refused
}

func sourceFiles(files map[string]string) fstest.MapFS {
	mapped := fstest.MapFS{}
	for name, text := range files {
		mapped[name] = &fstest.MapFile{Data: []byte(text)}
	}
	return mapped
}

func lineCount(text string) int {
	return len(strings.Split(strings.TrimSuffix(text, "\n"), "\n"))
}

// assertTrimmedLines compares lines with want, trailing blanks trimmed.
func assertTrimmedLines(t *testing.T, lines, want []string) {
	t.Helper()
	if len(lines) != len(want) {
		t.Fatalf("preprocessed to %q, want %q", lines, want)
	}
	for index, line := range lines {
		if got := strings.TrimRight(line, " \t"); got != want[index] {
			t.Errorf("line %d = %q, want %q", index+1, got, want[index])
		}
	}
}

func assertLocations(t *testing.T, got, want []shader.Location) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("At = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("At[%d] = %v, want %v", index, got[index], want[index])
		}
	}
}

// layeredFS opens from high, then from low, as a filesystem layering mounts
// does.
type layeredFS struct{ high, low fs.FS }

func (l layeredFS) Open(name string) (fs.File, error) {
	if file, err := l.high.Open(name); err == nil {
		return file, nil
	}
	return l.low.Open(name)
}
