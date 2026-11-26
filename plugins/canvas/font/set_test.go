package font_test

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"slices"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/plugins/canvas/font"
	"golang.org/x/image/font/gofont/goregular"
)

// The vertical measures of JetBrains Mono, at an em of 1000 world units,
// and of Go Regular, at 2048, each its font's units to the em, as their
// hhea and OS/2 tables hold them.
var (
	builtInMetrics = font.Metrics{Ascent: 1020, Descent: 300, LineHeight: 1320, XHeight: 550, CapHeight: 730}
	goMetrics      = font.Metrics{Ascent: 1935, Descent: 432, LineHeight: 2367, XHeight: 1086, CapHeight: 1480}
)

func TestAFontOfNoPathIsTheBuiltInJetBrainsMono(t *testing.T) {
	set, failures := font.Load(nil, font.SetDescriptor{})
	if len(failures) != 0 {
		t.Fatalf("the empty set failed with %v, want nothing", failures)
	}
	if got := set.Default().Metrics(1000); !nearMetrics(got, builtInMetrics) {
		t.Errorf("the default font's metrics are %+v, want JetBrains Mono's %+v", got, builtInMetrics)
	}
	if got := set.LanguageFont("ja"); got != set.Default() {
		t.Errorf("a language of no fonts is measured by %p, want the default font, %p", got, set.Default())
	}
}

func TestAFontOfACollectionIsTheOneAtItsIndex(t *testing.T) {
	set, failures := font.Load(makeFontFiles(t), font.SetDescriptor{
		Default:   font.Font{Path: "pair.ttc", Index: 1},
		Languages: []font.LanguageFonts{{Languages: []string{"ja"}, Fonts: []font.Font{{Path: "pair.ttc"}}}},
	})
	if len(failures) != 0 {
		t.Fatalf("the set failed with %v, want nothing", failures)
	}
	if got := set.Default().Metrics(2048); !nearMetrics(got, goMetrics) {
		t.Errorf("the collection's font at index 1 has the metrics %+v, want Go Regular's %+v", got, goMetrics)
	}
	if got := set.LanguageFont("ja").Metrics(1000); !nearMetrics(got, builtInMetrics) {
		t.Errorf("the collection's font at index 0 has the metrics %+v, want JetBrains Mono's %+v", got, builtInMetrics)
	}
}

func TestEachCharacterIsDrawnByTheFirstFontOfItsLanguageHavingItElseTheDefaultFont(t *testing.T) {
	set, _ := font.Load(makeFontFiles(t), font.SetDescriptor{Languages: []font.LanguageFonts{
		{Languages: []string{"zh-Hans"}, Fonts: []font.Font{{Path: "go.ttf"}, {Path: "hanzi.ttf"}}},
	}})
	goFont := set.LanguageFont("zh-Hans")
	for _, lookup := range makeCharacterLookups(set) {
		if got, hasCharacter := set.CharacterFont(lookup.language, lookup.character); got != lookup.font || hasCharacter != lookup.hasCharacter {
			t.Errorf("%q in %q is drawn by %s, having it %v, want %s, %v", lookup.character, lookup.language, got.Font().Path, hasCharacter, lookup.font.Font().Path, lookup.hasCharacter)
		}
	}
	if goFont.Font().Path != "go.ttf" {
		t.Errorf("zh-Hans is measured by %q, want its first font, go.ttf", goFont.Font().Path)
	}
}

func TestACharactersFontIsKeptAsTheLanguageChanges(t *testing.T) {
	set, _ := font.Load(makeFontFiles(t), font.SetDescriptor{Languages: []font.LanguageFonts{
		{Languages: []string{"zh-Hans"}, Fonts: []font.Font{{Path: "hanzi.ttf"}}},
	}})
	var counts []int
	for _, language := range []string{"ja", "zh-Hans", "ja"} {
		set.CharacterFont(language, '今')
		set.CharacterFont(language, '日')
		counts = append(counts, font.CountCharacterFonts(set))
	}
	if !slices.Equal(counts, []int{2, 4, 4}) {
		t.Errorf("the characters found are %v by language, want 2 in ja, then 4 kept as the language changes back", counts)
	}
}

