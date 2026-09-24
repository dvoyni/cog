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
			return Config{}, fmt.Errorf("model: invalid config %T", value)
		}
		config = WithDefaults(given)
	}
	if config.PoseSampleRate <= 0 {
		return Config{}, fmt.Errorf("model: PoseSampleRate must be positive, got %d", config.PoseSampleRate)
	}
	return config, nil
}

// Config is model's configuration; see model.Config. It is declared here
// because the Lookup resource holds it, and the root aliases it.
type Config struct {
	// PoseSampleRate is the global animation bake rate in Hz: every clip of
	// every model is baked to pose rows at this rate at load. It must be
	// positive.
	PoseSampleRate int
}

// WithDefaults fills every zero field of config with the value model runs with
// when it is given none: a 60 Hz bake rate.
func WithDefaults(config Config) Config {
	if config.PoseSampleRate == 0 {
		config.PoseSampleRate = 60
	}
	return config
}
