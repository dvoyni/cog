package canvas

import (
	"cmp"
	"fmt"
	"slices"
	"sync"
	"unsafe"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas/font"
)

// fontLibrary is the FontLibrary resource: the font sets made, the current
// language, and the icons and colours registered. Its reads run under the
// kernel's read lock of FontLibrary, and fontLibraryWriter's writes, and the default
// set's making as canvas starts, under its write lock alone, so it needs no
// lock of its own. It has no method that writes, so a FontLibrary held under the
// read lock cannot be asserted into writing.
type fontLibrary struct {
	// reporter is where reports go, and its inspector notes the layouts
	// and glyphs made for the inspection pending.
	reporter *reporter
	// language is the current language.
	language string
	// textSize is the em, in world units, of a run of text whose Size is
	// zero.
	textSize float32
	// sets are the font sets, by index, the default set at 0. A destroyed
	// set's slot is reused, its generation counted up, so a FontSet naming
	// it before names nothing.
	sets        []*fontSetSlot
	freeIndexes []uint32
	// defaultDescriptor is Config.DefaultFontSet, which the default set is
	// made of as canvas starts: it is kept here, since a task's argument
	// holds no pointer.
	defaultDescriptor font.SetDescriptor
	icons             map[string]registeredIcon
	// iconSerial counts the icons registered, each registered under the
	// count it makes, so a name registered again names another icon to
	// the layout cache's key.
	iconRegistrationCount uint64
	colors                map[string]m.Color
	// malformedMarkup are the strings of the runs whose markup did not
	// parse, each reported as it was first read: read each time a run is
	// drawn, under the read lock, so guarded by malformedMutex, and keyed
	// by the string itself, so a run read again makes no key.
	malformedMutex  sync.Mutex
	malformedMarkup map[string]struct{}
	// shapers are the text shapers layouts are made by, each a
	// *textShaper, one a layout, since a shaper is not safe for concurrent
	// use and layouts are made under the read lock.
	shapers sync.Pool
	// glyphs are the glyphs made for drawing, distance fields or a bitmap
	// font's coverage.
	glyphs glyphTable
	// layouts are the runs laid out lately, which Layout and DrawText
	// answer from.
	layouts layoutCache
}

// newFontLibrary answers the FontLibrary resource of config, its reports
// logged: its current language config's Language, a run of text
// of no size laid out at its TextSize, a glyph of a font giving no field em
// or range made a distance field of its GlyphFieldEm and GlyphFieldRange, a
// layout unused for its LayoutIdleFrames frames freed, each zero meaning
// its stated default, and its default set JetBrains Mono alone until canvas
// starts and makes it of its DefaultFontSet.
func newFontLibrary(reporter *reporter, config *Config) *fontLibrary {
	return &fontLibrary{
		reporter:          reporter,
		language:          config.Language,
		textSize:          cmp.Or(config.TextSize, 16),
		sets:              []*fontSetSlot{makeFontSetSlot(nil, reporter, font.SetDescriptor{})},
		defaultDescriptor: config.DefaultFontSet,
		icons:             map[string]registeredIcon{},
		colors:            map[string]m.Color{},
		malformedMarkup:   map[string]struct{}{},
		shapers:           sync.Pool{New: func() any { return new(textShaper) }},
		glyphs:            glyphTable{indexes: map[glyphKey]uint32{}, fieldEm: cmp.Or(config.GlyphFieldEm, 48), fieldRange: cmp.Or(config.GlyphFieldRange, 16)},
		layouts:           layoutCache{layouts: map[layoutInput]*cachedLayout{}, idleFrames: uint64(cmp.Or(config.LayoutIdleFrames, 2))},
	}
}

// Language answers the current language.
func (l *fontLibrary) Language() string { return l.language }

