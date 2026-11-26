package font

import "testing"

func TestALoadedFontIsAtItsVariations(t *testing.T) {
	plain, _, err := loadFont(nil, Font{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.parsed.Coordinates) != 0 {
		t.Errorf("JetBrains Mono at no variation has coordinates %v, want its default instance", plain.parsed.Coordinates)
	}
	bold, _, err := loadFont(nil, Font{Variations: [4]Variation{{}, {Axis: AxisTagFromName("wght"), Value: 800}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(bold.parsed.Coordinates) != 1 || bold.parsed.Coordinates[0] <= 0 {
		t.Errorf("JetBrains Mono at a weight of 800 has coordinates %v, want its one axis past its default", bold.parsed.Coordinates)
	}
	if bold.parsed.Font == plain.parsed.Font {
		t.Errorf("the two loads of JetBrains Mono share their parsed font, want each its own")
	}
}
