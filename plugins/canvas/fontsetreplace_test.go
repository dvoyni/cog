package canvas_test

import (
	"slices"
	"testing"

	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/canvas/font"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// replacedGlyphAtlasConfig is canvas's config whose glyph atlas has pages
// of 84 texels square, two at most: the upper half block's field and the
// full stop's fill the first, as smallGlyphAtlasConfig says, so the same
// glyphs of another set land on the first page only in the cells of a set
// let go, and on a second page otherwise.
var replacedGlyphAtlasConfig = &canvas.Config{AtlasPageSize: 84, AtlasLayerCeiling: 2}

// replacedSetCells are the cells of the upper half block and the full stop
// on the glyph atlas's first page.
var replacedSetCells = []render.TextureRegion{{}, {Y: 50}}

func TestAGlyphMadeAfterItsSetIsDestroyedLandsInAFreedCell(t *testing.T) {
	// The set of canvas's own JetBrains Mono file draws "▀." in the first
	// frame and is destroyed in the second; the default set's "▀.", drawn
	// in the third, lands in its cells rather than on a second page.
	var set canvas.FontSet
	frames := drawReplacedSetFrames(t, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			switch frame {
			case 0:
				set = fonts.MakeFontSet(font.SetDescriptor{Default: font.Font{Path: builtInFontPath}})
			case 1:
				fonts.DestroyFontSet(set)
			}
		},
		draw: func(list canvas.CommandList, frame int) {
			switch frame {
			case 0:
				list.DrawText(canvas.Text{String: "▀.", Font: set})
			case 2:
				list.DrawText(canvas.Text{String: "▀."})
			}
		},
	})
	cells := findGlyphCells(frames)

	if !slices.Equal(cells[0], replacedSetCells) {
		t.Fatalf("the set's glyphs were copied into cells %+v, want %+v", cells[0], replacedSetCells)
	}
	if !slices.Equal(cells[2], replacedSetCells) || countDrawnGlyphs(frames[2]) != 2 {
		t.Errorf("the default set's glyphs, made after the set was destroyed, were copied into cells %+v and %d drawn, want both drawn from the destroyed set's cells, %+v", cells[2], countDrawnGlyphs(frames[2]), replacedSetCells)
	}
}

func TestARunRecordedBeforeItsSetIsReplacedInItsFrameStillDrawsItsGlyphsLetGoInALaterFrame(t *testing.T) {
	// The set draws "▀." first in the second frame, then is destroyed in
	// that frame, after the run is recorded: its glyphs are still made and
	// drawn, and the default set's "▀.", drawn in the third, lands in their
	// cells.
	var set canvas.FontSet
	frames := drawReplacedSetFrames(t, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			if frame == 0 {
				set = fonts.MakeFontSet(font.SetDescriptor{Default: font.Font{Path: builtInFontPath}})
			}
		},
		draw: func(list canvas.CommandList, frame int) {
			switch frame {
			case 1:
				list.DrawText(canvas.Text{String: "▀.", Font: set})
			case 2:
				list.DrawText(canvas.Text{String: "▀."})
			}
		},
		lateWrite: func(fonts canvas.WriteFontLibrary, frame int) {
			if frame == 1 {
				fonts.DestroyFontSet(set)
			}
		},
	})
	cells := findGlyphCells(frames)

	if !slices.Equal(cells[1], replacedSetCells) || countDrawnGlyphs(frames[1]) != 2 {
		t.Fatalf("the run recorded before its set was destroyed copied glyphs into cells %+v and drew %d, want both drawn from %+v", cells[1], countDrawnGlyphs(frames[1]), replacedSetCells)
	}
	if !slices.Equal(cells[2], replacedSetCells) || countDrawnGlyphs(frames[2]) != 2 {
		t.Errorf("the default set's glyphs, drawn the frame after, were copied into cells %+v and %d drawn, want both drawn from the destroyed set's cells, %+v", cells[2], countDrawnGlyphs(frames[2]), replacedSetCells)
	}
}

// drawReplacedSetFrames draws three frames of user in a harness spawned with
// replacedGlyphAtlasConfig and a window, and answers the frames inspected.
func drawReplacedSetFrames(t *testing.T, user *fontLibraryUser) []canvas.Inspection {
	t.Helper()
	h := canvastest.NewHarness(t)
	h.Spawn(nil, replacedGlyphAtlasConfig, user)
	h.OpenWindow(platform.Size{Width: 320, Height: 240})
	for range 3 {
		h.Step()
	}
	frames := h.Frames()
	if len(frames) != 3 {
		t.Fatalf("the harness inspected %d frames, want 3", len(frames))
	}
	return frames
}