// Metrics answers the measures at size, its em in world units, of the font
// of set measuring language, an empty language meaning the current one: the
// language's first font in set, or set's default font, a bitmap font's size
// rounded to a whole multiple of its pixel size as a run's is. A set canvas
// does not hold is measured as the default set, reported once.
func (l *fontLibrary) Metrics(set FontSet, size float32, language string) font.Metrics {
	measuringFont := l.findHeldSlot(set, "measuring").fonts.LanguageFont(cmp.Or(language, l.language))
	return measuringFont.Metrics(measuringFont.DrawnEm(size))
}

// Layout answers text shaped, broken into lines and measured, in the font
// of its set measuring the current language, at its size, zero meaning
// Config.TextSize, from the layout cache, which DrawText lays out through.
// A set canvas does not hold is laid out as the default set, reported once.
func (l *fontLibrary) Layout(text Text) TextLayout {
	shaper := l.acquireShaper(text)
	defer l.releaseShaper(shaper)
	_, layout := l.layOutText(text, false, shaper)
	return layout
}

// Preload makes the distance fields of the glyphs of text, or a bitmap
// font's coverage, shaped on one line in set's fonts for the current
// language, which a run of any size draws, so the first draw of a run of it makes none. A set canvas does not
// hold preloads the default set's, reported once.
func (l *fontLibrary) Preload(set FontSet, text string) {
	slot := l.findHeldSlot(set, "preloading glyphs of")
	shaper := l.shapers.Get().(*textShaper)
	defer l.shapers.Put(shaper)
	layout := shaper.layOut(layoutInput{text: text, set: slot, em: l.textSize, language: l.language}, l.icons)
	_, madeCount := l.glyphs.assignFields(layout.glyphs, shaper)
	l.reporter.inspector.addMadeGlyphs(Text{String: text, Font: set, Language: l.language}, madeCount)
}

// acquireShaper answers a shaper from the pool for text, a run of spans,
// whose key it encodes, or of markup, which it reads, and nil for a run of
// a string, which acquires one only to be shaped.
func (l *fontLibrary) acquireShaper(text Text) *textShaper {
	if len(text.Spans) == 0 && !text.Markup {
		return nil
	}
	return l.shapers.Get().(*textShaper)
}

// releaseShaper gives shaper, nil for none, back to the pool.
func (l *fontLibrary) releaseShaper(shaper *textShaper) {
	if shaper != nil {
		l.shapers.Put(shaper)
	}
}

// layOutText answers text as drawn, its markup read into spans in
// shaper's scratch, and laid out as Layout lays it out, through the
// layout cache, shaped the first time, the layout it caches noted for the
// inspection pending, and, when drawn, its glyphs those
// it draws: a copy of the layout's, each assigned its field in the glyph
// table, without those of no field, found the first time the cached layout
// is drawn and kept beside it, the fields made noted for the inspection
// pending. Two jobs drawing it first at once each find
// them, and the first kept is answered to both. shaper is acquireShaper's for
// text, and the text answered reads its scratch until it goes back to the
// pool.
func (l *fontLibrary) layOutText(text Text, drawn bool, shaper *textShaper) (Text, TextLayout) {
	text, input := l.makeLayoutInput(text, shaper)
	cached := l.layouts.find(input)
	if cached == nil || drawn && cached.drawnGlyphs.Load() == nil {
		if shaper == nil {
			shaper = l.shapers.Get().(*textShaper)
			defer l.shapers.Put(shaper)
		}
		madeText := text
		madeText.Language = input.language
		if cached == nil {
			var added bool
			if cached, added = l.layouts.add(input, shaper.layOut(input, l.icons)); added {
				l.reporter.inspector.addMadeLayout(madeText)
			}
		}
		if drawn {
			drawnGlyphs, madeCount := l.glyphs.assignFields(slices.Clone(cached.layout.glyphs), shaper)
			l.reporter.inspector.addMadeGlyphs(madeText, madeCount)
			cached.drawnGlyphs.CompareAndSwap(nil, &drawnGlyphs)
		}
	}
	layout := cached.layout
	if drawn {
		layout.glyphs = *cached.drawnGlyphs.Load()
	}
	return text, layout
}

