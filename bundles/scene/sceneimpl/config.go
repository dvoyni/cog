package sceneimpl

import (
	"fmt"

	"github.com/dvoyni/cog/bundles/scene/internal"
)

// Config is scene's configuration. PoseSampleRate, the global animation bake
// rate in Hz, is the only configurable number in the plugin: everything else
// scene decides follows from what a frame records. A zero field takes its
// default - 60 Hz - so a caller names only what it changes:
//
//	kernel.New(map[kernel.PluginName]any{scene.Name: sceneimpl.Config{PoseSampleRate: 30}})
type Config = internal.Config

// resolveConfig reads the plugin's configuration value, filling every zero field
// from the defaults, and validates what results.
func resolveConfig(value any) (Config, error) {
	config := internal.DefaultConfig()
	if value != nil {
		given, ok := value.(Config)
		if !ok {
			return Config{}, fmt.Errorf("scene: invalid config %T", value)
		}
		if given.PoseSampleRate != 0 {
			config.PoseSampleRate = given.PoseSampleRate
		}
	}
	if config.PoseSampleRate <= 0 {
		return Config{}, fmt.Errorf("scene: PoseSampleRate must be positive, got %d", config.PoseSampleRate)
	}
	return config, nil
}
