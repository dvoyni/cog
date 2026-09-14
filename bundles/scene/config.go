package scene

import "github.com/dvoyni/cog/bundles/scene/internal/types"

// Config is scene's configuration. PoseSampleRate, the global animation bake
// rate in Hz, is the only configurable number in the plugin: everything else
// scene decides follows from what a frame records. It arrives through
// kernel.New's config map under Name, and a zero field takes its default -
// 60 Hz - so a caller names only what it changes:
//
//	kernel.New(map[kernel.PluginName]any{scene.Name: scene.Config{PoseSampleRate: 30}})
//
// It is declared in internal/types, because the Lookup resource holds it, and
// aliased here.
type Config = types.Config
