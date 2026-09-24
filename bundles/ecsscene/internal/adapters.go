package internal

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage"
)

// StorageReadMount is the Adapter through which ecsscene contributes its own
// shaders' sources - the debug shapes' - to storage as a read mount.
type StorageReadMount kernel.Adapter[storage.ReadMountPort]
