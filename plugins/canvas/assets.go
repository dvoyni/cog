package canvas

import (
	"embed"
	"io/fs"
)

const (
	// assetPrefix is the folder of platform's FS canvas's built-in assets
	// appear in, where a 2D material's shader includes canvas's include
	// from, as "cog.canvas/canvas.wgsl".
	assetPrefix = "cog.canvas"
	// defaultShaderPath is the path in platform's FS of the default
	// material's shader.
	defaultShaderPath = assetPrefix + "/default.wgsl"
	// spriteVertexEntryPoint is the include's vertex stage, for an entry
	// drawn as a quad.
	spriteVertexEntryPoint = "canvas_sprite_vertex"
)

// embeddedAssets are canvas's built-in assets: its include and its
// materials' shaders, in its assets folder, each shader's binding
// declaration beside it, which render's cmd/declarebindings generates.
//
//go:generate go run github.com/dvoyni/cog/plugins/render/cmd/declarebindings assets/canvas.wgsl assets/default.wgsl
//go:embed assets
var embeddedAssets embed.FS

// makeAssetFS answers canvas's built-in assets as an FS of their own, each
// named for its path in the assets folder, to mount under assetPrefix.
func makeAssetFS() fs.FS {
	assets, err := fs.Sub(embeddedAssets, "assets")
	if err != nil {
		panic(err) // "assets" is a valid path, which fs.Sub never refuses.
	}
	return assets
}
