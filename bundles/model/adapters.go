package model

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/storage"
)

// StorageReadMount is the Adapter through which model contributes the bundled
// PBR shader's sources to storage as a read mount.
type StorageReadMount kernel.Adapter[storage.ReadMountPort]
