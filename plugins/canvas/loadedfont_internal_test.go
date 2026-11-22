package canvas

import "testing"

func TestLoadedFontIsAtItsVariations(t *testing.T) {
	plain, err := loadFont(nil, Font{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.coordinates) != 0 {
		t.Errorf("JetBrains Mono at no variation has coordinates %v, want its default instance", plain.coordinates)
	}
	bold, err := loadFont(nil, Font{Variations: [4]Variation{{}, {Axis: AxisTagFromName("wght"), Value: 800}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(bold.coordinates) != 1 || bold.coordinates[0] <= 0 {
		t.Errorf("JetBrains Mono at a weight of 800 has coordinates %v, want its one axis past its default", bold.coordinates)
	}
}
