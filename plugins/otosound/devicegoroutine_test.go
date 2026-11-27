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
	clipID, _ := installClip(t, b)
	b.Emit(&sfx.FlushChanges{Starts: []sfx.VoiceStart{{Slot: 0, Clip: clipID}}})
	wantSilence(t, device)

	b.Emit(&sfx.FlushChanges{Stops: []sfx.VoiceSlot{0}, Destroys: []sfx.ClipID{clipID}})
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
	clipID, _ := installClip(t, b)
	b.Emit(&sfx.FlushChanges{Starts: []sfx.VoiceStart{{Slot: 0, Clip: clipID}}})
	wantSilence(t, device)
	b.Emit(&sfx.FlushChanges{Stops: []sfx.VoiceSlot{0}, Destroys: []sfx.ClipID{clipID}})
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

// loseDevice unplugs device and waits until plugin's ring is detached and
// emptied.
func loseDevice(t *testing.T, device *fakeDevice, plugin *Plugin) {
	t.Helper()
	device.unplug(errors.New("the headphones came out"))
	waitFor(t, "the ring being detached and emptied", func() bool {
		return !plugin.mixer.ring.pulled.Load() && pendingCount(plugin.mixer.ring) == 0
	})
}

// A loss empties the ring in the mixer's place, once its player has stopped:
// what it had published and the mixer never applied is dropped, and nothing
// in it keeps a clip alive through the loss, however long the retry fails to
// take the device again.
func TestALostDevicesRingHoldsNoClip(t *testing.T) {
	device := &fakeDevice{}
	_, plugin := spawnConfiguredOverFake(t, device, quickRetry)
	waitForReady(t, plugin)
	b := backend{plugin: plugin}
	clipID, _ := installClip(t, b)
	b.Emit(&sfx.FlushChanges{Starts: []sfx.VoiceStart{{Slot: 0, Clip: clipID}}})

	device.refuseOpens(errors.New("the context recorded an error"))
	loseDevice(t, device, plugin)
	b.Emit(&sfx.FlushChanges{Starts: []sfx.VoiceStart{{Slot: 1, Clip: clipID}}})

	ring := plugin.mixer.ring
	for index, changes := range append(slices.Clone(ring.slots), ring.staging) {
		for _, operation := range changes.operations[:cap(changes.operations)] {
			if operation.clip != nil {
				t.Fatalf("change list %d still holds a clip after the loss", index)
			}
		}
	}
}

// A device that comes back plays only what was emitted once it was ready:
// the starts published before the loss, and those emitted while it was
// gone, are dropped, since sfx restates every live voice when the device
// turns ready, and none of them reaches the first drain after the attach.
func TestNoStaleStartReachesTheFirstDrainAfterADeviceComesBack(t *testing.T) {
	device := &fakeDevice{}
	_, plugin := spawnConfiguredOverFake(t, device, quickRetry)
	waitForReady(t, plugin)
	b := backend{plugin: plugin}
	emitStart(b, 1)
	loseDevice(t, device, plugin)
	emitStart(b, 2)

	device.plugBackIn()
	waitForReady(t, plugin)
	emitStart(b, 3)

	applied := drained(plugin.mixer.ring)
	if len(applied) != 1 || applied[0].kind != operationKindStart || applied[0].clipID != 3 {
		t.Fatalf("the first drain after the device came back applied %+v, want only the start on clip 3", applied)
	}
}

// The flush emitting as fast as it can while the device is lost and comes
// back, again and again: under -race, the device goroutine emptying the ring
// in the mixer's place never touches a change list the flush side is
// swapping or recording into.
func TestEmitAndALossRunningTogetherShareTheRingSafely(t *testing.T) {
	device := &fakeDevice{}
	_, plugin := spawnConfiguredOverFake(t, device, quickRetry)
	waitForReady(t, plugin)
	b := backend{plugin: plugin}
	done := make(chan struct{})
	flushed := make(chan struct{})
	go func() {
		defer close(flushed)
		for clipID := sfx.ClipID(1); ; clipID++ {
			select {
			case <-done:
				return
			default:
			}
			emitStart(b, clipID)
		}
	}()

	for range 20 {
		loseDevice(t, device, plugin)
		device.plugBackIn()
		waitForReady(t, plugin)
	}
	close(done)
	<-flushed
}

