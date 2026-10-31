package internal

import (
	"errors"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/libs/assets"
	"github.com/dvoyni/cog/libs/m"
)

// drum is a second Clip, so a test can release one and keep the other.
const drum = "drum.ogg"

// twoClips is clipBytes with the second Clip beside the first.
var twoClips = fstest.MapFS{
	bell: {Data: []byte("not really ogg, and the fixture does not care")},
	drum: {Data: []byte("nor is this one")},
}

// The guarantee, stated the only way it can be checked from outside: after the
// flush in which a release is recorded, no Voice is playing that Clip.
//
// The Voice is looping, which is the sequence the retired deferral got wrong -
// a looping ambience's Voice never ends by itself, so a release that waited for
// it would wait forever, the memory would never come back and nothing would be
// reported. It is checked in three places at once, because "no Voice is playing
// it" is a claim about all three: the live view holds nothing, an ending fired
// saying why, and the batch that left in that same flush carries the stop.
//
// And the destroy is in that same batch, after the stop. Batch.Destroys is the
// only route a release takes, so the stop that drops the Mixer's reference and
// the destroy that frees the Clip are one tick's worth of operations, in that
// order, and the Mixer never applies a destroy for a Clip it is still mixing.
func TestAfterTheFlushThatRecordsAReleaseNoVoiceIsPlayingThatClip(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 30, channels: 2, rate: 48000})
	h := newHarness(t, backend, Config{}, twoClips)

	ambience := h.play(ClipWithResource(bell), 0, Params{Loop: m.Some(true)})
	second := h.play(ClipWithResource(bell), 0, Params{Loop: m.Some(true)})
	kept := h.play(ClipWithResource(drum), 0, Params{Loop: m.Some(true)})
	h.tick()
	if got := h.probe(ambience); !got.Found || got.Live != 3 {
		t.Fatalf("the three Voices did not start: found=%v live=%d", got.Found, got.Live)
	}
	before := len(h.backend.emitted())

	h.record(func(queue *Queue) { queue.Release(ClipWithResource(bell)) })
	h.tick()

	if got := h.probe(ambience); got.Found {
		t.Fatal("the looping ambience is still in the view after its Clip was released")
	}
	if got := h.probe(second); got.Found || got.Live != 1 {
		t.Fatalf("the view holds %d Voices, want only the one on the Clip nothing released", got.Live)
	}
	if got := h.probe(kept); !got.Found {
		t.Fatal("releasing one Clip stopped a Voice playing another")
	}
	for range 2 {
		if ended := h.waitEnded(); ended.Reason != ReasonReleased {
			t.Fatalf("%v ended as %v, want released", ended.Voice, ended.Reason)
		}
	}
	h.noEnding()

	batch := h.backend.emitted()[before]
	if len(batch.Stops) != 2 {
		t.Fatalf("the release emitted %d stops, want one per Voice on the Clip", len(batch.Stops))
	}
	if len(batch.Destroys) != 1 {
		t.Fatalf("the release emitted %d destroys, want the one Clip it released", len(batch.Destroys))
	}
}

// A pending Voice is cut the same way and ends released rather than failed.
// Nothing failed - the game changed its mind - and the one ending that means "a
// Clip could not be read or prepared" must keep meaning only that.
//
// The prepare that was in flight costs nothing when it lands: its entry is gone
// by the time the completion is drained, so it is dropped with nothing called
// and no ClipID is ever minted only to be destroyed.
func TestAPendingVoiceIsCutByAReleaseAndEndsReleasedRatherThanFailed(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 30, channels: 2, rate: 48000})
	backend.deferring = true
	h := newHarness(t, backend, Config{}, clipBytes)

	voice := h.play(ClipWithResource(bell), 0, Params{})
	h.tick()
	if got := h.probe(voice); !got.Found || got.Info.Duration != 0 {
		t.Fatalf("the Voice is not pending: found=%v duration=%v", got.Found, got.Info.Duration)
	}

	h.record(func(queue *Queue) { queue.Release(ClipWithResource(bell)) })
	h.tick()

	if ended := h.waitEnded(); ended.Voice != voice || ended.Reason != ReasonReleased {
		t.Fatalf("ended as %v/%v, want %v/released", ended.Voice, ended.Reason, voice)
	}

	backend.finishPrepares()
	h.tick()
	if _, installs := backend.counts(); installs != 0 {
		t.Fatalf("%d ids were minted for a Clip released while its prepare was in flight", installs)
	}
	if batch := h.backend.emitted(); len(batch[len(batch)-1].Destroys) != 0 {
		t.Fatal("a Clip that was never installed was destroyed anyway")
	}
}

