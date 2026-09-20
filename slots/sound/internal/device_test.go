package internal

import (
	"testing"
	"time"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/sound"
)

// The Device is absent, not ready yet, or lost, and a game sees one thing in
// all three: Ready is false. These are the Slot's half of that - the playhead
// rule, the silence about it, and the resync that recovery owes - asserted
// against a fixture Adapter whose Device says whatever the test says next.

// offsetAt is a playhead expressed the way a batch carries one, so a test says
// the tick it means rather than a number of nanoseconds.
func offsetAt(ticks int) time.Duration {
	return time.Duration(float64(ticks) * step * float64(time.Second))
}

// notReady composes the Slot over an Adapter that has no Device, which is the
// web case before the gesture and the machine with no sound card alike.
func notReady(t *testing.T, clip fakeClip) *harness {
	t.Helper()
	backend := newFakeBackend(clip)
	backend.setReady(false)
	return newHarness(t, backend, sound.Config{}, clipBytes)
}

// A Voice's playhead advances whether or not anyone can hear it. This is that
// rule in the direction a web game meets it: the player has not clicked yet,
// the Voice exists from the tick that recorded it, and when the Device finally
// arrives the Voice is simply mid-Clip.
//
// It is deliberately unlike the Clip rule. A Voice waiting on its Clip starts
// at the head, because a load is a bounded hiccup the engine is actively
// fixing; Device readiness may never come at all, so suspending time behind it
// would make the first audible moment unbounded.
func TestAVoiceAdvancesWhileTheDeviceIsNotReadyAndIsMidClipWhenItArrives(t *testing.T) {
	h := notReady(t, fakeClip{duration: 1, channels: 2, rate: 48000})

	voice := h.play(sound.ClipWithResource(bell), 0, sound.Params{})
	for range 32 {
		h.tick()
	}

	got := h.probe(voice)
	if !got.Found {
		t.Fatal("the Voice is gone, and nothing about a missing Device ends one")
	}
	if got.Device.Ready {
		t.Fatalf("the Device reads %+v, and nothing has opened one", got.Device)
	}
	if got.Info.Playhead != 0.5 {
		t.Fatalf("the playhead is at %v after half a second of silence, want 0.5", got.Info.Playhead)
	}

	h.backend.setReady(true)
	h.tick()

	batches := h.backend.emitted()
	last := batches[len(batches)-1]
	if len(last.Starts) != 1 {
		t.Fatalf("the Device arrived and %d Voices were started, want the one that is live", len(last.Starts))
	}
	if got := last.Starts[0].Offset; got != offsetAt(33) {
		t.Fatalf("the Voice resumed at %v, want the %v the world is at now", got, offsetAt(33))
	}
}

// The other half of the same rule: a Voice played thirty seconds before the
// player clicks is over before the click. A footstep is not queued up to arrive
// as a wall of sound whenever a Device does, and its ending is delivered on the
// tick its duration says, with no Device involved in the arithmetic at all.
func TestAVoiceThatOutlivesNoDeviceEndsOnSchedule(t *testing.T) {
	h := notReady(t, fakeClip{duration: 0.5, channels: 2, rate: 48000})

	voice := h.play(sound.ClipWithResource(bell), 0, sound.Params{})
	for range 31 {
		h.tick()
	}
	h.noEnding()

	h.tick()
	if ended := h.waitEnded(); ended.Voice != voice || ended.Reason != sound.ReasonFinished {
		t.Fatalf("ended as %v/%v, want %v/finished", ended.Voice, ended.Reason, voice)
	}
	if got := h.probe(voice); got.Found || got.Live != 0 {
		t.Fatalf("the view still holds %v with no Device", voice)
	}
}

// sound never reports a not-ready backend, and has no counterpart to
// gfx.ErrBackendNotReady. On web, not-ready is the normal case for as long as
// the player has not clicked, and the Slot cannot tell "I tried to open a
// device and failed" from "I am waiting for a gesture that may never come" -
// only the Adapter knows which. A frame that did not render is a bug; silence
// nobody asked about is not.
func TestSoundNeverReportsANotReadyDevice(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 4, channels: 2, rate: 48000})
	backend.setReady(false)

	reported := make(chan error, 8)
	h := newHarnessReporting(t, backend, clipBytes, func(err error) { reported <- err })

	h.play(sound.ClipWithResource(bell), 0, sound.Params{})
	for range 16 {
		h.tick()
	}

	select {
	case err := <-reported:
		t.Fatalf("a Device that is not ready was reported as %v", err)
	default:
	}
}

