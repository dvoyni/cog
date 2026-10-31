// Package nosoundplugin constructs the nosound plugin. Only composition roots
// and tests import it; everything else reaches nosound through its root.
package nosoundplugin

import (
	"github.com/dvoyni/cog/extensions/nosound/internal"
	"github.com/dvoyni/cog/kernel"
)

// New creates the nosound plugin. Its nosound.Config arrives through
// kernel.New's config map under nosound.Name, and it fills sound.BackendPort.
func New() kernel.Plugin { return internal.New() }
