package m

import "testing"

func TestNewColorLinearIsVerbatim(t *testing.T) {
	if got, want := NewColorLinear(0.25, 0.5, 0.75, 0.5), (Color{R: 0.25, G: 0.5, B: 0.75, A: 0.5}); got != want {
		t.Fatalf("NewColorLinear = %v, want %v", got, want)
	}
}

func TestSrgbTransferPinsItsFixedPointsAndMidpoint(t *testing.T) {
	cases := []struct {
		srgb, linear float32
	}{
		{0, 0},
		{1, 1},
		{0.5, 0.2140},
	}
	for _, testCase := range cases {
		color := NewColorSrgb(testCase.srgb, testCase.srgb, testCase.srgb, 1)
		if !near(color.R, testCase.linear) {
			t.Fatalf("NewColorSrgb(%v).R = %v, want %v", testCase.srgb, color.R, testCase.linear)
		}
		red, _, _, _ := color.Srgb()
		if !near(red, testCase.srgb) {
			t.Fatalf("Srgb() of %v = %v, want %v", testCase.srgb, red, testCase.srgb)
		}
	}
}

func TestSrgbIsTheInverseOfNewColorSrgb(t *testing.T) {
	wantRed, wantGreen, wantBlue, wantAlpha := float32(0.13), float32(0.62), float32(0.94), float32(0.4)
	red, green, blue, alpha := NewColorSrgb(wantRed, wantGreen, wantBlue, wantAlpha).Srgb()
	if !near(red, wantRed) || !near(green, wantGreen) || !near(blue, wantBlue) || !near(alpha, wantAlpha) {
		t.Fatalf("Srgb round trip = (%v, %v, %v, %v), want (%v, %v, %v, %v)", red, green, blue, alpha, wantRed, wantGreen, wantBlue, wantAlpha)
	}
}

func TestSrgb8RoundTripsEveryByte(t *testing.T) {
	for value := 0; value < 256; value++ {
		component := uint8(value)
		red, green, blue, alpha := NewColorSrgb8(component, component, component, component).Srgb8()
		if red != component || green != component || blue != component || alpha != component {
			t.Fatalf("Srgb8 round trip of %d = (%d, %d, %d, %d)", component, red, green, blue, alpha)
		}
	}
}

func TestAlphaNeverConverts(t *testing.T) {
	const alpha = float32(0.5)
	if got := NewColorSrgb(0.5, 0.5, 0.5, alpha).A; got != alpha {
		t.Fatalf("NewColorSrgb alpha = %v, want %v verbatim", got, alpha)
	}
	if got := NewColorHSLA(1.2, 0.8, 0.4, alpha).A; got != alpha {
		t.Fatalf("NewColorHSLA alpha = %v, want %v verbatim", got, alpha)
	}
	if got := NewColorSrgb8(128, 128, 128, 128).A; !near(got, 128.0/255) {
		t.Fatalf("NewColorSrgb8 alpha = %v, want %v verbatim", got, 128.0/255)
	}
	if _, _, _, got := NewColorLinear(0.5, 0.5, 0.5, alpha).Srgb(); got != alpha {
		t.Fatalf("Srgb alpha = %v, want %v verbatim", got, alpha)
	}
	if _, _, _, got := NewColorLinear(0.5, 0.5, 0.5, alpha).Srgb8(); got != 128 {
		t.Fatalf("Srgb8 alpha = %v, want 128 verbatim", got)
	}
}

func TestHslaStaysAnSrgbSpaceModel(t *testing.T) {
	// Lightness 0.5 with no saturation is mid grey to a colour picker, which is
	// sRGB 0.5 and so linear 0.2140 in the stored components.
	grey := NewColorHSLA(0, 0, 0.5, 1)
	if !near(grey.R, 0.2140) || !near(grey.G, 0.2140) || !near(grey.B, 0.2140) {
		t.Fatalf("mid grey = %v, want linear 0.2140 components", grey)
	}

	_, _, lightness, _ := NewColorSrgb(0.25, 0.25, 0.25, 1).Hsla()
	if !near(lightness, 0.25) {
		t.Fatalf("lightness of sRGB 0.25 grey = %v, want 0.25", lightness)
	}
}

func TestBareConstructorsStillWriteComponentsVerbatim(t *testing.T) {
	if got, want := NewColor(0.25, 0.5, 0.75, 0.5), (Color{R: 0.25, G: 0.5, B: 0.75, A: 0.5}); got != want {
		t.Fatalf("NewColor = %v, want %v", got, want)
	}
	if got, want := NewColor8(0, 128, 255, 255), (Color{R: 0, G: 128.0 / 255, B: 1, A: 1}); !colorNear(got, want) {
		t.Fatalf("NewColor8 = %v, want %v", got, want)
	}
}
