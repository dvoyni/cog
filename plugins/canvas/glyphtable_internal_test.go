package canvas

import (
	"testing"

	"github.com/dvoyni/cog/kernel"
)

func TestDroppingAFontsGlyphsForgetsThemAndFreesTheirFieldsKeepingAnotherFonts(t *testing.T) {
	// The default set and a set of its own copy of JetBrains Mono each draw
	// "ab"; dropping the default set's fonts leaves only the other set's
	// glyphs, their fields whole.
	canvasReporter := &reporter{logger: &kernel.Logger{}, inspector: &inspector{}}
	library := newFontLibrary(canvasReporter, &Config{})
	other := makeFontSetSlot(nil, canvasReporter, FontSetDescriptor{})
	other.set = FontSet{index: 1, generation: 1}
	library.sets = append(library.sets, other)
	_, dropped := library.layOutText(Text{String: "ab"}, true, nil)
	_, kept := library.layOutText(Text{String: "ab", Font: other.set}, true, nil)

	library.glyphs.dropFonts(library.sets[0].appendFonts(nil))

	if len(library.glyphs.indexes) != len(kept.glyphs) {
		t.Errorf("the table holds %d glyphs once the default set's are dropped, want the other set's %d", len(library.glyphs.indexes), len(kept.glyphs))
	}
	for _, glyph := range dropped.glyphs {
		if field := library.glyphs.findField(glyph.field); field.texels != nil {
			t.Errorf("the dropped glyph %d's field still holds %d texels, want it freed", glyph.id, len(field.texels))
		}
	}
	for _, glyph := range kept.glyphs {
		if field := library.glyphs.findField(glyph.field); field.texels == nil {
			t.Errorf("the other set's glyph %d's field holds no texels, want it kept", glyph.id)
		}
	}
}
