// Package gfxplugin constructs the gfx plugin. Only composition roots and tests
// import it; everything else reaches gfx through its root.
package gfxplugin

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx/internal"
)

// New creates the gfx plugin. It requires exactly one Adapter for
// gfx.BackendPort, which a driver such as wgpu provides.
func New() kernel.Plugin { return internal.New() }
