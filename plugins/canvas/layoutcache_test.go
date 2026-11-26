package canvas_test

import (
	"testing"

	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
)

func TestALabelDrawnEveryFrameIsShapedOnceAndFreedOnceUnusedForTheIdleFrames(t *testing.T) {
	// The label is drawn in frames 2 to 6, then not: each frame's read
	// sees the layout cached, without using it, as the frame before left
	// it. Unused in frames 7 and 8, the default 2 idle frames, it is freed
	// at the end of frame 8.
	label := canvas.Text{String: "Score: 120", Size: 20}
	var cached []*canvas.CachedLayout
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &fontLibraryUser{read: func(fonts canvas.FontLibrary, frame int) {
		cached = append(cached, canvas.FindCachedLayout(fonts, label))
	}})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	for frame := range 10 {
		if frame >= 2 && frame <= 6 {
			h.Record(func(list canvas.CommandList) { list.DrawText(label) })
		}
		h.Step()
	}

	if len(cached) != 10 {
		t.Fatalf("%d frames read, want 10", len(cached))
	}
	if cached[3] == nil {
		t.Fatal("frame 3 saw no layout cached of the label frame 2 drew")
	}
	for frame := 4; frame <= 8; frame++ {
		if cached[frame] != cached[3] {
			t.Errorf("frame %d saw the label's layout %p, want the one frame 2 shaped, %p, kept", frame, cached[frame], cached[3])
		}
	}
	if cached[9] != nil {
		t.Error("frame 9 saw the label's layout cached, want it freed at the end of frame 8, its second frame unused")
	}
}

func TestALabelMeasuredThenDrawnIsShapedOnce(t *testing.T) {
	// Measured in frame 0 and drawn in frames 1 to 3: the layout measured
	// is the one drawn, so drawing it keeps it, and frame 4 sees it still.
	label := canvas.Text{String: "Options", Size: 20}
	var cached []*canvas.CachedLayout
	h := canvastest.NewHarness(t)
	h.Spawn(nil, nil, &fontLibraryUser{read: func(fonts canvas.FontLibrary, frame int) {
		if frame == 0 {
			fonts.Layout(label)
		}
		cached = append(cached, canvas.FindCachedLayout(fonts, label))
	}})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})

	h.Step()
	for range 3 {
		h.Record(func(list canvas.CommandList) { list.DrawText(label) })
		h.Step()
	}
	h.Step()

	if cached[0] == nil || cached[4] != cached[0] {
		t.Errorf("frame 4 saw the label's layout %p, want the one frame 0 measured, %p, kept by drawing it", cached[4], cached[0])
	}
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

func TestRunsDifferingInWhatShapesThemAreLaidOutApart(t *testing.T) {
	// One string, laid out once plainly and then wrapped, spaced and
	// tracked in turn, each measured as itself rather than as the layout
	// cached before it.
	plain := canvas.Text{String: "ab cd", Size: 10}
	wrapped := plain
	wrapped.WrapWidth = 25
	spaced := plain
	spaced.LineHeight = 2
	tracked := plain
	tracked.Tracking = 0.5
	layouts, _ := layOutTexts(t, nil, plain, wrapped, spaced, tracked)

	if layouts[1].LineCount() != 2 {
		t.Errorf("the wrapped run has %d lines, want 2", layouts[1].LineCount())
	}
	if !(layouts[2].Size().Y > layouts[0].Size().Y) {
		t.Errorf("the run of twice the line height is %v tall, want taller than the plain run's %v", layouts[2].Size().Y, layouts[0].Size().Y)
	}
	if !(layouts[3].Size().X > layouts[0].Size().X) {
		t.Errorf("the tracked run is %v wide, want wider than the plain run's %v", layouts[3].Size().X, layouts[0].Size().X)
	}
}

func TestARunLaidOutAgainInAnotherLanguageTakesThatLanguagesFont(t *testing.T) {
	// The set's default font is Go Regular, and Japanese's JetBrains Mono:
	// an m laid out in English, then again once the language is Japanese,
	// is as wide as JetBrains Mono's advance.
	h := canvastest.NewHarness(t)
	var set canvas.FontSet
	var widths []float32
	h.Spawn(nil, &canvas.Config{Language: "en"}, &fileMounter{files: makeFontFiles(t)}, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			switch frame {
			case 0:
				set = fonts.MakeFontSet(canvas.FontSetDescriptor{
					Default:   canvas.Font{Path: "go.ttf"},
					Languages: []canvas.LanguageFonts{{Languages: []string{"ja"}, Fonts: []canvas.Font{{Path: builtInFontPath}}}},
				})
			case 1:
				fonts.SetLanguage("ja")
			}
		},
		read: func(fonts canvas.FontLibrary, frame int) {
			widths = append(widths, fonts.Layout(canvas.Text{String: "m", Font: set, Size: 10}).LineWidth(0))
		},
	})
	h.Step()
	h.Step()

	if widths[0] == widths[1] {
		t.Errorf("the m is %v wide in English and Japanese both, want Japanese's font's width", widths[0])
	}
	expectNear(t, "the m in Japanese", widths[1], builtInAdvance*10)
}
