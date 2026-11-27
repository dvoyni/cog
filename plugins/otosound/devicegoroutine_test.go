//go:build !js

package otosound

import (
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/sfxtest"
)

// The device's lifecycle from below: a device that never opens, a machine
// with none, a device that goes away, the silence about a loss, the retry,
// and the releases a loss must not hold on to. What none of it can assert is
// whether oto ever tells otosound a device went away at all, which needs
// headphones and a hand: the design records the run that would settle it.
// Everything here assumes the poll fires, and asserts what happens then.

// quickRetry is the DeviceRetry these tests run the device goroutine at, so a
// loss and a recovery take milliseconds rather than seconds.
var quickRetry = &Config{DeviceRetry: time.Millisecond}

// clipPath is where the game-runs-on tests mount their clip.
const clipPath = "music/pianoroll.ogg"

// waitFor waits until condition holds, and fails the test, naming what it
// waited for, when it does not hold within sfxtest.Timeout: the device
// goroutine says what it did through atomics and the fake device alone.
func waitFor(t testing.TB, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(sfxtest.Timeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen within %v", what, sfxtest.Timeout)
		}
		time.Sleep(time.Millisecond)
	}
}

// wantOneUnavailableWarning fails the test unless h's kernel logged exactly
// one warning, carrying an ErrDeviceUnavailable that wraps cause.
func wantOneUnavailableWarning(t testing.TB, h *sfxtest.Harness, cause error) {
	t.Helper()
	warnings := h.Warnings()
	if len(warnings) == 1 && slices.ContainsFunc(warnings[0].Args, func(arg any) bool {
		unavailable, isUnavailable := arg.(ErrDeviceUnavailable)
		return isUnavailable && errors.Is(unavailable, cause)
	}) {
		return
	}
	t.Fatalf("the warnings logged are %v, want one carrying ErrDeviceUnavailable over %v", warnings, cause)
}

// isConfigIgnored answers whether err is an ErrDeviceConfigIgnored.
func isConfigIgnored(err error) bool {
	_, ignored := errors.AsType[ErrDeviceConfigIgnored](err)
	return ignored
}

// isReady answers whether plugin's Device is ready.
func isReady(plugin *Plugin) bool { return backend{plugin: plugin}.Device().Ready }

// playFixture mounts the fixture clip, records a play of it and steps eight
// times, so the game runs on whatever its device did, and answers the
// voice's handle.
func playFixture(t *testing.T, h *sfxtest.Harness) sfx.Voice {
	t.Helper()
	// sfx's headless backend holds the fixture; otosound reads it from there
	// until a copy of its own lands beside its decoder.
	encoded, err := os.ReadFile("../sfx/headless/testdata/pianoroll.ogg")
	if err != nil {
		t.Fatal(err)
	}
	h.Mount(clipPath, encoded)
	var voice sfx.Voice
	h.Record(func(queue sfx.Queue) { voice = queue.Play(clipPath, 0, sfx.VoiceOptions{}) })
	for range 8 {
		h.Step()
	}
	return voice
}

// wantTheGameRanOn fails the test unless voice is still live, and the Device
// sfx's view answers not ready.
func wantTheGameRanOn(t *testing.T, h *sfxtest.Harness, voice sfx.Voice) {
	t.Helper()
	h.Read(func(voices sfx.LiveVoices, _ sfx.ClipTable, _ sfx.BusVolumes, _ sfx.Listener, device sfx.DeviceView) {
		if _, live := voices.Info(voice); !live {
			t.Error("the voice did not survive a device that never opened")
		}
		if device.Device().Ready {
			t.Error("sfx reads a ready device with nothing open")
		}
	})
}

// A device that could never be opened is reported once, as a warning, and
// the game runs on in silence rather than failing to start: the kernel
// spawns, the voice lives, and the Device reads not ready.
func TestADeviceThatCanNotBeOpenedIsReportedOnceAndTheGameRunsOn(t *testing.T) {
	refused := errors.New("no audio endpoint")
	device := &fakeDevice{openErr: refused}
	h, _ := spawnConfiguredOverFake(t, device, quickRetry)

	voice := playFixture(t, h)
	opens, _, _ := device.counts()
	waitFor(t, "the open being retried", func() bool { opened, _, _ := device.counts(); return opened > opens })

	wantOneUnavailableWarning(t, h, refused)
	wantTheGameRanOn(t, h, voice)
}

