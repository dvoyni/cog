// Package mcp is gfx's capture and frame-snapshot machinery and what it offers
// an agent through the mcp broker: the capture and snapshot slots, the
// ArmCaptureCmd and ArmFrameCmd that arm them, and the gfx_capture and
// gfx_frame tools. It watches gfx as an internal.FrameObserver, which
// gfxplugin hands to internal.New; it depends on internal, and internal never
// imports it.
package mcp