func TestCharacterFontsAreFoundTogether(t *testing.T) {
	// Jobs laying text out together each ask the font of every character;
	// under -race, the cache they share is guarded.
	set, _ := font.Load(makeFontFiles(t), font.SetDescriptor{Languages: []font.LanguageFonts{
		{Languages: []string{"zh-Hans"}, Fonts: []font.Font{{Path: "go.ttf"}, {Path: "hanzi.ttf"}}},
	}})
	lookups := makeCharacterLookups(set)
	var wait sync.WaitGroup
	for range 8 {
		wait.Go(func() {
			for _, lookup := range lookups {
				if got, hasCharacter := set.CharacterFont(lookup.language, lookup.character); got != lookup.font || hasCharacter != lookup.hasCharacter {
					t.Errorf("%q in %q is drawn by %s, having it %v, want %s, %v", lookup.character, lookup.language, got.Font().Path, hasCharacter, lookup.font.Font().Path, lookup.hasCharacter)
				}
			}
		})
	}
	wait.Wait()
}

func TestAFontNamedAgainIsLoadedOnce(t *testing.T) {
	// hanzi.ttf serves ja and zh-Hans in one entry and ko in another; go.ttf
	// is the default and ko's second font.
	files := &countingFS{files: makeFontFiles(t), openCounts: map[string]int{}}
	set, failures := font.Load(files, font.SetDescriptor{
		Default: font.Font{Path: "go.ttf"},
		Languages: []font.LanguageFonts{
			{Languages: []string{"ja", "zh-Hans"}, Fonts: []font.Font{{Path: "hanzi.ttf"}}},
			{Languages: []string{"ko"}, Fonts: []font.Font{{Path: "hanzi.ttf"}, {Path: "go.ttf"}}},
		},
	})
	if len(failures) != 0 {
		t.Fatalf("the set failed with %v, want nothing", failures)
	}
	if files.openCounts["hanzi.ttf"] != 1 || files.openCounts["go.ttf"] != 1 {
		t.Errorf("the files were opened %v times, want each once", files.openCounts)
	}
	hanzi := set.LanguageFont("ja")
	if set.LanguageFont("zh-Hans") != hanzi || set.LanguageFont("ko") != hanzi {
		t.Errorf("ja, zh-Hans and ko are measured by %p, %p and %p, want one font", hanzi, set.LanguageFont("zh-Hans"), set.LanguageFont("ko"))
	}
	if fonts := slices.Collect(set.Fonts()); !slices.Equal(fonts, []*font.Loaded{set.Default(), hanzi}) {
		t.Errorf("the set's fonts are %v, want the default font then hanzi.ttf, each once", fonts)
	}
}

func TestLoadAnswersWhatWentWrongWithEachFontOnce(t *testing.T) {
	ignoring := font.Font{Variations: [4]font.Variation{{Axis: font.AxisTagFromName("weight"), Value: 700}}}
	set, failures := font.Load(makeFontFiles(t), font.SetDescriptor{
		Default: font.Font{Path: "missing.ttf"},
		Languages: []font.LanguageFonts{{Languages: []string{"ja"}, Fonts: []font.Font{
			{Path: "broken.ttf"}, {Path: "pair.ttc", Index: 2}, ignoring, {Path: "broken.ttf"},
		}}},
	})

	wantFonts := []font.Font{{Path: "missing.ttf"}, {Path: "broken.ttf"}, {Path: "pair.ttc", Index: 2}, ignoring}
	if len(failures) != len(wantFonts) {
		t.Fatalf("Load answered %v, want a failure of each of %v", failures, wantFonts)
	}
	for index, failure := range failures {
		if failure.Font != wantFonts[index] {
			t.Errorf("failure %d is of %+v, want %+v", index, failure.Font, wantFonts[index])
		}
	}
	if !errors.Is(failures[0], fs.ErrNotExist) {
		t.Errorf("the missing file failed with %v, want fs.ErrNotExist", failures[0])
	}
	if !errors.Is(failures[3], font.ErrVariationIgnored) {
		t.Errorf("the variation naming no axis failed with %v, want font.ErrVariationIgnored", failures[3])
	}
	if got := set.Default(); got.Font() != (font.Font{}) || !nearMetrics(got.Metrics(1000), builtInMetrics) {
		t.Errorf("the failed default font is replaced by a font of %+v, want JetBrains Mono", got.Font())
	}
	if got := set.LanguageFont("ja").Font(); got != ignoring {
		t.Errorf("ja is measured by a font of %+v, want the one loaded, its variation ignored", got)
	}
}

