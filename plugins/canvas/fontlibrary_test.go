package canvas_test

import (
	"encoding/binary"
	"fmt"
	"io/fs"
	"math"
	"os"
	"slices"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
	"golang.org/x/image/font/gofont/goregular"
)

// fontLibraryUser is a test plugin whose task, hung off canvas.FrameEvent, calls
// write with FontLibrary's writing side in each frame, the first frame's 0, then,
// in tasks of their own after it, read with FontLibrary's reading side and,
// when set, draw with a draw list of its own.
type fontLibraryUser struct {
	testPlugin
	write func(fonts canvas.WriteFontLibrary, frame int)
	read  func(fonts canvas.FontLibrary, frame int)
	draw  func(list canvas.DrawList, frame int)
	// frameCount is the frames written so far.
	frameCount atomic.Int32
}

func (*fontLibraryUser) Name() kernel.PluginName { return "fonts writer" }

func (w *fontLibraryUser) Init(configurator *kernel.Configurator, _ any) error {
	write := configurator.RegisterTask2(func(k *kernel.Kernel, _ kernel.NoArg, fonts kernel.Write[canvas.FontLibrary], files kernel.Read[platform.FS]) int {
		frame := int(w.frameCount.Add(1)) - 1
		if w.write != nil {
			w.write(canvas.ElevateFontLibrary(fonts, files), frame)
		}
		return frame
	})
	read := configurator.RegisterTask1(func(k *kernel.Kernel, frame int, fonts kernel.Read[canvas.FontLibrary]) kernel.NoResult {
		if w.read != nil {
			w.read(fonts.Get(), frame)
		}
		return kernel.NoResult{}
	})
	var draw *kernel.Task[int, kernel.NoResult]
	if w.draw != nil {
		draw = configurator.RegisterTask1(func(k *kernel.Kernel, frame int, list canvas.DrawList) kernel.NoResult {
			w.draw(list, frame)
			return kernel.NoResult{}
		})
	}
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event canvas.FrameEvent) (canvas.FrameEvent, kernel.JobHandle) {
		written := ex.Schedule(write, kernel.NoArg{}, event.Ready)
		done := ex.Join(in, ex.ScheduleAfter(read, written).Handle())
		if w.draw != nil {
			done = ex.Join(done, ex.ScheduleAfter(draw, written).Handle())
		}
		return event, done
	})
	return nil
}

// builtInFontPath is canvas's JetBrains Mono in platform's FS.
const builtInFontPath = "cog.canvas/jetbrainsmono.ttf"

// The vertical measures of JetBrains Mono, of 1000 units an em, and of Go
// Regular, of 2048, each in its font units, as their hhea and OS/2 tables
// hold them: ascent, descent, line gap, x-height and cap height.
var (
	builtInFontUnits = fontUnits{unitsPerEm: 1000, ascent: 1020, descent: 300, lineGap: 0, xHeight: 550, capHeight: 730}
	goFontUnits      = fontUnits{unitsPerEm: 2048, ascent: 1935, descent: 432, lineGap: 0, xHeight: 1086, capHeight: 1480}
)

// fontUnits are a font's vertical measures in its own units.
type fontUnits struct {
	unitsPerEm, ascent, descent, lineGap, xHeight, capHeight float32
}

// atSize answers the measures at size, its em in world units.
func (u fontUnits) atSize(size float32) canvas.FontMetrics {
	scale := size / u.unitsPerEm
	return canvas.FontMetrics{
		Ascent:     u.ascent * scale,
		Descent:    u.descent * scale,
		LineGap:    u.lineGap * scale,
		LineHeight: (u.ascent + u.descent + u.lineGap) * scale,
		XHeight:    u.xHeight * scale,
		CapHeight:  u.capHeight * scale,
	}
}

// expectMetrics fails t unless got is the measures of units at size, each
// within a millionth of an em, a measure that is not a number never.
func expectMetrics(t *testing.T, label string, got canvas.FontMetrics, units fontUnits, size float32) {
	t.Helper()
	want := units.atSize(size)
	gotMeasures := [...]float32{got.Ascent, got.Descent, got.LineGap, got.LineHeight, got.XHeight, got.CapHeight}
	wantMeasures := [...]float32{want.Ascent, want.Descent, want.LineGap, want.LineHeight, want.XHeight, want.CapHeight}
	for index := range gotMeasures {
		if !(math.Abs(float64(gotMeasures[index]-wantMeasures[index])) <= float64(size)*1e-6) {
			t.Errorf("%s: metrics %+v, want %+v", label, got, want)
			return
		}
	}
}

