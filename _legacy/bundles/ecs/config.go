package ecs

import "github.com/dvoyni/cog/bundles/ecs/internal"

// Config is the ecs plugin's configuration, keyed by Name in the engine's
// configuration map. A zero field takes its default, so a caller names only
// what it changes:
//
//	kernel.New(map[kernel.PluginName]any{ecs.Name: ecs.Config{PrewarmEntities: 50_000}})
type Config = internal.Config
