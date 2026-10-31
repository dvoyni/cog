// Package jsstorage declares the browser Extension of storage: a plugin that
// provides storage.PermanentFS backed by the page's localStorage, under the key
// cog.storage.<AppId>.
//
// jsstorage is an Extension. Its root offers only Name, Config, its one
// Adapter, StoragePermanentFS, and its errors, each an alias of what its
// internal/ declares; the plugin, built by jsstorageplugin.New, is in its
// internal/ too. The plugin is built only for
// GOOS=js; a desktop composes diskstorage instead. Its Config is supplied under
// Name, and must name the application.
//
//	config := map[kernel.PluginName]any{
//	    storage.Name:   storage.Config{},
//	    jsstorage.Name: jsstorage.Config{AppId: "my-app"},
//	}
//	plugins := []kernel.Plugin{
//	    storageplugin.New(),
//	    jsstorageplugin.New(),
//	    …
//	}
package jsstorage
