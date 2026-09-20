//go:build js

package internal

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dvoyni/cog/extensions/jssound"
	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/slots/sound"
)

// The fixture clip's own facts, read off the file it was cut to. They are
// written down rather than computed so that a change to the file fails here
// rather than quietly moving every assertion that counts on it - and they are
// the same three numbers nosound's and otosound's suites write down, which is
// the parity those three Adapters exist to keep.
const (
	clipFrames   = 48704
	clipRate     = 44100
	clipChannels = 2
)

// clipDuration is the fixture's length in seconds as sound will hold it:
// float32, computed the way Prepare computes it.
const clipDuration = float32(clipFrames) / float32(clipRate)

func fixture(t *testing.T) assets.Blob {
	t.Helper()
	data, err := os.ReadFile("testdata/pianoroll.ogg")
	if err != nil {
		t.Fatalf("reading the fixture clip: %v", err)
	}
	return assets.NewBlob(data)
}

// started is a Backend with its slots sized and its Ogg probe settled, which is
// the state every Emit below assumes: sound calls Voices once before any Emit,
// and a prepare waits for the probe.
func started(t *testing.T, f *audioFake, nativeOgg bool) *backend {
	t.Helper()
	b := newBackend(jssound.Config{})
	b.Voices(4)
	if settled := f.settle(nativeOgg); settled != 1 {
		t.Fatalf("the Adapter asked for %d decodes at init, want the one probe", settled)
	}
	return b
}

// resident prepares and installs the fixture, driving the decode the way sound's
// flush does: Prepare, then the callback lands, then TakePrepared, then Install.
func resident(t *testing.T, f *audioFake, b *backend, encoded assets.Blob) (sound.ClipID, sound.PreparedClip) {
	t.Helper()
	prepared, done, err := b.Prepare("token", encoded)
	if err != nil {
		t.Fatalf("Prepare refused the fixture: %v", err)
	}
	if done || prepared != nil {
		t.Fatal("Prepare answered done, and every route through this Adapter is a callback")
	}
	f.settle(true)
	completed := b.TakePrepared()
	if len(completed) != 1 {
		t.Fatalf("TakePrepared drained %d prepares, want 1", len(completed))
	}
	if completed[0].Err != nil {
		t.Fatalf("the prepare failed: %v", completed[0].Err)
	}
	if completed[0].Token != "token" {
		t.Fatalf("the completion carries token %v, want the one Prepare was handed", completed[0].Token)
	}
	id, err := b.Install(completed[0].Clip)
	if err != nil {
		t.Fatalf("Install refused the prepared Clip: %v", err)
	}
	return id, completed[0].Clip
}

// Prepare answers done=false and the decodeAudioData callback appends to the
// slice TakePrepared drains, which is the interface otosound fills with a
// goroutine and nosound fills inline - one Port, three Adapters, no branch in
// sound.
func TestPrepareDefersToDecodeAudioDataAndTakePreparedDrainsTheCallback(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)

	prepared, done, err := b.Prepare("token", fixture(t))
	if err != nil || done || prepared != nil {
		t.Fatalf("Prepare = %v, %v, %v; want nil, false, nil", prepared, done, err)
	}
	if taken := b.TakePrepared(); len(taken) != 0 {
		t.Fatalf("TakePrepared drained %d before the browser answered", len(taken))
	}

	f.settle(true)
	taken := b.TakePrepared()
	if len(taken) != 1 {
		t.Fatalf("TakePrepared drained %d after the callback, want 1", len(taken))
	}
	clip := taken[0].Clip
	if clip.SampleRate() != clipRate || clip.Channels() != clipChannels || clip.Duration() != clipDuration {
		t.Fatalf("the Clip reports %d Hz, %d channels, %v; want %d, %d, %v",
			clip.SampleRate(), clip.Channels(), clip.Duration(), clipRate, clipChannels, clipDuration)
	}
	if taken := b.TakePrepared(); len(taken) != 0 {
		t.Fatalf("TakePrepared drained %d a second time, and it clears what it returns", len(taken))
	}
}

