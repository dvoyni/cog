package jsfs

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage"
)

// StoragePermanentFS is the Adapter through which jsfs fills storage's
// permanent filesystem Port.
type StoragePermanentFS kernel.Adapter[storage.PermanentFSPort]
