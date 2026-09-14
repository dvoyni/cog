package internal

import (
	"fmt"

	"github.com/dvoyni/cog/bundles/ecs"
)

// defaultPrewarmEntities is what a zero PrewarmEntities reserves.
const defaultPrewarmEntities = 1024

// resolveConfig reads the plugin's configuration value, filling every zero field
// from the defaults.
func resolveConfig(value any) (ecs.Config, error) {
	config := ecs.Config{PrewarmEntities: defaultPrewarmEntities}
	if value == nil {
		return config, nil
	}
	given, ok := value.(ecs.Config)
	if !ok {
		return ecs.Config{}, fmt.Errorf("ecs: invalid config %T", value)
	}
	if given.PrewarmEntities != 0 {
		config.PrewarmEntities = given.PrewarmEntities
	}
	return config, nil
}
