// Package shader is the gogpu plugin's WGSL preprocessor, v1's ported
// whole, which gfx's Device.PreprocessShader reaches through the plugin. It
// works on text over an fs.FS and knows nothing of the device, so a tool with
// no GPU calls Preprocess itself.
package shader

import (
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/dvoyni/cog/gfx"
)

// Preprocess answers the WGSL at path in files after v1's directives:
// #include, #define, #const and #if.
//
// A directive is a line starting with # or //#, the second keeping a source
// valid WGSL. #include splices a source once, its path relative to the
// including source when it starts with "./" or "../" and a path in files
// otherwise. #define NAME declares a flag, #const NAME=VALUE a WGSL const
// emitted on its own line, and #if, #elif, #else and #endif keep or blank the
// lines between them by a condition over the flags, with !, &, | and
// parentheses. WGSL's enable, requires and diagnostic directives are hoisted
// to the top, where WGSL wants them, which splicing would break. Every other
// line keeps its number in its source: comments, directives and skipped lines
// are blanked rather than removed. A refusal is a gfx.ErrShaderSource.
func Preprocess(files fs.FS, path string, defines []gfx.ShaderDefine) (string, error) {
	flattener := newFlattener(files, path, defines)
	return flattener.run()
}

// labelShader names a shader in a gfx.ErrShaderSource: its path, followed by
// its defines, sorted by name, in brackets. It also answers the defines kept,
// the last of each name, and the message for the first one with no legal
// spelling.
func labelShader(path string, defines []gfx.ShaderDefine) (label string, kept []gfx.ShaderDefine, malformed string) {
	for _, define := range defines {
		if index := slices.IndexFunc(kept, func(other gfx.ShaderDefine) bool { return other.Name == define.Name }); index >= 0 {
			kept[index] = define
			continue
		}
		kept = append(kept, define)
	}
	if len(kept) == 0 {
		return path, nil, ""
	}
	slices.SortFunc(kept, func(a, b gfx.ShaderDefine) int { return strings.Compare(a.Name, b.Name) })
	spellings := make([]string, len(kept))
	for index, define := range kept {
		spellings[index] = define.Name
		if define.Value != "" {
			spellings[index] += "=" + define.Value
		}
		switch {
		case malformed != "":
		case define.Name == "":
			malformed = "a define has an empty name"
		case strings.ContainsRune(define.Value, '\n'):
			malformed = fmt.Sprintf("define %q has a value containing a newline", define.Name)
		}
	}
	// A value's newline is spaced, as v1 did, so the error stays one line.
	return path + " [" + strings.ReplaceAll(strings.Join(spellings, " "), "\n", " ") + "]", kept, malformed
}
