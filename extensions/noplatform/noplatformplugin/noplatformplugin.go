// Package noplatformplugin constructs the noplatform plugin. Only composition
// roots and tests import it; everything else reaches noplatform through its
// root.
package noplatformplugin

import (
	"github.com/dvoyni/cog/extensions/noplatform/internal"
	"github.com/dvoyni/cog/kernel"
)

// New creates the noplatform plugin. It takes no Config, and it fills
// platform.BackendPort.
func New() kernel.Plugin { return internal.New() }
