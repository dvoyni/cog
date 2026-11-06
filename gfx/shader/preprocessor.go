// Package shader is gfx's WGSL preprocessor, v1's ported whole. It works on
// text over an fs.FS and knows nothing of the device.
package shader

import (
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
)

// Define is one name a shader is preprocessed with. With no Value it is
// a flag, read by the source's #if conditionals and never reaching WGSL;
// defining a name no source mentions is harmless. With a Value it overrides
// the #const of that name, and is ignored where no source declares one. The
// Value is WGSL text, never interpreted: "16" and "16u" differ. A name given
// twice keeps its last entry.
type Define struct {
	Name, Value string
}

// Location names one line of one shader source: its path in the
// filesystem preprocessed, and its line, from 1. The zero location is the
// defines given, which have no line.
type Location struct {
	Source string
	Line   int
}

// String answers "source:line", or "the defines" for the zero location.
func (l Location) String() string {
	if l == (Location{}) {
		return "the defines"
	}
	return l.Source + ":" + strconv.Itoa(l.Line)
}

// ErrSource is answered by Preprocess for a shader it refused.
// Shader names it: its path, followed by its defines in brackets, sorted by
// name, as "scene.wgsl [N=8 SKIN]". At is where, the offending line first:
// one line, or the chain of #include lines down to it, or the line a
// declaration conflicts with. It is empty for a define with no legal
// spelling.
type ErrSource struct {
	Shader  string
	At      []Location
	Message string
}

// Error reads as one line, naming the shader and every location.
func (e ErrSource) Error() string {
	var builder strings.Builder
	builder.WriteString("shader: ")
	builder.WriteString(strconv.Quote(e.Shader))
	builder.WriteString(": ")
	builder.WriteString(e.Message)
	for index, at := range e.At {
		if index == 0 {
			builder.WriteString(" (at ")
		} else {
			builder.WriteString(", ")
		}
		builder.WriteString(at.String())
	}
	if len(e.At) > 0 {
		builder.WriteByte(')')
	}
	return builder.String()
}

// Preprocess answers the WGSL at path in files after v1's directives:
// #include, #define, #const and #if. A task calls it under its own
// kernel.Read[platform.FS] and hands the result to gfx's
// Device.CreateShaderModule.
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
// are blanked rather than removed. A refusal is an ErrSource.
func Preprocess(files fs.FS, path string, defines []Define) (string, error) {
	flattener := newFlattener(files, path, defines)
	return flattener.run()
}

// labelShader names a shader in an ErrSource: its path, followed by its
// defines, sorted by name, in brackets. It also answers the defines kept, the
// last of each name, and the message for the first one with no legal
// spelling.
func labelShader(path string, defines []Define) (label string, kept []Define, malformed string) {
	for _, define := range defines {
		if index := slices.IndexFunc(kept, func(other Define) bool { return other.Name == define.Name }); index >= 0 {
			kept[index] = define
			continue
		}
		kept = append(kept, define)
	}
	if len(kept) == 0 {
		return path, nil, ""
	}
	slices.SortFunc(kept, func(a, b Define) int { return strings.Compare(a.Name, b.Name) })
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