// The device goroutine stores the Device, records its first failure and logs
// it through the kernel's Logger while the flush reads the Device and logs
// reports of its own through the same Logger: a machine with no output
// device, asked again every millisecond under a game whose clip fails and is
// released every step, gains a device, and then loses it and gets it back,
// again and again. The absence is reported once, and the losses never. Run
// under -race, it fails with the Device stored by a plain write, or with the
// mutex taken out of the Logger's record of what it logged once.
func TestTheDeviceGoroutineLogsAndStoresTheDeviceWhileTheFlushReadsIt(t *testing.T) {
	const brokenPath = "sounds/broken.ogg"
	device := &fakeDevice{missing: errNoOutputDevice}
	h, plugin := spawnConfiguredOverFake(t, device, quickRetry)
	h.ExpectError(func(err error) bool {
		_, failed := errors.AsType[sfx.ErrClipFailed](err)
		return failed
	})
	h.Mount(brokenPath, []byte("not an Ogg Vorbis stream"))
	// Each step releases the clip that failed, which forgets its report, and
	// plays it again, so the next step's flush reports it again.
	step := func() {
		h.Record(func(queue sfx.Queue) {
			queue.Release(brokenPath)
			queue.Play(brokenPath, 0, sfx.VoiceOptions{})
		})
		h.Step()
	}
	stepUntil := func(what string, condition func() bool) {
		t.Helper()
		waitFor(t, what, func() bool {
			if condition() {
				return true
			}
			step()
			return false
		})
	}

	for range 41 {
		step()
		time.Sleep(time.Millisecond)
	}
	device.installDevice()
	stepUntil("the Device turning ready", func() bool { return isReady(plugin) })
	for range 10 {
		device.unplug(errors.New("the headphones came out"))
		stepUntil("the Device going not ready", func() bool { return !isReady(plugin) })
		device.plugBackIn()
		stepUntil("the Device turning ready again", func() bool { return isReady(plugin) })
	}

	wantOneUnavailableWarning(t, h, errNoOutputDevice)
	// Every report is the clip's failure: the harness fails the test on any
	// other.
	if failures := len(h.Reports()); failures < 40 {
		t.Fatalf("the flush reported the clip's failure %d times, want one each step it was played again", failures)
	}
}

// Destroy stops the device goroutine and waits for it before it closes the
// player, so nothing of the goroutine's reaches the device, or the kernel's
// Logger, once the kernel is gone: the fake device's log of its calls is
// unguarded, as oto's player is, and the close is the last call it holds
// however long after Destroy it is read. Run under -race, it fails with the
// wait taken out of the goroutine's stop.
func TestDestroyStopsTheDeviceGoroutineBeforeItClosesThePlayer(t *testing.T) {
	device := &fakeDevice{}
	t.Run("the kernel, destroyed as it ends", func(t *testing.T) {
		_, plugin := spawnConfiguredOverFake(t, device, quickRetry)
		waitForReady(t, plugin)
		// The goroutine polls the playing device every DeviceRetry until
		// Destroy stops it.
		time.Sleep(10 * quickRetry.DeviceRetry)
	})

	calls := device.calls
	if len(calls) == 0 || calls[len(calls)-1] != "close" {
		t.Fatalf("the device's calls are %v, want them to end with Destroy's close", calls)
	}
	time.Sleep(10 * quickRetry.DeviceRetry)
	if later := device.calls[len(calls):]; len(later) != 0 {
		t.Fatalf("the device was called %v after Destroy closed it", later)
	}
}