// The four facts are the file's and never the browser's. decodeAudioData
// resamples into the context's rate, and an Adapter that reported that rate
// would make one Clip two cache entries the day the output device changed -
// which is the obligation not to bake the Device rate into a prepared Clip.
func TestAPreparedClipReportsTheFilesFactsAndNotTheContexts(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)

	_, clip := resident(t, f, b, fixture(t))
	if clip.SampleRate() == b.device.SampleRate {
		t.Fatal("the Clip reports the context's rate; the fixture is 44100 and the fake context is 48000")
	}
	if clip.SampleRate() != clipRate {
		t.Fatalf("SampleRate = %d, want the file's %d", clip.SampleRate(), clipRate)
	}
}

// Ogg support is probed once at init by decoding the embedded micro-clip, and a
// browser that refuses it decodes every Clip afterwards through the wasm
// decoder, with the samples reaching an AudioBuffer by copyToChannel.
func TestOggSupportIsProbedOnceAndTheWasmDecoderIsTheFallback(t *testing.T) {
	f := fakeAudio(t)
	b := newBackend(jssound.Config{})
	if b.support != oggUnknown {
		t.Fatal("the Adapter decided whether the browser decodes Ogg before it had asked")
	}
	if pending := f.value.Get("pending").Length(); pending != 1 {
		t.Fatalf("the Adapter asked for %d decodes at init, want the one probe", pending)
	}

	// The browser refuses the micro-clip: this is Safari on macOS before 15.4.
	f.settle(false)
	if b.support != oggWasm {
		t.Fatalf("a browser that refused the probe left support at %v, want the wasm decoder", b.support)
	}

	b.Voices(1)
	_, clip := resident(t, f, b, fixture(t))
	if pending := f.value.Get("pending").Length(); pending != 0 {
		t.Fatalf("the wasm route asked the browser to decode %d Clips", pending)
	}
	buffers := each(f.value.Get("buffers"))
	if len(buffers) == 0 {
		t.Fatal("the wasm route made no AudioBuffer")
	}
	made := buffers[len(buffers)-1]
	if made.Get("sampleRate").Int() != clipRate {
		t.Fatalf("the AudioBuffer was made at %v Hz, want the source's %d - the browser resamples at playback",
			made.Get("sampleRate"), clipRate)
	}
	if filled := made.Get("filled").Length(); filled != clipChannels {
		t.Fatalf("copyToChannel filled %d channels, want %d", filled, clipChannels)
	}
	if clip.Duration() != clipDuration {
		t.Fatalf("the wasm route reports %v, want the same %v the browser route reports", clip.Duration(), clipDuration)
	}
}

// A prepare that arrives before the probe has answered waits for it, because a
// Clip sent down the wrong route would be the probe doing nothing.
func TestAPrepareBeforeTheProbeAnswersWaitsForIt(t *testing.T) {
	f := fakeAudio(t)
	b := newBackend(jssound.Config{})
	b.Voices(1)

	if _, _, err := b.Prepare("early", fixture(t)); err != nil {
		t.Fatalf("Prepare refused a Clip named before the probe answered: %v", err)
	}
	if len(b.waiting) != 1 {
		t.Fatalf("%d prepares are waiting on the probe, want 1", len(b.waiting))
	}
	if taken := b.TakePrepared(); len(taken) != 0 {
		t.Fatalf("a prepare completed before the probe answered: %v", taken)
	}

	// The probe answers yes, which releases the waiting prepare down the
	// browser's route - and that then needs its own settle.
	f.settle(true)
	if len(b.waiting) != 0 {
		t.Fatalf("%d prepares are still waiting after the probe answered", len(b.waiting))
	}
	f.settle(true)
	if taken := b.TakePrepared(); len(taken) != 1 {
		t.Fatalf("TakePrepared drained %d, want the prepare that was waiting", len(taken))
	}
}

