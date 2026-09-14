package internal

import "embed"

const (
	builtinMountID = "builtin:canvas"

	// defaultFontLicensePath is embedded and mounted beside the font because
	// OFL-1.1 requires the licence to travel with the font software. JetBrains
	// Mono carries no Reserved Font Name, so it may be embedded, and later
	// subset, under its own name.
	defaultFontLicensePath = "builtin/canvas/jetbrainsmono-OFL.txt"
)

// builtinFS is what Start mounts at math.MaxInt priority: the built-in entry
// points, the seven published sources, the halo, and the default font with its
// licence. The paths inside it are the ones canvas.UniformsPath and the other
// published constants spell.
//
//go:embed builtin/canvas/*.wgsl builtin/canvas/*.ttf builtin/canvas/*.txt
var builtinFS embed.FS
