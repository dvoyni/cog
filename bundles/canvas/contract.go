// Package canvas records layered 2D sprites, text, primitives and custom
// triangles into a frame-local queue, and measures sprites and text through a
// persistent lookup.
//
// canvas is a Bundle. This package is its contract root and declares no plugin:
// the OpQueue and Lookup resources, the recording vocabulary, the material
// sets and the halo, the draw-snapshot command and its views, and the ordering
// identity FlushOnUpdate. The plugin - the flush that turns a recording into
// gfx draws, the batchers, the snapshot slot, the built-in shader mount and the
// mcp Provider - is in canvasimpl, which only composition roots and tests
// import. The code the two share, including the consume side of the queue and
// the atlases behind Lookup, is in internal.
//
// OpQueue and Lookup are concrete types, aliased from internal, so recording a
// sprite is a direct method call with nothing between the caller and the queue.
package canvas

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// Name is the canvas plugin's name.
const Name kernel.PluginName = "canvas"

// FlushOnUpdate is the subscription type of the plugin's flush on
// app.UpdateEvent. It is registered Last and ordered
// Before[gfx.PresentOnUpdate]: it translates the tick's recorded operations
// into gfx draws and resets the queue, so everything that records into
// *OpQueue during the tick orders before it.
type FlushOnUpdate kernel.Subscription[app.UpdateEvent]