// A Clip the browser reads as Ogg Vorbis and then refuses is a terminal Clip
// failure with its own name, and is not retried down the wasm route: the
// fallback's trigger is the probe, and a Clip that fails after a probe that
// passed is a broken Clip.
func TestAClipTheBrowserRefusesFailsTerminally(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)

	if _, _, err := b.Prepare("token", fixture(t)); err != nil {
		t.Fatalf("Prepare refused the fixture: %v", err)
	}
	f.settle(false)

	taken := b.TakePrepared()
	if len(taken) != 1 {
		t.Fatalf("TakePrepared drained %d, want the one failure", len(taken))
	}
	var refused jssound.ErrDecodeRefused
	if !errors.As(taken[0].Err, &refused) {
		t.Fatalf("the completion carries %v, want ErrDecodeRefused", taken[0].Err)
	}
	if taken[0].Clip != nil {
		t.Fatal("a failed prepare handed sound a Clip")
	}
}

// Bytes that are not Ogg Vorbis fail in Go, before the browser is asked
// anything: the headers are read here whichever decoder would follow, because
// nothing else can report the duration, the channels, the rate or the region.
func TestBytesThatAreNotOggFailBeforeTheBrowserIsAsked(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)

	_, _, err := b.Prepare("token", assets.NewBlobFromString("this is not an ogg file at all"))
	var notOgg jssound.ErrNotOggVorbis
	if !errors.As(err, &notOgg) {
		t.Fatalf("Prepare = %v, want ErrNotOggVorbis", err)
	}
	if pending := f.value.Get("pending").Length(); pending != 0 {
		t.Fatalf("the Adapter asked the browser to decode %d non-Ogg files", pending)
	}
}

// A Voice is four gain nodes carrying sound's own Gains[src][out], wired from a
// splitter's two outputs to a merger's two inputs - and no PannerNode is ever
// made, because the W3C arithmetic is sound's and two implementations of one
// equation can disagree about the same Clip.
func TestTheGainMatrixDrivesFourGainsAndNoPannerIsEverMade(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)
	id, _ := resident(t, f, b, fixture(t))

	want := [2][2]float32{{0.25, 0.5}, {0.75, 1}}
	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: id, Params: sound.VoiceParams{Gains: want, Rate: 1},
	}}})

	if got := f.matrix(); got != want {
		t.Fatalf("the graph carries %v, want sound's own %v\n%s", got, want, strings.Join(f.events(), "\n"))
	}
	if f.panners() != 0 {
		t.Fatalf("%d PannerNodes were made, and the arithmetic is sound's", f.panners())
	}
}

// A start is start(when, offset), and a Seek is a fresh one on the same slot at
// a new offset - which is what makes a Seek sample-accurate here for the same
// reason it is on the other two Adapters.
func TestAStartCarriesItsOffsetAndASeekIsAFreshStart(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)
	id, _ := resident(t, f, b, fixture(t))

	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: id, Params: sound.VoiceParams{Rate: 1},
	}}})
	if sources := f.sources(); len(sources) != 1 {
		t.Fatalf("a Play made %d source nodes, want 1", len(sources))
	} else if offset := sources[0].Get("started").Get("offset").Float(); offset != 0 {
		t.Fatalf("a Play started at %v, want the head", offset)
	}

	// A Seek arrives as a VoiceStart with an Offset on a slot that is already
	// live, and sound sends no stop with it.
	f.advance(0.1)
	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: id, Offset: 400 * time.Millisecond, Params: sound.VoiceParams{Rate: 1},
	}}})

	sources := f.sources()
	if len(sources) != 2 {
		t.Fatalf("a Seek made %d source nodes in total, want 2 - it cannot restart the first", len(sources))
	}
	if offset := sources[1].Get("started").Get("offset").Float(); offset != 0.4 {
		t.Fatalf("the Seek started at %v, want 0.4", offset)
	}
	if !sources[0].Get("stopped").Truthy() {
		t.Fatal("the Voice the Seek replaced was left playing")
	}
}

