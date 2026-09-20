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
	got := paddedRGBA(pixels, 2, 2, 1, fillExtrude)
	if len(got) != 4*4*4 {
		t.Fatalf("padded bytes = %d, want 64", len(got))
	}
	if !bytes.Equal(got[:4], pixels[:4]) || !bytes.Equal(got[len(got)-4:], pixels[len(pixels)-4:]) {
		t.Fatal("sprite edges were not extruded into padding")
	}
}

// A wrap-filled gutter carries the opposite edge, which is what makes bilinear
// filtering across a tiled sprite's wrap seam sample the texels it should. The
// top-left padding texel is therefore the bottom-right source texel, not the
// top-left one an extruded gutter would repeat.
func TestPaddedRGBAWrapsSpriteEdges(t *testing.T) {
	pixels := []byte{
		1, 2, 3, 4, 5, 6, 7, 8,
		9, 10, 11, 12, 13, 14, 15, 16,
	}
	got := paddedRGBA(pixels, 2, 2, 1, fillWrapBoth)
	if len(got) != 4*4*4 {
		t.Fatalf("padded bytes = %d, want 64", len(got))
	}
	if !bytes.Equal(got[:4], pixels[12:16]) {
		t.Errorf("top-left padding = %v, want the bottom-right texel %v", got[:4], pixels[12:16])
	}
	if !bytes.Equal(got[len(got)-4:], pixels[:4]) {
		t.Errorf("bottom-right padding = %v, want the top-left texel %v", got[len(got)-4:], pixels[:4])
	}
}

// The fill is per axis, and getting that wrong is visible: a strip that tiles
// horizontally and ends vertically wants its left and right gutters wrapped and
// its top and bottom extruded. Wrapping the axis that does not tile lays the far
// edge's texels along the near edge, which on a button's side is a thin dark line
// down the length of it.
func TestPaddedRGBAWrapsOnlyTheTiledAxis(t *testing.T) {
	// Rows are distinct so a wrapped y is visible: top row 1..8, bottom 9..16.
	pixels := []byte{
		1, 2, 3, 4, 5, 6, 7, 8,
		9, 10, 11, 12, 13, 14, 15, 16,
	}
	got := paddedRGBA(pixels, 2, 2, 1, fillWrapX)
	// Top-left padding: x wraps to the last column, y extrudes to the first row.
	if !bytes.Equal(got[:4], pixels[4:8]) {
		t.Errorf("top-left padding = %v, want the top-right texel %v: x wraps, y clamps", got[:4], pixels[4:8])
	}
	// Bottom-right padding: x wraps to the first column, y extrudes to the last row.
	if !bytes.Equal(got[len(got)-4:], pixels[8:12]) {
		t.Errorf("bottom-right padding = %v, want the bottom-left texel %v", got[len(got)-4:], pixels[8:12])
	}
}

// Its transpose, so neither axis can be wired to the other by accident.
func TestPaddedRGBAWrapsOnlyTheTiledAxisTransposed(t *testing.T) {
	pixels := []byte{
		1, 2, 3, 4, 5, 6, 7, 8,
		9, 10, 11, 12, 13, 14, 15, 16,
	}
	got := paddedRGBA(pixels, 2, 2, 1, fillWrapY)
	// Top-left padding: y wraps to the bottom row, x extrudes to the first column.
	if !bytes.Equal(got[:4], pixels[8:12]) {
		t.Errorf("top-left padding = %v, want the bottom-left texel %v: y wraps, x clamps", got[:4], pixels[8:12])
	}
}