// readMarkup answers text, its Spans those its String reads as, read into
// scratch, when its Markup is set and it gives none; markup that does not
// parse is reported once for its string, and its String drawn as written.
func (l *fontLibrary) readMarkup(text Text, scratch *[]Span) Text {
	if !text.Markup || len(text.Spans) > 0 {
		return text
	}
	var fault markupFault
	*scratch, fault = appendMarkupSpans((*scratch)[:0], text.String, l.colors)
	if fault.problem != "" {
		l.reportMarkupMalformed(text.String, fault)
		return text
	}
	text.Spans = *scratch
	return text
}

// reportMarkupMalformed reports markup, a run's String, not parsing as
// fault says, the first time it is read alone.
func (l *fontLibrary) reportMarkupMalformed(markup string, fault markupFault) {
	l.malformedMutex.Lock()
	_, reported := l.malformedMarkup[markup]
	if !reported {
		l.malformedMarkup[markup] = struct{}{}
	}
	l.malformedMutex.Unlock()
	if !reported {
		malformed := Report{Kind: ReportKindMarkupMalformed, Err: fmt.Errorf("canvas: the markup %q does not parse at byte %d: %s; it is drawn as written", markup, fault.offset, fault.problem)}
		l.reporter.reportOnce(markupMalformedLogKey+markup, malformed)
	}
}

// makeLayoutInput answers text as drawn and what its layout is made of, in
// its language, the current one unless it names its own, at its size, a
// bitmap font measuring the language rounding it to a whole multiple of its
// pixel size: a set canvas does not hold lays it out in the default set,
// reported once. shaper is
// acquireShaper's for text, nil for a run of a string: its markup is read into
// spans in shaper's scratch, and its Spans encoded into shaper's spanKey,
// with the serial each icon was registered under, which the input's text
// then reads, until the scratch is written again, so a lookup of a spanned
// run makes no string.
func (l *fontLibrary) makeLayoutInput(text Text, shaper *textShaper) (Text, layoutInput) {
	if shaper != nil {
		text = l.readMarkup(text, &shaper.markupSpans)
	}
	slot := l.findHeldSlot(text.Font, "laying out text in")
	language := cmp.Or(text.Language, l.language)
	input := layoutInput{
		text:       text.String,
		set:        slot,
		em:         slot.fonts.LanguageFont(language).DrawnEm(cmp.Or(text.Size, l.textSize)),
		wrapWidth:  text.WrapWidth,
		align:      text.Align,
		lineHeight: text.LineHeight,
		tracking:   text.Tracking,
		language:   language,
	}
	if len(text.Spans) > 0 {
		shaper.spanKey = appendEncodedSpans(shaper.spanKey[:0], text.Spans, l.icons)
		input.text = unsafe.String(unsafe.SliceData(shaper.spanKey), len(shaper.spanKey))
		input.spanned = true
	}
	return text, input
}

// findHeldSlot answers set's slot, or the default set's when canvas does
// not hold set, reported once, refused as use, what was being done with
// it, says.
func (l *fontLibrary) findHeldSlot(set FontSet, use string) *fontSetSlot {
	slot := l.findSlot(set)
	if slot == nil {
		l.reportSetNotHeld(set, use)
		slot = l.sets[0]
	}
	return slot
}

// findSlot answers set's slot, nil when canvas does not hold set.
func (l *fontLibrary) findSlot(set FontSet) *fontSetSlot {
	if int(set.index) >= len(l.sets) || l.sets[set.index].set != set {
		return nil
	}
	return l.sets[set.index]
}

// reportSetNotHeld reports set once, refused as use, what was done with it,
// says.
func (l *fontLibrary) reportSetNotHeld(set FontSet, use string) {
	refusal := Report{Kind: ReportKindFontSetNotHeld, Err: fmt.Errorf("canvas: %s font set %v, which canvas does not hold: never made by WriteFontLibrary.MakeFontSet, destroyed, or the default set, which is never destroyed; a set canvas does not hold is laid out and measured as the default set", use, set)}
	l.reporter.reportOnce(fontSetNotHeldLogKey+fmt.Sprint(set), refusal)
}
