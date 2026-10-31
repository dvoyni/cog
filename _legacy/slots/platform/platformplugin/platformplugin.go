// Package platformplugin constructs the platform plugin. Only composition roots
// and tests import it; everything else reaches platform through its root.
package platformplugin

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/platform/internal"
)

// New creates the platform plugin. Its platform.Config arrives through
// kernel.New's config map under platform.Name, and it requires exactly one
// Adapter for platform.BackendPort.
func New() kernel.Plugin { return internal.New() }
