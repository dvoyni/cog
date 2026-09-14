package canvas

import "github.com/dvoyni/cog/bundles/canvas/internal/types"

// Config sizes canvas's sprite and glyph atlases: AtlasSize is one page's width
// and height in texels, LayersPerArray how many pages one texture array holds
// (at least two), and MaxAtlasBytes the GPU memory budget one whole array must
// fit within. It arrives through kernel.New's config map under Name, and a zero
// field takes its default - 4096, 2 and 256 MiB - so a caller names only what it
// changes:
//
//	kernel.New(map[kernel.PluginName]any{canvas.Name: canvas.Config{AtlasSize: 2048}})
//
// It is declared in internal/types, because the atlases hold it, and aliased
// here.
type Config = types.Config
