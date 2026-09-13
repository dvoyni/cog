package ecssceneimpl

import (
	"sync/atomic"

	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/dvoyni/cog/kernel"
)

// backendAdapter provides a test's Backend to gfx, the way a driver provides
// its own: gfx is a Port, and a composition without one fails.
type backendAdapter struct{ backend gpu.Backend }

func (backendAdapter) Name() kernel.PluginName           { return "gfxbackendtest" }
func (backendAdapter) Dependencies() []kernel.PluginName { return nil }

func (a backendAdapter) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[gpu.Backend](a.backend)
	return nil
}

// detachedBackend is a Backend whose device never arrives. gfx asks a backend
// that is not ready only whether it is, and for ids, so nothing else is
// implemented: a test composed with it records, and renders nothing.
type detachedBackend struct {
	gpu.Backend
	next atomic.Uint32
}

func (*detachedBackend) Ready() bool                 { return false }
func (b *detachedBackend) NewTexture() gpu.TextureID { return gpu.TextureID(b.next.Add(1)) }
func (b *detachedBackend) NewBuffer() gpu.BufferID   { return gpu.BufferID(b.next.Add(1)) }
