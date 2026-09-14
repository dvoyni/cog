// Package wgpuplugin constructs the wgpu plugin. Only composition roots and tests
// import it.
package wgpuplugin

import (
	"github.com/dvoyni/cog/extensions/wgpu/internal"
	"github.com/dvoyni/cog/kernel"
)

// New creates the wgpu plugin, the engine's kernel.PluginHost. Its wgpu.Config
// arrives through kernel.New's config map under wgpu.Name, and it provides app's
// MainLoop Adapter and gfx's Backend Adapter.
func New() kernel.Plugin { return internal.New() }
