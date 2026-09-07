package canvas

import "embed"

const (
	builtinMountID        = "builtin:canvas"
	spriteShaderPath      = "builtin/canvas/sprite.wgsl"
	spriteBatchShaderPath = "builtin/canvas/spritebatch.wgsl"
	trianglesShaderPath   = "builtin/canvas/triangles.wgsl"
	textureShaderPath     = "builtin/canvas/texture.wgsl"

	// DefaultFontPath is the font Canvas draws with when Text is given no font
	// path. It ships inside the binary and is mounted alongside the built-in
	// shaders, so putting a number on screen costs no asset, no mount and no
	// configuration. It is exported so a caller can name it deliberately and mix
	// it with their own fonts, rather than reaching it only by omission.
	//
	// The file is google/fonts@ofl/jetbrainsmono/JetBrainsMono[wght].ttf, renamed
	// only because '[' is a glob metacharacter in a go:embed pattern. That build
	// covers Latin, Latin-Ext, Greek, Cyrillic, Cyrillic-Ext, arrows and
	// box-drawing in 1179 glyphs, and is smaller compressed than the upstream
	// statics. It is a variable font; x/image/font/sfnt rasterizes its default
	// instance, which is Regular.
	//
	// Its programming ligatures never fire here: opentype lays glyphs out rune by
	// rune with no GSUB shaping, so debug output is never silently rewritten.
	DefaultFontPath = "builtin/canvas/jetbrainsmono.ttf"

	// defaultFontLicensePath is embedded and mounted beside the font because
	// OFL-1.1 requires the licence to travel with the font software. JetBrains
	// Mono carries no Reserved Font Name, so it may be embedded, and later
	// subset, under its own name.
	defaultFontLicensePath = "builtin/canvas/jetbrainsmono-OFL.txt"
)

//go:embed builtin/canvas/*.wgsl builtin/canvas/*.ttf builtin/canvas/*.txt
var builtinFS embed.FS
