// Package jsfs declares the browser Extension of storage: a plugin that
// provides storage.PermanentFS backed by the page's localStorage, under the key
// cog.storage.<AppId>.
//
// jsfs is an Extension. Its root declares only Name, Config, its one Adapter,
// StoragePermanentFS, and its errors; the plugin, built by jsfsplugin.New, is in
// its internal/. The plugin is built only for GOOS=js; a desktop composes diskfs
// instead. Its Config is supplied under Name, and must name the application.
//
//	config := map[kernel.PluginName]any{
//	    storage.Name: storage.Config{},
//	    jsfs.Name:    jsfs.Config{AppId: "my-app"},
//	}
//	plugins := []kernel.Plugin{
//	    storageplugin.New(),
//	    jsfsplugin.New(),
//	    …
//	}
package jsfs
