package font

import "testing"

func TestALoadedFontIsAtItsVariations(t *testing.T) {
	plain, _, err := loadFont(nil, Font{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.coordinates) != 0 {
		t.Errorf("JetBrains Mono at no variation has coordinates %v, want its default instance", plain.coordinates)
	}
	bold, _, err := loadFont(nil, Font{Variations: [4]Variation{{}, {Axis: AxisTagFromName("wght"), Value: 800}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(bold.coordinates) != 1 || bold.coordinates[0] <= 0 {
		t.Errorf("JetBrains Mono at a weight of 800 has coordinates %v, want its one axis past its default", bold.coordinates)
	}
	if bold.parsed == plain.parsed {
		t.Errorf("the two loads of JetBrains Mono share their parsed font, want each its own")
	}
}
