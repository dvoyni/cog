// Package uiplugin constructs the ui plugin. Only composition roots and tests
// import it; everything else reaches ui through its root.
package uiplugin

import (
	"github.com/dvoyni/cog/bundles/ui/internal"
	"github.com/dvoyni/cog/kernel"
)

// New creates the ui plugin. ui has no configuration; it requires no Adapter.
func New() kernel.Plugin { return internal.New() }
