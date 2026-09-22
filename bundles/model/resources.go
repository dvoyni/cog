package model

import "github.com/dvoyni/cog/bundles/model/internal/types"

// Lookup is model's persistent resource, registered by the model plugin and
// read by every renderer. It holds everything that outlives a frame - loaded
// models, baked pose and morph buffers, the path-keyed texture cache,
// buffer-built meshes and the unit meshes - plus the deferred bakes and buffer
// releases a renderer's flush applies at the frame boundary.
//
// It never retains a filesystem or GPU handle of its own. Query and mutate it
// only through a scoped LookupAccess or LookupDeviceAccess, or, from the
// renderer that holds it for writing, through its own methods.
type Lookup = types.Lookup
