package canvas

import "github.com/dvoyni/cog/bundles/canvas/internal/types"

// OpQueue is the frame-local writable resource used to record layered Canvas
// operations. The Canvas plugin consumes and resets it at the end of each tick,
// in FlushOnUpdate. It is a concrete type, and consuming it is the plugin's
// alone.
type OpQueue = types.OpQueue

// Lookup is the single Canvas-owned resource that holds the sprite atlas, glyph
// atlas, font store, and the cached sprite metadata behind sizing and text
// measurement. Callers acquire it as a write dependency and operate on it
// through a scoped LookupAccess; the resource itself never retains filesystem or
// GPU handles. Deferred unloads are applied by the Canvas flush at the frame
// boundary.
type Lookup = types.Lookup
