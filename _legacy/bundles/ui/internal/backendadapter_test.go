package internal

import (
	"sync/atomic"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx"
)

// backendAdapter provides a test's Backend to gfx, the way a driver provides
// its own: gfx is a Slot, and a composition without one fails.
type backendAdapter struct{ backend gfx.Backend }

func (backendAdapter) Name() kernel.PluginName           { return "gfxbackendtest" }
func (backendAdapter) Dependencies() []kernel.PluginName { return nil }

func (a backendAdapter) Register(registrar *kernel.Registrar, _ any) error {
	registrar.ProvideAdapter[testGfxBackend](a.backend)
	return nil
}

// testGfxBackend is the Adapter this fixture fills gfx's backend Port as.
type testGfxBackend kernel.Adapter[gfx.BackendPort]

// detachedBackend is a Backend whose device never arrives. gfx asks a backend
// that is not ready only whether it is, and for ids, so nothing else is
// implemented: a test composed with it records, and renders nothing.
type detachedBackend struct {
	gfx.Backend
	next atomic.Uint32
}

func (*detachedBackend) Ready() bool                 { return false }
func (b *detachedBackend) NewTexture() gfx.TextureID { return gfx.TextureID(b.next.Add(1)) }
func (b *detachedBackend) NewBuffer() gfx.BufferID   { return gfx.BufferID(b.next.Add(1)) }
