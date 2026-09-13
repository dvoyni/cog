// Package jsfs is the browser Adapter of storage: a plugin that provides
// storage.PermanentFS backed by the page's localStorage, under the key
// cog.storage.<AppId>.
//
// It is built only for GOOS=js; a desktop composes diskfs instead. Only
// composition roots and tests import it.
//
//	plugins := []kernel.Plugin{
//	    storageimpl.New(),
//	    jsfs.New(jsfs.Config{AppId: "my-app"}),
//	    …
//	}
package jsfs
