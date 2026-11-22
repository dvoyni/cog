package canvas

import (
	"fmt"
	"io/fs"

	"github.com/dvoyni/cog/kernel"
)

// fontSetSlot is a font set as the FontLibrary resource holds it, at its
// FontSet's index: its fonts, loaded as it was made.
type fontSetSlot struct {
	// generation is its FontSet's generation: the default set's is zero, and
	// a made set's never is. Destroying a set counts it up, so a FontSet
	// naming the set destroyed names nothing.
	generation  uint32
	defaultFont loadedFont
	// languageFonts are the fonts serving each language that has any, in
	// the order its descriptor lists them.
	languageFonts map[string][]loadedFont
}

// makeFontSetSlot answers the set descriptor describes, every font file it
// names loaded through files, an empty path canvas's JetBrains Mono. A font
// that fails to load is reported once to logger for its path and left out
// of the set, a failed default font replaced by JetBrains Mono; a variation
// naming no axis is reported once for its font's path and ignored.
func makeFontSetSlot(files fs.FS, logger *kernel.Logger, descriptor FontSetDescriptor) fontSetSlot {
	slot := fontSetSlot{languageFonts: map[string][]loadedFont{}}
	defaultFont, loaded := loadSetFont(files, logger, descriptor.Label, descriptor.Default)
	if !loaded {
		defaultFont, _ = loadFont(files, Font{})
	}
	slot.defaultFont = defaultFont
	for _, entry := range descriptor.Languages {
		for _, fontDescriptor := range entry.Fonts {
			loadedFont, loaded := loadSetFont(files, logger, descriptor.Label, fontDescriptor)
			if !loaded {
				continue
			}
			for _, language := range entry.Languages {
				slot.languageFonts[language] = append(slot.languageFonts[language], loadedFont)
			}
		}
	}
	return slot
}

// findLanguageFont answers the font measuring language: its first font, or
// the default font when it has none.
func (s *fontSetSlot) findLanguageFont(language string) loadedFont {
	if fonts := s.languageFonts[language]; len(fonts) != 0 {
		return fonts[0]
	}
	return s.defaultFont
}

// loadSetFont loads fontDescriptor, a font of the set labelled setLabel,
// through files, as makeFontSetSlot does, and answers whether it loaded. A
// failure is reported once to logger for its path; one that loads re-arms
// that report, so a later failure of its file reports again.
func loadSetFont(files fs.FS, logger *kernel.Logger, setLabel string, fontDescriptor Font) (loadedFont, bool) {
	for _, variation := range fontDescriptor.Variations {
		if variation.Axis == (AxisTag{}) && variation.Value != 0 {
			ignored := Report{Kind: ReportKindVariationIgnored, Err: fmt.Errorf("canvas: font %q of font set %q has a variation of value %v naming no axis, such as AxisTagFromName answers for a name of other than four bytes, which is ignored", fontDescriptor.Path, setLabel, variation.Value)}
			logger.LogErrOnce(variationIgnoredLogKey+fontDescriptor.Path, ignored)
		}
	}
	loaded, err := loadFont(files, fontDescriptor)
	if err != nil {
		failure := Report{Kind: ReportKindFontNotLoaded, Err: fmt.Errorf("canvas: font %q of font set %q did not load, so the set is made without it, a default font replaced by JetBrains Mono: %w", fontDescriptor.Path, setLabel, err)}
		logger.LogErrOnce(fontNotLoadedLogKey+fontDescriptor.Path, failure)
		return loadedFont{}, false
	}
	logger.ForgetLogOnce(fontNotLoadedLogKey + fontDescriptor.Path)
	return loaded, true
}
