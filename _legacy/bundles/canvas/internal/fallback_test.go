package internal

import (
	"errors"
	"reflect"
	"testing"
	"testing/fstest"

	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
)

// The fallback tests lean on what three real fonts carry: ə is in canvas's
// default font and not in Go Regular, ‼ is in both Go fonts and not in the
// default, and 直 is in none of them.
const (
	onlyInDefault   = 'ə'
	onlyInFallbacks = '‼'
	inNoFont        = '直'
)

func fallbackFS() fstest.MapFS {
	return fstest.MapFS{
		"text.ttf": &fstest.MapFile{Data: goregular.TTF},
		"ja.ttf":   &fstest.MapFile{Data: goregular.TTF},
		"zh.ttf":   &fstest.MapFile{Data: gomono.TTF},
	}
}

func fallbackConfig(language string) Config {
	return Config{
		FallbackFonts:    []FallbackFont{{Language: "ja", Path: "ja.ttf"}, {Language: "zh", Path: "zh.ttf"}},
		FallbackLanguage: language,
	}
}

func TestACharacterIsDrawnByTheFirstFontOfTheChainThatHasIt(t *testing.T) {
	k, _, _ := testKernel(t, fallbackFS(), fallbackConfig(""), func(*OpQueue) {})
	probeFrame(k, func(fr *frame) {
		text := fr.face("text.ttf", 16)
		builtin := fr.face(DefaultFontPath, 16)
		fonts := fr.fonts()
		if owner := fonts.faceFor(text, 'a', ""); owner != text {
			t.Errorf("a is drawn by %q, want the text's own font", owner.path)
		}
		if owner := fonts.faceFor(text, onlyInDefault, ""); owner.path != DefaultFontPath {
			t.Errorf("%c is drawn by %q, want the default font next on the chain", onlyInDefault, owner.path)
		}
		if owner := fonts.faceFor(builtin, onlyInFallbacks, ""); owner.path != "ja.ttf" {
			t.Errorf("%c is drawn by %q, want the first fallback configured", onlyInFallbacks, owner.path)
		}
		if owner := fonts.faceFor(builtin, inNoFont, ""); owner != builtin {
			t.Errorf("%c, which no font has, is drawn by %q, want the text's own missing-glyph box", inNoFont, owner.path)
		}
		if owner := fonts.faceFor(builtin, onlyInFallbacks, ""); owner.px != builtin.px {
			t.Errorf("the fallback is baked at %d pixels, want the text's %d", owner.px, builtin.px)
		}
	})
}

func TestTheFallbackLanguageIsTriedFirstAndASpanOverridesIt(t *testing.T) {
	k, _, _ := testKernel(t, fallbackFS(), fallbackConfig("zh"), func(*OpQueue) {})
	probeFrame(k, func(fr *frame) {
		builtin := fr.face(DefaultFontPath, 16)
		fonts := fr.fonts()
		if owner := fonts.faceFor(builtin, onlyInFallbacks, ""); owner.path != "zh.ttf" {
			t.Errorf("outside a span %c is drawn by %q, want the fallback language's zh.ttf", onlyInFallbacks, owner.path)
		}
		if owner := fonts.faceFor(builtin, onlyInFallbacks, "ja"); owner.path != "ja.ttf" {
			t.Errorf("in a ja span %c is drawn by %q, want ja.ttf", onlyInFallbacks, owner.path)
		}
		if owner := fonts.faceFor(builtin, onlyInFallbacks, "ko"); owner.path != "ja.ttf" {
			t.Errorf("in a span of a language with no fallback %c is drawn by %q, want the configured order's ja.ttf", onlyInFallbacks, owner.path)
		}
	})
}

func TestSettingTheFallbackLanguageReachesDrawnGlyphsOnlyAfterAClear(t *testing.T) {
	k, _, _ := testKernel(t, fallbackFS(), fallbackConfig(""), func(*OpQueue) {})
	owner := func() string {
		var path string
		probeFrame(k, func(fr *frame) {
			path = fr.fonts().faceFor(fr.face(DefaultFontPath, 16), onlyInFallbacks, "").path
		})
		return path
	}
	if got := owner(); got != "ja.ttf" {
		t.Fatalf("before the change %c is drawn by %q, want ja.ttf", onlyInFallbacks, got)
	}
	if response := k.ExecuteCommand[SetFallbackLanguageCmd](SetFallbackLanguageRequest{Language: "zh"}); response.Err != nil {
		t.Fatalf("SetFallbackLanguageCmd zh: %v", response.Err)
	}
	if got := owner(); got != "ja.ttf" {
		t.Errorf("before a clear %c is drawn by %q, want the ja.ttf it was drawn with", onlyInFallbacks, got)
	}
	k.ExecuteCommand[ClearGlyphsCmd](ClearGlyphsRequest{})
	runFrame(k)
	if got := owner(); got != "zh.ttf" {
		t.Errorf("after a clear %c is drawn by %q, want zh.ttf", onlyInFallbacks, got)
	}
}

