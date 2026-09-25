//go:build !js

package internal

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/slots/sound"
)

// The fixture clip's own facts, read off the file it was cut to and written
// down rather than computed, so that a change to the file fails here rather
// than quietly moving everything downstream of it. They are the same three
// numbers nosound's suite writes down, which is the point: both Adapters report
// one Clip identically, so a game tested under nosound behaves the same under
// otosound.
const (
	clipFrames   = 48704
	clipRate     = 44100
	clipChannels = 2
)

const clipDuration = float32(clipFrames) / float32(clipRate)

func fixture(t *testing.T) assets.Blob {
	t.Helper()
	data, err := os.ReadFile("testdata/pianoroll.ogg")
	if err != nil {
		t.Fatalf("reading the fixture clip: %v", err)
	}
	return assets.NewBlob(data)
}

// newTestBackend composes the Adapter against a device that opens nothing, and
// takes it down again afterwards so that nothing is left running.
func newTestBackend(t *testing.T, hardware *fakeAudio) *backend {
	t.Helper()
	b := newBackend(Config{}, hardware)
	t.Cleanup(b.stop)
	b.Voices(8)
	return b
}

// waitPrepared drains TakePrepared until the goroutine Prepare spawned has
// finished, which is what sound's flush does a tick at a time.
func waitPrepared(t *testing.T, b *backend) sound.Prepared {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if taken := b.TakePrepared(); len(taken) > 0 {
			if len(taken) != 1 {
				t.Fatalf("TakePrepared drained %d completions, want 1", len(taken))
			}
			return taken[0]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the prepare never completed")
	return sound.Prepared{}
}

// Prepare answers done=false and decodes on a goroutine, so neither the tick
// nor the device thread ever carries a decode. The completion lands under the
// token sound handed it, which is how it finds the entry it belongs to.
func TestPrepareDecodesOffTheTickAndTheCompletionArrivesUnderItsToken(t *testing.T) {
	b := newTestBackend(t, &fakeAudio{})
	token := struct{ name string }{"pianoroll"}

	prepared, done, err := b.Prepare(token, fixture(t))
	if err != nil {
		t.Fatalf("Prepare failed outright: %v", err)
	}
	if done || prepared != nil {
		t.Fatal("Prepare answered done, and otosound decodes on a goroutine")
	}

	completed := waitPrepared(t, b)
	if completed.Token != any(token) {
		t.Fatalf("the completion carries token %v, want the one Prepare was handed", completed.Token)
	}
	if completed.Err != nil {
		t.Fatalf("decoding the fixture clip: %v", completed.Err)
	}
	if got := completed.Clip.SampleRate(); got != clipRate {
		t.Fatalf("SampleRate = %d, want the file's %d", got, clipRate)
	}
	if got := completed.Clip.Channels(); got != clipChannels {
		t.Fatalf("Channels = %d, want %d", got, clipChannels)
	}
	if got := completed.Clip.Duration(); got != clipDuration {
		t.Fatalf("Duration = %v, want %v", got, clipDuration)
	}
	if completed.Clip.LoopRegion().Present() {
		t.Fatal("a Clip with no LOOPSTART tag reported a Loop Region")
	}
}

// Prepare converts a Clip to the device rate once, off both threads, so the
// Mixer's Rate is pitch alone and no base rate is ever resampled on the device
// thread. What it reports is still the file's rate, so nothing above the seam
// can tell - and so a Device change costs the Clip cache nothing.
func TestPrepareConvertsToTheDeviceRateWithoutReportingIt(t *testing.T) {
	b := newTestBackend(t, &fakeAudio{})

	b.Prepare(nil, fixture(t))
	completed := waitPrepared(t, b)

	clip, ok := completed.Clip.(*clipData)
	if !ok {
		t.Fatalf("Prepare produced a %T", completed.Clip)
	}
	if clip.rate != defaultSampleRate {
		t.Fatalf("the samples are at %d Hz, want the device's %d", clip.rate, defaultSampleRate)
	}
	if clip.SampleRate() != clipRate {
		t.Fatalf("the Clip reports %d Hz, want the file's %d", clip.SampleRate(), clipRate)
	}
	want := clipFrames * defaultSampleRate / clipRate
	if clip.frames < want-2 || clip.frames > want+2 {
		t.Fatalf("the converted Clip is %d frames, want about %d", clip.frames, want)
	}
	if len(clip.samples) != clip.frames*clip.channels {
		t.Fatalf("%d samples for %d frames of %d channels", len(clip.samples), clip.frames, clip.channels)
	}
}

// A Clip failure is terminal and is reported as an error rather than as a Clip
// of no duration, because a zero duration makes the playhead fiction and
// silently removes ReasonFinished from every Voice that names it.
func TestPrepareRefusesBytesThatAreNotOggVorbis(t *testing.T) {
	b := newTestBackend(t, &fakeAudio{})

	b.Prepare(nil, assets.NewBlob([]byte("this is not an ogg stream")))
	completed := waitPrepared(t, b)

	var refused ErrNotOggVorbis
	if !errors.As(completed.Err, &refused) {
		t.Fatalf("bytes that are not Ogg Vorbis were prepared: err = %v", completed.Err)
	}
	if completed.Clip != nil {
		t.Fatal("a failed prepare handed back a Clip, which sound would install")
	}
}

// Install mints the id on sound's tick, where a Destroy is guaranteed to pair
// with it, and an id is never zero and never repeats.
func TestInstallMintsANonZeroIdThatNeverRepeats(t *testing.T) {
	b := newTestBackend(t, &fakeAudio{})
	clip := &clipData{samples: []float32{0}, frames: 1, channels: 1, rate: defaultSampleRate}

	first, err := b.Install(clip)
	if err != nil {
		t.Fatalf("installing a prepared Clip: %v", err)
	}
	second, err := b.Install(clip)
	if err != nil {
		t.Fatalf("installing a prepared Clip: %v", err)
	}
	if first == 0 || second == 0 || first == second {
		t.Fatalf("Install minted %d and %d", first, second)
	}
}

// This is "the Mixer never frees" as a test. A destroy travels inside the batch
// after the stops that precede it, and the last reference on the tick side goes
// only once the applied counter has passed the batch that carried it - so the
// Mixer can be mid-copy out of the Clip's samples for as long as it takes and
// never reads freed memory, while the tick never waits.
func TestAReleasedClipIsHeldUntilTheMixerHasPassedTheBatchThatDestroyedIt(t *testing.T) {
	b := newTestBackend(t, &fakeAudio{})
	clip := constantClip(4*testBlock, 1, 1)
	id, err := b.Install(clip)
	if err != nil {
		t.Fatalf("installing a prepared Clip: %v", err)
	}

	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: id, Params: sound.VoiceParams{Gains: mono(1), Rate: 1},
	}}})
	render(t, b.mixer, 1)
	if b.mixer.voices[0].clip != clip {
		t.Fatal("the Mixer is not reading the Clip that was installed")
	}

	b.Emit(&sound.Batch{Stops: []sound.VoiceSlot{0}, Destroys: []sound.ClipID{id}})
	if len(b.pending) != 1 {
		t.Fatalf("%d releases are held, want the one the Mixer has not passed", len(b.pending))
	}
	b.Emit(&sound.Batch{})
	if len(b.pending) != 1 {
		t.Fatal("a release was freed before the Mixer applied the batch that destroyed it")
	}

	render(t, b.mixer, 1)
	b.Emit(&sound.Batch{})
	if len(b.pending) != 0 {
		t.Fatalf("%d releases are still held after the Mixer passed them", len(b.pending))
	}
	if _, live := b.clips[id]; live {
		t.Fatal("the destroyed Clip is still in the table")
	}
}

