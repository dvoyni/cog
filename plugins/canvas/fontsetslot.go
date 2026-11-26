package canvas

import (
	"errors"
	"fmt"
	"io/fs"
	"sync/atomic"

	"github.com/dvoyni/cog/plugins/canvas/font"
)

// fontSetSlot is a font set as the FontLibrary resource holds it, at its
// FontSet's index: its fonts, loaded as it was made, which answer the font
// drawing each character in each language. A layout is keyed by the slot
// it was laid out in, so a set made anew, the default set as canvas starts
// among them, lays its text out anew.
type fontSetSlot struct {
	// set is the FontSet naming the slot, the default set's zero, a made
	// set's generation never zero. Destroying a set counts its generation
	// up, so a FontSet naming the set destroyed names nothing.
	set FontSet
	// reporter is where it reports a character no font of it has.
	reporter *reporter
	// fonts are the set's fonts, loaded of its descriptor as given, whose
	// Label is the set's and which a reload loads it again of.
	fonts *font.Set
	// fileStamps are the stamps of the font files the set names, by path,
	// each taken before the file was read.
	fileStamps map[string]fileStamp
	// characterMissingReported is whether a character no font of the set
	// has was reported, so a lookup of one after makes no report.
	characterMissingReported atomic.Bool
}

// makeFontSetSlot answers the set descriptor describes, every font file it
// names loaded through files, an empty path canvas's JetBrains Mono. A font
// that fails to load is reported once for its path and left out
// of the set, a failed default font replaced by JetBrains Mono; a variation
// naming no axis is reported once for its font's path and ignored. One that
// loads re-arms its path's failure, so a later failure of its file reports
// again. Each file is stamped before it is read. Its set is the default set
// until its maker names it.
func makeFontSetSlot(files fs.FS, reporter *reporter, descriptor font.SetDescriptor) *fontSetSlot {
	slot := &fontSetSlot{reporter: reporter, fileStamps: map[string]fileStamp{}}
	slot.stampFile(files, descriptor.Default.Path)
	for _, entry := range descriptor.Languages {
		for _, fontDescriptor := range entry.Fonts {
			slot.stampFile(files, fontDescriptor.Path)
		}
	}
	var failures []font.LoadError
	slot.fonts, failures = font.Load(files, descriptor)
	for loadedFont := range slot.fonts.Fonts() {
		reporter.forget(fontNotLoadedLogKey + loadedFont.Font().Path)
	}
	for _, failure := range failures {
		path := failure.Font.Path
		if errors.Is(failure.Err, font.ErrVariationIgnored) {
			ignored := Report{Kind: ReportKindVariationIgnored, Err: fmt.Errorf("canvas: font %q of font set %q has a variation of a non-zero value naming no axis, such as font.AxisTagFromName answers for a name of other than four bytes, which is ignored", path, descriptor.Label)}
			reporter.reportOnce(variationIgnoredLogKey+path, ignored)
			continue
		}
		notLoaded := Report{Kind: ReportKindFontNotLoaded, Err: fmt.Errorf("canvas: font %q of font set %q did not load, so the set is made without it, a default font replaced by JetBrains Mono: %w", path, descriptor.Label, failure.Err)}
		reporter.reportOnce(fontNotLoadedLogKey+path, notLoaded)
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

// findCharacterFont answers the font drawing character in language, as
// font.Set.CharacterFont answers it: a character no font of the set has is
// drawn as the default font's missing-glyph box, reported once for the set,
// the first time one is laid out.
func (s *fontSetSlot) findCharacterFont(language string, character rune) *font.Loaded {
	drawingFont, hasCharacter := s.fonts.CharacterFont(language, character)
	if !hasCharacter && !s.characterMissingReported.Swap(true) {
		missing := Report{Kind: ReportKindCharacterMissing, Err: fmt.Errorf("canvas: font set %q, %v, has no font drawing %q in language %q, neither the language's fonts nor its default font, so it is drawn as the default font's missing-glyph box", s.fonts.Descriptor().Label, s.set, character, language)}
		s.reporter.reportOnce(characterMissingLogKey+fmt.Sprint(s.set), missing)
	}
	return drawingFont
}
