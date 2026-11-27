package sfx_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/sfxtest"
)

// drumPath is where a test mounts a second clip, the fixture's bytes under
// another path, so it can release one clip and keep the other.
const drumPath = "music/drum.ogg"

// readClip answers what the clip table knows of the clip at path.
func readClip(h *sfxtest.Harness, path string) (sfx.ClipInfo, sfx.ClipState) {
	var info sfx.ClipInfo
	var state sfx.ClipState
	h.Read(func(_ sfx.LiveVoices, clips sfx.ClipTable, _ sfx.BusVolumes, _ sfx.Listener, _ sfx.DeviceView) {
		info, state = clips.Info(path)
	})
	return info, state
}

// The guarantee, checked from outside: after the flush in which a release is
// recorded, no voice is playing that clip. It is checked in three places,
// since "no voice is playing it" is a claim about all three: the live view
// holds none, an ending fired saying why, and the changes that left in that
// same flush carry the stops. The destroy is in those same changes, which a
// backend applies after their stops, so the mixer never frees a clip it is
// still mixing.
func TestAfterTheFlushThatRecordsAReleaseNoVoiceIsPlayingThatClip(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, seamPlugin{seam: seam})
	h.Mount(drumPath, readFixtureClip(t))

	var ambience, second, kept sfx.Voice
	h.Record(func(queue sfx.Queue) {
		ambience = queue.Play(clipPath, 0, sfx.VoiceOptions{})
		second = queue.Play(clipPath, 0, sfx.VoiceOptions{})
		kept = queue.Play(drumPath, 0, sfx.VoiceOptions{})
	})
	h.Step()
	if count := readLiveCount(h); count != 3 {
		t.Fatalf("%d voices started, want three", count)
	}
	released := seam.emitted()[0].Starts[0].Clip

	h.Record(func(queue sfx.Queue) { queue.Release(clipPath) })
	h.Step()

	for _, voice := range []sfx.Voice{ambience, second} {
		if _, live := readLiveVoice(h, voice); live {
			t.Fatalf("%v is still in the view after its clip was released", voice)
		}
	}
	if _, live := readLiveVoice(h, kept); !live {
		t.Fatal("releasing one clip stopped a voice playing another")
	}
	wantEndings(t, h,
		sfx.VoiceEndedEvent{Voice: ambience, Reason: sfx.ReasonReleased},
		sfx.VoiceEndedEvent{Voice: second, Reason: sfx.ReasonReleased})
	emitted := seam.emitted()
	release := emitted[len(emitted)-1]
	if !slices.Equal(release.Stops, []sfx.VoiceSlot{0, 1}) {
		t.Fatalf("the release's changes stop %v, want the two slots playing its clip", release.Stops)
	}
	if !slices.Equal(release.Destroys, []sfx.ClipID{released}) {
		t.Fatalf("the release's changes destroy %v, want the one clip it released, %v", release.Destroys, released)
	}
	if _, state := readClip(h, clipPath); state != sfx.ClipLoading {
		t.Fatalf("a released clip reads %d, want ClipLoading, as a path nothing named", state)
	}
	if _, state := readClip(h, drumPath); state != sfx.ClipReady {
		t.Fatalf("the clip nothing released reads %d, want ClipReady", state)
	}
}

// A pending voice is cut the same way, and ends released rather than failed:
// nothing failed, the game changed its mind, and the ending that means a clip
// could not be read or prepared keeps meaning only that. The prepare in
// flight costs nothing when it lands: nothing is installed, and nothing is
// destroyed.
func TestAPendingVoiceIsCutByAReleaseAndEndsReleasedRatherThanFailed(t *testing.T) {
	backend := &fakeBackend{deferring: true}
	seam := &seam{}
	h := newFakeHarness(t, backend, seamPlugin{seam: seam})

	voice := play(h)
	if entry, live := readLiveVoice(h, voice); !live || entry.Duration != 0 {
		t.Fatalf("the voice is not pending: live=%v, %+v", live, entry)
	}

	h.Record(func(queue sfx.Queue) { queue.Release(clipPath) })
	h.Step()
	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonReleased})

	backend.finishPrepares(nil)
	h.Step()
	if _, installCount := backend.counts(); installCount != 0 {
		t.Fatalf("%d IDs were minted for a clip released while its prepare was in flight", installCount)
	}
	for index, changes := range seam.emitted() {
		if len(changes.Starts) != 0 || len(changes.Stops) != 0 || len(changes.Destroys) != 0 {
			t.Fatalf("flush %d carried %+v, for a clip that was never installed", index+1, changes)
		}
	}
}

