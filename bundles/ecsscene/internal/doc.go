// Package internal is the ecsscene plugin: New, the registration of every
// Component ecsscene's root declares, the recording scratch, and the one
// System, subscribed as ecsscene.RecordOnUpdate, that copies every matching
// Entity into scene's op queue once a tick. Composition roots and tests reach
// New through ecssceneplugin; everything else reaches the binding through its
// root.
//
// The plugin requires no Adapter and contributes none.
package internal
