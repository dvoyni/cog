package diskfs

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage"
)

// StoragePermanentFS is the Adapter through which diskfs fills storage's
// permanent filesystem Port.
type StoragePermanentFS kernel.Adapter[storage.PermanentFSPort]
