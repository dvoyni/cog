// Package gfximpl is the gfx plugin: New, the handlers behind gfx's commands
// and subscriptions, the translator from recorded queues to a gpu.Queue, and the
// capture and frame-snapshot slots. Only composition roots and tests import it;
// everything else reaches gfx through its contract root.
//
// The plugin requires exactly one gpu.Backend Adapter. It reads the Adapter
// from Start onwards, and a frame rendered before the Backend is Ready is
// skipped. It contributes one mcp.Provider, offering gfx_capture and gfx_frame.
package gfximpl
