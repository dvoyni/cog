package m

import "math"

type Color struct{ R, G, B, A float32 }

var (
	Transparent = Color{}
	Black       = Color{A: 1}
	White       = Color{R: 1, G: 1, B: 1, A: 1}
)

func NewColor(red, green, blue, alpha float32) Color {
	return Color{R: red, G: green, B: blue, A: alpha}
}

func NewColor8(red, green, blue, alpha uint8) Color {
	const scale = 1.0 / 255
	return Color{
		R: float32(red) * scale,
		G: float32(green) * scale,
		B: float32(blue) * scale,
		A: float32(alpha) * scale,
	}
}

// NewColorHSLA creates a color from a hue in radians and saturation,
// lightness, and alpha in [0, 1]. Hue wraps to [0, 2*Pi); the other components
// are clamped.
func NewColorHSLA(hue, saturation, lightness, alpha float32) Color {
	hue = wrapHue(hue)
	saturation = Clamp(saturation, 0, 1)
	lightness = Clamp(lightness, 0, 1)
	alpha = Clamp(alpha, 0, 1)

	chroma := (1 - abs32(2*lightness-1)) * saturation
	hueSector := hue / (math.Pi / 3)
	secondary := chroma * (1 - abs32(float32(math.Mod(float64(hueSector), 2))-1))

	var red, green, blue float32
	switch int(hueSector) {
	case 0:
		red, green = chroma, secondary
	case 1:
		red, green = secondary, chroma
	case 2:
		green, blue = chroma, secondary
	case 3:
		green, blue = secondary, chroma
	case 4:
		red, blue = secondary, chroma
	default:
		red, blue = chroma, secondary
	}

	match := lightness - chroma/2
	return Color{R: red + match, G: green + match, B: blue + match, A: alpha}
}

// Hsla returns hue in radians in [0, 2*Pi), saturation and lightness in
// [0, 1], and the color's alpha. Achromatic colors have zero hue.
func (color Color) Hsla() (hue, saturation, lightness, alpha float32) {
	maximum := max(color.R, color.G, color.B)
	minimum := min(color.R, color.G, color.B)
	delta := maximum - minimum
	lightness = (maximum + minimum) / 2
	alpha = color.A

	if delta == 0 {
		return 0, 0, lightness, alpha
	}

	saturation = delta / (1 - abs32(2*lightness-1))
	switch maximum {
	case color.R:
		hue = float32(math.Mod(float64((color.G-color.B)/delta), 6))
	case color.G:
		hue = (color.B-color.R)/delta + 2
	default:
		hue = (color.R-color.G)/delta + 4
	}
	hue = wrapHue(hue * math.Pi / 3)
	return hue, saturation, lightness, alpha
}

func wrapHue(hue float32) float32 {
	const turn = 2 * math.Pi
	hue = float32(math.Mod(float64(hue), turn))
	if hue < 0 {
		hue += turn
	}
	return hue
}

func (color Color) Add(other Color) Color {
	return Color{color.R + other.R, color.G + other.G, color.B + other.B, color.A + other.A}
}

func (color Color) Sub(other Color) Color {
	return Color{color.R - other.R, color.G - other.G, color.B - other.B, color.A - other.A}
}

func (color Color) Mul(other Color) Color {
	return Color{color.R * other.R, color.G * other.G, color.B * other.B, color.A * other.A}
}

func (color Color) MulS(value float32) Color {
	return Color{color.R * value, color.G * value, color.B * value, color.A * value}
}

func (color Color) Lerp(other Color, amount float32) Color {
	return Color{
		R: Lerp(color.R, other.R, amount),
		G: Lerp(color.G, other.G, amount),
		B: Lerp(color.B, other.B, amount),
		A: Lerp(color.A, other.A, amount),
	}
}

// Fade returns color with alpha replaced by a value clamped to [0, 1].
func (color Color) Fade(alpha float32) Color {
	color.A = Clamp(alpha, 0, 1)
	return color
}

// Opacity scales the existing alpha by a factor clamped to [0, 1].
func (color Color) Opacity(factor float32) Color {
	color.A *= Clamp(factor, 0, 1)
	return color
}
