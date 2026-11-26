package canvas

import (
	"slices"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/canvas/font"
)

func TestRetiringAFontsGlyphsForgetsThemAndEmptiesTheirFieldsOnceTakenKeepingAnotherFonts(t *testing.T) {
	// The default set and a set of its own copy of JetBrains Mono each draw
	// "ab"; retiring the default set's fonts leaves only the other set's
	// glyphs, the retired fields whole until taken, the other set's whole
	// after.
	canvasReporter := &reporter{logger: &kernel.Logger{}, inspector: &inspector{}}
	library := newFontLibrary(canvasReporter, &Config{})
	other := makeFontSetSlot(nil, canvasReporter, font.SetDescriptor{})
	other.set = FontSet{index: 1, generation: 1}
	library.sets = append(library.sets, other)
	retired := drawGlyphFields(library, FontSet{})
	var kept TextLayout
	library.layOutText(Text{String: "ab", Font: other.set}, true, func(_ Text, laidOut TextLayout) { kept = laidOut })

	library.glyphs.retireFonts(slices.Collect(library.sets[0].fonts.Fonts()))

	if len(library.glyphs.indexes) != len(kept.glyphs) {
		t.Errorf("the table holds %d glyphs once the default set's are retired, want the other set's %d", len(library.glyphs.indexes), len(kept.glyphs))
	}
	for _, field := range retired {
		if library.glyphs.findField(field).Texels == nil {
			t.Errorf("the retired field %d holds no texels before it is taken, want it whole for the entries recorded before", field)
		}
	}
	if taken := library.glyphs.takeRetiredFields(nil); !holdSameFields(taken, retired) {
		t.Errorf("the fields let go are %v, want the retired glyphs' %v", taken, retired)
	}
	for _, field := range retired {
		if texels := library.glyphs.findField(field).Texels; texels != nil {
			t.Errorf("the retired field %d still holds %d texels once taken, want it emptied", field, len(texels))
		}
	}
	for _, glyph := range kept.glyphs {
		if field := library.glyphs.findField(glyph.field); field.Texels == nil {
			t.Errorf("the other set's glyph %d's field holds no texels, want it kept", glyph.id)
		}
	}
}

// holdSameFields answers whether got and want hold the same field
// indexes, each as often, in any order.
func holdSameFields(got, want []uint32) bool {
	sortedGot := slices.Sorted(slices.Values(got))
	return slices.Equal(sortedGot, slices.Sorted(slices.Values(want)))
}
