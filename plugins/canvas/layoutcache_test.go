package canvas_test

import (
	"slices"
	"testing"

	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
)

func TestALabelDrawnEveryFrameIsShapedOnceAndFreedOnceUnusedForTheIdleFrames(t *testing.T) {
	// Each label is drawn in frames 2 to 6, and once more: shaped in frame
	// 2 alone, and unused in frames 7 and 8, the default 2 idle frames, it
	// is freed at the end of frame 8, so the label drawn again in frame 8
	// finds its layout kept and the one drawn again in frame 9 is shaped
	// again.
	labels := map[int]canvas.Text{8: {String: "Score: 120", Size: 20}, 9: {String: "Score: 340", Size: 20}}
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil)
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	for frame := range 10 {
		h.Record(func(list canvas.CommandList) {
			for again, label := range labels {
				if frame >= 2 && frame <= 6 || frame == again {
					list.DrawText(label)
				}
			}
		})
		h.Step()
	}

	for again, want := range map[int][]int{8: {2}, 9: {2, 9}} {
		if shaped := findFramesLayingOut(h.Frames(), labels[again]); !slices.Equal(shaped, want) {
			t.Errorf("the label drawn again in frame %d is shaped in frames %v, want %v", again, shaped, want)
		}
	}
}

func TestALabelMeasuredThenDrawnIsShapedOnce(t *testing.T) {
	// Measured in frame 0 and drawn in frames 1 to 3: the layout measured
	// is the one drawn, so only frame 0 shapes it.
	label := canvas.Text{String: "Options", Size: 20}
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &fontLibraryUser{read: func(fonts canvas.FontLibrary, frame int) {
		if frame == 0 {
			fonts.Layout(label)
		}
	}})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	for range 3 {
		h.Record(func(list canvas.CommandList) { list.DrawText(label) })
		h.Step()
	}

	if shaped := findFramesLayingOut(h.Frames(), label); !slices.Equal(shaped, []int{0}) {
		t.Errorf("the label is shaped in frames %v, want frame 0 alone, which measured it", shaped)
	}
}

// findFramesLayingOut answers the index of each of frames that laid out
// run, measured or drawn, once for each layout of it made, as its
// MadeLayouts say: a run of its String, Size and Font, and of its Spans
// unless it is markup, whose spans are read from its String.
func findFramesLayingOut(frames []canvas.Inspection, run canvas.Text) []int {
	var laidOut []int
	for index, frame := range frames {
		for _, made := range frame.MadeLayouts {
			if made.String == run.String && made.Size == run.Size && made.Font == run.Font && (run.Markup || slices.Equal(made.Spans, run.Spans)) {
				laidOut = append(laidOut, index)
			}
		}
	}
	return laidOut
}

func TestAPreloadedStringsFirstDrawMakesNoGlyph(t *testing.T) {
	// Frame 0 preloads the string, frame 1 draws a run of it at a size of
	// its own, and frame 2 counts the glyphs made.
	const preloaded = "Loading…"
	var before, afterPreload, afterDraw int
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &fontLibraryUser{read: func(fonts canvas.FontLibrary, frame int) {
		switch frame {
		case 0:
			before = canvas.CountGlyphs(fonts)
			fonts.Preload(canvas.FontSet{}, preloaded)
			afterPreload = canvas.CountGlyphs(fonts)
		case 2:
			afterDraw = canvas.CountGlyphs(fonts)
		}
	}})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	h.Record(func(list canvas.CommandList) { list.DrawText(canvas.Text{String: preloaded, Size: 30}) })
	h.Step()
	h.Step()

	if afterPreload <= before {
		t.Errorf("preloading made %d glyphs, want the string's", afterPreload-before)
	}
	if afterDraw != afterPreload {
		t.Errorf("the first draw of the preloaded string made %d glyphs, want none", afterDraw-afterPreload)
	}
	if batches := h.Frames()[1].Cameras[0].Batches; len(batches) != 1 {
		t.Errorf("the preloaded string drew batches %+v, want one", batches)
	}
}
