package scene

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage"
)

// StorageReadMount is the Adapter through which scene contributes its bundled
// shaders to storage as a read mount.
type StorageReadMount kernel.Adapter[storage.ReadMountPort]
