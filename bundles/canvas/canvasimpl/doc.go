// Package canvasimpl is the canvas plugin: New, its Config, the flush that turns
// a tick's recorded canvas.OpQueue into gfx draws through the sprite and
// triangle batchers, the draw-snapshot slot behind canvas.ArmDrawsCmd, the Start
// mount of the built-in shaders and default font, and the mcp Provider offering
// canvas_draws. Only composition roots and tests import it; everything else
// reaches canvas through its contract root.
//
// The plugin requires no Adapter. It contributes one mcp.Provider.
package canvasimpl
