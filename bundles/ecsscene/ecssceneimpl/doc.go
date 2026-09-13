// Package ecssceneimpl is the ecsscene plugin: New, the registration of every
// Component ecsscene's contract root declares, the recording scratch, and the
// one System, subscribed as ecsscene.RecordOnUpdate, that copies every matching
// Entity into scene's op queue once a tick. Only composition roots and tests
// import it; everything else reaches the binding through its contract root.
//
// The plugin requires no Adapter and contributes none.
package ecssceneimpl
