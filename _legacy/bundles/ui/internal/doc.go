// Package internal is the ui plugin: New, the processing behind
// ui.ProcessOnUpdate that lays out the tick's ui.Frame, resolves the pointer
// into ui.Interactions and records into canvas, the private layout resource it
// keeps across ticks, the layout-snapshot slot behind ui.ArmLayoutCmd, and the
// mcp Provider offering ui_layout. Composition roots and tests reach New
// through uiplugin; everything else reaches ui through its root.
//
// ui has no configuration, so there is no Config. The plugin requires no
// Adapter. It contributes one mcp.Provider.
package internal
