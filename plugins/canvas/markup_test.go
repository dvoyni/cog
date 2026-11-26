package canvas_test

import (
	"slices"
	"testing"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
	"github.com/dvoyni/cog/plugins/canvas/canvastest"
	"github.com/dvoyni/cog/plugins/platform"
)

// markupColors are the colours these tests' markup names: keyword, a
// registered orange.
var markupColors = map[string]m.Color{"keyword": orange}

func TestMarkupReadsEachFormIntoSpans(t *testing.T) {
	red := m.NewColorSrgb8(255, 0, 0, 255)
	blue := m.NewColorSrgb8(0, 0, 255, 255)
	tests := []struct {
		name   string
		markup string
		want   []canvas.Span
	}{
		{"text alone", "fire", []canvas.Span{{Text: "fire"}}},
		{"nothing", "", nil},
		{"an icon, its name ending at a full stop", `Press \i:a.`, []canvas.Span{{Text: "Press "}, {Icon: "a"}, {Text: "."}}},
		{"an icon's name of letters, digits, _, - and /", `\i:pad/button_a-2 now`, []canvas.Span{{Icon: "pad/button_a-2"}, {Text: " now"}}},
		{"an icon's name of letters not Latin", `\i:кнопка!`, []canvas.Span{{Icon: "кнопка"}, {Text: "!"}}},
		{"a colour of #rgb", `\c:#f00{hot}`, []canvas.Span{{Text: "hot", Tint: red}}},
		{"a colour of #rrggbb", `\c:#0000FF{cold}`, []canvas.Span{{Text: "cold", Tint: blue}}},
		{"a colour of #rrggbbaa", `\c:#ff000080{dim}`, []canvas.Span{{Text: "dim", Tint: m.NewColorSrgb8(255, 0, 0, 0x80)}}},
		{"a registered colour", `cast \c:keyword{fire} now`, []canvas.Span{{Text: "cast "}, {Text: "fire", Tint: orange}, {Text: " now"}}},
		{"an empty span", `a\c:#f00{}b`, []canvas.Span{{Text: "a"}, {Text: "b"}}},
		{
			"spans nested, an icon in each",
			`a\c:#f00{b\c:#00f{c\i:x}d\i:y}e`,
			[]canvas.Span{{Text: "a"}, {Text: "b", Tint: red}, {Text: "c", Tint: blue}, {Icon: "x", Tint: blue}, {Text: "d", Tint: red}, {Icon: "y", Tint: red}, {Text: "e"}},
		},
		{"a backslash", `C:\\games`, []canvas.Span{{Text: "C:"}, {Text: `\`}, {Text: "games"}}},
		{"balanced braces within a span", `\c:#f00{f(x) {y}}`, []canvas.Span{{Text: "f(x) {y}", Tint: red}}},
		{"lone braces within a span", `\c:#f00{a\}b\{c}`, []canvas.Span{{Text: "a", Tint: red}, {Text: "}", Tint: red}, {Text: "b", Tint: red}, {Text: "{", Tint: red}, {Text: "c", Tint: red}}},
		{"braces outside any span", "{x}}", []canvas.Span{{Text: "{x}}"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spans, offset, problem := canvas.ReadMarkup(test.markup, markupColors)
			if problem != "" {
				t.Fatalf("%q does not parse at byte %d: %s", test.markup, offset, problem)
			}
			if !slices.Equal(spans, test.want) {
				t.Errorf("%q reads as %+v, want %+v", test.markup, spans, test.want)
			}
		})
	}
}

func TestMarkupThatDoesNotParseIsOneSpanAsWrittenAndSaysWhere(t *testing.T) {
	tests := []struct {
		name   string
		markup string
		offset int
	}{
		{"a backslash at its end", `ab\`, 2},
		{"a backslash before a letter not i or c", `a\x`, 1},
		{"an icon of no colon", `\ia`, 2},
		{"an icon of no name", `\i: a`, 3},
		{"a colour of no colon", `\c#f00{a}`, 2},
		{"a colour of no name", `\c:{a}`, 3},
		{"a colour of two digits", `\c:#f0{a}`, 4},
		{"a colour of seven digits", `\c:#ff00001{a}`, 4},
		{"a colour of no digits", `\c:#{a}`, 4},
		{"a colour of other than digits", `\c:#ffzzzz{a}`, 4},
		{"a colour of a name not registered", `\c:spell{a}`, 3},
		{"a colour of no brace after it", `\c:#f00 a`, 7},
		{"a span left open", `\c:#f00{a`, 9},
		{"a span whose braces leave it open", `\c:#f00{a{b}`, 12},
		{"a nested span left open", `\c:#f00{a\c:#00f{b}`, 19},
		{"an escape that does not parse within a span", `\c:#f00{a\q}`, 9},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spans, offset, problem := canvas.ReadMarkup(test.markup, markupColors)
			if problem == "" {
				t.Fatalf("%q parses as %+v, want it not to", test.markup, spans)
			}
			if offset != test.offset {
				t.Errorf("%q does not parse at byte %d (%s), want %d", test.markup, offset, problem, test.offset)
			}
			if spans != nil {
				t.Errorf("%q reads as %+v, want no spans, its string drawn as written", test.markup, spans)
			}
		})
	}
}

func TestAPlayerTypedStringWithABackslashDrawsAsWritten(t *testing.T) {
	// A name a player typed, C:\new\i:a, in a run without Markup, draws its
	// ten characters, each a glyph, a 0.6 em advance apart, spans neither
	// read nor reported; the same name written in markup, its backslashes
	// doubled, lays out as wide.
	h := canvastest.NewHarness(t)
	const name = `C:\new\i:a`
	var layouts []canvas.TextLayout
	h.Spawn(nil, nil, &fontLibraryUser{
		read: func(fonts canvas.FontLibrary, frame int) {
			layouts = append(layouts, fonts.Layout(canvas.Text{String: name, Size: 10}), fonts.Layout(canvas.Text{String: `C:\\new\\i:a`, Markup: true, Size: 10}))
		},
		draw: func(list canvas.CommandList, frame int) {
			list.DrawText(canvas.Text{String: name, Size: 10})
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Step()

	expectLineWidths(t, "the name as written", layouts[0], 10*builtInAdvance*10)
	expectLineWidths(t, "the name in markup", layouts[1], 10*builtInAdvance*10)
	frames := h.Frames()
	batches := frames[len(frames)-1].Cameras[0].Batches
	if len(batches) != 1 || batches[0].InstanceCount != 10 || batches[0].Entries[0].Text.Spans != nil {
		t.Errorf("the name drew batches %+v, want one of its ten glyphs, of no spans", batches)
	}
	if reports := h.Reports(); len(reports) != 0 {
		t.Errorf("the name reported %v, want nothing", reports)
	}
}

func TestMarkupThatDoesNotParseDrawsAsWrittenReportedOncePerString(t *testing.T) {
	// a\xb, its backslash before an x, draws its four characters as
	// written, in two runs, frame after frame, reported once, though the
	// same string drawn first without Markup, and as a span of its own, was
	// laid out as written; ab\ reported once more.
	h := canvastest.NewHarness(t)
	h.ExpectReport(canvas.ReportKindMarkupMalformed)
	h.Spawn(nil, nil, &fontLibraryUser{
		draw: func(list canvas.CommandList, frame int) {
			list.DrawText(canvas.Text{Spans: []canvas.Span{{Text: `a\xb`}}, Size: 10})
			list.DrawText(canvas.Text{String: `a\xb`, Size: 10})
			list.DrawText(canvas.Text{String: `a\xb`, Markup: true, Size: 10})
			list.DrawText(canvas.Text{String: `a\xb`, Markup: true, Size: 20})
			list.DrawText(canvas.Text{String: `ab\`, Markup: true, Size: 10})
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Step()
	h.Step()

	frames := h.Frames()
	batches := frames[len(frames)-1].Cameras[0].Batches
	if len(batches) != 1 || batches[0].InstanceCount != 4*4+3 {
		t.Fatalf("the runs drew batches %+v, want one of their 19 glyphs as written", batches)
	}
	for _, entry := range batches[0].Entries[2:] {
		if entry.Text.Spans != nil {
			t.Errorf("a run of markup that does not parse drew spans %+v, want none, its string as written", entry.Text.Spans)
		}
	}
	if count := countReports(h, canvas.ReportKindMarkupMalformed); count != 2 {
		t.Errorf("%d markups that do not parse reported, want each of 2 strings once", count)
	}
}

func TestARegisteredColoursSpanTakesTheColourRegisteredWhenDrawnItsLayoutKept(t *testing.T) {
	// \c:keyword{fire} is drawn in orange in frame 0 and in blue once
	// keyword is registered again in frame 1, through the one layout.
	h := canvastest.NewHarness(t)
	text := canvas.Text{String: `cast \c:keyword{fire}`, Markup: true, Size: 10}
	var cached []*canvas.CachedLayout
	h.Spawn(nil, nil, &fontLibraryUser{
		write: func(fonts canvas.WriteFontLibrary, frame int) {
			fonts.RegisterColor("keyword", []m.Color{orange, {B: 1, A: 1}}[min(frame, 1)])
		},
		read: func(fonts canvas.FontLibrary, frame int) {
			fonts.Layout(text)
			cached = append(cached, canvas.FindCachedLayout(fonts, text))
		},
		draw: func(list canvas.CommandList, frame int) {
			list.DrawText(text)
		},
	})
	h.OpenWindow(platform.Size{Width: 64, Height: 48})
	h.Step()
	h.Step()

	frames := h.Frames()
	for frame, want := range []m.Color{orange, {B: 1, A: 1}} {
		spans := frames[frame].Cameras[0].Batches[0].Entries[0].Text.Spans
		if wantSpans := []canvas.Span{{Text: "cast "}, {Text: "fire", Tint: want}}; !slices.Equal(spans, wantSpans) {
			t.Errorf("frame %d drew spans %+v, want %+v", frame, spans, wantSpans)
		}
	}
	if cached[0] == nil || cached[1] != cached[0] {
		t.Errorf("the run's layout is %p, then %p, want the first kept", cached[0], cached[1])
	}
}

func TestAColouredWordOfMarkupWrapsWithItsParagraphInItsOwnTint(t *testing.T) {
	// "ab " in the run's white and \c:#f00{cd} in red, wrapped as its spans
	// are: the red word goes to the second line alone in red.
	expectRedWordOnSecondLine(t, canvas.Text{String: `ab \c:#f00{cd}`, Markup: true})
}
