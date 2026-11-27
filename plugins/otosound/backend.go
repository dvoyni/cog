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

// Prepare answers not done, and no prepare ever finishes, so a voice on the
// clip waits for it.
func (b backend) Prepare(token any, encoded []byte) (sfx.PreparedClip, bool, error) {
	return nil, false, nil
}

// TakePrepared answers nothing, since no prepare finishes.
func (b backend) TakePrepared() []sfx.Prepared { return nil }

// Install refuses every clip: otosound installs only what its own Prepare
// made, and sfx installs nothing else.
func (b backend) Install(clip sfx.PreparedClip) (sfx.ClipID, error) {
	return 0, errNotOurClip
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
