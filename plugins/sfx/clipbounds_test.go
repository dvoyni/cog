package sfx_test

import (
	"testing"
	"time"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/sfxtest"
)

// A playhead is never outside its clip, and the backend is never handed an
// offset that is: sfx settles where a voice is once, so no backend has a rule
// of its own to disagree with. The voices here are paused wherever the test
// reads a playhead exactly, since a paused one does not advance.

// pausedLoop is a looping voice that stays where it is put.
var pausedLoop = sfx.VoiceOptions{Loop: m.Some(true), Paused: m.Some(true)}

// playAt records a play of the fixture clip from offset, steps, and answers
// its handle.
func playAt(h *sfxtest.Harness, offset time.Duration, options sfx.VoiceOptions) sfx.Voice {
	var voice sfx.Voice
	h.Record(func(queue sfx.Queue) { voice = queue.Play(clipPath, offset, options) })
	h.Step()
	return voice
}

// wantStartAt fails the test unless the last flush handed the backend one
// start from offset, and voice reads a playhead of offset.
func wantStartAt(t *testing.T, h *sfxtest.Harness, seam *seam, voice sfx.Voice, offset time.Duration) {
	t.Helper()
	starts := lastChanges(seam).Starts
	if len(starts) != 1 || starts[0].Offset != offset {
		t.Fatalf("the flush emitted the starts %+v, want one from %v", starts, offset)
	}
	if got := readPlayhead(h, voice); got != offset {
		t.Fatalf("the playhead reads %v, want %v", got, offset)
	}
}

func TestANegativePlayOffsetIsTheHead(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, seamPlugin{seam: seam})

	voice := playAt(h, -time.Second, sfx.VoiceOptions{Paused: m.Some(true)})

	wantStartAt(t, h, seam, voice, 0)
}

// The retry of a stolen looping voice plays it from its playhead plus the time
// since, which can be past the loop end: it lands where a voice that had been
// looping all along would be.
func TestALoopingPlayPastItsLoopEndIsCarriedBackByWholeLoops(t *testing.T) {
	for _, test := range []struct {
		name         string
		offset, want time.Duration
	}{
		{"in the intro", 250 * time.Millisecond, 250 * time.Millisecond},
		{"at the loop end", 750 * time.Millisecond, 500 * time.Millisecond},
		{"in the tail", 900 * time.Millisecond, 650 * time.Millisecond},
		{"past the clip's end", 2100 * time.Millisecond, 600 * time.Millisecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			seam := &seam{}
			h := newHarness(t, nil, loopRegionPlugin{region: introLoop}, seamPlugin{seam: seam})

			voice := playAt(h, test.offset, pausedLoop)

			wantStartAt(t, h, seam, voice, test.want)
		})
	}
}

// A one-shot played at or past its clip's end has finished, paused or not, and
// the backend never hears of it.
func TestAOneShotPlayedPastItsEndFinishesWithoutStarting(t *testing.T) {
	for _, test := range []struct {
		name   string
		offset time.Duration
		paused bool
	}{
		{"playing, at the end", clipDuration, false},
		{"paused, at the end", clipDuration, true},
		{"paused, past the end", 3 * time.Second, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			seam := &seam{}
			h := newHarness(t, nil, seamPlugin{seam: seam})

			voice := playAt(h, test.offset, sfx.VoiceOptions{Paused: m.Some(test.paused)})

			if entry, live := readLiveVoice(h, voice); live {
				t.Fatalf("the voice is live at %v of %v", entry.Playhead, entry.Duration)
			}
			if starts := countStarts(seam.emitted()); starts != 0 {
				t.Fatalf("%d starts crossed the seam", starts)
			}
			wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonFinished})
		})
	}
}

// A play followed by a SetVoice in one flush is a play that carried the same
// options, so where it starts is settled after both.
func TestAPlayPastTheEndMadeLoopingInTheSameFlushLoops(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, loopRegionPlugin{region: introLoop}, seamPlugin{seam: seam})

	var voice sfx.Voice
	h.Record(func(queue sfx.Queue) {
		voice = queue.Play(clipPath, 2100*time.Millisecond, sfx.VoiceOptions{Paused: m.Some(true)})
		queue.SetVoice(voice, sfx.VoiceOptions{Loop: m.Some(true)})
	})
	h.Step()

	wantStartAt(t, h, seam, voice, 600*time.Millisecond)
	wantEndings(t, h)
}

func TestASeekIntoTheTailOfALoopingVoiceIsCarriedBack(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, loopRegionPlugin{region: introLoop}, seamPlugin{seam: seam})

	voice := playAt(h, 0, pausedLoop)
	h.Record(func(queue sfx.Queue) { queue.Seek(voice, 900*time.Millisecond) })
	h.Step()

	wantStartAt(t, h, seam, voice, 650*time.Millisecond)
}

func TestTurningLoopOnInTheTailRestartsInsideTheLoop(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, loopRegionPlugin{region: introLoop}, seamPlugin{seam: seam})

	voice := playAt(h, 900*time.Millisecond, sfx.VoiceOptions{Paused: m.Some(true)})
	h.Record(func(queue sfx.Queue) { queue.SetVoice(voice, sfx.VoiceOptions{Loop: m.Some(true)}) })
	h.Step()

	wantStartAt(t, h, seam, voice, 650*time.Millisecond)
}

// While its clip loads a voice has no end to be past, so it shows what was
// asked, and the same rules apply once the length is known: a play carries a
// looping voice back by whole loops, and a seek past the end goes to the loop
// start, however long the load took. The fake clip is 4 s and loops whole.
func TestAPendingVoiceIsSettledOnceItsClipInstalls(t *testing.T) {
	for _, test := range []struct {
		name         string
		offset, seek time.Duration
		want         time.Duration
	}{
		{"a play", 9 * time.Second, 0, time.Second},
		{"a seek", 0, 9 * time.Second, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend := &fakeBackend{deferring: true}
			seam := &seam{}
			h := newFakeHarness(t, backend, seamPlugin{seam: seam})

			voice := playAt(h, test.offset, pausedLoop)
			if test.seek != 0 {
				h.Record(func(queue sfx.Queue) { queue.Seek(voice, test.seek) })
				h.Step()
			}
			if got, want := readPlayhead(h, voice), max(test.offset, test.seek); got != want {
				t.Fatalf("the pending voice reads %v, want the %v it was asked for", got, want)
			}

			backend.finishPrepares(nil)
			h.Step()

			wantStartAt(t, h, seam, voice, test.want)
		})
	}
}

func TestAPendingVoiceShowsANegativeOffsetAsTheHead(t *testing.T) {
	h := newFakeHarness(t, &fakeBackend{deferring: true})

	voice := playAt(h, -time.Second, sfx.VoiceOptions{})

	if got := readPlayhead(h, voice); got != 0 {
		t.Fatalf("the pending voice reads %v, want 0", got)
	}
}

func TestAPendingOneShotPastTheEndFinishesWhenItsClipInstalls(t *testing.T) {
	backend := &fakeBackend{deferring: true}
	seam := &seam{}
	h := newFakeHarness(t, backend, seamPlugin{seam: seam})

	voice := playAt(h, 9*time.Second, sfx.VoiceOptions{Paused: m.Some(true)})
	wantEndings(t, h)

	backend.finishPrepares(nil)
	h.Step()

	if _, live := readLiveVoice(h, voice); live {
		t.Fatal("the voice outlived the install of a clip it is past the end of")
	}
	if starts := countStarts(seam.emitted()); starts != 0 {
		t.Fatalf("%d starts crossed the seam", starts)
	}
	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonFinished})
}
