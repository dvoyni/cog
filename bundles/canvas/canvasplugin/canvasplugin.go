// Package canvasplugin constructs the canvas plugin. Only composition roots and
// tests import it; everything else reaches canvas through its root.
package canvasplugin

import (
	"github.com/dvoyni/cog/bundles/canvas/internal"
	"github.com/dvoyni/cog/kernel"
)

// New creates the canvas plugin. Configure it with a canvas.Config under
// canvas.Name; it requires no Adapter.
func New() kernel.Plugin { return internal.New() }
