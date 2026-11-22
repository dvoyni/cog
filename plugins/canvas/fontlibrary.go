package canvas

import (
	"cmp"
	"fmt"
	"sync"

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
	sets        []fontSetSlot
	freeIndexes []uint32
	// defaultDescriptor is Config.DefaultFontSet, which the default set is
	// made of as canvas starts: it is kept here, since a task's argument
	// holds no pointer.
	defaultDescriptor FontSetDescriptor
	icons             map[string]IconDescriptor
	colors            map[string]m.Color
	// shapers are the text shapers layouts are made by, each a
	// *textShaper, one a layout, since a shaper is not safe for concurrent
	// use and layouts are made under the read lock.
	shapers sync.Pool
	// glyphs are the glyphs made distance fields for drawing, each at
	// glyphFieldEm texels to the em and reaching glyphFieldRange texels
	// across its edge when its font gives none, Config.GlyphFieldEm's and
	// Config.GlyphFieldRange's.
	glyphs          glyphTable
	glyphFieldEm    int
	glyphFieldRange int
}

// newFontLibrary answers the FontLibrary resource, its reports logged to logger, its
// current language language, a run of text of no size laid out at
// textSize, a glyph of a font giving no field em or range made a distance
// field of glyphFieldEm texels to the em reaching glyphFieldRange texels
// across its edge, and its default set JetBrains Mono alone until canvas
// starts and makes it of defaultDescriptor.
func newFontLibrary(logger *kernel.Logger, language string, textSize float32, glyphFieldEm, glyphFieldRange int, defaultDescriptor FontSetDescriptor) *fontLibrary {
	return &fontLibrary{
		logger:            logger,
		language:          language,
		textSize:          textSize,
		glyphFieldEm:      glyphFieldEm,
		glyphFieldRange:   glyphFieldRange,
		sets:              []fontSetSlot{makeFontSetSlot(nil, logger, FontSetDescriptor{})},
		defaultDescriptor: defaultDescriptor,
		icons:             map[string]IconDescriptor{},
		colors:            map[string]m.Color{},
		shapers:           sync.Pool{New: func() any { return new(textShaper) }},
		glyphs:            glyphTable{indexes: map[glyphKey]uint32{}},
	}
}

// Language answers the current language.
func (l *fontLibrary) Language() string { return l.language }

// Metrics answers the measures at size, its em in world units, of the font
// of set measuring language, an empty language meaning the current one: the
// language's first font in set, or set's default font. A set canvas does
// not hold is measured as the default set, reported once.
func (l *fontLibrary) Metrics(set FontSet, size float32, language string) FontMetrics {
	slot := l.findSlot(set)
	if slot == nil {
		l.reportSetNotHeld(set, "measuring")
		slot = &l.sets[0]
	}
	return slot.findLanguageFont(cmp.Or(language, l.language)).findMetrics(size)
}

// Layout answers text shaped, broken into lines and measured, in the font
// of its set measuring the current language, at its size, zero meaning
// Config.TextSize. A set canvas does not hold is laid out as the default
// set, reported once.
func (l *fontLibrary) Layout(text Text) TextLayout {
	return l.layOutText(text, false)
}

// layOutText answers text laid out as Layout lays it out, and, when
// drawn, each of its glyphs' fields found in the glyph table, made the
// first time through the shaper's face of its font, at its font's field em
// and range, zero meaning the library's, its glyphs of no field left out.
func (l *fontLibrary) layOutText(text Text, drawn bool) TextLayout {
	slot := l.findSlot(text.Font)
	if slot == nil {
		l.reportSetNotHeld(text.Font, "laying out text in")
		slot = &l.sets[0]
	}
	textFont := slot.findLanguageFont(l.language)
	shaper := l.shapers.Get().(*textShaper)
	defer l.shapers.Put(shaper)
	layout := shaper.layOut(textFont, text, cmp.Or(text.Size, l.textSize), l.language)
	if drawn {
		layout.glyphs = l.glyphs.assignFields(layout.glyphs, shaper.findFace(textFont), textFont.font, cmp.Or(textFont.fieldEm, l.glyphFieldEm), cmp.Or(textFont.fieldRange, l.glyphFieldRange))
	}
	return layout
}

// findSlot answers set's slot, nil when canvas does not hold set.
func (l *fontLibrary) findSlot(set FontSet) *fontSetSlot {
	if int(set.index) >= len(l.sets) || l.sets[set.index].generation != set.generation {
		return nil
	}
	return &l.sets[set.index]
}

// reportSetNotHeld reports set once, refused as use, what was done with it,
// says.
func (l *fontLibrary) reportSetNotHeld(set FontSet, use string) {
	refusal := Report{Kind: ReportKindFontSetNotHeld, Err: fmt.Errorf("canvas: %s font set %v, which canvas does not hold: never made by WriteFontLibrary.MakeFontSet, destroyed, or the default set, which is never destroyed; a set canvas does not hold is laid out and measured as the default set", use, set)}
	l.logger.LogErrOnce(fontSetNotHeldLogKey+fmt.Sprint(set), refusal)
}
