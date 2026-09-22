package internal

import (
	"fmt"

	"github.com/dvoyni/cog/bundles/model"
	"github.com/dvoyni/cog/bundles/model/internal/types"
)

// resolveConfig reads the plugin's configuration value, filling every zero field
// from the defaults, and validates what results.
func resolveConfig(value any) (model.Config, error) {
	config := types.WithDefaults(model.Config{})
	if value != nil {
		given, ok := value.(model.Config)
		if !ok {
			return model.Config{}, fmt.Errorf("model: invalid config %T", value)
		}
		config = types.WithDefaults(given)
	}
	if config.PoseSampleRate <= 0 {
		return model.Config{}, fmt.Errorf("model: PoseSampleRate must be positive, got %d", config.PoseSampleRate)
	}
	return config, nil
}
