package canvas

import (
	"fmt"
	"io/fs"
	"sync"

	"github.com/dvoyni/cog/kernel"
)

// fontSetSlot is a font set as the FontLibrary resource holds it, at its
// FontSet's index: its fonts, loaded as it was made, and the font drawing
// each character in each language, found as text is laid out. A layout is
// keyed by the slot it was laid out in, so a set made anew, the default set
// as canvas starts among them, lays its text out anew.
type fontSetSlot struct {
	// set is the FontSet naming the slot, the default set's zero, a made
	// set's generation never zero. Destroying a set counts its generation
	// up, so a FontSet naming the set destroyed names nothing.
	set FontSet
	// label is its descriptor's Label, and logger what it reports a
	// character no font of it has to.
	label       string
	logger      *kernel.Logger
	defaultFont loadedFont
	// languageFonts are the fonts serving each language that has any, in
	// the order its descriptor lists them.
	languageFonts map[string][]loadedFont
	// characterFonts are the font drawing each character in each language,
	// found the first time the character is laid out in the language, so a
	// change of language clears nothing. They are found under the
	// FontLibrary resource's read lock, by jobs laying text out together,
	// so characterMutex guards them, a lookup taking it for read.
	characterMutex sync.RWMutex
	characterFonts map[languageCharacter]*loadedFont
}

// makeFontSetSlot answers the set descriptor describes, every font file it
// names loaded through files, an empty path canvas's JetBrains Mono. A font
// that fails to load is reported once to logger for its path and left out
// of the set, a failed default font replaced by JetBrains Mono; a variation
// naming no axis is reported once for its font's path and ignored. Its set
// is the default set until its maker names it.
func makeFontSetSlot(files fs.FS, logger *kernel.Logger, descriptor FontSetDescriptor) *fontSetSlot {
	slot := &fontSetSlot{label: descriptor.Label, logger: logger, languageFonts: map[string][]loadedFont{}, characterFonts: map[languageCharacter]*loadedFont{}}
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
func (s *fontSetSlot) findLanguageFont(language string) *loadedFont {
	if fonts := s.languageFonts[language]; len(fonts) != 0 {
		return &fonts[0]
	}
	return &s.defaultFont
}

// findCharacterFont answers the font drawing character in language: the
// first of the language's fonts that has it, else the default font, which
// draws a character no font of the set has as its missing-glyph box,
// reported once for the set. It is found the first time, then kept.
func (s *fontSetSlot) findCharacterFont(language string, character rune) *loadedFont {
	key := languageCharacter{language: language, character: character}
	s.characterMutex.RLock()
	drawingFont, cached := s.characterFonts[key]
	s.characterMutex.RUnlock()
	if cached {
		return drawingFont
	}
	drawingFont = &s.defaultFont
	fonts := s.languageFonts[language]
	for index := range fonts {
		if _, has := fonts[index].font.NominalGlyph(character); has {
			drawingFont = &fonts[index]
			break
		}
	}
	if _, has := drawingFont.font.NominalGlyph(character); !has {
		missing := Report{Kind: ReportKindCharacterMissing, Err: fmt.Errorf("canvas: font set %q, %v, has no font drawing %q in language %q, neither the language's fonts nor its default font, so it is drawn as the default font's missing-glyph box", s.label, s.set, character, language)}
		s.logger.LogErrOnce(characterMissingLogKey+fmt.Sprint(s.set), missing)
	}
	s.characterMutex.Lock()
	s.characterFonts[key] = drawingFont
	s.characterMutex.Unlock()
	return drawingFont
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

// languageCharacter is a character laid out in a language, by its tag.
type languageCharacter struct {
	language  string
	character rune
}
