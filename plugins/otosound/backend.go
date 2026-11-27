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
// one, and only then starts the device goroutine, which starts this kernel's
// player over the mixer, which drains the ring and plays silence: the mixer
// must exist complete before anything can pull a block out of it. It takes
// the device synchronously, since that does not wait for it and settles the
// settings in force: it asks the machine whether it has an output device
// and, if it does, takes the process's oto context or creates it. A retry
// on the device goroutine never moves the rate settled here, since the rate
// is the context's, fixed when it was created. The player is started on the device
// goroutine, which waits for the context to be ready and then sets the
// Device ready, at the rate and buffer size in force. It never blocks or
// fails the game: a device that cannot be taken, or whose player cannot
// start, leaves the Device not ready, is logged once as a warning, and is
// retried every Config.DeviceRetry.
func (b backend) Voices(n int) {
	b.plugin.mixer.ring = newHandoffRing(b.plugin.config.RingDepth, max(n, 1))
	settings, err := b.plugin.deviceGoroutine.take()
	b.plugin.deviceGoroutine.start(settings, err)
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
// ring is full, leaves them for the next Emit to merge into, or to discard
// while no player pulls the ring, and lets go of
// every destroyed clip the mixer has passed. It never waits on the mixer. It
// is called only after Voices, and panics before.
func (b backend) Emit(changes *sfx.FlushChanges) {
	ring := b.plugin.mixer.ring
	if ring == nil {
		panic("otosound: Emit was called before Voices")
	}
	staging := ring.staging
	// Staging changes left over hold what a full ring did not take. While no
	// player pulls the ring, this Emit discards them: sfx restates every live
	// voice when the device turns ready, so nothing is lost. Otherwise it
	// merges into them: the one case in which an update coalesces.
	// Cleared before truncated, so no discarded clip stays alive in the
	// spare capacity.
	if !ring.pulled.Load() {
		clear(staging.operations)
		staging.operations = staging.operations[:0]
	}
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
