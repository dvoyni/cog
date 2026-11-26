package canvas_test

import (
	"math"
	"os"
	"sync"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/canvas/font"
)

// japaneseFontPath is testdata/fonts' subset of Noto Sans JP, holding the
// characters these tests lay out.
const japaneseFontPath = "notosansjp.ttf"

// builtInAdvance is the advance of every glyph of JetBrains Mono, a
// monospaced font, in em.
const builtInAdvance = 0.6

// expectNear fails t unless got is want within a thousandth of a world unit.
func expectNear(t *testing.T, label string, got, want float32) {
	t.Helper()
	if !(math.Abs(float64(got-want)) <= 1e-3) {
		t.Errorf("%s: %v, want %v", label, got, want)
	}
}

// expectLineWidths fails t unless layout's lines are as many as want, each
// of its width.
func expectLineWidths(t *testing.T, label string, layout canvas.TextLayout, want ...float32) {
	t.Helper()
	if layout.LineCount() != len(want) {
		t.Errorf("%s: %d lines, want %d", label, layout.LineCount(), len(want))
		return
	}
	for line, width := range want {
		expectNear(t, label+": line width", layout.LineWidth(line), width)
	}
}

// TestLayoutsMadeAtOnceAreEachAsOneMadeAlone lays runs out at once, as
// tasks holding FontLibrary's read lock may, of two weights of JetBrains
// Mono, which share one parsed font, and of a font of its own; run under
// -race, it breaks a face or shaper shared between layouts.
func TestLayoutsMadeAtOnceAreEachAsOneMadeAlone(t *testing.T) {
	// JetBrains Mono, the default set's and the bold set's font, has no
	// ideographs, so each of those sets draws them as missing-glyph boxes.
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindCharacterMissing)
	sets := make([]canvas.FontSet, 3)
	var mismatchCount int
	h.Spawn(nil, nil, &fileMounter{files: os.DirFS("testdata/fonts")}, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			sets[1] = fonts.MakeFontSet(font.SetDescriptor{Default: font.Font{Variations: [4]font.Variation{{Axis: font.AxisTagFromName("wght"), Value: 800}}}})
			sets[2] = fonts.MakeFontSet(font.SetDescriptor{Default: font.Font{Path: japaneseFontPath}})
		},
		read: func(fonts canvas.FontLibrary, frame int) {
			text := canvas.Text{String: "日本 ffi words, wrapped", Size: 10, WrapWidth: 60}
			alone := make([]m.Vec2, len(sets))
			for index, set := range sets {
				text.Font = set
				alone[index] = fonts.Layout(text).Size()
			}
			var group sync.WaitGroup
			var lock sync.Mutex
			for worker := range 8 {
				group.Go(func() {
					text := text
					text.Font = sets[worker%len(sets)]
					for range 50 {
						if fonts.Layout(text).Size() != alone[worker%len(sets)] {
							lock.Lock()
							mismatchCount++
							lock.Unlock()
						}
					}
				})
			}
			group.Wait()
		},
	})
	h.Step()
	if mismatchCount != 0 {
		t.Errorf("%d runs laid out at once measured otherwise than alone, want none", mismatchCount)
	}
}