// Until a Mixer has pulled a block there is no device thread to be mid-copy out
// of anything, so a release is free at once. Without this, otosound under a
// Device that never opened would retain every Clip a game ever released, which
// is not "behaves exactly as nosound does".
func TestAReleasedClipIsFreedAtOnceWhileNoMixerHasEverPulled(t *testing.T) {
	b := newTestBackend(t, &fakeAudio{})
	id, err := b.Install(constantClip(64, 1, 1))
	if err != nil {
		t.Fatalf("installing a prepared Clip: %v", err)
	}

	b.Emit(&sound.Batch{Destroys: []sound.ClipID{id}})
	b.Emit(&sound.Batch{})

	if len(b.pending) != 0 {
		t.Fatalf("%d releases are held against a counter nothing will ever move", len(b.pending))
	}
}

// Every play is a table entry and a batch entry, both of which were made once,
// so an Adapter allocates nothing per play.
func TestAPlayAllocatesNothingInTheAdapter(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	b := newTestBackend(t, &fakeAudio{})
	id, err := b.Install(constantClip(4*testBlock, 1, 1))
	if err != nil {
		t.Fatalf("installing a prepared Clip: %v", err)
	}
	batch := &sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: id, Params: sound.VoiceParams{Gains: mono(1), Rate: 1},
	}}}
	buf := make([]byte, testBlock*bytesPerFrame)
	b.Emit(batch)
	b.mixer.Read(buf)

	allocations := testing.AllocsPerRun(200, func() {
		b.Emit(batch)
		b.mixer.Read(buf)
	})

	if allocations != 0 {
		t.Fatalf("a play allocated %v times in the Adapter", allocations)
	}
}

