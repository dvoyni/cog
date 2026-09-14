// Package internal is the gfx plugin: New, the handlers behind gfx's commands
// and subscriptions, the translator from recorded queues to a gfx.Queue, and
// the capture and frame-snapshot slots. Composition roots and tests reach New
// through gfxplugin; everything else reaches gfx through its root.
//
// The plugin requires exactly one gfx.Backend Adapter. It reads the Adapter
// from Start onwards, and a frame rendered before the Backend is Ready is
// skipped. It contributes one mcp.Provider, offering gfx_capture and gfx_frame.
package internal
