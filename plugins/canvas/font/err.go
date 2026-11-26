package font

import (
	"errors"
	"fmt"
)

// ErrVariationIgnored is a LoadError's Err for a font with a Variation of
// the zero Axis and a non-zero Value, such as AxisTagFromName answers for a
// name of other than four bytes: it names no axis, so it is ignored, and
// the font loads without it.
var ErrVariationIgnored = errors.New("font: a variation of a non-zero value names no axis")

// LoadError is one thing Load answers as going wrong with Font: Err is
// ErrVariationIgnored, for a font that loaded, or why it did not load,
// leaving it out of its set: its file failed to read or parse, or its
// collection has no font at its Index.
type LoadError struct {
	Font Font
	Err  error
}

// Error answers the font's path and what went wrong.
func (e LoadError) Error() string {
	return fmt.Sprintf("font %q: %v", e.Font.Path, e.Err)
}

// Unwrap answers Err.
func (e LoadError) Unwrap() error { return e.Err }