// Registration never fails on a Device, and Device is a field read: it answers
// not-ready until one opens and says so afterwards, and a game reads it rather
// than handling it.
func TestDeviceIsNotReadyUntilOneOpens(t *testing.T) {
	hardware := &fakeAudio{buffer: defaultBufferSize}
	b := newBackend(Config{}, hardware)
	t.Cleanup(b.stop)

	if device := b.Device(); device.Ready || device.Name != string(Name) {
		t.Fatalf("before Voices the Device is %+v, want a named, not-ready one", device)
	}

	b.Voices(8)
	device := waitReady(t, b)
	if device.SampleRate != defaultSampleRate || device.Channels != outChannels {
		t.Fatalf("the Device reports %d Hz x%d", device.SampleRate, device.Channels)
	}
	if device.Latency != defaultBufferSize {
		t.Fatalf("the Device reports a latency of %v, want the buffer actually in force", device.Latency)
	}
}

// A Device that could never be opened is recorded for the tick handler to
// report once, and otosound then behaves exactly as nosound does: it accepts
// everything, plays nothing, and the game runs.
func TestADeviceThatCanNotBeOpenedIsRecordedOnceAndTheGameRunsOn(t *testing.T) {
	boom := errors.New("no audio endpoint")
	b := newTestBackend(t, &fakeAudio{openErr: boom})

	failure := b.failure.Load()
	if failure == nil || !errors.Is(failure.err, boom) {
		t.Fatalf("the failed open recorded %v", failure)
	}
	if b.Device().Ready {
		t.Fatal("the Device reports Ready with nothing open")
	}

	id, err := b.Install(constantClip(64, 1, 1))
	if err != nil {
		t.Fatalf("installing a Clip with no Device: %v", err)
	}
	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{Slot: 0, Clip: id}}})
}

// The context is per process and the first composition owns it, so a second
// Engine shares it and takes its own player. Both are audible; what is recorded
// is that this one's Config was not the one in force.
func TestASecondEngineRecordsThatItsConfigWasIgnored(t *testing.T) {
	hardware := &fakeAudio{ignored: true, rate: 44100, buffer: 20 * time.Millisecond}
	b := newTestBackend(t, hardware)

	ignored := b.ignored.Load()
	if ignored == nil {
		t.Fatal("a second Engine's ignored Config was not recorded")
	}
	if ignored.SampleRate != 44100 || ignored.AskedSampleRate != defaultSampleRate {
		t.Fatalf("the report says %+v", *ignored)
	}
	device := waitReady(t, b)
	if device.SampleRate != 44100 {
		t.Fatalf("the Device reports %d Hz, want the rate actually in force", device.SampleRate)
	}
	if b.rate != 44100 {
		t.Fatalf("Clips are being prepared at %d Hz against a device running at 44100", b.rate)
	}
}

func waitReady(t *testing.T, b *backend) sound.Device {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if device := b.Device(); device.Ready {
			return device
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the Device never became ready")
	return sound.Device{}
}
