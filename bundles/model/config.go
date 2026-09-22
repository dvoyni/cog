package model

import "github.com/dvoyni/cog/bundles/model/internal/types"

// Config is model's configuration. PoseSampleRate, the global animation bake
// rate in Hz, is the only configurable number: every clip of every model is
// baked to pose rows at this rate at load. It arrives through kernel.New's
// config map under Name, and a zero field takes its default - 60 Hz - so a
// caller names only what it changes:
//
//	kernel.New(map[kernel.PluginName]any{model.Name: model.Config{PoseSampleRate: 30}})
//
// It is declared in internal/types, because the Lookup resource holds it, and
// aliased here.
type Config = types.Config
