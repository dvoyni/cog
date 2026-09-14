// Package diskstorage declares the desktop Extension of storage: a plugin that
// provides storage.PermanentFS as a directory confined under the user's data
// directory, named by the application id.
//
// diskstorage is an Extension. Its root declares only Name, Config, its one
// Adapter, StoragePermanentFS, and its errors; the plugin, built by
// diskstorageplugin.New, is in its internal/. The plugin is built only for
// desktop platforms (!js); a browser composes jsstorage instead. Its Config is
// supplied under Name.
//
//	config := map[kernel.PluginName]any{
//	    storage.Name:     storage.Config{},
//	    diskstorage.Name: diskstorage.Config{AppId: "my-app"},
//	}
//	plugins := []kernel.Plugin{
//	    storageplugin.New(),
//	    diskstorageplugin.New(),
//	    …
//	}
package diskstorage
