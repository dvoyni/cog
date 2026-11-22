package canvas

import (
	"cmp"
	"fmt"
	"slices"
	"sync"
	"unsafe"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
)

// fontLibrary is the FontLibrary resource: the font sets made, the current
// language, and the icons and colours registered. Its reads run under the
// kernel's read lock of FontLibrary, and fontLibraryWriter's writes, and the default
// set's making as canvas starts, under its write lock alone, so it needs no
// lock of its own. It has no method that writes, so a FontLibrary held under the
// read lock cannot be asserted into writing.
type fontLibrary struct {
	logger *kernel.Logger
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
	defaultDescriptor FontSetDescriptor
	icons             map[string]registeredIcon
	// iconSerial counts the icons registered, each registered under the
	// count it makes, so a name registered again names another icon to
	// the layout cache's key.
	iconRegistrationCount uint64
	colors                map[string]m.Color
	// shapers are the text shapers layouts are made by, each a
	// *textShaper, one a layout, since a shaper is not safe for concurrent
	// use and layouts are made under the read lock.
	shapers sync.Pool
	// glyphs are the glyphs made distance fields for drawing.
	glyphs glyphTable
	// layouts are the runs laid out lately, which Layout and DrawText
	// answer from.
	layouts layoutCache
}

// newFontLibrary answers the FontLibrary resource of config, its reports
// logged to logger: its current language config's Language, a run of text
// of no size laid out at its TextSize, a glyph of a font giving no field em
// or range made a distance field of its GlyphFieldEm and GlyphFieldRange, a
// layout unused for its LayoutIdleFrames frames freed, each zero meaning
// its stated default, and its default set JetBrains Mono alone until canvas
// starts and makes it of its DefaultFontSet.
func newFontLibrary(logger *kernel.Logger, config *Config) *fontLibrary {
	return &fontLibrary{
		logger:            logger,
		language:          config.Language,
		textSize:          cmp.Or(config.TextSize, 16),
		sets:              []*fontSetSlot{makeFontSetSlot(nil, logger, FontSetDescriptor{})},
		defaultDescriptor: config.DefaultFontSet,
		icons:             map[string]registeredIcon{},
		colors:            map[string]m.Color{},
		shapers:           sync.Pool{New: func() any { return new(textShaper) }},
		glyphs:            glyphTable{indexes: map[glyphKey]uint32{}, fieldEm: cmp.Or(config.GlyphFieldEm, 48), fieldRange: cmp.Or(config.GlyphFieldRange, 16)},
		layouts:           layoutCache{layouts: map[layoutInput]*cachedLayout{}, idleFrames: uint64(cmp.Or(config.LayoutIdleFrames, 2))},
	}
}

// Language answers the current language.
func (l *fontLibrary) Language() string { return l.language }

// Metrics answers the measures at size, its em in world units, of the font
// of set measuring language, an empty language meaning the current one: the
// language's first font in set, or set's default font. A set canvas does
// not hold is measured as the default set, reported once.
func (l *fontLibrary) Metrics(set FontSet, size float32, language string) FontMetrics {
	return l.findHeldSlot(set, "measuring").findLanguageFont(cmp.Or(language, l.language)).findMetrics(size)
}

// Layout answers text shaped, broken into lines and measured, in the font
// of its set measuring the current language, at its size, zero meaning
// Config.TextSize, from the layout cache, which DrawText lays out through.
// A set canvas does not hold is laid out as the default set, reported once.
func (l *fontLibrary) Layout(text Text) TextLayout {
	return l.layOutText(text, false)
}

// Preload makes the distance fields of the glyphs of text, shaped on one
// line in set's fonts for the current language, which a run of any size
// draws, so the first draw of a run of it makes none. A set canvas does not
// hold preloads the default set's, reported once.
func (l *fontLibrary) Preload(set FontSet, text string) {
	slot := l.findHeldSlot(set, "preloading glyphs of")
	shaper := l.shapers.Get().(*textShaper)
	defer l.shapers.Put(shaper)
	layout := shaper.layOut(layoutInput{text: text, set: slot, em: l.textSize, language: l.language}, l.icons)
	l.glyphs.assignFields(layout.glyphs, shaper)
}

// layOutText answers text laid out as Layout lays it out, through the
// layout cache, shaped the first time, and, when drawn, its glyphs those
// it draws: a copy of the layout's, each assigned its field in the glyph
// table, without those of no field, found the first time the cached layout
// is drawn and kept beside it. Two jobs drawing it first at once each find
// them, and the first kept is answered to both.
func (l *fontLibrary) layOutText(text Text, drawn bool) TextLayout {
	// A shaper is taken for a run of spans, whose key it encodes, or to
	// shape, not for a run of a string found cached.
	var shaper *textShaper
	var spanKey *[]byte
	if len(text.Spans) > 0 {
		shaper = l.shapers.Get().(*textShaper)
		defer l.shapers.Put(shaper)
		spanKey = &shaper.spanKey
	}
	input := l.makeLayoutInput(text, spanKey)
	cached := l.layouts.find(input)
	if cached == nil || drawn && cached.drawnGlyphs.Load() == nil {
		if shaper == nil {
			shaper = l.shapers.Get().(*textShaper)
			defer l.shapers.Put(shaper)
		}
		if cached == nil {
			cached = l.layouts.add(input, shaper.layOut(input, l.icons))
		}
		if drawn {
			drawnGlyphs := l.glyphs.assignFields(slices.Clone(cached.layout.glyphs), shaper)
			cached.drawnGlyphs.CompareAndSwap(nil, &drawnGlyphs)
		}
	}
	layout := cached.layout
	if drawn {
		layout.glyphs = *cached.drawnGlyphs.Load()
	}
	return layout
}

// makeLayoutInput answers what text's layout is made of, in its language,
// the current one unless it names its own: a set canvas does not hold lays
// it out in the default set, reported once. Its Spans, when given, are
// encoded into scratch, with the serial each icon was registered under,
// which the input's text then reads, until scratch is written again, so a
// lookup of a spanned run makes no string.
func (l *fontLibrary) makeLayoutInput(text Text, scratch *[]byte) layoutInput {
	input := layoutInput{
		text:       text.String,
		set:        l.findHeldSlot(text.Font, "laying out text in"),
		em:         cmp.Or(text.Size, l.textSize),
		wrapWidth:  text.WrapWidth,
		align:      text.Align,
		lineHeight: text.LineHeight,
		tracking:   text.Tracking,
		language:   cmp.Or(text.Language, l.language),
	}
	if len(text.Spans) > 0 {
		*scratch = appendEncodedSpans((*scratch)[:0], text.Spans, l.icons)
		input.text = unsafe.String(unsafe.SliceData(*scratch), len(*scratch))
		input.spanned = true
	}
	return input
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
	l.logger.LogErrOnce(fontSetNotHeldLogKey+fmt.Sprint(set), refusal)
}
