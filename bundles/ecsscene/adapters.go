package ecsscene

import "github.com/dvoyni/cog/bundles/ecsscene/internal"

// StorageReadMount is the Adapter through which ecsscene contributes its own
// shaders' sources - the debug shapes' - to storage as a read mount.
type StorageReadMount = internal.StorageReadMount