// A completion whose clip was released while its prepare was in flight is
// dropped with nothing called: no ClipID is minted only to be destroyed. That
// holds whether the clip's entry is gone when the completion arrives, or has
// been made again since by a later preload, whose own prepare is then the one
// that installs.
func TestACompletionForAClipReleasedBeforeItArrivedMintsNoClipID(t *testing.T) {
	backend := &fakeBackend{deferring: true}
	h := newFakeHarness(t, backend)
	preload := func(queue sfx.Queue) { queue.Preload(clipPath) }
	release := func(queue sfx.Queue) { queue.Release(clipPath) }

	h.Record(preload)
	h.Step()
	h.Record(release)
	h.Step()
	backend.finishPrepares(nil)
	h.Step()
	if _, installCount := backend.counts(); installCount != 0 {
		t.Fatalf("a completion for a clip no longer held minted %d IDs", installCount)
	}

	h.Record(preload)
	h.Step()
	h.Record(release)
	h.Record(preload)
	h.Step()
	backend.finishFirstPrepare()
	h.Step()
	if _, state := readClip(h, clipPath); state != sfx.ClipLoading {
		t.Fatalf("a completion of the clip before its release left the clip named again %d, want ClipLoading", state)
	}
	if _, installCount := backend.counts(); installCount != 0 {
		t.Fatalf("a completion of the clip before its release minted %d IDs", installCount)
	}

	backend.finishPrepares(nil)
	h.Step()
	if _, state := readClip(h, clipPath); state != sfx.ClipReady {
		t.Fatalf("the completion of the clip's own prepare left it %d, want ClipReady", state)
	}
	if _, installCount := backend.counts(); installCount != 1 {
		t.Fatalf("the clip's own prepare minted %d IDs, want one", installCount)
	}
}

// Preload makes a clip resident without playing it, which is the lever for
// choosing which frame eats the read: a loading screen rather than the first
// shot fired. Nothing crosses the seam, since nothing plays, and a play that
// follows binds in the flush that applies it.
func TestPreloadMakesAClipResidentWithoutPlayingIt(t *testing.T) {
	backend := &fakeBackend{deferring: true}
	seam := &seam{}
	h := newFakeHarness(t, backend, seamPlugin{seam: seam})

	h.Record(func(queue sfx.Queue) { queue.Preload(clipPath) })
	h.Step()
	if info, state := readClip(h, clipPath); state != sfx.ClipLoading || info != (sfx.ClipInfo{}) {
		t.Fatalf("a clip whose prepare is in flight reads %+v and %d, want zero facts and ClipLoading", info, state)
	}

	backend.finishPrepares(nil)
	h.Step()
	info, state := readClip(h, clipPath)
	if state != sfx.ClipReady {
		t.Fatalf("a preloaded clip reads %d, want ClipReady", state)
	}
	if want := (sfx.ClipInfo{Duration: fakeClipSeconds * time.Second, Channels: 1, SampleRate: 48000}); info != want {
		t.Fatalf("Info answered %+v, want the clip's own facts, %+v", info, want)
	}
	if starts := countStarts(seam.emitted()); starts != 0 {
		t.Fatalf("a preload started %d voices", starts)
	}

	voice := play(h)
	if entry, live := readLiveVoice(h, voice); !live || entry.Duration != fakeClipSeconds*time.Second {
		t.Fatalf("a play of a preloaded clip did not bind in its own flush: live=%v, %+v", live, entry)
	}
	if prepareCount, _ := backend.counts(); prepareCount != 1 {
		t.Fatalf("a preload and a play of one clip cost %d prepares, want one", prepareCount)
	}
}

