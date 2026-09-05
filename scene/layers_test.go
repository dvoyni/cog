package scene

import "testing"

func TestZeroReadsAsEveryLayerOnBothSides(t *testing.T) {
	// The degenerate frame writes no mask anywhere and still draws, which is the
	// whole reason zero cannot mean "no layers".
	cases := []struct {
		name      string
		layers    LayerMask
		cull      LayerMask
		wantDrawn bool
	}{
		{"nothing written on either side", 0, 0, true},
		{"item unmasked, camera picky", 0, Layer(3), true},
		{"item on a layer, camera unmasked", Layer(3), 0, true},
		{"matching layers", Layer(3), Layer(3) | Layer(4), true},
		{"disjoint layers", Layer(3), Layer(4), false},
		{"everything against one layer", LayersAll, Layer(7), true},
	}
	for _, test := range cases {
		if got := test.layers.drawnBy(test.cull); got != test.wantDrawn {
			t.Errorf("%s: drawn = %v, want %v", test.name, got, test.wantDrawn)
		}
	}
}

func TestLayerBitsAreDistinct(t *testing.T) {
	seen := map[LayerMask]uint{}
	for i := uint(0); i < 32; i++ {
		bit := Layer(i)
		if bit == 0 {
			t.Fatalf("Layer(%d) is zero, which would read as every layer", i)
		}
		if previous, clash := seen[bit]; clash {
			t.Fatalf("Layer(%d) collides with Layer(%d)", i, previous)
		}
		seen[bit] = i
	}
}
