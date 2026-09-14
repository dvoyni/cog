package jsfs

import (
	"github.com/dvoyni/cog/extensions/storage"
	"github.com/dvoyni/cog/kernel"
)

// StoragePermanentFS is the Adapter through which jsfs fills storage's
// permanent filesystem Port.
type StoragePermanentFS kernel.Adapter[storage.PermanentFSPort]
