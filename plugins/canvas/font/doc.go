// Package font loads the fonts of a font set: TrueType and OpenType files,
// and collections of them, at their variations, read through an fs.FS,
// with go-text, none of which reaches its interface.
//
// A SetDescriptor names a default font and the fonts serving each
// language; Load loads every font it names once, a font serving several
// languages among them, and answers the Set and what went wrong:
//
//	set, failures := font.Load(files, font.SetDescriptor{
//		Label:     "title",
//		Default:   font.Font{Path: "fonts/inter-variable.ttf", Variations: [4]font.Variation{{Axis: font.AxisTagFromName("wght"), Value: 800}}},
//		Languages: []font.LanguageFonts{{Languages: []string{"ja", "zh-Hans"}, Fonts: []font.Font{{Path: "fonts/noto-sans-cjk.ttc"}}}},
//	})
//
// A font that fails to load is left out of the set, a failed default font
// replaced by the built-in JetBrains Mono, which a Font of no Path loads,
// and each failure is a LoadError among failures. A Set then answers the
// font measuring a language and the font drawing each of its characters,
// each Loaded measuring itself at a size.
package font
