package types

import (
	"fmt"
	pathpkg "path"
	"strings"

	"github.com/dvoyni/cog/kernel"
)

// One rule, in one place, applied where a path enters canvas: the draw path and
// the measuring verbs used to disagree about what a resource path is, and the
// draw path's weaker rule resolved "." to the empty path, which drew a silent
// white quad where a directory had been named.

// SpritePath validates one sprite path where it enters canvas and says what to
// draw for it: the cleaned path, or - with invalid set - the path exactly as it
// was written, for the caller to report once and then draw nothing. An invalid
// path never reaches a cache, so nothing is keyed on it and nothing is opened
// for it.
//
// The empty path is not invalid: it names the white texel every fill, line and
// stroke draws with, and is the one sprite canvas resolves without opening
// anything. "." is invalid, because it names a directory where a file belongs.
func SpritePath(path string) (recorded string, invalid bool) {
	if path == "" {
		return "", false
	}
	clean, ok := ValidateResourcePath(path)
	if !ok {
		return path, true
	}
	return clean, false
}

// ValidateResourcePath normalizes a resource path and rejects empty, absolute,
// NUL-bearing, or root-escaping inputs, so every path canvas opens has been
// through one rule and one security boundary.
//
// The empty path is refused here and given its meaning by each caller, because
// the callers disagree about it: a draw reads it as the white texel, and a
// measurement has nothing to measure.
func ValidateResourcePath(path string) (string, bool) {
	if path == "" || strings.ContainsRune(path, 0) {
		return "", false
	}
	cleaned := pathpkg.Clean(strings.ReplaceAll(path, "\\", "/"))
	if cleaned == "" || cleaned == "." {
		return "", false
	}
	if strings.HasPrefix(cleaned, "/") {
		return "", false
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}

// invalidSpritePath is the report-once key canvas speaks an unusable sprite path
// under. It is canvas's own type, so it shares a namespace with nothing: the
// Library reports a failed read under the descriptor itself, and a path this
// rule refuses never becomes one.
type invalidSpritePath string

// ReportInvalidSpritePath says once that a path canvas was asked for is not one
// it will open. It is exported for canvas's internal/, which meets icon paths at
// flush rather than at record time because an icon path arrives inside the text
// it is written in.
func ReportInvalidSpritePath(k kernel.Kernel, path string) {
	k.ReportErrorOnce(invalidSpritePath(path), fmt.Errorf("canvas: invalid sprite path %q", path))
}
