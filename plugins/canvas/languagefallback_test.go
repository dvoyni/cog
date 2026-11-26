package canvas_test

import (
	"image"
	"os"
	"slices"
	"sync"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/canvas/font"
)

// The fonts of testdata/fonts, subsets of Noto Sans Arabic and of Noto
// Sans SC, the second holding Basic Latin and the ideographs these tests
// lay out.
const (
	arabicFontPath  = "notosansarabic.ttf"
	chineseFontPath = "notosanssc.ttf"
)

// captureFontDrawing draws record's entries under canvas whose default set
// is set and whose current language is language, testdata/fonts mounted,
// in a subtest of t named name, whose kernel quits before another starts,
// and answers the picture.
func captureFontDrawing(t *testing.T, name string, set font.SetDescriptor, language string, record func(list canvas.CommandList)) windowPicture {
	t.Helper()
	var picture windowPicture
	t.Run(name, func(t *testing.T) {
		config := &canvas.Config{DefaultFontSet: set, Language: language}
		picture = captureHarnessDrawing(t, canvastest.NewHarness(t), config, record, &fileMounter{files: os.DirFS("testdata/fonts")})
	})
	if picture.picture == nil {
		t.FailNow()
	}
	return picture
}

// countDifferingPixels answers how many pixels of a within bounds differ
// from b's moved right by shift by more than tolerance in a channel, and
// the largest difference in a channel.
func countDifferingPixels(a, b windowPicture, bounds image.Rectangle, shift int, tolerance int) (count, largest int) {
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixelA := a.picture.NRGBAAt(x, y)
			pixelB := b.picture.NRGBAAt(x+shift, y)
			difference := 0
			for _, channels := range [][2]uint8{{pixelA.R, pixelB.R}, {pixelA.G, pixelB.G}, {pixelA.B, pixelB.B}, {pixelA.A, pixelB.A}} {
				difference = max(difference, int(channels[0])-int(channels[1]), int(channels[1])-int(channels[0]))
			}
			largest = max(largest, difference)
			if difference > tolerance {
				count++
			}
		}
	}
	return count, largest
}

func TestARunInJapaneseAndOneInChineseDrawThroughEachLanguagesFontFromOneSet(t *testing.T) {
	// 今 is drawn twice by a set serving ja with Noto Sans JP and zh-Hans
	// with Noto Sans SC: on the left in the current language, ja, and on
	// the right in its own, zh-Hans. Each half is as the language's font
	// alone draws it, and the two fonts draw it apart.
	left := canvas.Text{String: "今", Size: 80, Transform: m.Translation2x3(-pictureWidth/4, 0)}
	right := left
	right.Transform = m.Translation2x3(pictureWidth/4, 0)
	right.Language = "zh-Hans"
	both := captureFontDrawing(t, "both languages", font.SetDescriptor{Languages: []font.LanguageFonts{
		{Languages: []string{"ja"}, Fonts: []font.Font{{Path: japaneseFontPath}}},
		{Languages: []string{"zh-Hans"}, Fonts: []font.Font{{Path: chineseFontPath}}},
	}}, "ja", func(list canvas.CommandList) {
		list.DrawText(left)
		list.DrawText(right)
	})
	japanese := captureFontDrawing(t, "Noto Sans JP alone", font.SetDescriptor{Default: font.Font{Path: japaneseFontPath}}, "", func(list canvas.CommandList) {
		list.DrawText(left)
	})
	right.Language = ""
	chinese := captureFontDrawing(t, "Noto Sans SC alone", font.SetDescriptor{Default: font.Font{Path: chineseFontPath}}, "", func(list canvas.CommandList) {
		list.DrawText(right)
	})

	bounds := both.picture.Bounds()
	leftHalf := image.Rect(bounds.Min.X, bounds.Min.Y, bounds.Dx()/2, bounds.Max.Y)
	rightHalf := image.Rect(bounds.Dx()/2, bounds.Min.Y, bounds.Max.X, bounds.Max.Y)
	// A glyph packed elsewhere in the glyph atlas than alone reads its field
	// through texture coordinates rounded otherwise, a level apart at most.
	if count, largest := countDifferingPixels(both, japanese, leftHalf, 0, 1); count != 0 {
		t.Errorf("the run in ja draws %d pixels otherwise than Noto Sans JP alone draws them, by up to %d, want none by more than 1", count, largest)
	}
	if count, largest := countDifferingPixels(both, chinese, rightHalf, 0, 1); count != 0 {
		t.Errorf("the run in zh-Hans draws %d pixels otherwise than Noto Sans SC alone draws them, by up to %d, want none by more than 1", count, largest)
	}
	if count, _ := countDifferingPixels(both, both, leftHalf, bounds.Dx()/2, 1); count < 100 {
		t.Errorf("the runs in ja and zh-Hans draw %d pixels apart, want each language's font drawing its own 今, hundreds apart", count)
	}
}

