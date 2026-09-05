package m

import "math"

// Color holds linear components. Light transport only adds up in linear, and
// canvas and scene share one frame buffer and one Color, so the engine is
// linear throughout and a caller says which space it is speaking by choosing a
// constructor. Alpha is coverage rather than light and never converts.
type Color struct{ R, G, B, A float32 }

var (
	Transparent = Color{}
	Black       = Color{A: 1}
	White       = Color{R: 1, G: 1, B: 1, A: 1}
)

// NewColor writes components verbatim without naming a colour space. It is
// being replaced by NewColorLinear and NewColorSrgb and will be deleted.
func NewColor(red, green, blue, alpha float32) Color {
	return Color{R: red, G: green, B: blue, A: alpha}
}

// NewColor8 scales bytes into components without naming a colour space. It is
// being replaced by NewColorSrgb8 and will be deleted.
func NewColor8(red, green, blue, alpha uint8) Color {
	const scale = 1.0 / 255
	return Color{
		R: float32(red) * scale,
		G: float32(green) * scale,
		B: float32(blue) * scale,
		A: float32(alpha) * scale,
	}
}

// NewColorLinear creates a color from components already in linear space:
// light, not a picker value.
func NewColorLinear(red, green, blue, alpha float32) Color {
	return Color{R: red, G: green, B: blue, A: alpha}
}

// NewColorSrgb creates a color from gamma-encoded components in [0, 1], what a
// colour picker, a CSS literal, or an artist means by a value. Alpha is taken
// verbatim.
func NewColorSrgb(red, green, blue, alpha float32) Color {
	return Color{R: srgbToLinear(red), G: srgbToLinear(green), B: srgbToLinear(blue), A: alpha}
}

// NewColorSrgb8 creates a color from gamma-encoded bytes, what a hex literal or
// a decoded PNG texel holds. Alpha is scaled but not converted.
func NewColorSrgb8(red, green, blue, alpha uint8) Color {
	const scale = 1.0 / 255
	return NewColorSrgb(float32(red)*scale, float32(green)*scale, float32(blue)*scale, float32(alpha)*scale)
}

// Srgb is the inverse of NewColorSrgb, returning gamma-encoded components and
// the unconverted alpha.
func (color Color) Srgb() (red, green, blue, alpha float32) {
	return linearToSrgb(color.R), linearToSrgb(color.G), linearToSrgb(color.B), color.A
}

// Srgb8 is the inverse of NewColorSrgb8, clamping to [0, 1] and rounding to the
// nearest byte.
func (color Color) Srgb8() (red, green, blue, alpha uint8) {
	return toByte(linearToSrgb(color.R)), toByte(linearToSrgb(color.G)), toByte(linearToSrgb(color.B)), toByte(color.A)
}

// srgbToLinear applies the sRGB electro-optical transfer function. Every input
// at or below the linear segment, negatives included, takes the linear branch,
// so extrapolated components never become NaN.
func srgbToLinear(value float32) float32 {
	if value <= 0.04045 {
		return value / 12.92
	}
	return float32(math.Pow(float64((value+0.055)/1.055), 2.4))
}

func linearToSrgb(value float32) float32 {
	if value <= 0.0031308 {
		return value * 12.92
	}
	return float32(1.055*math.Pow(float64(value), 1/2.4) - 0.055)
}

func toByte(value float32) uint8 { return uint8(round(Clamp01(value) * 255)) }

// NewColorHSLA creates a color from a hue in radians and saturation,
// lightness, and alpha in [0, 1]. Hue wraps to [0, 2*Pi); the other components
// are clamped. HSL is a model over gamma-encoded RGB everywhere it is used, so
// it stays one here and converts at the boundary: lightness 0.5 keeps meaning
// what a colour picker shows.
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
	return NewColorSrgb(red+match, green+match, blue+match, alpha)
}

// Hsla returns hue in radians in [0, 2*Pi), saturation and lightness in
// [0, 1], and the color's alpha. Achromatic colors have zero hue. Like
// NewColorHSLA it works in sRGB space, converting the components on the way in.
func (color Color) Hsla() (hue, saturation, lightness, alpha float32) {
	red, green, blue, alpha := color.Srgb()
	maximum := max(red, green, blue)
	minimum := min(red, green, blue)
	delta := maximum - minimum
	lightness = (maximum + minimum) / 2

	if delta == 0 {
		return 0, 0, lightness, alpha
	}

	saturation = delta / (1 - abs32(2*lightness-1))
	switch maximum {
	case red:
		hue = float32(math.Mod(float64((green-blue)/delta), 6))
	case green:
		hue = (blue-red)/delta + 2
	default:
		hue = (red-green)/delta + 4
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

// Add sums two colors in linear space, where light adds up.
func (color Color) Add(other Color) Color {
	return Color{color.R + other.R, color.G + other.G, color.B + other.B, color.A + other.A}
}

func (color Color) Sub(other Color) Color {
	return Color{color.R - other.R, color.G - other.G, color.B - other.B, color.A - other.A}
}

// Mul modulates one color by another in linear space, which is what a tint or a
// texture sample times a base color means physically.
func (color Color) Mul(other Color) Color {
	return Color{color.R * other.R, color.G * other.G, color.B * other.B, color.A * other.A}
}

func (color Color) MulS(value float32) Color {
	return Color{color.R * value, color.G * value, color.B * value, color.A * value}
}

// Lerp blends in linear space, with no perceptual variant: it is a light
// transport operation, and two functions differing invisibly is worse than one
// that is right. A black to white fade therefore passes through 73% grey on
// screen rather than 50%.
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
