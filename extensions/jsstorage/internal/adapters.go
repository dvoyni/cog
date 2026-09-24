package internal

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage"
)

// StoragePermanentFS is the Adapter through which jsstorage fills storage's
// permanent filesystem Port.
type StoragePermanentFS kernel.Adapter[storage.PermanentFSPort]