// Asking never starts a load: it reads sfx's own table. A path nothing has
// named answers zero facts and ClipLoading, however often it is asked about,
// and a duration of zero beside ClipLoading is "not yet", not an answer.
func TestAskingAboutAClipNeverStartsALoad(t *testing.T) {
	backend := &fakeBackend{}
	h := newFakeHarness(t, backend)

	for range 3 {
		if info, state := readClip(h, clipPath); state != sfx.ClipLoading || info != (sfx.ClipInfo{}) {
			t.Fatalf("asking about a path nothing named answered %+v and %d, want zero facts and ClipLoading", info, state)
		}
		h.Step()
	}
	if prepareCount, installCount := backend.counts(); prepareCount != 0 || installCount != 0 {
		t.Fatalf("asking cost %d prepares and %d installs, and it must cost neither", prepareCount, installCount)
	}
}

// A clip that failed reads ClipFailed, and only a release clears it. The
// failure is terminal, so naming it again reads nothing, prepares nothing and
// reports nothing; a release forgets the clip and its report, so the path is
// read again and is reported again when it fails again.
func TestOnlyAReleaseClearsAClipThatFailed(t *testing.T) {
	backend := &fakeBackend{prepareErr: errors.New("this clip is not ogg vorbis")}
	h := newFakeHarness(t, backend)
	expectClipFailures(h)
	preload := func(queue sfx.Queue) { queue.Preload(clipPath) }

	h.Record(preload)
	h.Step()
	h.Record(preload)
	h.Step()
	if _, state := readClip(h, clipPath); state != sfx.ClipFailed {
		t.Fatalf("a clip that could not be prepared reads %d, want ClipFailed", state)
	}
	if reports := h.Reports(); len(reports) != 1 {
		t.Fatalf("a terminal failure was reported %d times, want once: %v", len(reports), reports)
	}

	h.Record(func(queue sfx.Queue) { queue.Release(clipPath) })
	h.Step()
	if _, state := readClip(h, clipPath); state != sfx.ClipLoading {
		t.Fatalf("a released clip still reads %d, and a release is what clears a failure", state)
	}

	h.Record(preload)
	h.Step()
	if reports := h.Reports(); len(reports) != 2 {
		t.Fatalf("the failure was reported %d times over two loads, want once each: %v", len(reports), reports)
	}
	if prepareCount, _ := backend.counts(); prepareCount != 2 {
		t.Fatalf("the clip was prepared %d times, want once before its release and once after", prepareCount)
	}
}

// ReleaseAll cuts every voice, which is what a teardown means, and destroys
// every clip in the same flush's changes, after their stops.
func TestReleaseAllCutsEveryVoiceAndDestroysEveryClip(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, seamPlugin{seam: seam})
	h.Mount(drumPath, readFixtureClip(t))

	var first, second sfx.Voice
	h.Record(func(queue sfx.Queue) {
		first = queue.Play(clipPath, 0, sfx.VoiceOptions{})
		second = queue.Play(drumPath, 0, sfx.VoiceOptions{})
	})
	h.Step()
	var installed []sfx.ClipID
	for _, start := range seam.emitted()[0].Starts {
		installed = append(installed, start.Clip)
	}

	h.Record(func(queue sfx.Queue) { queue.ReleaseAll() })
	h.Step()

	if count := readLiveCount(h); count != 0 {
		t.Fatalf("the view holds %d voices after a teardown", count)
	}
	wantEndings(t, h,
		sfx.VoiceEndedEvent{Voice: first, Reason: sfx.ReasonReleased},
		sfx.VoiceEndedEvent{Voice: second, Reason: sfx.ReasonReleased})
	emitted := seam.emitted()
	teardown := emitted[len(emitted)-1]
	if !slices.Equal(teardown.Stops, []sfx.VoiceSlot{0, 1}) {
		t.Fatalf("the teardown's changes stop %v, want both slots", teardown.Stops)
	}
	destroyed := slices.Sorted(slices.Values(teardown.Destroys))
	if !slices.Equal(destroyed, slices.Sorted(slices.Values(installed))) {
		t.Fatalf("the teardown's changes destroy %v, want every clip installed, %v", teardown.Destroys, installed)
	}
	for _, path := range []string{clipPath, drumPath} {
		if _, state := readClip(h, path); state != sfx.ClipLoading {
			t.Fatalf("%s still reads %d after a teardown", path, state)
		}
	}
}

