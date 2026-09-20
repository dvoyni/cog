//go:build !js

package internal

import (
	"errors"
	"testing"
	"time"

	"github.com/dvoyni/cog/extensions/otosound"
	"github.com/dvoyni/cog/slots/sound"
)

// The device lifecycle from below: a Device that goes away, the silence about
// it, the retry, and the releases a loss must not hold on to.
//
// What is not here, and cannot be, is whether oto ever tells us a device went
// away at all. That needs headphones and a hand, and the Gap is recorded on
// watch in backend-device.go with the run that would settle it. Everything
// below assumes the poll fires and asserts what happens then.

// testCadence is what the suite runs the device goroutine at. It is retryEvery
// in every composition; a second per poll would make one loss-and-recovery test
// longer than the rest of the package put together.
const testCadence = time.Millisecond

// newWatchedBackend composes the Adapter over a fixture device, on a cadence
// fast enough to watch.
func newWatchedBackend(t *testing.T, hardware *fakeAudio) *backend {
	t.Helper()
	b := newBackend(otosound.Config{}, hardware)
	b.cadence = testCadence
	t.Cleanup(b.stop)
	b.Voices(8)
	return b
}

// waitFor spins until condition holds, which is how a test on this side waits
// for a goroutine that reports itself through an atomic and nothing else.
func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(testCadence)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// Loss is ordinary, and is not a failure. Ready goes false, which is the whole
// of the notification: no ReportError, not even once, and the open is retried
// on the cadence until a device comes back.
//
// The reopen failing is still a loss, not a failure. A poll that errors and a
// reattach that will not take a player are the same device being gone, and a
// game that has already heard something must not be told the machine has no
// sound card.
func TestALostDeviceIsNeverReportedAndTheOpenIsRetriedUntilItComesBack(t *testing.T) {
	hardware := &fakeAudio{buffer: defaultBufferSize}
	b := newWatchedBackend(t, hardware)
	waitReady(t, b)

	closes := hardware.closeCount()
	hardware.unplug(errors.New("the headphones came out"))
	waitFor(t, "the Device to go away", func() bool { return !b.Device().Ready })

	if failure := b.failure.Load(); failure != nil {
		t.Fatalf("losing a Device reported %v, and loss is reported never", failure.err)
	}
	if got := hardware.closeCount(); got <= closes {
		t.Fatal("the player was left attached to a device that is gone")
	}
	attempts := hardware.playCount()
	waitFor(t, "the open to be retried", func() bool { return hardware.playCount() > attempts })

	hardware.plugBackIn()
	device := waitReady(t, b)

	if failure := b.failure.Load(); failure != nil {
		t.Fatalf("a Device that came back reported %v on the way", failure.err)
	}
	if device.SampleRate != defaultSampleRate || device.Latency != defaultBufferSize {
		t.Fatalf("the recovered Device is %+v, want the one that was in force before", device)
	}
}

// A release after a loss is freed at once, the same way a release under a
// Device that never opened is.
//
// The failing sequence: a Voice plays, so the Mixer has pulled and the applied
// counter is what a release waits for; the headphones come out, so the Mixer
// never pulls again and the counter never moves; and every Clip the game
// releases from then on is held for as long as the device stays away. Bounded
// by what the game releases, so a leak rather than a crash - and unbounded in
// time, which is the part that makes it worth a mechanism.
func TestAReleaseAfterALostDeviceIsFreedAtOnce(t *testing.T) {
	hardware := &fakeAudio{buffer: defaultBufferSize}
	b := newWatchedBackend(t, hardware)
	waitReady(t, b)

	id, err := b.Install(constantClip(4*testBlock, 1, 1))
	if err != nil {
		t.Fatalf("installing a prepared Clip: %v", err)
	}
	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: id, Params: sound.VoiceParams{Gains: mono(1), Rate: 1},
	}}})
	render(t, b.mixer, 1)

	b.Emit(&sound.Batch{Stops: []sound.VoiceSlot{0}, Destroys: []sound.ClipID{id}})
	if len(b.pending) != 1 {
		t.Fatalf("%d releases are held while the Mixer is pulling, want the one it has not passed", len(b.pending))
	}

	hardware.unplug(errors.New("the default output changed"))
	waitFor(t, "the ring to be detached", func() bool { return !b.ring.pulled.Load() })

	b.Emit(&sound.Batch{})
	if len(b.pending) != 0 {
		t.Fatalf("%d releases are held against a counter a lost Device will never move", len(b.pending))
	}
}

// And the other side of it: a Device that comes back is a Mixer pulling again,
// so a release is held against the applied counter once more. The detach is the
// loss saying "nothing is mid-copy", not the Adapter giving the rule up.
func TestAReleaseIsHeldAgainstTheCounterOnceADeviceComesBack(t *testing.T) {
	hardware := &fakeAudio{buffer: defaultBufferSize}
	b := newWatchedBackend(t, hardware)
	waitReady(t, b)

	hardware.unplug(errors.New("the headphones came out"))
	waitFor(t, "the ring to be detached", func() bool { return !b.ring.pulled.Load() })
	hardware.plugBackIn()
	waitReady(t, b)

	id, err := b.Install(constantClip(4*testBlock, 1, 1))
	if err != nil {
		t.Fatalf("installing a prepared Clip: %v", err)
	}
	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: id, Params: sound.VoiceParams{Gains: mono(1), Rate: 1},
	}}})
	render(t, b.mixer, 1)

	b.Emit(&sound.Batch{Stops: []sound.VoiceSlot{0}, Destroys: []sound.ClipID{id}})
	b.Emit(&sound.Batch{})
	if len(b.pending) != 1 {
		t.Fatalf("%d releases are held after a recovery, want the one the Mixer has not passed", len(b.pending))
	}
}

// A Device that never opened at all is still the one thing this Adapter
// reports, and it is reported once however many times the retry runs. This is
// what everReady separates from the test above it: the same retry loop, the
// same cadence, and the opposite answer about whether anyone is told.
func TestADeviceThatNeverOpenedIsReportedOnceHoweverOftenItIsRetried(t *testing.T) {
	boom := errors.New("no audio endpoint")
	hardware := &fakeAudio{openErr: boom}
	b := newWatchedBackend(t, hardware)

	failure := b.failure.Load()
	if failure == nil || !errors.Is(failure.err, boom) {
		t.Fatalf("a Device that could not be opened recorded %v", failure)
	}
	opens := hardware.openCount()
	waitFor(t, "the open to be retried", func() bool { return hardware.openCount() > opens })

	if got := b.failure.Load(); got != failure {
		t.Fatal("a retried open recorded a second failure, and the condition is worth saying once")
	}
	if b.Device().Ready {
		t.Fatal("the Device reports Ready with nothing open")
	}
}
