// Package internal is the wgpu plugin: New, the kernel.PluginHost that owns the
// gogpu main loop, the fixed-step accumulator and tick source, the input bridge,
// the gfx.Backend it provides, and its mcp provider. Composition roots and tests
// reach New through wgpuplugin.
//
// gogpu's OnUpdate becomes ordered fixed-timestep app.UpdateEvent values through
// an accumulator, and OnDraw publishes app.RenderEvent{Alpha} as a render-thread
// barrier. The plugin also provides gfx's Backend Adapter, forwards OS input into
// the input plugin, and reports window and framebuffer sizes to the viewport.
package internal
