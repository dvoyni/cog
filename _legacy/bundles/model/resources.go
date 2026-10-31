package model

import "github.com/dvoyni/cog/bundles/model/internal"

// Lookup is model's persistent resource, registered by the model plugin and
// read by every renderer. It holds everything that outlives a frame - loaded
// models, baked pose and morph buffers, the path-keyed texture cache,
// buffer-built meshes and the unit meshes - plus the deferred bakes and buffer
// releases a renderer's flush applies at the frame boundary.
//
// It never retains a filesystem or GPU handle of its own. It has two facades.
// Under a read lock, LookupReadAccess answers only for resident models and
// never loads. Under the write lock, the load facade is LookupAccess,
// LookupDeviceAccess and, for the renderer that holds it, the Lookup's own
// methods: they load, preload, unload and drive the bake and release queues.
type Lookup = internal.Lookup
