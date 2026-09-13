package canvas

import "embed"

const (
	builtinMountID = "builtin:canvas"

	// The three entry-point sources: the roots a material names, one per family.
	// They are not includable - an app that wants their pieces includes the
	// published sources below instead - and there is one sprite path, which is
	// instanced, a lone sprite being its degenerate one-instance case.
	spriteShaderPath    = "builtin/canvas/sprite.wgsl"
	trianglesShaderPath = "builtin/canvas/triangles.wgsl"
	textureShaderPath   = "builtin/canvas/texture.wgsl"

	// The fourth entry point, and the one that is not a default: the halo, which
	// paints a soft outward band and no mark at all. It is a sprite-family root
	// like sprite.wgsl and reached only through HaloMaterialSet - unexported
	// because the material is, and the material is because a draw naming it
	// would take none of its scope's parameters and render at the material's own
	// defaults, silently ignoring every profile named above it.
	//
	// Its WGSL is not published either. keycolor.wgsl is, because a custom
	// triangles material must reproduce the key-colour ramp or key every texel
	// against black; nothing has to reproduce a halo.
	haloShaderPath = "builtin/canvas/halo.wgsl"

	// The seven published sources: the WGSL an app includes when it writes a
	// canvas material, so it declares six lines and three includes instead of
	// copying seventy lines of contract it would then have to keep in sync by
	// hand. Each is included by absolute storage name, which is what these
	// constants spell - a relative include has no directory to resolve against
	// from a ShaderWithText root, and an inline Go string is the shape a custom
	// canvas material actually has.
	//
	// Every one of them states in its header exactly what it declares, because
	// the rule an app must obey is "do not declare anything a source you included
	// declares" and a duplicated binding costs the whole frame with nothing
	// reported anywhere.
	//
	// Each resolves through the full mount overlay, so an app that mounts its own
	// builtin/canvas/keycolor.wgsl at higher priority replaces that one source
	// inside canvas's own module and keeps the rest. That is cog's customization
	// mechanism working as designed, recorded here as available rather than left
	// to be discovered.

	// UniformsPath declares struct CanvasUniforms and the group 0 binding.
	// Include it only if you are NOT extending the block: an extending material
	// hand-writes those six lines, which is exactly why the block is its own
	// source.
	UniformsPath = "builtin/canvas/uniforms.wgsl"

	// ClipPath declares canvasClipped, and nothing else. Calling it is offered
	// rather than required, and a hand-written fs_main that omits it draws
	// outside the clip rectangle with no error anywhere.
	ClipPath = "builtin/canvas/clip.wgsl"

	// SpriteBindingsPath declares the sprite family's group 1 sampler and array
	// texture, its group 2 instance buffer, and the SpriteInstance, Instances and
	// VertexOut structs.
	SpriteBindingsPath = "builtin/canvas/spritebindings.wgsl"

	// SpriteVertexPath includes SpriteBindingsPath and declares vs_main. Include
	// it to replace only fs_main.
	SpriteVertexPath = "builtin/canvas/spritevertex.wgsl"

	// TrianglesBindingsPath declares the triangles family's group 1 sampler and
	// texture and its VertexOut.
	TrianglesBindingsPath = "builtin/canvas/trianglesbindings.wgsl"

	// TrianglesVertexPath includes TrianglesBindingsPath and declares vs_main.
	TrianglesVertexPath = "builtin/canvas/trianglesvertex.wgsl"

	// KeyColorPath declares keyColorRamp, the sRGB transfer functions it is
	// written in, and the three key* constants, so a custom material wears the
	// exact ramp the built-ins do rather than a re-typed approximation.
	KeyColorPath = "builtin/canvas/keycolor.wgsl"

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
