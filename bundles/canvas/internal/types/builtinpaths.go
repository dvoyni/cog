package types

// The built-in shader paths a canvas-owned material names, and the default
// font. canvas's internal/ mounts the files behind them at Start; the root
// publishes the includable sources and DefaultFontPath under its own names.
const (
	// The three entry-point sources: the roots a material names, one per family.
	// They are not includable - an app that wants their pieces includes the
	// published sources instead - and there is one sprite path, which is
	// instanced, a lone sprite being its degenerate one-instance case.
	SpriteShaderPath    = "builtin/canvas/sprite.wgsl"
	TrianglesShaderPath = "builtin/canvas/triangles.wgsl"
	TextureShaderPath   = "builtin/canvas/texture.wgsl"

	// HaloShaderPath is the fourth entry point, and the one that is not a
	// default: the halo, which paints a soft outward band and no mark at all. It
	// is a sprite-family root like sprite.wgsl and reached only through
	// canvas.HaloMaterialSet - unpublished because the material is, and the
	// material is because a draw naming it would take none of its scope's
	// parameters and render at the material's own defaults, silently ignoring
	// every profile named above it.
	//
	// Its WGSL is not published either. keycolor.wgsl is, because a custom
	// triangles material must reproduce the key-colour ramp or key every texel
	// against black; nothing has to reproduce a halo.
	HaloShaderPath = "builtin/canvas/halo.wgsl"

	// DefaultFontPath is the font Canvas draws with when Text is given no font
	// path. The root re-exports it; see canvas.DefaultFontPath.
	DefaultFontPath = "builtin/canvas/jetbrainsmono.ttf"
)
