package gfx

import "github.com/dvoyni/cog/slots/gfx/internal"

// Backend is the low-level realization interface: a vendor-neutral,
// "wgpu-shaped" API that a driver (e.g. gogpu) implements. The gfx plugin holds
// one Backend and never imports a GPU library. All methods are called on the
// driver's render thread. Create methods mint an opaque handle synchronously.
type Backend = internal.Backend

// BackendPort is the Port gfx requires exactly one Adapter for: the GPU backend
// a driver such as gogpu provides. A composition without one fails with
// kernel.ErrMissingAdapter.
type BackendPort = internal.BackendPort