func TestACharacterNoFontOfTheSetHasDrawsTheDefaultFontsMissingGlyphBoxReportedOnce(t *testing.T) {
	// JetBrains Mono, the default set's only font, has no Hangul: each of
	// two runs of it draws its missing-glyph box, frame after frame, and the
	// set is reported once.
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindCharacterMissing)
	picture := captureHarnessDrawing(t, h, nil, func(list canvas.CommandList) {
		list.DrawText(canvas.Text{String: "한", Size: 40, Transform: m.Translation2x3(-40, 0)})
		list.DrawText(canvas.Text{String: "글", Size: 40, Transform: m.Translation2x3(40, 0)})
	})
	for _, centre := range []m.Vec2{{X: -40}, {X: 40}} {
		inks := picture.findColumnInk(centre.Add(m.Vec2{X: -10}), centre.Add(m.Vec2{X: 10}), -15, 15)
		if slices.Max(inks) < 255 {
			t.Errorf("the run at %v drew columns of ink %v, want JetBrains Mono's missing-glyph box", centre, inks)
		}
	}
	if count := countReports(h, canvas.ReportKindCharacterMissing); count != 1 {
		t.Errorf("%d characters missing reported, want the default set once", count)
	}
}

func TestChangingTheLanguageClearsNothing(t *testing.T) {
	// The label is laid out in ja in frame 0, in zh-Hans in frame 1, then
	// in ja again in frame 2, which finds the layout of frame 0 kept.
	set := font.SetDescriptor{Languages: []font.LanguageFonts{
		{Languages: []string{"ja"}, Fonts: []font.Font{{Path: japaneseFontPath}}},
		{Languages: []string{"zh-Hans"}, Fonts: []font.Font{{Path: chineseFontPath}}},
	}}
	label := canvas.Text{String: "今日", Size: 10}
	languages := []string{"ja", "zh-Hans", "ja"}
	h := canvastest.NewHarness(t)
	h.Spawn(nil, &canvas.Config{DefaultFontSet: set}, &fileMounter{files: os.DirFS("testdata/fonts")}, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			fonts.SetLanguage(languages[frame])
		},
		read: func(fonts canvas.FontLibrary, frame int) {
			fonts.Layout(label)
		},
	})
	for range languages {
		h.Step()
	}

	var shapedIn [][]string
	for _, frame := range h.Frames() {
		var frameLanguages []string
		for _, made := range frame.MadeLayouts {
			frameLanguages = append(frameLanguages, made.Language)
		}
		shapedIn = append(shapedIn, frameLanguages)
	}
	if want := [][]string{{"ja"}, {"zh-Hans"}, nil}; !slices.EqualFunc(shapedIn, want, slices.Equal[[]string]) {
		t.Errorf("the frames shape the label in %q, want %q, frame 0's layout kept", shapedIn, want)
	}
}

func TestRunsLaidOutAtOnceFindTheFontsOfTheirCharactersTogether(t *testing.T) {
	// Eight jobs lay out runs in zh-Hans at once, served by Noto Sans
	// Arabic, then Noto Sans SC, then JetBrains Mono, some characters shared
	// between runs, so they find fonts for the same set together. Each run
	// laid out again at twice the size, from the fonts found, is twice as
	// wide.
	set := font.SetDescriptor{Languages: []font.LanguageFonts{
		{Languages: []string{"zh-Hans"}, Fonts: []font.Font{{Path: arabicFontPath}, {Path: chineseFontPath}}},
	}}
	texts := []string{"W今→", "A日←", "B本↑", "C语↓", "D文→", "E章←", "F骨↑", "G今↓"}
	widths := make([]float32, len(texts))
	var doubledWidths []float32
	h := canvastest.NewHarness(t)
	h.Spawn(nil, &canvas.Config{DefaultFontSet: set, Language: "zh-Hans"}, &fileMounter{files: os.DirFS("testdata/fonts")}, &fontLibraryUser{
		read: func(fonts canvas.FontLibrary, frame int) {
			var group sync.WaitGroup
			for worker, text := range texts {
				group.Go(func() {
					widths[worker] = fonts.Layout(canvas.Text{String: text, Size: 10}).LineWidth(0)
				})
			}
			group.Wait()
			for _, text := range texts {
				doubledWidths = append(doubledWidths, fonts.Layout(canvas.Text{String: text, Size: 20}).LineWidth(0))
			}
		},
	})
	h.Step()

	for index, text := range texts {
		expectNear(t, text+" laid out at once, doubled", 2*widths[index], doubledWidths[index])
	}
}