// makeFontFiles answers files holding Go Regular at go.ttf, and at
// gapped.ttf with its hhea table's line gap goGappedLineGap, a file that is
// no font at broken.ttf, and a collection of JetBrains Mono, then Go
// Regular, at pair.ttc.
func makeFontFiles(t *testing.T) fstest.MapFS {
	t.Helper()
	builtIn, err := os.ReadFile("assets/jetbrainsmono.ttf")
	if err != nil {
		t.Fatal(err)
	}
	return fstest.MapFS{
		"go.ttf":     {Data: goregular.TTF},
		"gapped.ttf": {Data: makeGappedFont(goregular.TTF, goGappedLineGap)},
		"broken.ttf": {Data: []byte("no font")},
		"pair.ttc":   {Data: makeCollection(builtIn, goregular.TTF)},
	}
}

// goGappedLineGap is the line gap of gapped.ttf, Go Regular's measured
// from its hhea table, in its font units.
const goGappedLineGap = 200

// makeGappedFont answers a copy of font, a TrueType font file, its hhea
// table's line gap lineGap font units.
func makeGappedFont(font []byte, lineGap int16) []byte {
	gapped := slices.Clone(font)
	for table := range int(binary.BigEndian.Uint16(font[4:])) {
		record := gapped[12+16*table:]
		if string(record[:4]) == "hhea" {
			binary.BigEndian.PutUint16(gapped[binary.BigEndian.Uint32(record[8:])+8:], uint16(lineGap))
		}
	}
	return gapped
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
	for index, font := range fonts {
		for len(collection)%4 != 0 {
			collection = append(collection, 0)
		}
		start := len(collection)
		binary.BigEndian.PutUint32(collection[12+4*index:], uint32(start))
		collection = append(collection, font...)
		tableCount := int(binary.BigEndian.Uint16(font[4:]))
		for table := range tableCount {
			offset := collection[start+12+16*table+8:]
			binary.BigEndian.PutUint32(offset, binary.BigEndian.Uint32(offset)+uint32(start))
		}
	}
	return collection
}

func TestDefaultFontSetIsJetBrainsMonoMeasuredAtItsSize(t *testing.T) {
	h := canvastest.NewHarness(t)
	var got canvas.FontMetrics
	h.Spawn(nil, nil, &fontLibraryUser{read: func(fonts canvas.FontLibrary, frame int) {
		got = fonts.Metrics(canvas.FontSet{}, 10, "")
	}})
	h.Step()
	expectMetrics(t, "the default set at 10", got, builtInFontUnits, 10)
}

func TestLineHeightIsAscentDescentAndLineGap(t *testing.T) {
	h := canvastest.NewHarness(t)
	var set canvas.FontSet
	var got canvas.FontMetrics
	h.Spawn(nil, nil, &fileMounter{files: makeFontFiles(t)}, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			set = fonts.MakeFontSet(canvas.FontSetDescriptor{Default: canvas.Font{Path: "gapped.ttf"}})
		},
		read: func(fonts canvas.FontLibrary, frame int) { got = fonts.Metrics(set, 32, "") },
	})
	h.Step()
	gapped := goFontUnits
	gapped.lineGap = goGappedLineGap
	expectMetrics(t, "Go Regular gapped", got, gapped, 32)
}

func TestCurrentLanguageIsConfigsUntilSetLanguage(t *testing.T) {
	h := canvastest.NewHarness(t)
	var languages []string
	h.Spawn(nil, &canvas.Config{Language: "en"}, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			if frame == 1 {
				fonts.SetLanguage("ja")
			}
		},
		read: func(fonts canvas.FontLibrary, frame int) { languages = append(languages, fonts.Language()) },
	})
	h.Step()
	h.Step()
	if len(languages) != 2 || languages[0] != "en" || languages[1] != "ja" {
		t.Errorf("languages %q, want en, then ja once set", languages)
	}
}

func TestFontSetMeasuresEachLanguageByItsFirstFontAndTheRestByItsDefault(t *testing.T) {
	h := canvastest.NewHarness(t)
	var set canvas.FontSet
	got := map[string]canvas.FontMetrics{}
	h.Spawn(nil, &canvas.Config{Language: "en"}, &fileMounter{files: makeFontFiles(t)}, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			switch frame {
			case 0:
				set = fonts.MakeFontSet(canvas.FontSetDescriptor{
					Label:   "body",
					Default: canvas.Font{Path: "go.ttf"},
					Languages: []canvas.LanguageFonts{
						{Languages: []string{"ja", "zh-Hans"}, Fonts: []canvas.Font{{Path: builtInFontPath}, {Path: "go.ttf"}}},
					},
				})
			case 1:
				fonts.SetLanguage("ja")
			}
		},
		read: func(fonts canvas.FontLibrary, frame int) {
			if frame == 0 {
				for _, language := range []string{"en", "ja", "zh-Hans", "zh"} {
					got[language] = fonts.Metrics(set, 32, language)
				}
			}
			got[fmt.Sprint("current at frame ", frame)] = fonts.Metrics(set, 32, "")
		},
	})
	h.Step()
	h.Step()
	for _, want := range []struct {
		label string
		units fontUnits
	}{
		{"en", goFontUnits},
		{"ja", builtInFontUnits},
		{"zh-Hans", builtInFontUnits},
		{"zh", goFontUnits},
		{"current at frame 0", goFontUnits},
		{"current at frame 1", builtInFontUnits},
	} {
		expectMetrics(t, want.label, got[want.label], want.units, 32)
	}
}