// Preload makes a Clip resident without playing it, which is the lever for
// choosing which frame eats the read - a loading screen rather than the first
// shot fired. Nothing crosses the seam, because nothing is playing.
//
// What it promises is residency and not a free Play: the Play that follows a
// Preload binds in the flush that recorded it, with no pending tick, and that
// is as much as a Clip's tier lets anyone promise. For a streamed Clip the
// Adapter still opens a decoder per Voice and the Voice is silent until its
// read-ahead primes, which is below this seam and invisible here by design.
func TestPreloadMakesAClipResidentWithoutPlayingIt(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 30, channels: 2, rate: 44100})
	backend.deferring = true
	h := newHarness(t, backend, Config{}, clipBytes)

	h.record(func(queue *Queue) { queue.Preload(ClipWithResource(bell)) })
	h.tick()
	if _, state := h.askClip(ClipWithResource(bell)); state != ClipLoading {
		t.Fatalf("a Clip whose prepare is in flight reports %v, want loading", state)
	}

	backend.finishPrepares()
	h.tick()
	info, state := h.askClip(ClipWithResource(bell))
	if state != ClipReady {
		t.Fatalf("a preloaded Clip reports %v, want ready", state)
	}
	if info != (ClipInfo{Duration: 30, Channels: 2, SampleRate: 44100}) {
		t.Fatalf("ClipInfoOf answered %+v, want the Clip's own three facts", info)
	}
	for _, batch := range h.backend.emitted() {
		if len(batch.Starts) != 0 {
			t.Fatal("a Preload started a Voice")
		}
	}

	voice := h.play(ClipWithResource(bell), 0, Params{})
	h.tick()
	if got := h.probe(voice); !got.Found || got.Info.Duration != 30 {
		t.Fatalf("a Play on a preloaded Clip did not bind in its own flush: %+v", got.Info)
	}
}

// Asking never starts a load. It reads sound's own table and never the Library,
// whose Get is the only read it has and loads on a miss - so a Clip nothing has
// named reports zero facts and loading, and stays there however often it is
// asked about.
//
// A duration of zero and a state of loading are different answers to different
// questions, which is the whole reason the state is returned beside the facts: a
// descriptor that reports a size of zero forever cannot tell "not yet" from
// "never", and that is the lie this is here not to repeat.
func TestAskingAboutAClipNeverStartsALoad(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 30, channels: 2, rate: 48000})
	h := newHarness(t, backend, Config{}, clipBytes)

	for range 3 {
		info, state := h.askClip(ClipWithResource(bell))
		if state != ClipLoading || info != (ClipInfo{}) {
			t.Fatalf("asking about an unnamed Clip answered %+v/%v, want zero facts and loading", info, state)
		}
		h.tick()
	}

	if prepares, installs := backend.counts(); prepares != 0 || installs != 0 {
		t.Fatalf("asking cost %d prepares and %d installs, and it must cost neither", prepares, installs)
	}
}

