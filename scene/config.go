package scene

import "fmt"

// Config is scene's configuration. PoseSampleRate, the global animation bake
// rate in Hz, is the only configurable number in the plugin: everything else
// scene decides follows from what a frame records.
type Config struct {
	PoseSampleRate int
}

func DefaultConfig() Config { return Config{PoseSampleRate: 60} }

func resolveConfig(value any) (Config, error) {
	config := DefaultConfig()
	if value != nil {
		var ok bool
		config, ok = value.(Config)
		if !ok {
			return Config{}, fmt.Errorf("scene: invalid config %T", value)
		}
	}
	if config.PoseSampleRate <= 0 {
		return Config{}, fmt.Errorf("scene: PoseSampleRate must be positive, got %d", config.PoseSampleRate)
	}
	return config, nil
}
