package canvas_test

import (
	"bytes"
	"slices"
	"testing"

	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// smallGlyphAtlasConfig is canvas's config whose glyph atlas is one page of
// 84 texels square: the upper half block's field, a cell 49 by 50 texels,
// takes the first shelf, and the full stop's, 27 by 27, a second below it,
// which leaves room for neither the small a's, 40 by 46, which the upper
// half block's cell holds, nor the equals sign's, 39 by 34, which the full
// stop's shelf, merged with the rows below it, holds.
var smallGlyphAtlasConfig = &canvas.Config{AtlasPageSize: 84, AtlasLayerCeiling: 1}

// drawGlyphFrames draws a frame of each of drawnStrings in a harness spawned
// with config, a run of it alone, and answers the frames inspected.
func drawGlyphFrames(t *testing.T, config *canvas.Config, expectedReports []canvas.ReportKind, drawnStrings ...string) (*canvastest.Harness, []canvas.Inspection) {
	t.Helper()
	h := canvastest.NewHarness(t)
	for _, kind := range expectedReports {
		h.ExpectReport(kind)
	}
	h.Spawn(nil, config)
	h.OpenWindow(platform.Size{Width: 320, Height: 240})
	for _, drawnString := range drawnStrings {
		h.Record(func(list canvas.CommandList) {
			list.DrawText(canvas.Text{String: drawnString})
		})
		h.Step()
	}
	frames := h.Frames()
	if len(frames) != len(drawnStrings) {
		t.Fatalf("the harness inspected %d frames, want %d", len(frames), len(drawnStrings))
	}
	return h, frames
}

// findGlyphCells answers the cells each of frames copied glyphs into, each
// its top left on its page, the glyph atlas found among the textures any of
// them made.
func findGlyphCells(frames []canvas.Inspection) [][]render.TextureRegion {
	labels := findTextureLabels(findRenderFrames(frames)...)
	cells := make([][]render.TextureRegion, len(frames))
	for index, frame := range frames {
		for _, region := range findCopyRegions(frame.Render, labels, glyphAtlasLabel) {
			cells[index] = append(cells[index], render.TextureRegion{Layer: region.Layer, X: region.X, Y: region.Y})
		}
	}
	return cells
}

// countDrawnGlyphs answers how many glyphs frame's first camera drew.
func countDrawnGlyphs(frame canvas.Inspection) uint32 {
	var count uint32
	for _, batch := range frame.Cameras[0].Batches {
		count += batch.InstanceCount
	}
	return count
}

func TestAFullGlyphAtlasEvictsOfGlyphsLastDrawnInOneFrameTheOneMadeFirst(t *testing.T) {
	// The upper half block and the full stop, drawn in the first frame,
	// fill the atlas; the small a, drawn in the second, evicts the upper
	// half block, made first, and takes its cell. The full stop, drawn in
	// the third, is still packed.
	_, frames := drawGlyphFrames(t, smallGlyphAtlasConfig, nil, "▀.", "a", ".")
	cells := findGlyphCells(frames)

	if got, want := cells[0], []render.TextureRegion{{}, {Y: 50}}; !slices.Equal(got, want) {
		t.Fatalf("the first frame copied glyphs into cells %+v, want the upper half block's at the top left and the full stop's below it, %+v", got, want)
	}
	if got, want := cells[1], []render.TextureRegion{{}}; !slices.Equal(got, want) || countDrawnGlyphs(frames[1]) != 1 {
		t.Errorf("the second frame copied glyphs into cells %+v and drew %d glyphs, want the small a drawn from the upper half block's cell, %+v", got, countDrawnGlyphs(frames[1]), want)
	}
	if got := cells[2]; len(got) != 0 || countDrawnGlyphs(frames[2]) != 1 {
		t.Errorf("the third frame copied glyphs into cells %+v and drew %d glyphs, want the full stop drawn from its cell, copying nothing", got, countDrawnGlyphs(frames[2]))
	}
}

func TestAFullGlyphAtlasEvictsTheGlyphDrawnLeastLately(t *testing.T) {
	// The upper half block, made first, is drawn again in the second
	// frame, so the equals sign, drawn in the third, evicts the full stop,
	// drawn least lately, and takes its shelf. The upper half block, drawn
	// in the fourth, is still packed.
	_, frames := drawGlyphFrames(t, smallGlyphAtlasConfig, nil, "▀.", "▀", "=", "▀")
	cells := findGlyphCells(frames)

	if got, want := cells[2], []render.TextureRegion{{Y: 50}}; !slices.Equal(got, want) || countDrawnGlyphs(frames[2]) != 1 {
		t.Errorf("the third frame copied glyphs into cells %+v and drew %d glyphs, want the equals sign drawn from the full stop's cell, %+v", got, countDrawnGlyphs(frames[2]), want)
	}
	if got := cells[3]; len(got) != 0 || countDrawnGlyphs(frames[3]) != 1 {
		t.Errorf("the fourth frame copied glyphs into cells %+v and drew %d glyphs, want the upper half block drawn from its cell, copying nothing", got, countDrawnGlyphs(frames[3]))
	}
}

func TestAGlyphTheFullAtlasHasNoRoomForBesideTheFramesOwnIsRefusedReportedOnceAndDrawnInALaterFrameWithRoom(t *testing.T) {
	// The small a, drawn beside the upper half block and the full stop,
	// which fill the atlas, evicts neither of them: it draws nothing in
	// the first two frames, reported once, and in the third, drawn alone,
	// takes the upper half block's cell.
	h, frames := drawGlyphFrames(t, smallGlyphAtlasConfig, []canvas.ReportKind{canvas.ReportKindAtlasFull}, "▀.a", "▀.a", "a")
	cells := findGlyphCells(frames)

	for index, frame := range frames[:2] {
		if count := countDrawnGlyphs(frame); count != 2 {
			t.Errorf("frame %d drew %d glyphs, want the upper half block and the full stop alone", index, count)
		}
	}
	if got := cells[1]; len(got) != 0 {
		t.Errorf("the second frame copied glyphs into cells %+v, want nothing copied, the small a refused again", got)
	}
	if count := countReports(h, canvas.ReportKindAtlasFull); count != 1 {
		t.Errorf("the small a refused was reported %d times, want once", count)
	}
	if got, want := cells[2], []render.TextureRegion{{}}; !slices.Equal(got, want) || countDrawnGlyphs(frames[2]) != 1 {
		t.Errorf("the third frame copied glyphs into cells %+v and drew %d glyphs, want the small a drawn from the upper half block's cell, %+v", got, countDrawnGlyphs(frames[2]), want)
	}
}

func TestABitmapGlyphPackedIntoAnEvictedGlyphsCellDrawsAsInAnEmptyAtlas(t *testing.T) {
	// A bitmap font of 10 pixels to the em on a glyph atlas of one page of
	// 12 texels: the upper half block's cell, 10 texels square, opaque to
	// its edges, leaves room for no full stop, 4 by 5, which evicts it and
	// takes its cell. Drawn ten times its pixel size with linear filtering,
	// the full stop's edges read its gutter: they draw as the full stop
	// packed into an empty atlas draws, holding nothing of the block.
	config := *bitmapFontConfig
	config.AtlasPageSize = 12
	config.AtlasLayerCeiling = 1
	fullStop := canvas.Text{String: ".", Size: 100, Filter: canvas.FilterLinear}
	// Each harness's window closes as its test ends.
	var reference windowPicture
	t.Run("in an empty atlas", func(t *testing.T) {
		reference = captureHarnessDrawing(t, canvastest.NewHarness(t), &config, func(list canvas.CommandList) {
			list.DrawText(fullStop)
		})
	})
	if reference.picture == nil {
		t.FailNow()
	}
	h := canvastest.NewHarness(t)
	frameCount := 0
	evictedCellPicture := captureHarnessDrawing(t, h, &config, func(list canvas.CommandList) {
		frameCount++
		if frameCount == 1 {
			list.DrawText(canvas.Text{String: "▀", Size: 100, Filter: canvas.FilterLinear})
			return
		}
		list.DrawText(fullStop)
	})

	cells := findGlyphCells(h.Frames())
	if len(cells) < 2 || !slices.Equal(cells[0], []render.TextureRegion{{}}) || !slices.Equal(cells[1], []render.TextureRegion{{}}) {
		t.Fatalf("the frames copied glyphs into cells %+v, want the upper half block's at the top left, then the full stop's there", cells)
	}
	if !bytes.Equal(evictedCellPicture.picture.Pix, reference.picture.Pix) {
		t.Errorf("the full stop packed into the upper half block's cell drew otherwise than packed into an empty atlas, want it to hold nothing of the block")
	}
}
