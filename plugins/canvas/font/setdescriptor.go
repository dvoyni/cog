package font

// SetDescriptor describes a font set: a default font, and the fonts serving
// each language. Text names a set and a size, never a font, so switching
// the language switches the typeface.
type SetDescriptor struct {
	// Label names the set in reports.
	Label string
	// Default is the font a language with no fonts of its own is measured
	// and drawn with. Its zero is the built-in JetBrains Mono, which also
	// replaces a default font whose file fails to load.
	Default Font
	// Languages are the fonts serving each language, a language's first
	// font measuring it.
	Languages []LanguageFonts
}

// LanguageFonts are the fonts serving Languages, each a tag such as "ja" or
// "zh-Hans", matched exactly, so one font may serve several languages.
type LanguageFonts struct {
	Languages []string
	Fonts     []Font
}
