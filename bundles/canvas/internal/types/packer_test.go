package types

import (
	"bytes"
	"testing"
)

func TestPaddedRGBAExtrudesSpriteEdges(t *testing.T) {
	pixels := []byte{
		1, 2, 3, 4, 5, 6, 7, 8,
		9, 10, 11, 12, 13, 14, 15, 16,
	}
	got := paddedRGBA(pixels, 2, 2, 1, true)
	if len(got) != 4*4*4 {
		t.Fatalf("padded bytes = %d, want 64", len(got))
	}
	if !bytes.Equal(got[:4], pixels[:4]) || !bytes.Equal(got[len(got)-4:], pixels[len(pixels)-4:]) {
		t.Fatal("sprite edges were not extruded into padding")
	}
}