// Rate is playbackRate, set outright: a step in it is a step in the derivative
// and never in the amplitude, so it is a pitch change rather than the click
// declicking exists for. otosound sets it the same way.
func TestRateIsPlaybackRate(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)
	id, _ := resident(t, f, b, fixture(t))

	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: id, Params: sound.VoiceParams{Rate: 0.5},
	}}})
	source := f.sources()[0]
	if rate := source.Get("playbackRate").Get("value").Float(); rate != 0.5 {
		t.Fatalf("playbackRate = %v, want 0.5", rate)
	}

	f.advance(0.1)
	b.Emit(&sound.Batch{Updates: []sound.VoiceUpdate{{Slot: 0, Params: sound.VoiceParams{Rate: 2}}}})
	if rate := source.Get("playbackRate").Get("value").Float(); rate != 2 {
		t.Fatalf("playbackRate after an update = %v, want 2", rate)
	}
}

// Paused stops the source rather than zeroing its gain, and a resume starts a
// fresh one where the pause left the playhead. Zeroing a gain does not suspend:
// a resident buffer started with start() keeps advancing whatever its gain is,
// so a resume would land wherever the wall clock had reached.
func TestPausedStopsTheSourceAndResumingStartsItWhereItStopped(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)
	id, _ := resident(t, f, b, fixture(t))

	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: id, Params: sound.VoiceParams{Gains: [2][2]float32{{1, 1}}, Rate: 1},
	}}})

	// A quarter of a second of world time goes by, and then the engine pauses.
	f.advance(0.25)
	b.Emit(&sound.Batch{Updates: []sound.VoiceUpdate{{
		Slot: 0, Params: sound.VoiceParams{Gains: [2][2]float32{{1, 1}}, Rate: 1, Paused: true},
	}}})
	if !f.sources()[0].Get("stopped").Truthy() {
		t.Fatal("a pause left the source playing, and a silenced source still advances")
	}

	// Two seconds pass while paused; nothing of them is heard, and nothing of
	// them moves the playhead.
	f.advance(2)
	b.Emit(&sound.Batch{Updates: []sound.VoiceUpdate{{
		Slot: 0, Params: sound.VoiceParams{Gains: [2][2]float32{{1, 1}}, Rate: 1},
	}}})

	sources := f.sources()
	if len(sources) != 2 {
		t.Fatalf("a resume made %d sources in total, want 2", len(sources))
	}
	if offset := sources[1].Get("started").Get("offset").Float(); offset < 0.2499 || offset > 0.2501 {
		t.Fatalf("the resume started at %v, want the 0.25 the pause suspended at", offset)
	}
}

// A looping Voice sets loopStart and loopEnd from the Clip's own Loop Region,
// and a Clip that declares none loops to the granule end rather than to the
// AudioBuffer's end - which is why the headers are parsed in Go at all.
func TestALoopingVoiceSetsLoopStartAndLoopEnd(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)
	tagged := retagged(t, fixture(t), "LOOPSTART=11025", "LOOPLENGTH=22050")
	id, _ := resident(t, f, b, tagged)

	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: id, Loop: true, Params: sound.VoiceParams{Rate: 1},
	}}})

	source := f.sources()[0]
	if !source.Get("loop").Bool() {
		t.Fatal("a looping Voice did not set loop on its source")
	}
	wantStart, wantEnd := float64(seconds(11025)), float64(seconds(33075))
	if start := source.Get("loopStart").Float(); start != wantStart {
		t.Fatalf("loopStart = %v, want %v", start, wantStart)
	}
	if end := source.Get("loopEnd").Float(); end != wantEnd {
		t.Fatalf("loopEnd = %v, want %v", end, wantEnd)
	}
}

func TestAnUntaggedLoopingVoiceLoopsToTheGranuleEnd(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)
	id, _ := resident(t, f, b, fixture(t))

	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: id, Loop: true, Params: sound.VoiceParams{Rate: 1},
	}}})

	source := f.sources()[0]
	if start := source.Get("loopStart").Float(); start != 0 {
		t.Fatalf("loopStart = %v, want the head", start)
	}
	if end := source.Get("loopEnd").Float(); float32(end) != clipDuration {
		t.Fatalf("loopEnd = %v, want the granule end %v and never the buffer's", end, clipDuration)
	}
}

