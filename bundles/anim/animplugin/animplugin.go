// Package animplugin constructs the anim plugin. Only composition roots and
// tests import it; everything else reaches anim through its root.
package animplugin

import (
	"github.com/dvoyni/cog/bundles/anim/internal"
	"github.com/dvoyni/cog/kernel"
)

// New creates the anim plugin. It takes no configuration and requires no
// Adapter.
func New() kernel.Plugin { return internal.New() }
