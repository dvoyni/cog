// Package storageplugin constructs the storage plugin. Only composition roots
// and tests import it; everything else reaches storage through its root.
package storageplugin

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage/internal"
)

// New creates the storage plugin. Its storage.Config arrives through
// kernel.New's config map under storage.Name, and it requires exactly one
// Adapter for storage.PermanentFSPort.
func New() kernel.Plugin { return internal.New() }
