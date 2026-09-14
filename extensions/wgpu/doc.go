// Package wgpu declares cog's window, input, frame timing and WebGPU driver,
// built on gogpu: the one kernel.PluginHost. It owns the OS main loop and fills
// app's MainLoop Port with it, driving the Loop app attaches: the real frame time
// app turns into fixed-timestep app.UpdateEvents, and each drawn frame's
// app.RenderEvent.
//
// wgpu is an Extension. Its root declares only Name, Config, its two Adapters,
// AppMainLoop and GfxBackend, and its errors; the plugin, built by
// wgpuplugin.New, is in its internal/. Its Config is supplied under Name, and
// its zero value is the default.
//
//	config := map[kernel.PluginName]any{
//	    wgpu.Name: wgpu.Config{}.WithTitle("My App"),
//	}
//	plugins := []kernel.Plugin{
//	    storageplugin.New(),
//	    inputplugin.New(),
//	    appplugin.New(),
//	    gfxplugin.New(),
//	    wgpuplugin.New(),
//	    …
//	}
package wgpu
