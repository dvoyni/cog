package internal

import (
	"fmt"

	"github.com/dvoyni/cog/bundles/canvas"
	"github.com/dvoyni/cog/bundles/canvas/internal/types"
)

// resolveConfig reads the plugin's configuration value, filling every zero field
// from the defaults, and validates what results.
func resolveConfig(value any) (canvas.Config, error) {
	config := types.WithDefaults(canvas.Config{})
	if value != nil {
		given, ok := value.(canvas.Config)
		if !ok {
			return canvas.Config{}, fmt.Errorf("canvas: invalid config %T", value)
		}
		config = types.WithDefaults(given)
	}
	if config.AtlasSize <= 0 || config.LayersPerArray < 2 || config.MaxAtlasBytes <= 0 {
		return canvas.Config{}, fmt.Errorf("canvas: invalid config values")
	}
	arrayBytes := int64(config.AtlasSize) * int64(config.AtlasSize) * 4 * int64(config.LayersPerArray)
	if arrayBytes > int64(config.MaxAtlasBytes) {
		return canvas.Config{}, fmt.Errorf("canvas: atlas array requires %d bytes, budget is %d", arrayBytes, config.MaxAtlasBytes)
	}
	return config, nil
}
