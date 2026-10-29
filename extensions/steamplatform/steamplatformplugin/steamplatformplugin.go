//go:build windows || linux || darwin

// Package steamplatformplugin constructs the steamplatform plugin. Only
// composition roots and tests import it; everything else reaches steamplatform
// through its root.
package steamplatformplugin

import (
	"github.com/dvoyni/cog/extensions/steamplatform/internal"
	"github.com/dvoyni/cog/kernel"
)

// New creates the steamplatform plugin. Its steamplatform.Config arrives
// through kernel.New's config map under steamplatform.Name, and it fills
// platform.BackendPort.
func New() kernel.Plugin { return internal.New() }
