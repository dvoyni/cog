package canvas

// IconDescriptor is a picture text places like a glyph, registered under a
// name with WriteFontLibrary.RegisterIcon.
type IconDescriptor struct {
	// Image is the icon's picture, and Frame the frame of its sheet it
	// draws, so a game's button prompts are one image.
	Image Image
	Frame uint32
	// Scale multiplies the font's cap height, the icon's height, zero
	// meaning 1.
	Scale float32
	// Offset moves the icon's bottom from the baseline, in em.
	Offset float32
	// Tinted takes its span's tint; otherwise the icon takes the run's alpha
	// alone.
	Tinted bool
}
