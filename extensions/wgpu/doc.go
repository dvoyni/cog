// Package wgpu declares cog's window, input, timing and WebGPU driver, built on
// gogpu: the one kernel.PluginHost. It owns the OS main loop and drives the
// engine's fixed-timestep app.UpdateEvent and per-frame app.RenderEvent.
//
// wgpu is an Extension. Its root declares only Name, Config, its two Adapters,
// GfxBackend and McpProvider, and its errors; the plugin, built by
// wgpuplugin.New, is in its internal/. Its Config is supplied under Name, and
// its zero value is the default.
//
//	config := map[kernel.PluginName]any{
//	    wgpu.Name: wgpu.Config{}.WithTitle("My App"),
//	}
//	plugins := []kernel.Plugin{
//	    storageplugin.New(),
//	    inputplugin.New(),
//	    gfxplugin.New(),
//	    wgpuplugin.New(),
//	    …
//	}
package wgpu
