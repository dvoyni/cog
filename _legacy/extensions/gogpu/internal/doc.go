// Package internal is the gogpu plugin: New, the kernel.PluginHost that owns
// the gogpu library's main loop, the app.MainLoop and gfx.Backend it provides,
// and the input bridge. Composition roots and tests reach New through
// gogpuplugin.
//
// In this package's code the identifier gogpu is the upstream library,
// github.com/gogpu/gogpu, imported under its own name, and the Extension's root
// is imported as cgogpu.
//
// gogpu's OnUpdate hands the real frame time to the app.Loop's Frame, which
// turns it into fixed-timestep app.UpdateEvents, and OnDraw has the Loop publish
// app.RenderEvent as a render-thread barrier. The plugin also forwards OS input
// into the input plugin, and reports window and framebuffer sizes to app and the
// viewport.
package internal
