package render

import (
	"embed"
	"io/fs"
)

const (
	// assetPrefix is the folder of platform's FS render's built-in assets
	// appear in.
	assetPrefix = "cog.render"
	// mipShaderPath is the path in platform's FS of render's mip shader.
	mipShaderPath = assetPrefix + "/mipshader.wgsl"
)

// embeddedAssets are render's built-in assets, its shaders among them, in
// its assets folder, each shader's binding declaration beside it, which
// cmd/declarebindings generates.
//
//go:generate go run ./cmd/declarebindings assets/mipshader.wgsl
//go:embed assets
var embeddedAssets embed.FS

// makeAssetFS answers render's built-in assets as an FS of their own, each
// named for its path in the assets folder, to mount under assetPrefix.
func makeAssetFS() fs.FS {
	assets, err := fs.Sub(embeddedAssets, "assets")
	if err != nil {
		panic(err) // "assets" is a valid path, which fs.Sub never refuses.
	}
	return assets
}
