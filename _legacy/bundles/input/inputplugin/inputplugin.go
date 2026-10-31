// Package inputplugin constructs the input plugin. Only composition roots and
// tests import it; everything else reaches input through its root.
package inputplugin

import (
	"github.com/dvoyni/cog/bundles/input/internal"
	"github.com/dvoyni/cog/kernel"
)

// New creates the input plugin. It takes no configuration and requires no
// Adapter.
func New() kernel.Plugin { return internal.New() }
