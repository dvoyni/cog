package internal

import (
	"fmt"
)

// defaultPrewarmEntities is what a zero PrewarmEntities reserves.
const defaultPrewarmEntities = 1024

// resolveConfig reads the plugin's configuration value, filling every zero field
// from the defaults.
func resolveConfig(value any) (Config, error) {
	config := Config{PrewarmEntities: defaultPrewarmEntities}
	if value == nil {
		return config, nil
	}
	given, ok := value.(Config)
	if !ok {
		return Config{}, fmt.Errorf("ecs: invalid config %T", value)
	}
	if given.PrewarmEntities != 0 {
		config.PrewarmEntities = given.PrewarmEntities
	}
	return config, nil
}

// Config is the ecs plugin's configuration, keyed by Name in the engine's
// configuration map. A zero field takes its default, so a caller names only
// what it changes:
//
//	kernel.New(map[kernel.PluginName]any{ecs.Name: ecs.Config{PrewarmEntities: 50_000}})
type Config struct {
	// PrewarmEntities is how many Entities the authority reserves room for up
	// front: the peak concurrent count the app expects. It is not a limit —
	// exceeding it costs a growth, not an error. Zero means the default, 1024.
	PrewarmEntities uint32
}
