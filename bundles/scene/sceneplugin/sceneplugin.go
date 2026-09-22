// Package sceneplugin constructs the scene plugin. Only composition roots and
// tests import it; everything else reaches scene through its root.
package sceneplugin

import (
	"github.com/dvoyni/cog/bundles/scene/internal"
	"github.com/dvoyni/cog/kernel"
)

// New creates the scene plugin. It takes no configuration and requires no
// Adapter; register the model plugin, whose Lookup it draws from, before it.
func New() kernel.Plugin { return internal.New() }