func TestABitmapFontsSizeIsDrawnAtAWholeMultipleOfItsPixelSize(t *testing.T) {
	set, _ := font.Load(nil, font.SetDescriptor{
		Default:   font.Font{BitmapEm: 10},
		Languages: []font.LanguageFonts{{Languages: []string{"ja"}, Fonts: []font.Font{{}}}},
	})
	for size, want := range map[float32]float32{3: 10, 14: 10, 16: 20, 30: 30} {
		if got := set.Default().DrawnEm(size); got != want {
			t.Errorf("a size of %v is drawn at %v, want %v", size, got, want)
		}
	}
	if got := set.LanguageFont("ja").DrawnEm(14); got != 14 {
		t.Errorf("a font of distance fields draws a size of 14 at %v, want 14", got)
	}
}

// makeFontFiles answers files holding Go Regular at go.ttf, Noto Sans SC's
// ideographs at hanzi.ttf, a file that is no font at broken.ttf, and a
// collection of JetBrains Mono, then Go Regular, at pair.ttc.
func makeFontFiles(t *testing.T) fstest.MapFS {
	t.Helper()
	builtIn, err := fs.ReadFile(font.BuiltInFiles(), "jetbrainsmono.ttf")
	if err != nil {
		t.Fatal(err)
	}
	hanzi, err := os.ReadFile("../testdata/fonts/notosanssc-hanzi.ttf")
	if err != nil {
		t.Fatal(err)
	}
	return fstest.MapFS{
		"go.ttf":     {Data: goregular.TTF},
		"hanzi.ttf":  {Data: hanzi},
		"broken.ttf": {Data: []byte("no font")},
		"pair.ttc":   {Data: makeCollection(builtIn, goregular.TTF)},
	}
}

// makeCollection answers a font collection, a TrueType collection's header
// then each of fonts, each a TrueType font file, its tables' offsets moved
// to where it lies in the collection.
func makeCollection(fonts ...[]byte) []byte {
	headerLength := 12 + 4*len(fonts)
	collection := make([]byte, headerLength)
	copy(collection, "ttcf")
	binary.BigEndian.PutUint32(collection[4:], 0x00010000)
	binary.BigEndian.PutUint32(collection[8:], uint32(len(fonts)))
	for index, fontFile := range fonts {
		for len(collection)%4 != 0 {
			collection = append(collection, 0)
		}
		start := len(collection)
		binary.BigEndian.PutUint32(collection[12+4*index:], uint32(start))
		collection = append(collection, fontFile...)
		for table := range int(binary.BigEndian.Uint16(fontFile[4:])) {
			offset := collection[start+12+16*table+8:]
			binary.BigEndian.PutUint32(offset, binary.BigEndian.Uint32(offset)+uint32(start))
		}
	}
	return collection
}

// nearMetrics answers whether got is want, each measure within a
// thousandth, and none not a number.
func nearMetrics(got, want font.Metrics) bool {
	gotMeasures := [...]float32{got.Ascent, got.Descent, got.LineGap, got.LineHeight, got.XHeight, got.CapHeight}
	wantMeasures := [...]float32{want.Ascent, want.Descent, want.LineGap, want.LineHeight, want.XHeight, want.CapHeight}
	for index := range gotMeasures {
		if !isNear(gotMeasures[index], wantMeasures[index]) {
			return false
		}
	}
	return true
}
