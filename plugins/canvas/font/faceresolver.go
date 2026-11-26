package font

import gotext "github.com/go-text/typesetting/font"

// faceResolver is the shaper as go-text's shaping.Fontmap, which its
// segmenter asks the face drawing each character of.
type faceResolver Shaper

// ResolveFace answers the shaper's face of the font drawing character in
// the run's set and language, the default font's for an object, and notes
// the rune among the layout's missing runes when no font of the set has
// it.
func (r *faceResolver) ResolveFace(character rune) *gotext.Face {
	s := (*Shaper)(r)
	for s.nextRuneToResolve < len(s.runes) && s.runes[s.nextRuneToResolve] != character {
		s.nextRuneToResolve++
	}
	index := s.nextRuneToResolve
	s.nextRuneToResolve++
	if character == objectCharacter {
		return s.findFace(s.input.Set.Default())
	}
	characterFont, hasCharacter := s.input.Set.CharacterFont(s.input.Language, character)
	if !hasCharacter && index < len(s.runes) {
		s.layout.MissingRunes = append(s.layout.MissingRunes, index)
	}
	return s.findFace(characterFont)
}