func TestFontThatFailsToLoadIsReportedOnceAndLeftOut(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindFontNotLoaded)
	descriptor := canvas.FontSetDescriptor{
		Label:   "broken",
		Default: canvas.Font{Path: "missing.ttf"},
		Languages: []canvas.LanguageFonts{
			{Languages: []string{"ja"}, Fonts: []canvas.Font{{Path: "broken.ttf"}}},
		},
	}
	var set canvas.FontSet
	got := map[string]canvas.FontMetrics{}
	h.Spawn(nil, nil, &fileMounter{files: makeFontFiles(t)}, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) { set = fonts.MakeFontSet(descriptor) },
		read: func(fonts canvas.FontLibrary, frame int) {
			got["en"] = fonts.Metrics(set, 10, "en")
			got["ja"] = fonts.Metrics(set, 10, "ja")
		},
	})
	h.Step()
	h.Step()
	if count := countReports(h, canvas.ReportKindFontNotLoaded); count != 2 {
		t.Errorf("%d fonts not loaded reported over two makings, want the missing and the broken file once each", count)
	}
	expectMetrics(t, "the failed default replaced", got["en"], builtInFontUnits, 10)
	expectMetrics(t, "a language whose font failed", got["ja"], builtInFontUnits, 10)
}

func TestCollectionsFontIsTheOneAtItsIndex(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindFontNotLoaded)
	var sets [3]canvas.FontSet
	var got [3]canvas.FontMetrics
	h.Spawn(nil, nil, &fileMounter{files: makeFontFiles(t)}, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			for index := range sets {
				sets[index] = fonts.MakeFontSet(canvas.FontSetDescriptor{Default: canvas.Font{Path: "pair.ttc", Index: uint32(index)}})
			}
		},
		read: func(fonts canvas.FontLibrary, frame int) {
			for index, set := range sets {
				got[index] = fonts.Metrics(set, 32, "")
			}
		},
	})
	h.Step()
	expectMetrics(t, "the collection's first font", got[0], builtInFontUnits, 32)
	expectMetrics(t, "the collection's second font", got[1], goFontUnits, 32)
	expectMetrics(t, "an index past the collection, replaced", got[2], builtInFontUnits, 32)
	if count := countReports(h, canvas.ReportKindFontNotLoaded); count != 1 {
		t.Errorf("%d fonts not loaded reported, want the index past the collection once", count)
	}
}

func TestConfigsDefaultFontSetIsMadeAsCanvasStarts(t *testing.T) {
	h := canvastest.NewHarness(t)
	var got canvas.FontMetrics
	h.Spawn(nil, &canvas.Config{DefaultFontSet: canvas.FontSetDescriptor{Default: canvas.Font{Path: "go.ttf"}}}, &fileMounter{files: makeFontFiles(t)}, &fontLibraryUser{
		read: func(fonts canvas.FontLibrary, frame int) { got = fonts.Metrics(canvas.FontSet{}, 32, "") },
	})
	h.Step()
	expectMetrics(t, "the default set", got, goFontUnits, 32)
}

func TestConfigsFailedDefaultFontIsJetBrainsMonoReportedOnce(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindFontNotLoaded)
	var got canvas.FontMetrics
	h.Spawn(nil, &canvas.Config{DefaultFontSet: canvas.FontSetDescriptor{Default: canvas.Font{Path: "missing.ttf"}}}, &fontLibraryUser{
		read: func(fonts canvas.FontLibrary, frame int) { got = fonts.Metrics(canvas.FontSet{}, 10, "") },
	})
	h.Step()
	expectMetrics(t, "the default set", got, builtInFontUnits, 10)
	if count := countReports(h, canvas.ReportKindFontNotLoaded); count != 1 {
		t.Errorf("%d fonts not loaded reported, want the missing default once", count)
	}
}