// A Clip that failed reports failed, and only a release clears it. The failure
// is terminal, so a second Play reports nothing and retries nothing; a release
// drops the entry and forgets the report, so the path can be read again and can
// speak again when it fails again.
func TestOnlyAReleaseClearsAClipThatFailed(t *testing.T) {
	backend := newFakeBackend(fakeClip{})
	backend.prepareErr = errors.New("this clip is not ogg vorbis")
	var reported []error
	h := newHarnessReporting(t, backend, clipBytes, func(err error) { reported = append(reported, err) })

	h.record(func(queue *Queue) { queue.Preload(ClipWithResource(bell)) })
	h.tick()
	h.record(func(queue *Queue) { queue.Preload(ClipWithResource(bell)) })
	h.tick()
	if _, state := h.askClip(ClipWithResource(bell)); state != ClipFailed {
		t.Fatalf("a Clip that could not be prepared reports %v, want failed", state)
	}
	if len(reported) != 1 {
		t.Fatalf("a terminal failure was reported %d times, want once", len(reported))
	}

	h.record(func(queue *Queue) { queue.Release(ClipWithResource(bell)) })
	h.tick()
	if _, state := h.askClip(ClipWithResource(bell)); state != ClipLoading {
		t.Fatalf("a released Clip still reports %v, and a release is what clears a failure", state)
	}

	h.record(func(queue *Queue) { queue.Preload(ClipWithResource(bell)) })
	h.tick()
	if len(reported) != 2 {
		t.Fatalf("the failure was reported %d times over two loads, want once each", len(reported))
	}
}

// ReleaseAll cuts everything, which is what a teardown call means, and it
// destroys every Clip in one batch. A game that wants one track to bridge a
// transition releases per Clip and keeps the bridging one.
//
// It is also where a game would notice a Device lost after its Mixer had pulled
// once, which is #485's to fix and not this ticket's: above the seam ReleaseAll
// is an ordinary release over every entry, and everything it can be held up by
// is on the Adapter's side of Emit.
func TestReleaseAllCutsEveryVoiceAndDestroysEveryClip(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 30, channels: 2, rate: 48000})
	h := newHarness(t, backend, Config{}, twoClips)

	first := h.play(ClipWithResource(bell), 0, Params{Loop: m.Some(true)})
	second := h.play(ClipWithResource(drum), 0, Params{Loop: m.Some(true)})
	h.tick()
	before := len(h.backend.emitted())

	h.record(func(queue *Queue) { queue.ReleaseAll() })
	h.tick()

	if got := h.probe(first); got.Found || got.Live != 0 {
		t.Fatalf("the view holds %d Voices after a teardown", got.Live)
	}
	if got := h.probe(second); got.Found {
		t.Fatal("a Voice survived ReleaseAll")
	}
	for range 2 {
		if ended := h.waitEnded(); ended.Reason != ReasonReleased {
			t.Fatalf("%v ended as %v, want released", ended.Voice, ended.Reason)
		}
	}
	batch := h.backend.emitted()[before]
	if len(batch.Stops) != 2 || len(batch.Destroys) != 2 {
		t.Fatalf("the teardown emitted %d stops and %d destroys, want two of each",
			len(batch.Stops), len(batch.Destroys))
	}
	for _, clip := range []string{bell, drum} {
		if _, state := h.askClip(ClipWithResource(clip)); state != ClipLoading {
			t.Fatalf("%s still reports %v after a teardown", clip, state)
		}
	}
}

// Asking again reloads. A release drops the bytes as well as the samples, so
// the next Play reads the file again and installs a Clip of its own - which is
// what makes "let ReleaseAll cut it and start the bridge afterwards" work, and
// what makes a release that was a mistake cost a read rather than silence.
//
// The ordering is the point of the last two ticks: Release and Play in one tick
// are applied in the order they were recorded, so a Play after a Release of the
// same Clip reloads it, and a Play before one is cut by it.
func TestAReleasedClipIsReadAgainWhenItIsNamedAgain(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 30, channels: 2, rate: 48000})
	h := newHarness(t, backend, Config{}, clipBytes)

	h.play(ClipWithResource(bell), 0, Params{Loop: m.Some(true)})
	h.tick()
	if prepares, installs := backend.counts(); prepares != 1 || installs != 1 {
		t.Fatalf("the first play cost %d prepares and %d installs, want one each", prepares, installs)
	}

	var cut, reloaded Voice
	h.record(func(queue *Queue) {
		cut = queue.Play(ClipWithResource(bell), 0, Params{Loop: m.Some(true)})
		queue.Release(ClipWithResource(bell))
		reloaded = queue.Play(ClipWithResource(bell), 0, Params{Loop: m.Some(true)})
	})
	h.tick()

	if got := h.probe(cut); got.Found {
		t.Fatal("a Play recorded before a Release of the same Clip survived it")
	}
	if got := h.probe(reloaded); !got.Found {
		t.Fatal("a Play recorded after a Release of the same Clip did not reload it")
	}
	if prepares, installs := backend.counts(); prepares != 2 || installs != 2 {
		t.Fatalf("the reload cost %d prepares and %d installs in total, want two each", prepares, installs)
	}
}