func TestAnUnknownFallbackLanguageIsRefused(t *testing.T) {
	k, _, _ := testKernel(t, fallbackFS(), fallbackConfig("zh"), func(*OpQueue) {})
	response := k.ExecuteCommand[SetFallbackLanguageCmd](SetFallbackLanguageRequest{Language: "ko"})
	var unknown ErrUnknownFallbackLanguage
	if !errors.As(response.Err, &unknown) || unknown.Language != "ko" {
		t.Fatalf("SetFallbackLanguageCmd ko answered %v, want ErrUnknownFallbackLanguage", response.Err)
	}
	probeFrame(k, func(fr *frame) {
		if fr.lookup.fallbackLanguage != "zh" {
			t.Errorf("the refused change left %q, want zh in place", fr.lookup.fallbackLanguage)
		}
	})
}

func TestAMeasuredLineAdvancesEachCharacterInTheFontThatDrawsIt(t *testing.T) {
	k, _, _ := testKernel(t, fallbackFS(), fallbackConfig(""), func(*OpQueue) {})
	probeFrame(k, func(fr *frame) {
		text := fr.face("text.ttf", 16)
		builtin := fr.face(DefaultFontPath, 16)
		advance, ok := builtin.Face.GlyphAdvance(onlyInDefault)
		if !ok {
			t.Fatal("the default font has no advance for its own character")
		}
		got := MeasureLine(fr.fonts(), text, string(onlyInDefault), "")
		if want := float32(advance) / 64; got != want {
			t.Errorf("%c measures %v in the text's font, want the default font's %v", onlyInDefault, got, want)
		}
	})
}

func TestALanguageSpanParsesToARunInThatLanguage(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []InlineSegment
	}{
		{"a$[ja]{直 b}c", []InlineSegment{{Text: "a"}, {Text: "直 b", Language: "ja"}, {Text: "c"}}},
		{"$[zh-Hans]{x\\}y}", []InlineSegment{{Text: "x}y", Language: "zh-Hans"}}},
		{"$[ja]{${icon.png}}", []InlineSegment{{Text: "${icon.png", Language: "ja"}, {Text: "}"}}},
		{"\\$[ja]{x}", []InlineSegment{{Text: "$[ja]{x}"}}},
		{"$[]{x}", []InlineSegment{{Text: "$[]{x}"}}},
		{"$[ja]x}", []InlineSegment{{Text: "$[ja]x}"}}},
		{"$[ja]{x", []InlineSegment{{Text: "$[ja]{x"}}},
		{"$[j a]{x}", []InlineSegment{{Text: "$[j a]{x}"}}},
		{"$[ja]{}b", []InlineSegment{{Text: "b"}}},
	} {
		lines := ParseInlineText(tc.text)
		if len(lines) != 1 || !reflect.DeepEqual(lines[0], tc.want) {
			t.Errorf("ParseInlineText(%q) = %+v, want %+v", tc.text, lines, [][]InlineSegment{tc.want})
		}
	}
}

func TestWrappingKeepsASpansLanguage(t *testing.T) {
	lines := WrapInlineText(ParseInlineText("$[ja]{aaa bbb}"), 4, measuredInlineRunes)
	want := [][]InlineSegment{{{Text: "aaa", Language: "ja"}}, {{Text: "bbb", Language: "ja"}}}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("wrapped = %+v, want %+v", lines, want)
	}
	split := WrapInlineText(ParseInlineText("$[ja]{abcdef}"), 4, measuredInlineRunes)
	for _, line := range split {
		for _, segment := range line {
			if segment.Language != "ja" {
				t.Errorf("a span split inside a word lost its language: %+v", split)
			}
		}
	}
}

func TestAnInvalidFallbackIsRefused(t *testing.T) {
	for name, config := range map[string]Config{
		"no language":       {FallbackFonts: []FallbackFont{{Path: "a.ttf"}}},
		"no path":           {FallbackFonts: []FallbackFont{{Language: "ja"}}},
		"language twice":    {FallbackFonts: []FallbackFont{{Language: "ja", Path: "a.ttf"}, {Language: "ja", Path: "b.ttf"}}},
		"unknown preferred": {FallbackFonts: []FallbackFont{{Language: "ja", Path: "a.ttf"}}, FallbackLanguage: "zh"},
	} {
		if _, err := resolveConfig(config); err == nil {
			t.Errorf("%s: resolveConfig accepted %+v", name, config)
		}
	}
}
