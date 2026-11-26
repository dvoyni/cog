package font

// Metrics are a font's vertical measures at a size, in world units, each a
// distance: Ascent above the baseline and Descent below it, LineGap the gap
// the font asks between lines, LineHeight the three together, one line's
// baseline to the next's, and XHeight and CapHeight the heights of its
// lowercase letters and its capitals above the baseline.
type Metrics struct {
	Ascent     float32
	Descent    float32
	LineGap    float32
	LineHeight float32
	XHeight    float32
	CapHeight  float32
}