func TestDestroyedFontSetIsMeasuredAsTheDefaultSetReportedOnce(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindFontSetNotHeld)
	var set canvas.FontSet
	var got []canvas.FontMetrics
	h.Spawn(nil, &canvas.Config{DefaultFontSet: canvas.FontSetDescriptor{Default: canvas.Font{Path: "go.ttf"}}}, &fileMounter{files: makeFontFiles(t)}, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			switch frame {
			case 0:
				set = fonts.MakeFontSet(canvas.FontSetDescriptor{Default: canvas.Font{Path: builtInFontPath}})
			case 1:
				fonts.DestroyFontSet(set)
			}
		},
		read: func(fonts canvas.FontLibrary, frame int) {
			got = append(got, fonts.Metrics(set, 32, ""))
			if frame == 1 {
				got = append(got, fonts.Metrics(set, 32, ""))
			}
		},
	})
	h.Step()
	h.Step()
	if len(got) != 3 {
		t.Fatalf("%d measures, want 3", len(got))
	}
	expectMetrics(t, "the set made", got[0], builtInFontUnits, 32)
	expectMetrics(t, "the set destroyed", got[1], goFontUnits, 32)
	expectMetrics(t, "the set destroyed, measured again", got[2], goFontUnits, 32)
	if count := countReports(h, canvas.ReportKindFontSetNotHeld); count != 1 {
		t.Errorf("%d font sets not held reported, want the destroyed set once", count)
	}
}

func TestDestroyingTheDefaultOrADestroyedFontSetIsReportedOnceEach(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindFontSetNotHeld)
	var got canvas.FontMetrics
	h.Spawn(nil, nil, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			set := fonts.MakeFontSet(canvas.FontSetDescriptor{})
			fonts.DestroyFontSet(set)
			fonts.DestroyFontSet(set)
			fonts.DestroyFontSet(canvas.FontSet{})
			fonts.DestroyFontSet(canvas.FontSet{})
		},
		read: func(fonts canvas.FontLibrary, frame int) { got = fonts.Metrics(canvas.FontSet{}, 10, "") },
	})
	h.Step()
	expectMetrics(t, "the default set, never destroyed", got, builtInFontUnits, 10)
	if count := countReports(h, canvas.ReportKindFontSetNotHeld); count != 2 {
		t.Errorf("%d font sets not held reported, want the destroyed set and the default set once each", count)
	}
}

func TestAxisTagFromNameAnswersFourBytesOrTheZeroTag(t *testing.T) {
	if tag := canvas.AxisTagFromName("wght"); tag != (canvas.AxisTag{'w', 'g', 'h', 't'}) {
		t.Errorf("wght's tag %q", tag)
	}
	for _, name := range []string{"", "wgh", "weight"} {
		if tag := canvas.AxisTagFromName(name); tag != (canvas.AxisTag{}) {
			t.Errorf("%q's tag %q, want the zero tag", name, tag)
		}
	}
}

func TestVariationNamingNoAxisIsReportedOnceAndIgnored(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindVariationIgnored)
	var got canvas.FontMetrics
	var set canvas.FontSet
	h.Spawn(nil, nil, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			set = fonts.MakeFontSet(canvas.FontSetDescriptor{Default: canvas.Font{Variations: [4]canvas.Variation{
				{Axis: canvas.AxisTagFromName("weight"), Value: 700},
				{Axis: canvas.AxisTagFromName("wght"), Value: 800},
			}}})
		},
		read: func(fonts canvas.FontLibrary, frame int) { got = fonts.Metrics(set, 10, "") },
	})
	h.Step()
	h.Step()
	expectMetrics(t, "the variable font", got, builtInFontUnits, 10)
	if count := countReports(h, canvas.ReportKindVariationIgnored); count != 1 {
		t.Errorf("%d variations ignored reported over two makings, want the one naming no axis once", count)
	}
}

// switchedFS is an FS holding its files when on, and none when off, so a
// test repairs and breaks a font between frames.
type switchedFS struct {
	files fs.FS
	on    atomic.Bool
}

func (s *switchedFS) Open(path string) (fs.File, error) {
	if !s.on.Load() {
		return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
	}
	return s.files.Open(path)
}

func TestFontThatLoadsAgainIsReportedAgainOnceItFails(t *testing.T) {
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindFontNotLoaded)
	files := &switchedFS{files: makeFontFiles(t)}
	descriptor := canvas.FontSetDescriptor{Default: canvas.Font{Path: "go.ttf"}}
	h.Spawn(nil, nil, &fileMounter{files: files}, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			files.on.Store(frame == 2)
			fonts.MakeFontSet(descriptor)
		},
	})
	for range 4 {
		h.Step()
	}
	if count := countReports(h, canvas.ReportKindFontNotLoaded); count != 2 {
		t.Errorf("%d fonts not loaded reported over failing, failing, loading and failing, want 2: once, then once more after it loaded", count)
	}
}