// Loss is ordinary. Playback keeps being simulated through it, nothing is
// reported, and on recovery the Voice resumes where the world is now rather
// than where it was when the Device went away - which is what a Mixer whose
// voice table survived the outage would otherwise do.
//
// The failing sequence, without the resync: a Voice plays for four ticks, the
// headphones come out for thirty, they go back in, and the game hears the
// clip from four ticks in while its own view, its VoiceEndedEvent and every
// other Voice in the mix are thirty-four ticks in.
func TestARecoveryRepinsEveryLiveVoiceToWhereTheWorldIsNow(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 4, channels: 2, rate: 48000})
	h := newHarness(t, backend, sound.Config{}, clipBytes)

	voice := h.play(sound.ClipWithResource(bell), 0, sound.Params{})
	for range 4 {
		h.tick()
	}
	started := countStarts(h.backend.emitted())
	if started != 1 {
		t.Fatalf("%d starts crossed the seam before the loss, want the one Play", started)
	}

	backend.setReady(false)
	for range 30 {
		h.tick()
	}
	if got := h.probe(voice); !got.Found || got.Info.Playhead != float32(34*step) {
		t.Fatalf("the playhead is at %v after a lost Device, want %v", got.Info.Playhead, float32(34*step))
	}
	if got := countStarts(h.backend.emitted()); got != started {
		t.Fatalf("%d starts crossed the seam during the outage, want none", got-started)
	}

	backend.setReady(true)
	h.tick()

	batches := h.backend.emitted()
	last := batches[len(batches)-1]
	if len(last.Starts) != 1 {
		t.Fatalf("recovery started %d Voices, want the one that is live", len(last.Starts))
	}
	if got := last.Starts[0].Offset; got != offsetAt(35) {
		t.Fatalf("the Voice resumed at %v, want the %v the world is at now", got, offsetAt(35))
	}

	// And once only. Ready staying true is not an arrival, so the tick after a
	// recovery is an ordinary tick.
	h.tick()
	if got := h.backend.emitted(); len(got[len(got)-1].Starts) != 0 {
		t.Fatal("a Voice was restarted on a tick where nothing about the Device changed")
	}
}

// Pause is not a Device state. A suspended Voice changes nothing about what is
// audible in principle - the Device stays open and keeps being fed silence,
// because closing it would risk a reopen that fails and costs the open again -
// so nothing a pause does reaches the Device at all, and in particular a paused
// engine is not a not-ready one.
func TestAPausedVoiceDoesNotChangeTheDevice(t *testing.T) {
	h := newHarness(t, newFakeBackend(fakeClip{duration: 4, channels: 2, rate: 48000}), sound.Config{}, clipBytes)

	voice := h.play(sound.ClipWithResource(bell), 0, sound.Params{})
	h.tick()
	before := h.probe(voice).Device

	h.record(func(queue *sound.Queue) {
		queue.SetVoice(voice, sound.Params{Paused: m.Some(true)})
	})
	for range 10 {
		h.tick()
	}

	if got := h.probe(voice).Device; got != before {
		t.Fatalf("the Device reads %+v with a Voice paused, want the %+v it read before", got, before)
	}
}

// A paused Voice suspends: the playhead stops and resumes on the same sample.
// A recovery re-pins it to that sample rather than to the world clock, because
// the world moving on is exactly what a pause is a refusal to follow - and the
// start that carries it says Paused, so the Adapter installs a frozen slot
// rather than one that runs away the moment it is addressable again.
func TestARecoveryRepinsAPausedVoiceAtTheSampleItSuspendedOn(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 4, channels: 2, rate: 48000})
	h := newHarness(t, backend, sound.Config{}, clipBytes)

	voice := h.play(sound.ClipWithResource(bell), 0, sound.Params{})
	for range 4 {
		h.tick()
	}
	h.record(func(queue *sound.Queue) {
		queue.SetVoice(voice, sound.Params{Paused: m.Some(true)})
	})
	h.tick()

	backend.setReady(false)
	for range 20 {
		h.tick()
	}
	backend.setReady(true)
	h.tick()

	batches := h.backend.emitted()
	last := batches[len(batches)-1]
	if len(last.Starts) != 1 {
		t.Fatalf("recovery started %d Voices, want the one that is live", len(last.Starts))
	}
	if got := last.Starts[0].Offset; got != offsetAt(4) {
		t.Fatalf("the paused Voice resumed at %v, want the %v it suspended on", got, offsetAt(4))
	}
	if !last.Starts[0].Params.Paused {
		t.Fatal("the restart says the Voice is running, and it is paused")
	}
}
