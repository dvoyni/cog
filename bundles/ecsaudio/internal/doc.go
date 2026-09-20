// Package internal is the ecsaudio plugin: New, the registration of the three
// Components ecsaudio's root declares, the plugin-owned Entity-to-Voice table,
// and the one System, subscribed as ecsaudio.RecordOnUpdate, that reconciles
// every Emitter against the Voice it already has and writes the Listener, into
// sound's queue once a tick. Composition roots and tests reach New through
// ecsaudioplugin; everything else reaches the binding through its root.
//
// The plugin requires no Adapter and contributes none.
package internal
