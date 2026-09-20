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

// retryEvery is how often a Device that could not be opened is tried again. It
// is a constant: a configurable cadence is a knob nothing has asked for.
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
		if !b.watch() {
			return
		}
		// The device went away. Ready goes false and nothing is reported; the
		// open is retried once a second, indefinitely, and on recovery every
		// live Voice resumes where the world is now.
		b.device.Store(&sound.Device{Name: string(otosound.Name)})
		b.audio.close()
		if !b.sleep() {
			return
		}
	}
}

// watch polls for a device that has gone away. It reports false when the engine
// is shutting down.
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
	timer := time.NewTimer(retryEvery)
	defer timer.Stop()
	select {
	case <-b.done:
		return false
	case <-timer.C:
		return true
	}
}

// fail records a Device that could not be opened, for the tick handler that
// holds a Kernel to report once. Only the first failure is kept: the condition
// is true every second after that and is noise at that rate.
func (b *backend) fail(err error) {
	b.failure.CompareAndSwap(nil, &deviceFailure{err: err})
}

// stop closes the device down and waits for its goroutine, so an engine that
// has shut down leaves nothing running behind it.
func (b *backend) stop() {
	select {
	case <-b.done:
	default:
		close(b.done)
	}
	b.wg.Wait()
	b.audio.close()
}
