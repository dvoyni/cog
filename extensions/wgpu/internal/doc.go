// Package internal is the wgpu plugin: New, the kernel.PluginHost that owns the
// gogpu main loop, the app.MainLoop and gfx.Backend it provides, and the input
// bridge. Composition roots and tests reach New through wgpuplugin.
//
// gogpu's OnUpdate hands the real frame time to the app.Loop's Frame, which
// turns it into fixed-timestep app.UpdateEvents, and OnDraw has the Loop publish
// app.RenderEvent as a render-thread barrier. The plugin also forwards OS input
// into the input plugin, and reports window and framebuffer sizes to app and the
// viewport.
package internal
