// Package gotextfont hands canvas the go-text font each font.Loaded was
// parsed into, which canvas's text shaper and glyph table make faces of
// while they are canvas's, so no go-text reaches the font package's
// interface.
package gotextfont

import "github.com/go-text/typesetting/font"

// Parsed is a loaded font as go-text holds it: its parsed font, read-only
// and its own, and the coordinates of its variations, which a face of it is
// made at.
type Parsed struct {
	Font        *font.Font
	Coordinates []font.VarCoord
}

// FindParsed answers what loaded, a *font.Loaded, was parsed into. Package
// font sets it as it is initialised.
var FindParsed func(loaded any) Parsed
