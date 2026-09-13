package scene

import (
	"github.com/dvoyni/cog/bundles/scene/internal"
	"github.com/dvoyni/cog/kernel"
)

// OpQueue is the frame-local writable resource gameplay records cameras, models,
// meshes, lights and debug shapes into. The scene plugin consumes and
// republishes it at the end of each update tick, in FlushOnUpdate.
//
// It double-buffers: the flush publishes the frame it just consumed and starts
// recording into the buffer the previous publication vacated, so Ops and Passes
// describe the frame that was actually flushed and stay valid until the next
// one. Nothing is copied to achieve that - the two halves swap.
//
// It is a concrete type, so recording a draw is a direct method call with
// nothing between the caller and the queue; consuming it is sceneimpl's alone.
type OpQueue = internal.OpQueue

// Lookup is the single scene-owned persistent resource. It holds everything
// that outlives a frame — resident models, baked pose and morph buffers, the
// path-keyed texture cache, buffer-built meshes and scene's own unit meshes —
// plus the deferred unloads and bakes the flush applies at the frame boundary.
//
// It never retains a filesystem or GPU handle of its own. Query and mutate it
// only through a scoped LookupAccess.
type Lookup = internal.Lookup

// NewLookup builds an empty Lookup at scene's default configuration. The plugin
// creates its own from its configuration; this constructor lets tests and
// embedders build one to drive a LookupAccess directly.
func NewLookup() *Lookup { return internal.NewLookup(internal.DefaultConfig()) }

// LookupAccess is the scoped facade every query and mutation of a Lookup goes
// through. Acquire a *Lookup write dependency in a handler, build one with
// NewLookupAccess, and pass it to consumers for the duration of that handler.
// Never store the result: the handles behind it are valid only while the
// handler holds its lock.
type LookupAccess = internal.LookupAccess

// NewLookupAccess builds a scoped facade. Call it inside a handler that holds
// the *Lookup write lock.
//
// Two dependencies, not three: it takes no storage.FileSystem, unlike canvas's
// equivalent, because scene's load command opens, parses and bakes the file
// itself holding no locks. A consumer system therefore declares one fewer
// resource than canvas's, which reads as an oversight unless it is said out
// loud.
func NewLookupAccess(k kernel.Kernel, lookup *Lookup) LookupAccess {
	return internal.NewLookupAccess(k, lookup)
}
