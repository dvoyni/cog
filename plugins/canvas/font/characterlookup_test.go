package font_test

import (
	"slices"

	"github.com/dvoyni/cog/plugins/canvas/font"
)

// characterLookup is a character asked in a language, and the font drawing
// it and whether that font has it.
type characterLookup struct {
	language     string
	character    rune
	font         *font.Loaded
	hasCharacter bool
}

// makeCharacterLookups answers the lookups of set, of JetBrains Mono's
// default font and Go Regular then hanzi.ttf for zh-Hans: each language's
// first font having a character draws it, else the default font, which
// has neither ideographs nor Hangul.
func makeCharacterLookups(set *font.Set) []characterLookup {
	goFont := set.LanguageFont("zh-Hans")
	hanzi := slices.Collect(set.Fonts())[2]
	return []characterLookup{
		{"zh-Hans", 'a', goFont, true},
		{"zh-Hans", '今', hanzi, true},
		{"zh-Hans", '한', set.Default(), false},
		{"", 'a', set.Default(), true},
		{"", '今', set.Default(), false},
	}
}
