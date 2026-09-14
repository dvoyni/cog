package gfx

import (
	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/dvoyni/cog/kernel"
)

// BackendPort is the Port gfx requires exactly one Adapter for: the GPU backend
// a driver such as wgpu provides. A composition without one fails with
// kernel.ErrMissingAdapter.
type BackendPort kernel.RequiredPort[gpu.Backend]
