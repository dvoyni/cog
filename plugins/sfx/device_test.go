package sfx_test

import (
	"testing"
	"time"

	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/sfxtest"
)

// The device is absent, not ready yet, or lost, and a game sees one thing in
// all three: Ready is false. These are sfx's half of that: the playhead rule,
// the silence about it, and the restart an arrival owes, asserted over a fake
// backend whose device says whatever the test says next.

// readDevice answers the device as DeviceView holds it after the last step.
func readDevice(h *sfxtest.Harness) sfx.Device {
	var device sfx.Device
	h.Read(func(_ sfx.LiveVoices, _ sfx.ClipTable, _ sfx.BusVolumes, _ sfx.Listener, view sfx.DeviceView) {
		device = view.Device()
	})
	return device
}

// lastChanges answers the changes the last step's flush handed the backend.
func lastChanges(seam *seam) sfx.FlushChanges {
	emitted := seam.emitted()
	return emitted[len(emitted)-1]
}

// The device is polled once per flush, and DeviceView reads what the last
// flush polled: nothing before the first, and a device going away at the
// flush after it does.
func TestTheDeviceIsPolledEveryFlush(t *testing.T) {
	backend := &fakeBackend{}
	h := newFakeHarness(t, backend)

	if got := readDevice(h); got != (sfx.Device{}) {
		t.Fatalf("the device reads %+v before the first flush", got)
	}

	h.Step()
	if got := readDevice(h); got != fakeDevice {
		t.Fatalf("the device reads %+v after a flush, want %+v", got, fakeDevice)
	}

	backend.setReady(false)
	h.Step()
	if got := readDevice(h); got != notReadyFakeDevice {
		t.Fatalf("the device reads %+v after the flush that found it gone", got)
	}
}

// Under the headless backend the device is constant, so a test may assert it
// here and nowhere else: ready, because it is a working device that plays
// nothing, and at zero latency, because nothing is buffered.
func TestSfxReportsTheHeadlessDevice(t *testing.T) {
	h := newHarness(t, nil)
	h.Step()

	if got, want := readDevice(h), (sfx.Device{Ready: true, Name: "headless", SampleRate: 48000, Channels: 2}); got != want {
		t.Fatalf("sfx reports %+v under the headless backend, want %+v", got, want)
	}
}

// A voice's playhead advances whether or not anyone can hear it. The voice
// exists from the flush that applied its play, and when the device finally
// arrives it is simply mid-clip, restarted where the world is now.
//
// It is deliberately unlike the clip rule. A voice waiting on its clip starts
// at the head, because a load is a bounded hiccup sfx is actively fixing; a
// device's readiness may never come at all, so suspending time behind it
// would make the first audible moment unbounded.
func TestAVoiceAdvancesWhileTheDeviceIsNotReadyAndIsMidClipWhenItArrives(t *testing.T) {
	backend := &fakeBackend{notReady: true}
	seam := &seam{}
	h := newFakeHarness(t, backend, seamPlugin{seam: seam})

	voice := play(h)
	for range 31 {
		h.Step()
	}

	got, live := readLiveVoice(h, voice)
	if !live {
		t.Fatal("the voice is gone, and nothing about a missing device ends one")
	}
	if device := readDevice(h); device != notReadyFakeDevice {
		t.Fatalf("the device reads %+v, want the fake backend's, which nothing has opened", device)
	}
	if got.Playhead != 32*sfxtest.StepInterval {
		t.Fatalf("the playhead is at %v after half a second of silence, want %v", got.Playhead, 32*sfxtest.StepInterval)
	}

	backend.setReady(true)
	h.Step()

	starts := lastChanges(seam).Starts
	if len(starts) != 1 || starts[0].Slot != 0 || starts[0].Clip == 0 {
		t.Fatalf("the device arrived and the starts are %+v, want the one live voice on its clip", starts)
	}
	if starts[0].Offset != 33*sfxtest.StepInterval {
		t.Fatalf("the voice resumed at %v, want the %v the world is at now", starts[0].Offset, 33*sfxtest.StepInterval)
	}
}

// The other half of the same rule: a voice played long before the device
// arrives is over before it does. A footstep is not queued up to arrive as a
// wall of sound whenever a device does, and its ending is delivered on the
// flush its duration says, with no device in the arithmetic at all.
func TestAVoiceThatOutlivesNoDeviceEndsOnSchedule(t *testing.T) {
	// fakeEndsOnStep is the step a one-shot of the fake clip played on step
	// 1 ends on.
	const fakeEndsOnStep = int(fakeClipSeconds * time.Second / sfxtest.StepInterval)
	h := newFakeHarness(t, &fakeBackend{notReady: true})

	voice := play(h)
	for range fakeEndsOnStep - 2 {
		h.Step()
	}
	wantEndings(t, h)

	h.Step()
	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonFinished})
	if _, live := readLiveVoice(h, voice); live || readLiveCount(h) != 0 {
		t.Fatalf("the view still holds %v with no device", voice)
	}
}

