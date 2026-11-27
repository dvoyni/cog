//go:build !js

package otosound

import "github.com/dvoyni/cog/plugins/sfx"

// backend is otosound's sfx.BackendSocket, the flush side of the seam, over
// the plugin, which holds the state it works on. sfx calls it from its Start
// and from the flush alone, one call at a time; what it shares with the
// device goroutine crosses through the plugin's atomics.
type backend struct {
	plugin *Plugin
}

// Voices opens the device and starts this kernel's player over the mixer,
// which plays silence, so it sizes nothing for the n slots. It takes the
// process's oto context, or creates it, synchronously, since that does not
// wait for the device; the player is started on the device goroutine, which
// waits for the context to be ready and then sets the Device ready, at the
// rate and buffer size in force. It never blocks or fails the game: a device
// that cannot be opened, or whose player cannot start, leaves the Device not
// ready.
func (b backend) Voices(n int) {
	settings, err := b.plugin.device.open(b.plugin.config.SampleRate, b.plugin.config.BufferSize)
	if err != nil {
		return
	}
	b.plugin.deviceGoroutine.Go(func() { b.startPlayer(settings) })
}

// Emit accepts one flush's changes and plays none of them.
func (b backend) Emit(changes *sfx.FlushChanges) {}

// Device answers the Device as it is now: one atomic load and a copy, cheap
// enough to poll every flush.
func (b backend) Device() sfx.Device { return *b.plugin.status.Load() }

// Prepare answers not done and prepares the clip on a goroutine of its own,
// off both the flush and the device thread: a clip whose decoded size is
// within Config.DecodedClipLimit is decoded there, once, and the encoded
// bytes, otosound's from the call on, are let go behind it. Its completion,
// the prepared clip or the error, comes back under token through
// TakePrepared. A clip over the limit streams, which is not built yet, so its
// completion is errStreamingNotBuilt; the prepare's other errors are
// ErrNotOggVorbis, ErrNoStreamFormat and ErrNoStreamLength.
func (b backend) Prepare(token any, encoded []byte) (sfx.PreparedClip, bool, error) {
	decodedClipLimit := b.plugin.config.DecodedClipLimit
	b.plugin.prepareGoroutines.Go(func() {
		completion := sfx.Prepared{Token: token}
		// A failed prepare hands sfx a nil clip, never a typed nil, which sfx
		// would install rather than report.
		if clip, err := prepareClip(encoded, decodedClipLimit); err != nil {
			completion.Err = err
		} else {
			completion.Clip = clip
		}
		b.plugin.finishedPreparesMutex.Lock()
		defer b.plugin.finishedPreparesMutex.Unlock()
		b.plugin.finishedPrepares = append(b.plugin.finishedPrepares, completion)
	})
	return nil, false, nil
}

// TakePrepared answers the prepares that finished since the last call, and
// clears them.
func (b backend) TakePrepared() []sfx.Prepared {
	b.plugin.finishedPreparesMutex.Lock()
	defer b.plugin.finishedPreparesMutex.Unlock()
	taken := b.plugin.finishedPrepares
	b.plugin.finishedPrepares = nil
	return taken
}

// Install mints the clip's ID, non-zero and never repeating, and keeps the
// clip in the clip table under it. A clip otosound's own Prepare did not make
// is refused with errNotOurClip.
func (b backend) Install(prepared sfx.PreparedClip) (sfx.ClipID, error) {
	clip, ours := prepared.(*preparedClip)
	if !ours || clip == nil {
		return 0, errNotOurClip
	}
	b.plugin.lastClipID++
	b.plugin.clips[b.plugin.lastClipID] = clip
	return b.plugin.lastClipID, nil
}

// startPlayer starts this kernel's player over the mixer and, once it plays,
// sets the Device ready at settings. It runs on the device goroutine, since
// the player waits for the context to be ready.
func (b backend) startPlayer(settings deviceSettings) {
	if err := b.plugin.device.play(&b.plugin.mixer); err != nil {
		return
	}
	b.plugin.status.Store(&sfx.Device{
		Ready:      true,
		Name:       string(PluginName),
		SampleRate: settings.sampleRate,
		Channels:   outputChannels,
		Latency:    settings.bufferSize,
	})
}
