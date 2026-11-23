package canvas

import (
	"testing"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/go-text/typesetting/font"
)

func TestAGlyphsFieldHoldsItsDistanceAcrossItsEdgeAndReachesHalfItsRangePastIt(t *testing.T) {
	// JetBrains Mono's full block spans -10 to 610 of its 1000 units to the
	// em across, -300 to 1020 up: at 100 texels to the em, -1 to 61 and
	// -102 to 30 down, each on a whole texel, and its field reaches 9
	// texels past them, half its range of 17 rounded up.
	builtIn := findBuiltInFont()
	face := font.NewFace(builtIn)
	block, _ := face.NominalGlyph('█')

	field := makeGlyphField(face, block, 100, 17)

	wantBounds := m.Rect{X: -0.10, Y: -1.11, Width: 0.80, Height: 1.50}
	if field.width != 80 || field.height != 150 || field.spread != 8.5 || !nearRect(field.bounds, wantBounds) {
		t.Errorf("the block's field is %d by %d texels, of spread %v, over %+v em, want 80 by 150, 8.5, over %+v", field.width, field.height, field.spread, field.bounds, wantBounds)
	}
	// Along the middle row, each texel centre lies a whole and a half
	// texels from the left edge, at texel 9: 0.5 + d/17 of the way up, in a
	// byte, exactly.
	row := field.texels[75*field.width:][:field.width]
	for texel, want := range map[int]byte{0: 0, 7: 105, 8: 120, 9: 135, 10: 150, 17: 255, 40: 255} {
		if row[texel] != want {
			t.Errorf("texel %d of the middle row is %d, want %d", texel, row[texel], want)
		}
	}
}

func TestASpaceHasNoField(t *testing.T) {
	face := font.NewFace(findBuiltInFont())
	space, _ := face.NominalGlyph(' ')
	if field := makeGlyphField(face, space, 48, 16); field.texels != nil {
		t.Errorf("a space's field has %d texels, want none", len(field.texels))
	}
}

func TestGlyphsLaidOutForDrawingSitOnTheirClustersOnAlignedLinesAndShareFields(t *testing.T) {
	// Two centred lines of a monospaced font, the second wider, each glyph
	// one cluster's.
	library := newFontLibrary(&reporter{logger: &kernel.Logger{}, inspector: &inspector{}}, &Config{})

	_, layout := library.layOutText(Text{String: "a\nbbb", Align: TextAlignCenter}, true, nil)

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

	_, layout := library.layOutText(Text{String: "x́"}, true, nil)

	if len(layout.glyphs) != 2 {
		t.Fatalf("the run laid out %d glyphs, want the x and the accent", len(layout.glyphs))
	}
	if accent, clusterEnd := layout.glyphs[1].pen.X, layout.ClusterPosition(1).X; accent != clusterEnd || clusterEnd <= layout.glyphs[0].pen.X {
		t.Errorf("the accent's pen is at %v, want past the x's advance, at %v", accent, clusterEnd)
	}
}