// Declicking is setTargetAtTime over the browser's own render quantum: 128
// frames at the context's rate, which is the only block rate this Adapter has
// and the reason declicking is the Adapter's job rather than sound's.
func TestDeclickingRampsOverTheBrowsersOwnQuantum(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)
	id, _ := resident(t, f, b, fixture(t))

	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: id, Params: sound.VoiceParams{Gains: [2][2]float32{{1, 1}}, Rate: 1},
	}}})
	f.advance(0.016)
	b.Emit(&sound.Batch{Updates: []sound.VoiceUpdate{{
		Slot: 0, Params: sound.VoiceParams{Gains: [2][2]float32{{0.5, 0.5}}, Rate: 1},
	}}})

	wantConstant := float64(renderQuantum) / f.context().Get("sampleRate").Float()
	if got := f.value.Get("lastTimeConstant").Float(); got != wantConstant {
		t.Fatalf("the ramp's time constant is %v, want the quantum's %v", got, wantConstant)
	}
	if got := f.matrix(); got[0][0] != 0.5 {
		t.Fatalf("the gain moved to %v, want the target 0.5", got[0][0])
	}
	if !hasEvent(f, "setTargetAtTime 0.5") {
		t.Fatalf("no ramp to the new target was scheduled:\n%s", strings.Join(f.events(), "\n"))
	}
}

// A stop ramps to silence and stops the source behind the ramp, which is why
// VoiceEndedEvent precedes the silence by a few milliseconds.
func TestAStopRampsToSilenceAndStopsTheSourceBehindIt(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)
	id, _ := resident(t, f, b, fixture(t))

	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: id, Params: sound.VoiceParams{Gains: [2][2]float32{{1, 1}}, Rate: 1},
	}}})
	f.advance(0.016)
	b.Emit(&sound.Batch{Stops: []sound.VoiceSlot{0}})

	source := f.sources()[0]
	stopped := source.Get("stopped")
	if !stopped.Truthy() {
		t.Fatal("a stop left the source playing")
	}
	tail := float64(declickTails) * (float64(renderQuantum) / 48000)
	if when := stopped.Get("when").Float(); when < 0.016+tail-1e-9 {
		t.Fatalf("the source stops at %v, before the ramp behind it finishes at %v", when, 0.016+tail)
	}
	if got := f.matrix(); got != [2][2]float32{} {
		t.Fatalf("the gains ended at %v, want silence", got)
	}
}

// A play and a stop in one tick is audible for zero samples, which is what the
// seam promises: the two land at the same instant of the context clock, so the
// ramp that would otherwise declick the stop has nothing to declick.
func TestAPlayAndAStopInOneTickIsAudibleForZeroSamples(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)
	id, _ := resident(t, f, b, fixture(t))

	// The clock does not move between the two, which is what "in one tick" means
	// once a Voice's whole life is scheduled against the context clock: sound
	// records the play and the stop in one tick, and the stop reaches the
	// Adapter in the batch after the start it cancels.
	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: id, Params: sound.VoiceParams{Gains: [2][2]float32{{1, 1}}, Rate: 1},
	}}})
	b.Emit(&sound.Batch{Stops: []sound.VoiceSlot{0}})

	source := f.sources()[0]
	if when := source.Get("stopped").Get("when").Float(); when != source.Get("started").Get("when").Float() {
		t.Fatalf("the source stops at %v having started at %v; a Voice stopped where it started is heard for no samples",
			when, source.Get("started").Get("when"))
	}
}

