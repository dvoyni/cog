package gfx

import (
	"io/fs"

	"github.com/dvoyni/cog/extensions/storage"
	"github.com/dvoyni/cog/kernel"
)

// PluginEdges is the part of the gfx plugin that still names storage and mcp.
// The plugin lives in gfximpl, and storage and mcp are not Ports yet, so gfximpl
// may not import them; the contract root may, and gfximpl's plugin embeds this
// to reach them. It is not for any other use, and it goes away as storage and
// mcp become Ports (dvoyni/cog#334, dvoyni/cog#335), when what it carries moves
// into gfximpl.
type PluginEdges struct{}

// Dependencies reports the plugins gfx requires: storage, from which it loads
// shader and texture resources.
func (PluginEdges) Dependencies() []kernel.PluginName { return []kernel.PluginName{storage.Name} }

// ReadFiles declares a read lock on storage's FileSystem inside a handler's
// lock, and returns what reads it. The read happens only when called, so a
// frame that loads nothing boxes nothing.
func (PluginEdges) ReadFiles(access kernel.ResourceAccess) func() fs.FS {
	filesystem := access.GetRead[storage.FileSystem]()
	return func() fs.FS { return filesystem.Get() }
}
