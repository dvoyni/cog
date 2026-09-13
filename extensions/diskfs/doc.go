// Package diskfs is the desktop Adapter of storage: a plugin that provides
// storage.PermanentFS as a directory confined under the user's data directory,
// named by the application id.
//
// It is built only for desktop platforms (!js); a browser composes jsfs
// instead. Only composition roots and tests import it.
//
//	plugins := []kernel.Plugin{
//	    storageimpl.New(),
//	    diskfs.New(diskfs.Config{AppId: "my-app"}),
//	    …
//	}
package diskfs
