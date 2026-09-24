package internal

import (
	"fmt"
)

// resolveConfig reads the plugin's configuration value, filling every zero field
// from the defaults, and validates what results.
func resolveConfig(value any) (Config, error) {
	config := WithDefaults(Config{})
	if value != nil {
		given, ok := value.(Config)
		if !ok {
			return Config{}, fmt.Errorf("canvas: invalid config %T", value)
		}
		config = WithDefaults(given)
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

// Config sizes canvas's atlases; see canvas.Config. It is declared here because
// the atlases and the Lookup resource hold it, and the root aliases it.
type Config struct {
	// AtlasSize is the width and height of one atlas page, in texels.
	AtlasSize int
	// LayersPerArray is how many pages one atlas texture array holds. It must be
	// at least two.
	LayersPerArray int
	// MaxAtlasBytes bounds the GPU memory one atlas may allocate; one whole
	// array must fit within it.
	MaxAtlasBytes int
}

// WithDefaults fills every zero field of config with the value canvas runs with
// when it is given none: 4096-texel pages, two pages per array and 256 MiB.
func WithDefaults(config Config) Config {
	if config.AtlasSize == 0 {
		config.AtlasSize = 4096
	}
	if config.LayersPerArray == 0 {
		config.LayersPerArray = 2
	}
	if config.MaxAtlasBytes == 0 {
		config.MaxAtlasBytes = 256 << 20
	}
	return config
}
