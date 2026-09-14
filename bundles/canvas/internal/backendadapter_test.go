package internal

import (
	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/dvoyni/cog/kernel"
)

// backendAdapter provides a test's Backend to gfx, the way a driver provides
// its own: gfx is a Port, and a composition without one fails.
type backendAdapter struct{ backend gpu.Backend }

func (backendAdapter) Name() kernel.PluginName           { return "gfxbackendtest" }
func (backendAdapter) Dependencies() []kernel.PluginName { return nil }

func (a backendAdapter) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[testGfxBackend](a.backend)
	return nil
}

// testGfxBackend is the Adapter this fixture fills gfx's backend Port as.
type testGfxBackend kernel.Adapter[gfx.BackendPort]
