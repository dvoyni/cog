// Package recording is the recording adapter: a gfx backend that records
// every call gfx makes instead of drawing, for tests to assert on. A test's
// own plugin binds it with configurator.BindSocket[gfx.InstanceSocket].
//
// It checks a fixed subset of what wgpu rejects (a handle missing at its
// index, usage flags, write and copy alignment, pass state) and answers what
// a real backend would. Buffers hold bytes, which writes, buffer copies and
// clears apply to; textures hold no texels, and captures complete
// zero-filled. Real pixels without a GPU come from gogpu on wgpu's software
// adapter, not from here.
package recording

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/platform"
)

// Instance is the adapter's gfx.InstanceSocket. It records under a mutex,
// since gfx calls it from many goroutines.
type Instance struct{}

// NewInstance returns an adapter with an empty recording.
func NewInstance() *Instance { return &Instance{} }

// Recording answers what was recorded so far; a test reads it once the
// kernel has settled.
func (instance *Instance) Recording() *Recording { panic("gfx listing: no bodies") }

// AnswerNext makes the next call of kind answer answer instead of what it
// would: an AcquireStatus for an acquire, a failure for a map, an error,
// gfx.ErrDeviceLost included, for any call.
func (instance *Instance) AnswerNext(kind CallKind, answer Answer) { panic("gfx listing: no bodies") }

// RequestAdapter answers the recording adapter.
func (instance *Instance) RequestAdapter(options gfx.RequestAdapterOptions) (gfx.BackendAdapter, error) {
	panic("gfx listing: no bodies")
}

// CreateSurface records window's surface.
func (instance *Instance) CreateSurface(window platform.Window, native platform.NativeHandle) error {
	panic("gfx listing: no bodies")
}

// ReleaseSurface records window's surface released.
func (instance *Instance) ReleaseSurface(window platform.Window) { panic("gfx listing: no bodies") }

// Release records the instance released.
func (instance *Instance) Release() { panic("gfx listing: no bodies") }
