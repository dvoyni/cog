// Package internal is the canvas plugin: New, the resolution of canvas.Config,
// the flush that turns a tick's recorded canvas.OpQueue into gfx draws through
// the sprite and triangle batchers, the draw-snapshot slot behind
// canvas.ArmDrawsCmd, the Start mount of the built-in shaders and default font,
// and the mcp Provider offering canvas_draws. Composition roots and tests reach
// New through canvasplugin; everything else reaches canvas through its root.
//
// The plugin requires no Adapter. It contributes one mcp.Provider.
package internal