// A released clip is read again when it is named again, which is what makes
// "let ReleaseAll cut it and start the bridge afterwards" work. A release and
// the plays around it apply in the order recorded: a play before a release of
// its clip is cut by it, and a play after reads the clip again.
func TestAReleasedClipIsReadAgainWhenItIsNamedAgain(t *testing.T) {
	backend := &fakeBackend{}
	h := newFakeHarness(t, backend)

	first := play(h)
	if prepareCount, installCount := backend.counts(); prepareCount != 1 || installCount != 1 {
		t.Fatalf("the first play cost %d prepares and %d installs, want one each", prepareCount, installCount)
	}

	var cut, reloaded sfx.Voice
	h.Record(func(queue sfx.Queue) {
		cut = queue.Play(clipPath, 0, sfx.VoiceOptions{})
		queue.Release(clipPath)
		reloaded = queue.Play(clipPath, 0, sfx.VoiceOptions{})
	})
	h.Step()

	if _, live := readLiveVoice(h, cut); live {
		t.Fatal("a play recorded before a release of its clip survived it")
	}
	if _, live := readLiveVoice(h, reloaded); !live {
		t.Fatal("a play recorded after a release of its clip did not read it again")
	}
	wantEndings(t, h,
		sfx.VoiceEndedEvent{Voice: first, Reason: sfx.ReasonReleased},
		sfx.VoiceEndedEvent{Voice: cut, Reason: sfx.ReasonReleased})
	if prepareCount, installCount := backend.counts(); prepareCount != 2 || installCount != 2 {
		t.Fatalf("the reload cost %d prepares and %d installs in all, want two each", prepareCount, installCount)
	}
}

// The clip surface is optional and total: releasing a path nothing named,
// releasing one twice, preloading one already playing, and a teardown of a
// table holding nothing are each a no-op with nothing to check.
func TestTheClipSurfaceIsTotal(t *testing.T) {
	backend := &fakeBackend{}
	seam := &seam{}
	h := newFakeHarness(t, backend, seamPlugin{seam: seam})

	h.Record(func(queue sfx.Queue) {
		queue.Release("music/never-named.ogg")
		queue.ReleaseAll()
	})
	h.Step()
	wantEndings(t, h)

	var voice sfx.Voice
	h.Record(func(queue sfx.Queue) {
		voice = queue.Play(clipPath, 0, sfx.VoiceOptions{})
		queue.Preload(clipPath)
	})
	h.Step()
	if _, live := readLiveVoice(h, voice); !live {
		t.Fatal("a preload of a clip already playing disturbed its voice")
	}
	if prepareCount, _ := backend.counts(); prepareCount != 1 {
		t.Fatalf("a play and a preload of one clip cost %d prepares, want one", prepareCount)
	}

	h.Record(func(queue sfx.Queue) {
		queue.Release(clipPath)
		queue.Release(clipPath)
	})
	h.Step()
	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonReleased})
	emitted := seam.emitted()
	if destroys := emitted[len(emitted)-1].Destroys; len(destroys) != 1 {
		t.Fatalf("releasing one clip twice in one flush destroyed %v, want it once", destroys)
	}
	for index, changes := range emitted[:len(emitted)-1] {
		if len(changes.Destroys) != 0 {
			t.Fatalf("flush %d destroyed %v before anything was released", index+1, changes.Destroys)
		}
	}
}
