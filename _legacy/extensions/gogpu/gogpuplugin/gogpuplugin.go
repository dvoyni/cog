// Package gogpuplugin constructs the gogpu plugin. Only composition roots and
// tests import it.
package gogpuplugin

import (
	"github.com/dvoyni/cog/extensions/gogpu/internal"
	"github.com/dvoyni/cog/kernel"
)

// New creates the gogpu plugin, the engine's kernel.PluginHost. Its
// gogpu.Config arrives through kernel.New's config map under gogpu.Name, and it
// provides app's MainLoop Adapter and gfx's Backend Adapter.
func New() kernel.Plugin { return internal.New() }
