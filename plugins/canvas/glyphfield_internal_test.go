package canvas

import (
	"io/fs"
	"os"
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas/font"
)

func TestAGlyphsFieldHoldsItsDistanceAcrossItsEdgeAndReachesHalfItsRangePastIt(t *testing.T) {
	// JetBrains Mono's full block spans -10 to 610 of its 1000 units to the
	// em across, -300 to 1020 up: at 100 texels to the em, -1 to 61 and
	// -102 to 30 down, each on a whole texel, and its field reaches 9
	// texels past them, half its range of 17 rounded up.
	builtIn := loadDefaultFont(t, nil, font.Font{})
	var shaper font.Shaper
	block := findGlyph(&shaper, builtIn, '█')

	field := makeGlyphField(&shaper, builtIn.Default(), block, 100, 17)

	wantBounds := m.Rect{X: -0.10, Y: -1.11, Width: 0.80, Height: 1.50}
	if field.Width != 80 || field.Height != 150 || field.spread != 8.5 || !nearRect(field.Bounds, wantBounds) {
		t.Errorf("the block's field is %d by %d texels, of spread %v, over %+v em, want 80 by 150, 8.5, over %+v", field.Width, field.Height, field.spread, field.Bounds, wantBounds)
	}
	// Along the middle row, each texel centre lies a whole and a half
	// texels from the left edge, at texel 9: 0.5 + d/17 of the way up, in a
	// byte, exactly.
	row := field.Texels[75*field.Width:][:field.Width]
	for texel, want := range map[int]byte{0: 0, 7: 105, 8: 120, 9: 135, 10: 150, 17: 255, 40: 255} {
		if row[texel] != want {
			t.Errorf("texel %d of the middle row is %d, want %d", texel, row[texel], want)
		}
	}
}

func TestASpaceHasNoField(t *testing.T) {
	builtIn := loadDefaultFont(t, nil, font.Font{})
	var shaper font.Shaper
	space := findGlyph(&shaper, builtIn, ' ')
	if field := makeGlyphField(&shaper, builtIn.Default(), space, 48, 16); field.Texels != nil {
		t.Errorf("a space's field has %d texels, want none", len(field.Texels))
	}
}

func TestGlyphsLaidOutForDrawingSitOnTheirClustersOnAlignedLinesAndShareFields(t *testing.T) {
	// Two centred lines of a monospaced font, the second wider, each glyph
	// one cluster's.
	library := newFontLibrary(&reporter{logger: &kernel.Logger{}, inspector: &inspector{}}, &Config{})

	var layout TextLayout
	library.layOutText(Text{String: "a\nbbb", Align: TextAlignCenter}, true, func(_ Text, laidOut TextLayout) { layout = laidOut })

	// The newline is cluster 1, and draws no glyph.
	clusters := []int{0, 2, 3, 4}
	if len(layout.glyphs) != len(clusters) {
		t.Fatalf("the run laid out %d glyphs, want %d", len(layout.glyphs), len(clusters))
	}
	for index, cluster := range clusters {
		if pen, want := layout.glyphs[index].pen, layout.ClusterPosition(cluster); !nearVec2(pen, want) {
			t.Errorf("glyph %d's pen is %v, want cluster %d's position, %v", index, pen, cluster, want)
		}
	}
	fieldOfB := layout.glyphs[1].field
	if layout.glyphs[0].field == fieldOfB || layout.glyphs[2].field != fieldOfB || layout.glyphs[3].field != fieldOfB {
		t.Errorf("the glyphs' fields are %v, want a's its own and the three b's one", layout.glyphs)
	}
	if len(library.glyphs.fields) != 2 {
		t.Errorf("the glyph table holds %d fields, want a's and b's", len(library.glyphs.fields))
	}
}

func TestAGlyphsPenFollowsTheAdvancesBeforeItInItsCluster(t *testing.T) {
	// A combining acute accent after an x is one cluster of two glyphs,
	// which JetBrains Mono shapes with no offset: the accent's pen lies past
	// the x's advance, where the cluster ends.
	library := newFontLibrary(&reporter{logger: &kernel.Logger{}, inspector: &inspector{}}, &Config{})

	var layout TextLayout
	library.layOutText(Text{String: "x́"}, true, func(_ Text, laidOut TextLayout) { layout = laidOut })

	if len(layout.glyphs) != 2 {
		t.Fatalf("the run laid out %d glyphs, want the x and the accent", len(layout.glyphs))
	}
	if accent, clusterEnd := layout.glyphs[1].pen.X, layout.ClusterPosition(1).X; accent != clusterEnd || clusterEnd <= layout.glyphs[0].pen.X {
		t.Errorf("the accent's pen is at %v, want past the x's advance, at %v", accent, clusterEnd)
	}
}

// BenchmarkMakeGlyphField benchmarks making a glyph's distance field at its
// first use, reporting the time per glyph: JetBrains Mono's Latin letters
// at an em of 48 texels, canvas's default, and Noto Sans SC's ideographs
// at 64, as a CJK font takes, each of a range of 16.
func BenchmarkMakeGlyphField(b *testing.B) {
	hanziSet := loadDefaultFont(b, os.DirFS("testdata/fonts"), font.Font{Path: "notosanssc-hanzi.ttf"})
	latinSet := loadDefaultFont(b, nil, font.Font{})
	var shaper font.Shaper
	var latin, hanzi []font.GlyphID
	for letter := 'a'; letter <= 'z'; letter++ {
		latin = append(latin, findGlyph(&shaper, latinSet, letter))
	}
	for ideograph := rune(0x4E00); ideograph < 0x4F00; ideograph++ {
		hanzi = append(hanzi, findGlyph(&shaper, hanziSet, ideograph))
	}
	for _, glyphs := range []struct {
		name string
		set  *font.Set
		ids  []font.GlyphID
		em   int
	}{
		{"latin/em=48", latinSet, latin, 48},
		{"han/em=64", hanziSet, hanzi, 64},
	} {
		b.Run(glyphs.name, func(b *testing.B) {
			index := 0
			for b.Loop() {
				makeGlyphField(&shaper, glyphs.set.Default(), glyphs.ids[index%len(glyphs.ids)], glyphs.em, 16)
				index++
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N), "ns/glyph")
		})
	}
}

// loadDefaultFont answers a set of descriptor alone, loaded through files
// as its default, failing tb when it does not load.
func loadDefaultFont(tb testing.TB, files fs.FS, descriptor font.Font) *font.Set {
	set, failures := font.Load(files, font.SetDescriptor{Default: descriptor})
	if len(failures) != 0 {
		tb.Fatal(failures)
	}
	return set
}

// findGlyph answers the glyph of set's default font character is shaped
// as alone, laid out by shaper.
func findGlyph(shaper *font.Shaper, set *font.Set, character rune) font.GlyphID {
	return shaper.LayOut(font.Input{Set: set, Runes: []rune{character}, Em: 1}).Glyphs[0].ID
}
