//go:build !js

package internal

import (
	"time"

	"github.com/dvoyni/cog/extensions/otosound"
	"github.com/dvoyni/cog/slots/sound"
)

// This is the backend's other subject: taking a device, watching it, and giving
// it up. It is split out because it is the only part of the Adapter that runs
// on a goroutine of its own, and because everything it touches - the settled
// rate, the Device, the two things it has to say out loud - crosses back to the
// tick through an atomic and nothing else.

// retryEvery is how often a Device that could not be opened is tried again, and
// how often an open one is checked for having gone away. It is a constant: a
// configurable cadence is a knob nothing has asked for, and the backend field
// it seeds is a test seam that no Config reaches.
const retryEvery = time.Second

// open settles the rate the Mixer produces and then hands the device off to a
// goroutine. It never blocks the game and never fails it: registration succeeds
// whether or not a device exists, which is gfx's shape one Slot over.
//
// Settling the rate synchronously is what keeps a Clip from being prepared
// against a rate that is not the device's. The only case where the two differ
// is a second Engine in one process, and by then the context already exists, so
// the answer is available at once rather than a tick or two later.
func (b *backend) open() {
	settled, err := b.audio.open(b.askedRate, b.askedBuffer)
	if err != nil {
		b.fail(err)
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			if reopened, ok := b.retry(); ok {
				b.attach(reopened)
			}
		}()
		return
	}
	b.rate = settled.sampleRate
	if settled.ignored {
		b.ignored.Store(&otosound.ErrDeviceConfigIgnored{
			AskedSampleRate: b.askedRate, SampleRate: settled.sampleRate,
			AskedBufferSize: b.askedBuffer, BufferSize: settled.bufferSize,
		})
	}
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		b.attach(settled)
	}()
}

// attach starts the player and then watches it. A device that never comes up is
// reported once and the game runs on in silence; a device that comes up and is
// then lost is reported never, because loss is ordinary - Ready going false is
// the whole of that notification.
func (b *backend) attach(settled facts) {
	for {
		latency, err := b.audio.play(b.mixer)
		if err != nil {
			b.fail(err)
			reopened, ok := b.retry()
			if !ok {
				return
			}
			settled = reopened
			continue
		}
		b.device.Store(&sound.Device{
			Ready:      true,
			Name:       string(otosound.Name),
			SampleRate: settled.sampleRate,
			Channels:   outChannels,
			Latency:    latency,
		})
		// From here on, nothing this goroutine hits is reportable. A Device
		// that was once audible and then was not is a loss, and a loss is not a
		// failure however it presents itself - as a poll that errors, or as the
		// reopen after it refusing to play.
		b.everReady.Store(true)
		if !b.watch() {
			return
		}
		// The device went away. Ready goes false and nothing is reported - loss
		// is ordinary, and Ready going false is the whole of the notification -
		// and the open is retried on the same cadence, indefinitely.
		//
		// The order below is the whole of the release fix. Ready goes false
		// first, so the tick sees the loss no later than it sees the freeing;
		// then the player is stopped, which does not return until it has
		// finished reading the Mixer; and only then is the ring detached, which
		// is the tick's permission to free a destroyed Clip at once again.
		// Detaching before the player had stopped would be claiming nothing is
		// mid-copy while something still might be.
		//
		// Without this, the applied counter stops the moment the device goes
		// and every release after it is held for as long as the Device stays
		// gone: a leak rather than a crash, bounded by what the game releases,
		// but unbounded in time.
		b.device.Store(&sound.Device{Name: string(otosound.Name)})
		b.audio.close()
		b.ring.detach()
		if !b.sleep() {
			return
		}
	}
}

// watch polls for a device that has gone away. It reports false when the engine
// is shutting down.
//
// Polling rather than a callback because oto has nothing to push: a Device that
// disappears is visible only as an error on the context or the player, and only
// to whoever asks.
//
// # Gap: whether this ever fires is unmeasured
//
// oto v3's behaviour on a device change was not measured in the 2026-09-12
// spike and has not been measured since, so it is not known whether ctx.Err()
// or player.Err() ever reports one. The real possibility the spec records is
// that oto keeps writing into a device that is gone and never surfaces
// anything, in which case this poll never fires, Ready stays true, and the game
// is permanently silent with nothing saying so. If that is what happens,
// polling is the wrong mechanism and an explicit close-and-reopen on a cadence
// is needed instead; the shape around it - loss is silent, the playhead
// advances anyway, recovery re-pins every Voice to the world's current state -
// does not change either way.
//
// It cannot be settled from a test: it needs hardware events. What would settle
// it, on a machine with a real sound card, is running a composition that plays
// a long looping Clip and prints Device().Ready once a second, and then, one at
// a time, unplugging the headphones it is playing through, switching the
// default output device in the OS mixer while it plays, and sleeping and waking
// the machine. For each: does Ready go false within a second or two, and does
// sound come back within a second or two of the device returning? A run where
// the audio stops but Ready stays true is the failure mode above, and is the
// answer that changes the mechanism.
func (b *backend) watch() bool {
	for {
		if !b.sleep() {
			return false
		}
		if b.audio.err() != nil {
			return true
		}
	}
}

// retry waits out the cadence and tries the open again, indefinitely, until it
// succeeds or the engine stops.
//
// It never moves b.rate, and it cannot: the rate is the process-wide context's
// and is fixed the first time one is created, so a retry that finally succeeds
// opens at exactly the rate every Clip was already prepared against.
func (b *backend) retry() (facts, bool) {
	for b.sleep() {
		settled, err := b.audio.open(b.askedRate, b.askedBuffer)
		if err != nil {
			continue
		}
		return settled, true
	}
	return facts{}, false
}

// sleep waits one retry interval, or reports false the moment the engine stops.
func (b *backend) sleep() bool {
	timer := time.NewTimer(b.cadence)
	defer timer.Stop()
	select {
	case <-b.done:
		return false
	case <-timer.C:
		return true
	}
}

// fail records a Device that could not be opened at all, for the tick handler
// that holds a Kernel to report once. Only the first failure is kept: the
// condition is true every cadence after that and is noise at that rate.
//
// A Device that was once ready records nothing, ever. That is the whole of
// "loss is reported never": once a game has heard something, a failed poll and
// a reopen that will not play are both the Device having gone away, and Ready
// going false is the entire notification either is owed.
func (b *backend) fail(err error) {
	if b.everReady.Load() {
		return
	}
	b.failure.CompareAndSwap(nil, &deviceFailure{err: err})
}

// stop closes the device down and waits for its goroutine, so an engine that
// has shut down leaves nothing running behind it.
//
// Every read-ahead goes too. A streamed Voice's goroutine ends with its Voice,
// and an Engine shutting down ends every Voice there is, whether or not sound
// got as far as emitting the stops.
func (b *backend) stop() {
	for slot := range b.streams {
		b.haltReadAhead(sound.VoiceSlot(slot))
	}
	select {
	case <-b.done:
	default:
		close(b.done)
	}
	b.wg.Wait()
	b.audio.close()
}
