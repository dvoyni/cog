// Package internal is the scene plugin: New, the registration of every
// Component scene's root aliases, and its Systems with their scratches.
// The load System, subscribed as scene.LoadOnUpdate, keys changed Entities
// into Batches and is the only one that loads. The recording System,
// subscribed as scene.RecordOnUpdate, buckets every drawable Entity into
// its Batch and draws the frame into gfx once a tick, over its own arena,
// culling, sorting and emission. The debug shapes' ten Systems,
// the first subscribed as scene.DebugOnUpdate, bake each shape into a Mesh
// before the load System runs. Composition roots and tests reach New through
// sceneplugin; everything else reaches the binding through its root.
//
// The plugin requires no Adapter. It contributes scene.StorageReadMount,
// which mounts shaderFS - the debug shapes' shader - in storage.
package internal
