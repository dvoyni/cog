package internal

import (
	"image/color"
	"testing"

	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

func TestACaptureUnStridesWithNoShear(t *testing.T) {
	// Five texels is twenty bytes a row, which the GPU pads to two hundred and
	// fifty-six: the width whose padding an un-stride that copies straight
	// through would smear across the image, one row further left each row.
	const width, height = 5, 3
	want := func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(10 + x*20), G: uint8(200 - y*50), B: uint8(x + y), A: 255}
	}
	picture := paddedCapture(width, height, want).Image()
	if picture == nil {
		t.Fatal("an 8-bit RGBA capture produced no image")
	}
	if size := picture.Bounds().Size(); size.X != width || size.Y != height {
		t.Fatalf("image size = %v, want %dx%d", size, width, height)
	}
	for y := range height {
		for x := range width {
			if got := color.NRGBAModel.Convert(picture.At(x, y)); got != want(x, y) {
				t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, got, want(x, y))
			}
		}
	}
}

func TestDepthAndOtherFormatsAreNotAnImage(t *testing.T) {
	for _, format := range []types.TextureFormat{types.FormatDepth32F, types.TextureFormat(99)} {
		capture := Capture{
			Pixels: make([]byte, 256), Width: 2, Height: 2, Format: format, BytesPerRow: 256,
		}
		if capture.Image() != nil {
			t.Fatalf("%s produced an image; only 8-bit RGBA can be one", format.String())
		}
	}
}