// The same promise on the machine that breaks it: no output device at all,
// and an open that would succeed anyway, as oto's Windows driver's null
// context does. What it gets is what a device that could never be opened
// gets: said once, and the game runs.
func TestAMachineWithNoOutputDeviceIsReportedOnceAndTheGameRunsOn(t *testing.T) {
	device := &fakeDevice{missing: errNoOutputDevice}
	h, _ := spawnConfiguredOverFake(t, device, quickRetry)

	voice := playFixture(t, h)
	probes := device.probes()
	waitFor(t, "the machine being asked again", func() bool { return device.probes() > probes })

	wantOneUnavailableWarning(t, h, errNoOutputDevice)
	wantTheGameRanOn(t, h, voice)
}

// A device that never opened is the one thing otosound reports, and it is
// reported once however often the retry runs.
func TestADeviceThatNeverOpenedIsReportedOnceHoweverOftenItIsRetried(t *testing.T) {
	refused := errors.New("no audio endpoint")
	device := &fakeDevice{openErr: refused}
	h, plugin := spawnConfiguredOverFake(t, device, quickRetry)

	waitFor(t, "five opens", func() bool { opens, _, _ := device.counts(); return opens >= 5 })

	wantOneUnavailableWarning(t, h, refused)
	if isReady(plugin) {
		t.Fatal("the Device reads ready with nothing open")
	}
}

// A machine with no output device is reported even though its open and play
// would succeed, and nothing is opened on it: on Windows an open is what
// creates the null context the process is then stuck with.
func TestAMachineWithNoOutputDeviceIsReportedOnceAndNoPlayerIsTaken(t *testing.T) {
	device := &fakeDevice{missing: errNoOutputDevice}
	h, plugin := spawnConfiguredOverFake(t, device, quickRetry)

	waitFor(t, "five probes", func() bool { return device.probes() >= 5 })

	wantOneUnavailableWarning(t, h, errNoOutputDevice)
	if isReady(plugin) {
		t.Fatal("the Device reads ready on a machine that can play nothing")
	}
	if opens, plays, _ := device.counts(); opens != 0 || plays != 0 {
		t.Fatalf("a machine with no output device was opened %d times and played %d, and a context taken on it is silent for the life of the process", opens, plays)
	}
}

// A device turning up on a machine that had none is found by the retry that
// was already running, and nobody is told a second time. The probe running
// before the open is what makes this possible: no context was created while
// nothing existed, so the one created now is real.
func TestADeviceThatAppearsOnAMachineWithNoneIsPickedUpByTheRetry(t *testing.T) {
	device := &fakeDevice{missing: errNoOutputDevice}
	h, plugin := spawnConfiguredOverFake(t, device, quickRetry)
	waitFor(t, "the absence being reported", func() bool { return len(h.Warnings()) > 0 })

	device.installDevice()
	waitFor(t, "the Device turning ready", func() bool { return isReady(plugin) })

	want := sfx.Device{Ready: true, Name: "otosound", SampleRate: 48000, Channels: 2, Latency: 10 * time.Millisecond}
	if got := (backend{plugin: plugin}).Device(); got != want {
		t.Errorf("the device that turned up is %+v, want %+v, the one the Config asked for", got, want)
	}
	wantOneUnavailableWarning(t, h, errNoOutputDevice)
	if _, plays, _ := device.counts(); plays != 1 {
		t.Fatalf("the player was taken %d times, want the one the recovery needed", plays)
	}
}

// Loss is ordinary, and no failure: the Device goes not ready, which is the
// whole of the notice, nothing is logged, and the open is retried until the
// device comes back. The reopen failing meanwhile is still the loss, not a
// failure: a game that has heard something is never told the machine has no
// sound card.
func TestALostDeviceIsNeverReportedAndTheOpenIsRetriedUntilItComesBack(t *testing.T) {
	device := &fakeDevice{}
	h, plugin := spawnConfiguredOverFake(t, device, quickRetry)
	ready := waitForReady(t, plugin)

	_, _, closes := device.counts()
	device.unplug(errors.New("the headphones came out"))
	waitFor(t, "the Device going not ready", func() bool { return !isReady(plugin) })
	if _, _, closed := device.counts(); closed <= closes {
		t.Fatal("the player was left on a device that is gone")
	}
	_, plays, _ := device.counts()
	waitFor(t, "the play being retried", func() bool { _, played, _ := device.counts(); return played > plays })

	device.plugBackIn()
	if got := waitForReady(t, plugin); got != ready {
		t.Errorf("the device that came back is %+v, want %+v, the one in force before", got, ready)
	}
	if warnings, reports := h.Warnings(), h.Reports(); len(warnings) != 0 || len(reports) != 0 {
		t.Fatalf("a lost device logged %v and %v, and a loss is reported never", warnings, reports)
	}
}

