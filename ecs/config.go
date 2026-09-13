package ecs

import "fmt"

// Config is the ecs plugin's configuration, keyed by Name in the engine's
// configuration map.
type Config struct {
	// PrewarmEntities is how many Entities the authority reserves room for up
	// front: the peak concurrent count the app expects. It is not a limit —
	// exceeding it costs a growth, not an error.
	PrewarmEntities uint32
}

// DefaultConfig is the configuration a plugin with no entry in the map runs on.
func DefaultConfig() Config { return Config{PrewarmEntities: 1024} }

// WithPrewarmEntities replaces how many Entities the authority reserves room for.
func (c Config) WithPrewarmEntities(count uint32) Config {
	c.PrewarmEntities = count
	return c
}

func resolveConfig(value any) (Config, error) {
	if value == nil {
		return DefaultConfig(), nil
	}
	config, ok := value.(Config)
	if !ok {
		return Config{}, fmt.Errorf("ecs: invalid config %T", value)
	}
	return config, nil
}
