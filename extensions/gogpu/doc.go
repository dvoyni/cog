// Package gogpu declares cog's window, input, frame timing and WebGPU driver,
// built on, and named for, the gogpu library (github.com/gogpu/gogpu): the one
// kernel.PluginHost. It owns the OS main loop and fills app's MainLoop Port
// with it, driving the Loop app attaches: the real frame time app turns into
// fixed-timestep app.UpdateEvents, and each drawn frame's app.RenderEvent.
//
// gogpu is an Extension. Its root offers only Name, Config, its two Adapters,
// AppMainLoop and GfxBackend, and its errors, each an alias of what its
// internal/ declares; the plugin, built by gogpuplugin.New, is in its internal/
// too. Its Config is supplied under Name, and
// its zero value is the default.
//
//	config := map[kernel.PluginName]any{
//	    gogpu.Name: gogpu.Config{}.WithTitle("My App"),
//	}
//	plugins := []kernel.Plugin{
//	    storageplugin.New(),
//	    inputplugin.New(),
//	    appplugin.New(),
//	    gfxplugin.New(),
//	    gogpuplugin.New(),
//	    …
//	}
package gogpu
