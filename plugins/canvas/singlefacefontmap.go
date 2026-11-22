package canvas

import "github.com/go-text/typesetting/font"

// singleFaceFontmap is a go-text font map shaping every character of a run
// in one face.
type singleFaceFontmap struct {
	face *font.Face
}

// ResolveFace answers the map's face, whatever character is.
func (f singleFaceFontmap) ResolveFace(character rune) *font.Face { return f.face }
