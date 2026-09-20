// Package internal is the sound plugin: New, the six resources, and the
// once-per-tick flush that turns a tick's recorded operations into one Emit.
// Composition roots and tests reach New through soundplugin; everything else
// reaches sound through its root.
//
// The plugin requires exactly one sound.Backend Adapter. A composition with
// none fails with kernel.ErrMissingAdapter.
package internal
