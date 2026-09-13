package canvas

import "fmt"

type Config struct {
	AtlasSize      int
	LayersPerArray int
	MaxAtlasBytes  int
}

func DefaultConfig() Config {
	return Config{
		AtlasSize:      4096,
		LayersPerArray: 2,
		MaxAtlasBytes:  256 << 20,
	}
}

func resolveConfig(value any) (Config, error) {
	config := DefaultConfig()
	if value != nil {
		var ok bool
		config, ok = value.(Config)
		if !ok {
			return Config{}, fmt.Errorf("canvas: invalid config %T", value)
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
