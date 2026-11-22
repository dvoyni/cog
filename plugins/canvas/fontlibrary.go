package canvas

import (
	"cmp"
	"fmt"

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
}

// newFontLibrary answers the FontLibrary resource, its reports logged to logger, its
// current language language, and its default set JetBrains Mono alone until
// canvas starts and makes it of defaultDescriptor.
func newFontLibrary(logger *kernel.Logger, language string, defaultDescriptor FontSetDescriptor) *fontLibrary {
	return &fontLibrary{
		logger:            logger,
		language:          language,
		sets:              []fontSetSlot{makeFontSetSlot(nil, logger, FontSetDescriptor{})},
		defaultDescriptor: defaultDescriptor,
		icons:             map[string]IconDescriptor{},
		colors:            map[string]m.Color{},
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
	refusal := Report{Kind: ReportKindFontSetNotHeld, Err: fmt.Errorf("canvas: %s font set %v, which canvas does not hold: never made by WriteFontLibrary.MakeFontSet, destroyed, or the default set, which is never destroyed; a set canvas does not hold is measured as the default set", use, set)}
	l.logger.LogErrOnce(fontSetNotHeldLogKey+fmt.Sprint(set), refusal)
}