// A steal is a stop and a start on one slot in one batch. The stop is applied
// first, so the Voice being stolen ramps away on its own gain nodes while the
// Voice that took its slot plays at full gain through its own - which is what a
// per-start graph buys and a shared gain stage could not.
func TestAStealRampsTheVictimAwayWhileTheThiefPlays(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)
	id, _ := resident(t, f, b, fixture(t))

	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: id, Params: sound.VoiceParams{Gains: [2][2]float32{{1, 1}}, Rate: 1},
	}}})
	f.advance(0.016)
	b.Emit(&sound.Batch{
		Stops: []sound.VoiceSlot{0},
		Starts: []sound.VoiceStart{{
			Slot: 0, Clip: id, Params: sound.VoiceParams{Gains: [2][2]float32{{0.8, 0.2}}, Rate: 1},
		}},
	})

	sources := f.sources()
	if len(sources) != 2 {
		t.Fatalf("a steal made %d sources, want the victim's and the thief's", len(sources))
	}
	if !sources[0].Get("stopped").Truthy() {
		t.Fatal("the victim was left playing")
	}
	if sources[1].Get("stopped").Truthy() {
		t.Fatal("the thief was stopped by the victim's stop; they must not share a gain stage")
	}
	if got := f.matrix(); got != [2][2]float32{{0.8, 0.2}} {
		t.Fatalf("the thief's gains are %v, want its own %v", got, [2][2]float32{{0.8, 0.2}})
	}
}

// A release drops the Clip from the Adapter's table, and the stops before it in
// the same batch have already told every source on it to stop. Nothing frees a
// buffer by hand: the browser holds an AudioBuffer for as long as a source is
// still playing it, and syscall/js drops the reference when the Go value goes.
func TestADestroyDropsTheClipBehindTheStopsThatPrecedeIt(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)
	id, _ := resident(t, f, b, fixture(t))

	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{
		Slot: 0, Clip: id, Params: sound.VoiceParams{Gains: [2][2]float32{{1, 1}}, Rate: 1},
	}}})
	f.advance(0.016)
	b.Emit(&sound.Batch{Stops: []sound.VoiceSlot{0}, Destroys: []sound.ClipID{id}})

	if _, held := b.clips[id]; held {
		t.Fatal("a destroyed Clip is still in the table")
	}
	if !f.sources()[0].Get("stopped").Truthy() {
		t.Fatal("the destroy arrived without the stop that precedes it having been applied")
	}
	// A start on the released id is a start on a Clip that is not there, and it
	// makes no sound rather than panicking.
	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{Slot: 1, Clip: id, Params: sound.VoiceParams{Rate: 1}}}})
	if len(f.sources()) != 1 {
		t.Fatal("a start on a released Clip made a source node")
	}
}

// Device.Ready is false until the context resumes, and the resume comes from the
// player's first gesture - which the Adapter listens for itself, because sound's
// Port has no verb for it and a game is given Ready and nothing else.
func TestTheDeviceIsNotReadyUntilAGestureResumesTheContext(t *testing.T) {
	f := fakeAudio(t)
	b := newBackend(jssound.Config{})
	b.Voices(1)

	if b.Device().Ready {
		t.Fatal("the Device is ready before any gesture, and a browser suspends a fresh context")
	}
	if name := b.Device().Name; name != string(jssound.Name) {
		t.Fatalf("the Device names %q, want %q", name, jssound.Name)
	}

	f.gesture("pointerdown")

	device := b.Device()
	if !device.Ready {
		t.Fatalf("the Device is still not ready after a gesture:\n%s", strings.Join(f.events(), "\n"))
	}
	if device.SampleRate != 48000 || device.Channels != outChannels {
		t.Fatalf("the Device reports %d Hz and %d channels, want 48000 and %d",
			device.SampleRate, device.Channels, outChannels)
	}
	if device.Latency < 29*time.Millisecond || device.Latency > 31*time.Millisecond {
		t.Fatalf("Latency = %v, want the context's base plus output latency, about 30ms", device.Latency)
	}

	// The listener comes off the page once it has fired: a second gesture must
	// not ask a running context to resume again.
	before := len(f.events())
	f.gesture("pointerdown")
	for _, event := range f.events()[before:] {
		if event == "resume" {
			t.Fatal("the gesture listener was still installed after it had fired")
		}
	}
}

