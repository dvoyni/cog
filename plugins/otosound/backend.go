//go:build !js

package otosound

import (
	"slices"

	"github.com/dvoyni/cog/plugins/sfx"
)

// backend is otosound's sfx.BackendSocket, the flush side of the seam, over
// the plugin, which holds the state it works on. sfx calls it from its Start
// and from the flush alone, one call at a time; what it shares with the
// device goroutine crosses through the plugin's atomics.
type backend struct {
	plugin *Plugin
}

// Voices sizes the handoff ring for n slots, an n of zero or less taken as
// one, and only then opens the device and starts this kernel's player over
// the mixer, which drains the ring and plays silence: the mixer must exist
// complete before anything can pull a block out of it. It takes the
// process's oto context, or creates it, synchronously, since that does not
// wait for the device, and so settles the rate every clip is converted to,
// the context's; the player is started on the device goroutine, which
// waits for the context to be ready and then sets the Device ready, at the
// rate and buffer size in force. It never blocks or fails the game: a device
// that cannot be opened, or whose player cannot start, leaves the Device not
// ready.
func (b backend) Voices(n int) {
	b.plugin.mixer.ring = newHandoffRing(b.plugin.config.RingDepth, max(n, 1))
	settings, err := b.plugin.device.open(b.plugin.config.SampleRate, b.plugin.config.BufferSize)
	if err != nil {
		return
	}
	b.plugin.outputSampleRate = settings.sampleRate
	b.plugin.deviceGoroutine.Go(func() { b.startPlayer(settings) })
}

// Emit copies one flush's changes, which are sfx's and valid only for the
// call, into the staging changes, in the order the seam states: the stops
// first, then the starts, each ClipID resolved to the clip it names while the
// clip table is still the flush side's own, then the updates, then the
// destroys, each moving its clip out of the table to be held until the mixer
// has passed it. Stops come before starts because a steal puts both in one
// flush's changes, and recorded the other way round the stop would fade out
// the voice that had just replaced it.
//
// It then publishes the staging changes onto the handoff ring, or, when the
// ring is full, leaves them for the next Emit to merge into, and lets go of
// every destroyed clip the mixer has passed. It never waits on the mixer. It
// is called only after Voices, and panics before.
func (b backend) Emit(changes *sfx.FlushChanges) {
	ring := b.plugin.mixer.ring
	if ring == nil {
		panic("otosound: Emit was called before Voices")
	}
	staging := ring.staging
	// Staging changes left over hold what a full ring did not take, so this
	// Emit merges into them: the one case in which an update coalesces.
	merging := len(staging.operations) > 0
	for _, slot := range changes.Stops {
		staging.record(operation{kind: operationKindStop, slot: slot}, merging)
	}
	for index := range changes.Starts {
		start := &changes.Starts[index]
		staging.record(operation{
			kind:   operationKindStart,
			slot:   start.Slot,
			clipID: start.Clip,
			clip:   b.plugin.clips[start.Clip],
			offset: start.Offset,
			loop:   start.Loop,
			params: start.Params,
		}, merging)
	}
	for _, update := range changes.Updates {
		staging.record(operation{kind: operationKindUpdate, slot: update.Slot, params: update.Params}, merging)
	}
	for _, clipID := range changes.Destroys {
		clip, installed := b.plugin.clips[clipID]
		if !installed {
			continue
		}
		delete(b.plugin.clips, clipID)
		staging.record(operation{kind: operationKindDestroy, clipID: clipID}, merging)
		// The staging changes go out as the sequence published counts now,
		// however many full-ring publishes they wait through first.
		b.plugin.releases = append(b.plugin.releases, clipRelease{clip: clip, sequence: ring.published.Load()})
	}
	ring.publish()
	// A clip whose changes the mixer has not applied yet is let go a flush or
	// two later, the price of the flush never waiting. While no mixer has
	// drained the ring, nothing is mid-copy out of anything, so every clip is
	// let go at once rather than held against a counter nothing moves.
	// DeleteFunc clears the slice behind what it keeps: a clip left in its
	// spare capacity would keep its samples alive.
	pulled := ring.pulled.Load()
	applied := ring.applied.Load()
	b.plugin.releases = slices.DeleteFunc(b.plugin.releases, func(release clipRelease) bool {
		return !pulled || release.sequence < applied
	})
}

// Device answers the Device as it is now: one atomic load and a copy, cheap
// enough to poll every flush.
func (b backend) Device() sfx.Device { return *b.plugin.status.Load() }

// Prepare answers not done and prepares the clip on a goroutine of its own,
// off both the flush and the device thread: a clip whose decoded size is
// within Config.DecodedClipLimit is decoded there, once, and converted to
// the rate the mixer produces, and the encoded bytes, otosound's from the
// call on, are let go behind it. The clip still answers its source's facts,
// its rate among them, never the device's. Its completion,
// the prepared clip or the error, comes back under token through
// TakePrepared. A clip over the limit streams, which is not built yet, so its
// completion is errStreamingNotBuilt; the prepare's other errors are
// ErrNotOggVorbis, ErrNoStreamFormat and ErrNoStreamLength.
func (b backend) Prepare(token any, encoded []byte) (sfx.PreparedClip, bool, error) {
	decodedClipLimit := b.plugin.config.DecodedClipLimit
	outputSampleRate := b.plugin.outputSampleRate
	b.plugin.prepareGoroutines.Go(func() {
		completion := sfx.Prepared{Token: token}
		// A failed prepare hands sfx a nil clip, never a typed nil, which sfx
		// would install rather than report.
		if clip, err := prepareClip(encoded, decodedClipLimit, outputSampleRate); err != nil {
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
