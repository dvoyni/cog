package canvas

import (
	"fmt"
	"io/fs"
	"sync"

	"github.com/go-text/typesetting/font"
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
	// reporter is where it reports a character no font of it has.
	reporter    *reporter
	defaultFont loadedFont
	// descriptor is what it was made of, as given, its Label the set's,
	// which a reload makes it again of, and fileStamps the stamps of the
	// font files it names, by path, each taken before the file was read.
	descriptor FontSetDescriptor
	fileStamps map[string]fileStamp
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
// that fails to load is reported once for its path and left out
// of the set, a failed default font replaced by JetBrains Mono; a variation
// naming no axis is reported once for its font's path and ignored. Each
// file is stamped before it is read. Its set is the default set until its
// maker names it.
func makeFontSetSlot(files fs.FS, reporter *reporter, descriptor FontSetDescriptor) *fontSetSlot {
	slot := &fontSetSlot{reporter: reporter, descriptor: descriptor, fileStamps: map[string]fileStamp{}, languageFonts: map[string][]loadedFont{}, characterFonts: map[languageCharacter]*loadedFont{}}
	slot.stampFile(files, descriptor.Default.Path)
	defaultFont, loaded := loadSetFont(files, reporter, descriptor.Label, descriptor.Default)
	if !loaded {
		defaultFont, _ = loadFont(files, Font{})
	}
	slot.defaultFont = defaultFont
	for _, entry := range descriptor.Languages {
		for _, fontDescriptor := range entry.Fonts {
			slot.stampFile(files, fontDescriptor.Path)
			loadedFont, loaded := loadSetFont(files, reporter, descriptor.Label, fontDescriptor)
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

// stampFile stamps the font file at path in files, unless path is empty,
// canvas's built-in font, which never changes, or stamped already.
func (s *fontSetSlot) stampFile(files fs.FS, path string) {
	if _, stamped := s.fileStamps[path]; path == "" || stamped {
		return
	}
	s.fileStamps[path] = readFileStamp(files, path)
}

// namesChangedFile answers whether a font file the slot names changed in
// files since it was stamped.
func (s *fontSetSlot) namesChangedFile(files fs.FS) bool {
	for path, stamp := range s.fileStamps {
		if !readFileStamp(files, path).equal(stamp) {
			return true
		}
	}
	return false
}

// appendFonts appends the parsed font of each of the slot's fonts to fonts.
func (s *fontSetSlot) appendFonts(fonts []*font.Font) []*font.Font {
	fonts = append(fonts, s.defaultFont.font)
	for _, languageFonts := range s.languageFonts {
		for _, languageFont := range languageFonts {
			fonts = append(fonts, languageFont.font)
		}
	}
	return fonts
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
		missing := Report{Kind: ReportKindCharacterMissing, Err: fmt.Errorf("canvas: font set %q, %v, has no font drawing %q in language %q, neither the language's fonts nor its default font, so it is drawn as the default font's missing-glyph box", s.descriptor.Label, s.set, character, language)}
		s.reporter.reportOnce(characterMissingLogKey+fmt.Sprint(s.set), missing)
	}
	s.characterMutex.Lock()
	s.characterFonts[key] = drawingFont
	s.characterMutex.Unlock()
	return drawingFont
}

// loadSetFont loads fontDescriptor, a font of the set labelled setLabel,
// through files, as makeFontSetSlot does, and answers whether it loaded. A
// failure is reported once for its path; one that loads re-arms
// that report, so a later failure of its file reports again.
func loadSetFont(files fs.FS, reporter *reporter, setLabel string, fontDescriptor Font) (loadedFont, bool) {
	for _, variation := range fontDescriptor.Variations {
		if variation.Axis == (AxisTag{}) && variation.Value != 0 {
			ignored := Report{Kind: ReportKindVariationIgnored, Err: fmt.Errorf("canvas: font %q of font set %q has a variation of value %v naming no axis, such as AxisTagFromName answers for a name of other than four bytes, which is ignored", fontDescriptor.Path, setLabel, variation.Value)}
			reporter.reportOnce(variationIgnoredLogKey+fontDescriptor.Path, ignored)
		}
	}
	loaded, err := loadFont(files, fontDescriptor)
	if err != nil {
		failure := Report{Kind: ReportKindFontNotLoaded, Err: fmt.Errorf("canvas: font %q of font set %q did not load, so the set is made without it, a default font replaced by JetBrains Mono: %w", fontDescriptor.Path, setLabel, err)}
		reporter.reportOnce(fontNotLoadedLogKey+fontDescriptor.Path, failure)
		return loadedFont{}, false
	}
	reporter.forget(fontNotLoadedLogKey + fontDescriptor.Path)
	return loaded, true
}

// languageCharacter is a character laid out in a language, by its tag.
type languageCharacter struct {
	language  string
	character rune
}
