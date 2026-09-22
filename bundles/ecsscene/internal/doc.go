// Package internal is the ecsscene plugin: New, the registration of every
// Component ecsscene's root declares, and its two Systems with their
// scratches. The load System, subscribed as ecsscene.LoadOnUpdate, keys
// changed Entities into Batches and is the only one that loads. The recording
// System, subscribed as ecsscene.RecordOnUpdate, buckets every drawable Entity
// into its Batch and draws the frame into gfx once a tick, over its own copy of
// scene's arena, culling, sorting and emission. Composition roots and tests reach
// New through ecssceneplugin; everything else reaches the binding through its
// root.
//
// The plugin requires no Adapter and contributes none.
package internal
