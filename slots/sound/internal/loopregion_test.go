package internal

import (
	"testing"
	"time"

	"github.com/dvoyni/cog/libs/m"
)

// sound keeps a Clip's Loop Region for two reasons of its own, and these are
// them: a looping Voice's playhead wraps at the loop end rather than at the
// duration, and a Seek past the end wraps to the loop start rather than to
// zero. Everything else about the region is the Adapter's.
//
// Nothing on the game face moves for it. Params.Loop is still a bool and
// VoiceStart.Loop is still a bool; Loop has only stopped meaning repeat the
// whole Clip and started meaning repeat the way this Clip says to.

// loopingClip is a one-second Clip with an intro, a loop and a tail: it repeats
// between 0.5 s and 0.75 s, and the quarter second after that is never heard by
// a looping Voice.
func loopingClip() fakeClip {
	return fakeClip{
		duration: 1,
		channels: 2,
		rate:     48000,
		region:   m.Some(LoopRegion{Start: 0.5, End: 0.75}),
	}
}

// The playhead wraps at the loop end and not at the duration. A Voice that
// wrapped at the duration would report itself playing through a tail the
// Adapter stopped playing a bar ago, and the view would be describing a
// different sound from the one in the room.
func TestALoopingPlayheadWrapsAtTheLoopEndAndNotAtTheDuration(t *testing.T) {
	h := newHarness(t, newFakeBackend(loopingClip()), Config{}, clipBytes)

	voice := h.play(ClipWithResource(bell), 0, Params{Loop: m.Some(true)})

	// 0.75 s at a sixty-fourth of a second a tick is 48 ticks, so tick 48 is
	// the first one to reach the loop end, and it lands back on the loop start.
	for tick := 1; tick < 48; tick++ {
		h.tick()
		if got, want := h.probe(voice).Info.Playhead, float32(float64(tick)*step); got != want {
			t.Fatalf("after tick %d the playhead is %v, want %v", tick, got, want)
		}
	}
	h.tick()

	got := h.probe(voice)
	if !got.Found {
		t.Fatal("the Voice ended at its loop end, and a looping Voice never ends by itself")
	}
	if got.Info.Playhead != 0.5 {
		t.Fatalf("the wrap landed at %v, want the loop start 0.5", got.Info.Playhead)
	}
	if got.Info.Duration != 1 {
		t.Fatalf("the view reports duration %v, want the Clip's whole 1 - the region is not the length",
			got.Info.Duration)
	}
	h.noEnding()

	// And it keeps going round the region rather than round the Clip.
	for range 16 {
		h.tick()
	}
	if replay := h.probe(voice).Info.Playhead; replay != 0.5 {
		t.Fatalf("a second pass of the loop landed at %v, want the loop start 0.5", replay)
	}
}

// A seek past the end of a looping Voice lands on the loop start, which is what
// the Loop Region is felt as on the game face: a seek off the end of a track
// with an intro lands in the loop rather than replaying the intro.
//
// What crosses the seam for it is an ordinary VoiceStart carrying an Offset in
// seconds. No signature in sound names a sample.
func TestASeekPastTheEndOfALoopingVoiceLandsOnTheLoopStart(t *testing.T) {
	h := newHarness(t, newFakeBackend(loopingClip()), Config{}, clipBytes)

	voice := h.play(ClipWithResource(bell), 0, Params{Loop: m.Some(true)})
	h.tick()

	h.record(func(queue *Queue) { queue.Seek(voice, 9) })
	h.tick()

	got := h.probe(voice)
	if !got.Found {
		t.Fatal("a seek past the end ended a looping Voice, and a looping Voice never ends by itself")
	}
	if want := float32(0.5 + step); got.Info.Playhead != want {
		t.Fatalf("the seek left the playhead at %v, want the loop start plus a tick, %v",
			got.Info.Playhead, want)
	}

	batches := h.backend.emitted()
	restart := batches[len(batches)-1].Starts
	if len(restart) != 1 {
		t.Fatalf("the seek produced %d starts, want the one the seam says a seek with", len(restart))
	}
	if restart[0].Offset != time.Second/2 {
		t.Fatalf("the start carries an offset of %v, want the loop start 500ms", restart[0].Offset)
	}
	if !restart[0].Loop {
		t.Fatal("the restart does not carry the Loop the Voice was playing with")
	}
}

// A Clip that declares no region loops the whole of itself, which is what an
// absent region means and what every Clip said before the tags existed.
func TestAClipWithNoRegionStillLoopsWhole(t *testing.T) {
	h := newHarness(t, newFakeBackend(fakeClip{duration: 1, channels: 2, rate: 48000}),
		Config{}, clipBytes)

	voice := h.play(ClipWithResource(bell), 0, Params{Loop: m.Some(true)})
	for range 64 {
		h.tick()
	}

	if got := h.probe(voice).Info.Playhead; got != 0 {
		t.Fatalf("an untagged Clip wrapped to %v after a whole pass, want 0", got)
	}
	h.record(func(queue *Queue) { queue.Seek(voice, 9) })
	h.tick()
	if got := h.probe(voice).Info.Playhead; got != float32(step) {
		t.Fatalf("a seek past the end of an untagged looping Clip landed at %v, want a tick past 0", got)
	}
}

// A region a Clip's own duration does not contain is not trusted into the wrap.
// The Adapter drops a malformed region itself and reports it, so this is not
// distrust of the Adapter - it is that wrap subtracts a span in a loop, and a
// span that is not inside the Clip is a loop with no reason to stop.
func TestARegionOutsideTheClipLoopsTheWholeClipInstead(t *testing.T) {
	h := newHarness(t, newFakeBackend(fakeClip{
		duration: 1, channels: 2, rate: 48000,
		region: m.Some(LoopRegion{Start: 0.5, End: 4}),
	}), Config{}, clipBytes)

	voice := h.play(ClipWithResource(bell), 0, Params{Loop: m.Some(true)})
	for range 64 {
		h.tick()
	}

	if got := h.probe(voice).Info.Playhead; got != 0 {
		t.Fatalf("a region past the Clip's end wrapped the playhead to %v, want the whole Clip's 0", got)
	}
}
