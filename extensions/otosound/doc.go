// Package otosound declares the desktop Extension of sound: our own software
// Mixer over ebitengine/oto/v3, in pure Go, decoding Ogg Vorbis with
// jfreymuth/oggvorbis. It is the half of audio that touches a device thread,
// and everything that makes it one is invisible through the Port: the ring that
// carries a tick's batch across, the applied counter that lets the tick free
// what the Mixer has passed, the per-block declick ramp, and the residency of a
// decoded Clip.
//
// The device thread never stalls. It may touch its own voice table, the ring
// through atomics and the prepared Clip data it was given; it may not allocate,
// take a lock, free anything, call into sound or kernel, or decode. A callback
// that waited on the scheduler is audible as a click, and a buffer it failed to
// fill is audible as silence, so the expensive work - decoding a Clip and
// converting it to the device rate - runs on a goroutine spawned from Prepare
// and the Mixer only ever copies, multiplies and interpolates.
//
// No tick is lost. A batch is a delta rather than a frame, so the handoff is a
// ring of preallocated batches published with one atomic store and drained
// whole at a block boundary, and never gfx's latest-wins triple buffer: two
// ticks landing between two blocks would publish the second over the first, and
// a Play the first carried would never sound with nothing reporting it. A full
// ring - a device thread that has not run for eight ticks - merges into a
// staging batch the Mixer cannot be reading rather than blocking the game or
// dropping the starts the contract promised to keep.
//
// The plugin is built only for desktop platforms (!js). Go's wasm target is
// single-threaded, so the Mixer would compete with frame work, and oto requests
// each chunk by postMessage to the main thread and fills silence when the reply
// is late - a spike measured 1.880s of audio produced for a 4.000s window at a
// 20ms buffer on an idle page. A web build composing otosound therefore fails
// to compile, because otosoundplugin.New does not exist there, rather than
// quietly shipping a Mixer that starves; the browser gets jssound.
//
// otosound is an Extension. Its root declares only Name, Config, its one
// Adapter, SoundBackend, and its errors, and is untagged so the declarations
// build everywhere; the plugin, built by otosoundplugin.New, is in its
// internal/, and that is what carries the tag. Its Config is supplied under
// Name.
//
//	config := map[kernel.PluginName]any{
//	    sound.Name:    sound.Config{},
//	    otosound.Name: otosound.Config{},
//	}
//	plugins := []kernel.Plugin{
//	    soundplugin.New(),
//	    otosoundplugin.New(),
//	    …
//	}
package otosound
