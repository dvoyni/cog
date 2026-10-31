// Package ecsphysics2dplugin constructs the physics plugin. Only composition
// roots and tests import it; everything else reaches ecsphysics2d through its
// root.
package ecsphysics2dplugin

import (
	"github.com/dvoyni/cog/bundles/ecsphysics2d/internal"
	"github.com/dvoyni/cog/kernel"
)

// New creates the ecsphysics2d plugin, which registers the Components a Body is
// made of and chains Integrate, Index, Detect and Solve on app.UpdateEvent. It
// takes its settings from ecsphysics2d.Config and requires no Adapter.
func New() kernel.Plugin { return internal.New() }
