package canvas

import "github.com/dvoyni/cog/bundles/canvas/internal/types"

// OpQueue is the frame-local writable resource used to record layered Canvas
// operations. The Canvas plugin consumes and resets it at the end of each tick,
// in FlushOnUpdate. It is a concrete type, and consuming it is the plugin's
// alone.
type OpQueue = types.OpQueue

// Lookup is the single Canvas-owned resource that holds canvas's asset caches -
// the sprite atlas, the standalone textures tiled sprites sample, and the sprite
// headers sizing and text measurement answer from - with the two atlas packers
// behind them and the font store. Callers acquire it as a write dependency and
// operate on it through a scoped LookupAccess for the measuring verbs or a
// LookupDeviceAccess for the unloading ones; the resource itself never retains
// filesystem or GPU handles. An unload frees at the call, and a later draw
// reloads what it freed.
type Lookup = types.Lookup