// sfx never reports a device that is not ready. It cannot tell "a device was
// tried and failed" from "a device is not ready yet"; only the backend knows
// which, and reports it. A silence nobody asked about is not a bug, and an
// error for it would end the game under the default logger.
func TestSfxNeverReportsANotReadyDevice(t *testing.T) {
	h := newFakeHarness(t, &fakeBackend{notReady: true})

	play(h)
	for range 15 {
		h.Step()
	}

	if reports := h.Reports(); len(reports) != 0 {
		t.Fatalf("a device that is not ready was reported as %v", reports)
	}
}

// Loss is ordinary. Playback keeps being simulated through it, nothing
// crosses the seam for it, and when the device comes back the voice resumes
// where the world is now rather than where it was when the device went away,
// which is what a mixer whose voice table survived the outage would
// otherwise do.
//
// The failing sequence, without the restart: a voice plays for four flushes,
// the headphones come out for thirty, they go back in, and the game hears the
// clip from four flushes in while its own view, its VoiceEndedEvent and every
// other voice in the mix are thirty-four flushes in.
func TestARecoveryRepinsEveryLiveVoiceToWhereTheWorldIsNow(t *testing.T) {
	backend := &fakeBackend{}
	seam := &seam{}
	h := newFakeHarness(t, backend, seamPlugin{seam: seam})

	voice := play(h)
	for range 3 {
		h.Step()
	}
	if started := countStarts(seam.emitted()); started != 1 {
		t.Fatalf("%d starts crossed the seam before the loss, want the one play", started)
	}

	backend.setReady(false)
	for range 30 {
		h.Step()
	}
	if got, live := readLiveVoice(h, voice); !live || got.Playhead != 34*sfxtest.StepInterval {
		t.Fatalf("the voice reads %+v after a lost device, want it %v in", got, 34*sfxtest.StepInterval)
	}
	if started := countStarts(seam.emitted()); started != 1 {
		t.Fatalf("%d starts crossed the seam during the outage, want none", started-1)
	}

	backend.setReady(true)
	h.Step()
	changes := lastChanges(seam)
	if len(changes.Starts) != 1 || len(changes.Updates) != 0 {
		t.Fatalf("the recovery emitted %+v, want the one live voice restarted and nothing beside it", changes)
	}
	if got := changes.Starts[0].Offset; got != 35*sfxtest.StepInterval {
		t.Fatalf("the voice resumed at %v, want the %v the world is at now", got, 35*sfxtest.StepInterval)
	}

	// And once only: a device staying ready is no arrival, so the flush after
	// a recovery is an ordinary one.
	h.Step()
	if starts := lastChanges(seam).Starts; len(starts) != 0 {
		t.Fatalf("the flush after the recovery restarted %+v, and nothing about the device changed", starts)
	}
}

// A pause is not a device state. A suspended voice changes nothing about what
// is audible in principle: the device stays open and keeps being fed
// silence, because closing it would risk a reopen that fails and costs the
// open again. So nothing a pause does reaches the device, and in particular
// a paused game is not one whose device is not ready.
func TestAPausedVoiceDoesNotChangeTheDevice(t *testing.T) {
	h := newHarness(t, nil)

	play(h)
	before := readDevice(h)

	setPaused(h, true)
	for range 10 {
		h.Step()
	}

	if got := readDevice(h); got != before || !got.Ready {
		t.Fatalf("the device reads %+v with every voice paused, want the ready %+v it read before", got, before)
	}
}

// A paused voice suspends: the playhead stops and resumes on the same sample.
// A recovery re-pins it to that sample rather than to the world, because the
// world moving on is exactly what a pause is a refusal to follow, and the
// start that carries it says Paused, so the backend installs a frozen slot
// rather than one that runs away the moment it is addressable again.
func TestARecoveryRepinsAPausedVoiceAtTheSampleItSuspendedOn(t *testing.T) {
	backend := &fakeBackend{}
	seam := &seam{}
	h := newFakeHarness(t, backend, seamPlugin{seam: seam})

	play(h)
	for range 3 {
		h.Step()
	}
	setPaused(h, true)

	backend.setReady(false)
	for range 20 {
		h.Step()
	}
	backend.setReady(true)
	h.Step()

	starts := lastChanges(seam).Starts
	if len(starts) != 1 {
		t.Fatalf("the recovery started %+v, want the one live voice", starts)
	}
	if starts[0].Offset != 4*sfxtest.StepInterval {
		t.Fatalf("the paused voice resumed at %v, want the %v it suspended on", starts[0].Offset, 4*sfxtest.StepInterval)
	}
	if !starts[0].Params.Paused {
		t.Fatal("the restart says the voice is running, and it is paused")
	}
}