// The whole surface is optional and total: releasing a Clip nothing named,
// releasing one twice, preloading one that is already playing, and tearing down
// an engine that holds nothing are all no-ops with nothing to check.
func TestTheClipSurfaceIsTotal(t *testing.T) {
	backend := newFakeBackend(fakeClip{duration: 30, channels: 2, rate: 48000})
	h := newHarness(t, backend, Config{}, clipBytes)

	h.record(func(queue *Queue) {
		queue.Release(ClipWithResource("never-named.ogg"))
		queue.ReleaseAll()
	})
	h.tick()

	voice := h.play(ClipWithResource(bell), 0, Params{Loop: m.Some(true)})
	h.record(func(queue *Queue) { queue.Preload(ClipWithResource(bell)) })
	h.tick()
	if got := h.probe(voice); !got.Found {
		t.Fatal("a Preload of a Clip already playing disturbed its Voice")
	}
	if prepares, _ := backend.counts(); prepares != 1 {
		t.Fatalf("a Play and a Preload of one Clip cost %d prepares, want the one entry", prepares)
	}

	h.record(func(queue *Queue) {
		queue.Release(ClipWithResource(bell))
		queue.Release(ClipWithResource(bell))
	})
	h.tick()
	if ended := h.waitEnded(); ended.Reason != ReasonReleased {
		t.Fatalf("ended as %v, want released", ended.Reason)
	}
	h.noEnding()
	batch := h.backend.emitted()
	if got := len(batch[len(batch)-1].Destroys); got != 1 {
		t.Fatalf("releasing one Clip twice in one tick emitted %d destroys, want one", got)
	}
}

// A release keys on Clip identity and not on field identity. A ref with a path
// is named by that path alone, whatever bytes it carries beside it, which is
// the Library's own key rule and the rule sound's own clip table is spelled
// with - so the two must not disagree about which Voice a release cuts.
//
// This is the one case that separates the two, and it is not constructible from
// the root's surface: ClipWithResource sets a name and ClipWithBytes sets a
// blob, so only here can a ref carry both. A stopClip written with == would
// leave this Voice playing a Clip that has been destroyed.
func TestAReleaseCutsOnClipIdentityRatherThanFieldIdentity(t *testing.T) {
	bytes := assets.NewBlobFromString("ogg bytes the play carried beside its path")
	carrying := ClipRef{name: "bell.ogg", blob: bytes}
	named := ClipWithResource("bell.ogg")

	if carrying == named {
		t.Fatal("the two refs compare equal as fields, so this test proves nothing")
	}

	voices := NewVoices(2)
	var endings []Ending
	voices.start(Operation{Kind: OpPlay, Voice: newVoice(0, 1), Clip: carrying}, clipFacts{}, &endings)
	voices.start(Operation{Kind: OpPlay, Voice: newVoice(1, 1), Clip: ClipWithResource("drum.ogg")}, clipFacts{}, &endings)

	voices.stopClip(named, &endings)

	if len(endings) != 1 {
		t.Fatalf("a release of bell.ogg ended %d Voices, want the one playing it", len(endings))
	}
	if endings[0].Reason != ReasonReleased {
		t.Fatalf("the released Voice ended as %v, want released", endings[0].Reason)
	}
	if voices.Len() != 1 {
		t.Fatalf("%d Voices are live, want the one on the Clip nothing released", voices.Len())
	}
}
