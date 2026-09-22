package scene

import "github.com/dvoyni/cog/bundles/model"

// Config is model's configuration, aliased here until the sweep rewrites its
// callers. scene takes no configuration of its own: the pose sample rate is a
// fact of the models the Lookup bakes, so it arrives under model.Name, and a
// Config handed to scene under scene.Name is ignored.
//
//	kernel.New(map[kernel.PluginName]any{model.Name: model.Config{PoseSampleRate: 30}})
type Config = model.Config
