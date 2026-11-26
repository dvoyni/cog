package font

import (
	"io/fs"
	"iter"
	"slices"
	"sync"
)

// Set is a font set as Load loaded it: its fonts, each loaded once, and the
// font drawing each character in each language, found as it is asked. It
// is safe for concurrent use.
type Set struct {
	descriptor  SetDescriptor
	defaultFont *Loaded
	// languageFonts are the fonts serving each language that has any, in
	// the order its descriptor lists them, and loadedFonts each font of the
	// set once, the default font first.
	languageFonts map[string][]*Loaded
	loadedFonts   []*Loaded
	// characterFonts are the font drawing each character in each language,
	// found the first time it is asked, so a change of language clears
	// nothing. characterMutex guards them, a lookup taking it for read.
	characterMutex sync.RWMutex
	characterFonts map[languageCharacter]characterFont
}

// Load loads the set descriptor describes, every font file it names read
// through files once, a font named again, serving several languages or
// listed twice, loaded once, and an empty Path the built-in JetBrains
// Mono. It answers what went wrong, each font's once: a font with a
// variation naming no axis is a LoadError of ErrVariationIgnored, and
// loads without it; a font that fails to load is a LoadError of why, and
// is left out of the set, a failed default font replaced by JetBrains Mono.
// The set keeps descriptor as given, which Descriptor answers, so its
// slices are not changed after.
func Load(files fs.FS, descriptor SetDescriptor) (*Set, []LoadError) {
	set := &Set{descriptor: descriptor, languageFonts: map[string][]*Loaded{}, characterFonts: map[languageCharacter]characterFont{}}
	var failures []LoadError
	// fontsByDescriptor are the fonts loaded by what they were loaded of,
	// nil for one that failed, so each is loaded once.
	fontsByDescriptor := map[Font]*Loaded{}
	loadOnce := func(fontDescriptor Font) *Loaded {
		if loadedFont, tried := fontsByDescriptor[fontDescriptor]; tried {
			return loadedFont
		}
		loadedFont, variationIgnored, err := loadFont(files, fontDescriptor)
		if variationIgnored {
			failures = append(failures, LoadError{Font: fontDescriptor, Err: ErrVariationIgnored})
		}
		if err != nil {
			failures = append(failures, LoadError{Font: fontDescriptor, Err: err})
		} else {
			set.loadedFonts = append(set.loadedFonts, loadedFont)
		}
		fontsByDescriptor[fontDescriptor] = loadedFont
		return loadedFont
	}
	set.defaultFont = loadOnce(descriptor.Default)
	if set.defaultFont == nil {
		set.defaultFont = loadOnce(Font{})
	}
	for _, entry := range descriptor.Languages {
		for _, fontDescriptor := range entry.Fonts {
			loadedFont := loadOnce(fontDescriptor)
			if loadedFont == nil {
				continue
			}
			for _, language := range entry.Languages {
				set.languageFonts[language] = append(set.languageFonts[language], loadedFont)
			}
		}
	}
	return set, failures
}

// Descriptor answers what the set was loaded of, as Load was given it.
func (s *Set) Descriptor() SetDescriptor { return s.descriptor }

// Default answers the set's default font: its descriptor's Default, or
// JetBrains Mono when that failed to load.
func (s *Set) Default() *Loaded { return s.defaultFont }

// LanguageFont answers the font measuring language: its first font that
// loaded, or the default font when it has none.
func (s *Set) LanguageFont(language string) *Loaded {
	if fonts := s.languageFonts[language]; len(fonts) != 0 {
		return fonts[0]
	}
	return s.defaultFont
}

// CharacterFont answers the font drawing character in language: the first
// of the language's fonts that has it, else the default font, and whether
// that font has it, false when no font of the set does, so the default
// font draws it as its missing-glyph box. It is found the first time,
// then kept.
func (s *Set) CharacterFont(language string, character rune) (*Loaded, bool) {
	key := languageCharacter{language: language, character: character}
	s.characterMutex.RLock()
	drawingFont, cached := s.characterFonts[key]
	s.characterMutex.RUnlock()
	if cached {
		return drawingFont.font, drawingFont.hasCharacter
	}
	for _, languageFont := range s.languageFonts[language] {
		if _, has := languageFont.parsed.Font.NominalGlyph(character); has {
			drawingFont = characterFont{font: languageFont, hasCharacter: true}
			break
		}
	}
	if drawingFont.font == nil {
		drawingFont.font = s.defaultFont
		_, drawingFont.hasCharacter = s.defaultFont.parsed.Font.NominalGlyph(character)
	}
	s.characterMutex.Lock()
	s.characterFonts[key] = drawingFont
	s.characterMutex.Unlock()
	return drawingFont.font, drawingFont.hasCharacter
}

// Fonts answers each font of the set once, the default font first: what a
// set replaced lets go of.
func (s *Set) Fonts() iter.Seq[*Loaded] { return slices.Values(s.loadedFonts) }

// languageCharacter is a character laid out in a language, by its tag.
type languageCharacter struct {
	language  string
	character rune
}

// characterFont is the font drawing a character, and whether it has the
// character, false for the default font drawing it as its missing-glyph
// box.
type characterFont struct {
	font         *Loaded
	hasCharacter bool
}