// One AudioContext per Engine. Two would be two devices in one game, and the
// second's Voices would be inaudible against the first's in a way nothing above
// the seam could see.
func TestOneEngineOpensExactlyOneContext(t *testing.T) {
	f := fakeAudio(t)
	b := started(t, f, true)
	id, _ := resident(t, f, b, fixture(t))
	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{Slot: 0, Clip: id, Params: sound.VoiceParams{Rate: 1}}}})

	if contexts := f.value.Get("contexts").Length(); contexts != 1 {
		t.Fatalf("the Engine opened %d AudioContexts, want 1", contexts)
	}
}

// The latency hint reaches the context, and a zero one is interactive rather
// than a literal zero seconds - which is a real distinction: zero seconds is not
// a latency any browser can give, and asking for it is asking for nothing.
func TestTheLatencyHintReachesTheContext(t *testing.T) {
	f := fakeAudio(t)

	newBackend(jssound.Config{})
	newBackend(jssound.Config{}.WithLatencyHint(40 * time.Millisecond))

	contexts := each(f.value.Get("contexts"))
	if len(contexts) != 2 {
		t.Fatalf("%d contexts were opened, want one per Engine", len(contexts))
	}
	if hint := contexts[0].Get("options").Get("latencyHint").String(); hint != "interactive" {
		t.Fatalf("a zero LatencyHint reached the context as %q, want interactive", hint)
	}
	if hint := contexts[1].Get("options").Get("latencyHint").Float(); hint != 0.04 {
		t.Fatalf("a 40ms LatencyHint reached the context as %v, want 0.04 seconds", hint)
	}
}

// A page with no Web Audio at all registers, prepares, installs and runs - which
// is jssound behaving exactly as nosound does. A Voice on such a Clip exists,
// advances and ends on schedule, and is simply never heard.
func TestWithNoWebAudioTheAdapterBehavesAsNosoundDoes(t *testing.T) {
	noWebAudio(t)
	b := newBackend(jssound.Config{})
	b.Voices(2)

	if b.failure == nil {
		t.Fatal("a page with no AudioContext recorded no failure to report")
	}
	if b.Device().Ready {
		t.Fatal("the Device is ready with no Web Audio at all")
	}

	if _, done, err := b.Prepare("token", fixture(t)); err != nil || done {
		t.Fatalf("Prepare = %v, %v; want the same deferred answer every route gives", done, err)
	}
	taken := b.TakePrepared()
	if len(taken) != 1 || taken[0].Err != nil {
		t.Fatalf("TakePrepared drained %v, want the one header-only Clip", taken)
	}
	clip := taken[0].Clip
	if clip.Duration() != clipDuration || clip.Channels() != clipChannels || clip.SampleRate() != clipRate {
		t.Fatalf("the Clip reports %v, %d, %d; want the file's facts so a Voice on it still ends on time",
			clip.Duration(), clip.Channels(), clip.SampleRate())
	}
	id, err := b.Install(clip)
	if err != nil || id == 0 {
		t.Fatalf("Install = %v, %v; a Voice waiting on a Clip starts when that Clip installs", id, err)
	}
	// Every operation is accepted and none is audible.
	b.Emit(&sound.Batch{Starts: []sound.VoiceStart{{Slot: 0, Clip: id, Params: sound.VoiceParams{Rate: 1}}}})
	b.Emit(&sound.Batch{Stops: []sound.VoiceSlot{0}, Destroys: []sound.ClipID{id}})
	if _, held := b.clips[id]; held {
		t.Fatal("a release left the Clip in the table on a page with no Device")
	}
}

// Install refuses a prepared value this Adapter did not make, rather than
// minting an id for something no start could ever resolve.
func TestInstallRefusesAClipThisAdapterDidNotMake(t *testing.T) {
	fakeAudio(t)
	b := newBackend(jssound.Config{})
	if _, err := b.Install(nil); !errors.Is(err, errNotOurClip) {
		t.Fatalf("Install = %v, want errNotOurClip", err)
	}
}

// hasEvent reports whether the fake recorded a line containing want.
func hasEvent(f *audioFake, want string) bool {
	for _, event := range f.events() {
		if strings.Contains(event, want) {
			return true
		}
	}
	return false
}
