package internal

import (
	"fmt"

	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/bundles/scene/internal/types"
)

// resolveConfig reads the plugin's configuration value, filling every zero field
// from the defaults, and validates what results.
func resolveConfig(value any) (scene.Config, error) {
	config := types.WithDefaults(scene.Config{})
	if value != nil {
		given, ok := value.(scene.Config)
		if !ok {
			return scene.Config{}, fmt.Errorf("scene: invalid config %T", value)
		}
		config = types.WithDefaults(given)
	}
	if config.PoseSampleRate <= 0 {
		return scene.Config{}, fmt.Errorf("scene: PoseSampleRate must be positive, got %d", config.PoseSampleRate)
	}
	return config, nil
}
