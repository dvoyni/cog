package canvas

import (
	"io/fs"
	"os"
	"runtime"
	"slices"
	"testing"
	"testing/fstest"
	"weak"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/canvas/font"
)

func TestMakingAFontSetNamesItsSlotAtAFreeIndexUnderItsNextGeneration(t *testing.T) {
	library, writer := makeWriterOverFiles(t)
	first := writer.MakeFontSet(font.SetDescriptor{Default: font.Font{Path: "body.ttf"}})
	if first != (FontSet{index: 1, generation: 1}) || library.sets[1].set != first || library.sets[1].fonts.Default().Font().Path != "body.ttf" {
		t.Fatalf("the set made is %v, its slot %v, want index 1 of generation 1 naming its slot of body.ttf", first, library.sets[1].set)
	}
	writer.DestroyFontSet(first)
	second := writer.MakeFontSet(font.SetDescriptor{})
	if second != (FontSet{index: 1, generation: 2}) || library.sets[1].set != second {
		t.Errorf("the set made at the freed index is %v, its slot %v, want index 1 of generation 2", second, library.sets[1].set)
	}
}

func TestDestroyingAFontSetNamesAnEmptySlotUnderItsNextGenerationAndRetiresItsFonts(t *testing.T) {
	library, writer := makeWriterOverFiles(t)
	set := writer.MakeFontSet(font.SetDescriptor{Default: font.Font{Path: "body.ttf"}})
	retired := library.sets[set.index]
	fields := drawGlyphFields(library, set)

	writer.DestroyFontSet(set)

	if slot := library.sets[set.index]; slot.set != (FontSet{index: 1, generation: 2}) || slot.fonts != nil || library.findSlot(set) != nil {
		t.Errorf("the destroyed set's index holds slot %v of fonts %v, want an empty slot of generation 2 that the set no longer names", slot.set, slot.fonts)
	}
	if !slices.Contains(library.freeIndexes, set.index) {
		t.Errorf("the free indexes are %v, want the destroyed set's %d among them", library.freeIndexes, set.index)
	}
	expectRetired(t, library, retired, fields)
}

func TestReloadingAChangedFontSetNamesItsNewSlotUnderItsOwnHandleAndRetiresItsOldFonts(t *testing.T) {
	library, writer := makeWriterOverFiles(t)
	set := writer.MakeFontSet(font.SetDescriptor{Default: font.Font{Path: "reloaded.ttf"}})
	retired := library.sets[set.index]
	fields := drawGlyphFields(library, set)
	arabic, err := os.ReadFile("testdata/fonts/notosansarabic.ttf")
	if err != nil {
		t.Fatal(err)
	}
	writer.files.(fstest.MapFS)["reloaded.ttf"] = &fstest.MapFile{Data: arabic}

	writer.reloadChanged()

	if slot := library.sets[set.index]; slot == retired || slot.set != set || library.findSlot(set) != slot {
		t.Errorf("the reloaded set's index holds slot %v, the old slot again %v, want a new slot that set names", slot.set, slot == retired)
	}
	expectRetired(t, library, retired, fields)
}

func TestMakingTheDefaultSetNamesItsSlotUnderTheZeroHandleAndRetiresJetBrainsMono(t *testing.T) {
	library, writer := makeWriterOverFiles(t)
	retired := library.sets[0]
	fields := drawGlyphFields(library, FontSet{})

	writer.loadDefaultSet()

	if slot := library.sets[0]; slot == retired || slot.set != (FontSet{}) || slot.fonts.Default().Font().Path != "body.ttf" {
		t.Errorf("the default set's index holds slot %v, the old slot again %v, want a new slot of Config.DefaultFontSet's body.ttf under the zero handle", slot.set, slot == retired)
	}
	expectRetired(t, library, retired, fields)
}

func TestADestroyedSetsFontsAreCollectedOnceItsLayoutsHaveGoneIdle(t *testing.T) {
	library, writer := makeWriterOverFiles(t)
	set := writer.MakeFontSet(font.SetDescriptor{Default: font.Font{Path: "body.ttf"}})
	retiredFont := weak.Make(library.sets[set.index].fonts.Default())
	// The shaper laying the set out goes back to the pool holding a face of
	// its font, which one collection would only move to the pool's victims.
	library.layOutText(Text{String: "ab", Font: set}, true, nil)

	writer.DestroyFontSet(set)
	for range library.layouts.idleFrames + 1 {
		library.layouts.endFrame()
	}
	runtime.GC()

	if retiredFont.Value() != nil {
		t.Error("the destroyed set's font is still held once its layouts went idle")
	}
}

// makeWriterOverFiles answers a font library whose default set is to be
// loaded of body.ttf, canvas's own JetBrains Mono until it is, and its
// writing side over files holding JetBrains Mono's file at body.ttf and
// reloaded.ttf.
func makeWriterOverFiles(t *testing.T) (*fontLibrary, fontLibraryWriter) {
	t.Helper()
	builtIn, err := fs.ReadFile(font.BuiltInFiles(), "jetbrainsmono.ttf")
	if err != nil {
		t.Fatal(err)
	}
	library := newFontLibrary(&reporter{logger: &kernel.Logger{}, inspector: &inspector{}}, &Config{DefaultFontSet: font.SetDescriptor{Default: font.Font{Path: "body.ttf"}}})
	files := fstest.MapFS{
		"body.ttf":     {Data: builtIn},
		"reloaded.ttf": {Data: builtIn},
	}
	return library, makeFontLibraryWriter(library, files)
}

// drawGlyphFields answers the fields of the glyphs of "ab" laid out for
// drawing in set.
func drawGlyphFields(library *fontLibrary, set FontSet) []uint32 {
	_, layout := library.layOutText(Text{String: "ab", Font: set}, true, nil)
	var fields []uint32
	for _, glyph := range layout.glyphs {
		fields = append(fields, glyph.field)
	}
	return fields
}

// expectRetired fails t unless the glyph table holds no glyph of the fonts
// of retired, a slot replaced, and queues fields, its glyphs', to be let
// go.
func expectRetired(t *testing.T, library *fontLibrary, retired *fontSetSlot, fields []uint32) {
	t.Helper()
	retiredFonts := slices.Collect(retired.fonts.Fonts())
	for key := range library.glyphs.indexes {
		if slices.Contains(retiredFonts, key.font) {
			t.Errorf("the glyph table still holds glyph %d of a retired font", key.id)
		}
	}
	if !holdSameFields(library.glyphs.retiredFields, fields) {
		t.Errorf("the glyph table queues fields %v to let go, want the retired glyphs' %v", library.glyphs.retiredFields, fields)
	}
}
