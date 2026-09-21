// Package ecsplugin constructs the ecs plugin. Only composition roots and tests
// import it; everything else reaches ecs through its root.
package ecsplugin

import (
	"github.com/dvoyni/cog/bundles/ecs/internal"
	"github.com/dvoyni/cog/kernel"
)

// New creates the ecs plugin, which publishes the id authority, *ecs.Entities,
// sized by ecs.Config, registers ecs.ShrinkCmd, and registers the m.Transform
// Store every binding reads an Entity's placement from. It requires no Adapter.
func New() kernel.Plugin { return internal.New() }