// A release after a loss is let go at once, as one under a device that
// never opened is. The failing sequence it guards: a voice plays, so the
// mixer has pulled and a release waits for the applied counter; the
// headphones come out, so the mixer never pulls again and the counter never
// moves; and every clip the game releases from then on is held for as long
// as the device stays away.
func TestAReleaseAfterALostDeviceIsFreedAtOnce(t *testing.T) {
	device := &fakeDevice{}
	_, plugin := spawnConfiguredOverFake(t, device, quickRetry)
	waitForReady(t, plugin)
	b := backend{plugin: plugin}
	plugin.clips[1] = &fakeClip{}
	b.Emit(&sfx.FlushChanges{Starts: []sfx.VoiceStart{{Slot: 0, Clip: 1}}})
	wantSilence(t, device)

	b.Emit(&sfx.FlushChanges{Stops: []sfx.VoiceSlot{0}, Destroys: []sfx.ClipID{1}})
	if held := heldCount(plugin); held != 1 {
		t.Fatalf("%d released clips are held while the mixer pulls, want the one it has not passed", held)
	}

	device.unplug(errors.New("the default output changed"))
	waitFor(t, "the ring being detached", func() bool { return !plugin.mixer.ring.pulled.Load() })
	b.Emit(&sfx.FlushChanges{})
	if held := heldCount(plugin); held != 0 {
		t.Fatalf("%d released clips are held against a counter a lost device will never move", held)
	}
}

// And the other side of it: a device that comes back is a mixer pulling
// again, so a release is held against the applied counter once more. The
// detach is the loss saying nothing is mid-copy, not the backend giving the
// rule up.
func TestAReleaseIsHeldAgainstTheCounterOnceADeviceComesBack(t *testing.T) {
	device := &fakeDevice{}
	_, plugin := spawnConfiguredOverFake(t, device, quickRetry)
	waitForReady(t, plugin)
	device.unplug(errors.New("the headphones came out"))
	waitFor(t, "the ring being detached", func() bool { return !plugin.mixer.ring.pulled.Load() })
	device.plugBackIn()
	waitForReady(t, plugin)

	b := backend{plugin: plugin}
	plugin.clips[1] = &fakeClip{}
	b.Emit(&sfx.FlushChanges{Starts: []sfx.VoiceStart{{Slot: 0, Clip: 1}}})
	wantSilence(t, device)
	b.Emit(&sfx.FlushChanges{Stops: []sfx.VoiceSlot{0}, Destroys: []sfx.ClipID{1}})
	b.Emit(&sfx.FlushChanges{})
	if held := heldCount(plugin); held != 1 {
		t.Fatalf("%d released clips are held after a recovery, want the one the mixer has not passed", held)
	}
}

// The process's context is the first kernel's, so a later kernel asking for
// another rate shares it on a player of its own: it is audible, at the rate
// in force, and its ignored settings are logged once, as an error, however
// often the device is taken again.
func TestASecondKernelRecordsThatItsConfigWasIgnored(t *testing.T) {
	inForce := deviceSettings{sampleRate: 48000, bufferSize: 10 * time.Millisecond}
	device := &fakeDevice{settled: inForce}
	h, plugin := spawnConfiguredOverFake(t, device, &Config{SampleRate: 44100, DeviceRetry: time.Millisecond})
	h.ExpectError(isConfigIgnored)

	if got := waitForReady(t, plugin); got.SampleRate != 48000 || got.Latency != 10*time.Millisecond {
		t.Errorf("the device is %+v, want it at the 48000 Hz and 10ms in force", got)
	}
	device.unplug(errors.New("the headphones came out"))
	waitFor(t, "the Device going not ready", func() bool { return !isReady(plugin) })
	device.plugBackIn()
	waitForReady(t, plugin)

	want := ErrDeviceConfigIgnored{AskedSampleRate: 44100, SampleRate: 48000, AskedBufferSize: 10 * time.Millisecond, BufferSize: 10 * time.Millisecond}
	if reports := h.Reports(); len(reports) != 1 || reports[0] != error(want) {
		t.Fatalf("the reports are %v, want %v once", reports, want)
	}
}
