// Package internal is the otosound plugin: New, the Backend it provides for
// sound's BackendPort, the ring that carries a tick's operations to the Mixer,
// the Mixer itself, and the decode that turns encoded Ogg into resident
// samples. Composition roots and tests reach New through otosoundplugin;
// everything else reaches otosound through its root.
//
// The package is cut along the thread boundary rather than along the Backend
// interface, because that boundary is the thing most likely to be eroded by one
// convenient call:
//
//   - handoff.go is the seam itself - the ordered operations, the preallocated
//     ring of batches, the staging batch a full ring merges into, and the two
//     monotonic counters that are the whole of the synchronisation.
//   - mixer.go is everything the device thread runs, and it may touch its own
//     voice table, the ring through atomics and the prepared Clip data it was
//     given, and nothing else.
//   - backend.go, clip.go and device.go are everything the tick and the
//     goroutines run: the clip table, the decode, and opening the device.
//
// The plugin is built only for desktop platforms (!js). It requires no Adapter
// and provides one, otosound.SoundBackend.
package internal
