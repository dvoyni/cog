package ecs

import "github.com/dvoyni/cog/kernel"

// Read and Write are the System parameters that name a kernel resource, and
// together they are the whole of the binding mechanism cog#246 went looking
// for. A System that draws, plays a sound or steps physics reaches that plugin
// through the frame-local resource the plugin already publishes -- scene's
// *scene.OpQueue, gfx's *gfx.OpQueue -- and the ECS contributes nothing else:
// no binding type, no adapter, no registration call of its own.
//
// The lock they declare is the kernel's own, taken at registration like every
// other, so it joins the System's lock set beside its Query's Stores and is
// visible in the signature exactly as a Component is. Two Systems both writing
// one resource therefore serialise against each other whatever their Queries
// touch, which is a property of the bound plugin's API rather than of the ECS:
// scene publishes one OpQueue, so scene recording is one lock wide.
//
// Neither is a place to keep anything. The value is refreshed per tick and is
// valid only for the body of the System, under the kernel's standing rule that
// a value read from a handle lives only as long as the handler holds its lock.
type Read[T any] struct {
	h kernel.Read[T]
	v T
}

func (r *Read[T]) plan(a kernel.ResourceAccess, _ *Entities) { r.h = a.GetRead[T]() }
func (r *Read[T]) refresh()                                  { r.v = r.h.Get() }

// Get returns the resource for the body of this System call.
func (r *Read[T]) Get() T { return r.v }

// Write is Read's writing form, and the one a recording System takes: a draw is
// appended to the queue, so recording is a write however read-only the
// gameplay behind it was.
type Write[T any] struct {
	h kernel.Write[T]
	v T
}

func (w *Write[T]) plan(a kernel.ResourceAccess, _ *Entities) { w.h = a.GetWrite[T]() }
func (w *Write[T]) refresh()                                  { w.v = w.h.Get() }

// Get returns the resource for the body of this System call.
func (w *Write[T]) Get() T { return w.v }
