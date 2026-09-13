package canvasimpl

import (
	"fmt"

	"github.com/dvoyni/cog/bundles/canvas/internal"
)

// Config sizes canvas's sprite and glyph atlases: AtlasSize is one page's width
// and height in texels, LayersPerArray how many pages one texture array holds
// (at least two), and MaxAtlasBytes the GPU memory budget one whole array must
// fit within. A zero field takes its default - 4096, 2 and 256 MiB - so a
// caller names only what it changes:
//
//	kernel.New(map[kernel.PluginName]any{canvas.Name: canvasimpl.Config{AtlasSize: 2048}})
type Config = internal.Config

// resolveConfig reads the plugin's configuration value, filling every zero field
// from the defaults, and validates what results.
func resolveConfig(value any) (Config, error) {
	config := internal.DefaultConfig()
	if value != nil {
		given, ok := value.(Config)
		if !ok {
			return Config{}, fmt.Errorf("canvas: invalid config %T", value)
		}
		if given.AtlasSize != 0 {
			config.AtlasSize = given.AtlasSize
		}
		if given.LayersPerArray != 0 {
			config.LayersPerArray = given.LayersPerArray
		}
		if given.MaxAtlasBytes != 0 {
			config.MaxAtlasBytes = given.MaxAtlasBytes
		}
	}
	if config.AtlasSize <= 0 || config.LayersPerArray < 2 || config.MaxAtlasBytes <= 0 {
		return Config{}, fmt.Errorf("canvas: invalid config values")
	}
	arrayBytes := int64(config.AtlasSize) * int64(config.AtlasSize) * 4 * int64(config.LayersPerArray)
	if arrayBytes > int64(config.MaxAtlasBytes) {
		return Config{}, fmt.Errorf("canvas: atlas array requires %d bytes, budget is %d", arrayBytes, config.MaxAtlasBytes)
	}
	return config, nil
}
