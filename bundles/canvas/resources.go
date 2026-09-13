package canvas

import (
	"github.com/dvoyni/cog/bundles/canvas/internal"
	"github.com/dvoyni/cog/extensions/storage"
	"github.com/dvoyni/cog/kernel"
)

// OpQueue is the frame-local writable resource used to record layered Canvas
// operations. The Canvas plugin consumes and resets it at the end of each tick,
// in FlushOnUpdate. It is a concrete type, and consuming it is canvasimpl's
// alone.
type OpQueue = internal.OpQueue

// Lookup is the single Canvas-owned resource that holds the sprite atlas, glyph
// atlas, font store, and the cached sprite metadata behind sizing and text
// measurement. Callers acquire it as a write dependency and operate on it
// through a scoped LookupAccess; the resource itself never retains filesystem or
// GPU handles. Deferred unloads are applied by the Canvas flush at the frame
// boundary.
type Lookup = internal.Lookup

// LookupAccess is a handler-scoped facade over a Lookup. It carries the kernel
// (for error reporting) and the read filesystem needed to lazily load sprites
// and fonts, without ever retaining them past the handler's lock scope. Acquire
// a *Lookup write dependency plus storage.FileSystem in a handler, build a
// LookupAccess with NewLookupAccess, and pass it to consumers for the duration
// of that handler.
type LookupAccess = internal.LookupAccess

// FontMetrics reports a font's vertical metrics at a given size, in logical
// pixels, for baseline placement and inline-icon alignment.
type FontMetrics = internal.FontMetrics

// NewLookup builds an empty Lookup resource with canvas's default atlas sizes.
// The Canvas plugin creates its own from its configuration; this constructor
// lets tests and embedders build a Lookup to drive a LookupAccess directly.
func NewLookup() *Lookup { return internal.NewLookup(internal.DefaultConfig()) }

// NewLookupAccess builds a scoped facade. Call it inside a handler that holds
// the *Lookup write lock and the storage.FileSystem read lock; never store the
// result.
func NewLookupAccess(k kernel.Kernel, lookup *Lookup, filesystem storage.FileSystem) LookupAccess {
	return internal.NewLookupAccess(k, lookup, filesystem)
}
