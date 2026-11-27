package sfx_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/sfxtest"
)

// sfx keeps a clip's loop region for two reasons of its own, and these are
// them: a looping voice's playhead wraps at the loop end rather than at the
// duration, and a seek past the end wraps to the loop start rather than to
// zero. Everything else about the region is the backend's. Nothing on the
// game's face moves for it: Loop is still a bool, and has only stopped
// meaning repeat the whole clip and started meaning repeat the way this clip
// says to.

// introLoop is a region with an intro and a tail around it inside the
// fixture: it repeats between 0.5 s and 0.75 s, the 48th step, and the rest
// of the clip after it is never heard by a looping voice.
var introLoop = m.Some(sfx.LoopRegion{Start: 0.5, End: 0.75})

// loopRegionPlugin wraps sfx's BackendSocket so every clip it prepares
// declares region, and answers regionErr from LoopRegionErr, as a backend
// that read the clip's tags would.
type loopRegionPlugin struct {
	region    m.Maybe[sfx.LoopRegion]
	regionErr error
}

func (loopRegionPlugin) Name() kernel.PluginName { return "loopregion" }

func (loopRegionPlugin) Dependencies() []kernel.PluginName { return nil }

func (p loopRegionPlugin) Init(configurator *kernel.Configurator, _ any) error {
	configurator.WrapSocket[sfx.BackendSocket](func(bound sfx.BackendSocket) sfx.BackendSocket {
		return loopRegionBackend{BackendSocket: bound, plugin: p}
	})
	return nil
}

func (loopRegionPlugin) Start(*kernel.Kernel) error { return nil }

func (loopRegionPlugin) Stop(*kernel.Kernel) error { return nil }

func (loopRegionPlugin) Destroy() error { return nil }

// loopRegionBackend is the bound backend, its prepared clips declaring the
// plugin's region.
type loopRegionBackend struct {
	sfx.BackendSocket
	plugin loopRegionPlugin
}

func (b loopRegionBackend) Prepare(token any, encoded []byte) (sfx.PreparedClip, bool, error) {
	prepared, done, err := b.BackendSocket.Prepare(token, encoded)
	if prepared != nil {
		prepared = loopRegionClip{PreparedClip: prepared, region: b.plugin.region, regionErr: b.plugin.regionErr}
	}
	return prepared, done, err
}

// loopRegionClip is a prepared clip declaring region, regionErr saying why
// one was dropped.
type loopRegionClip struct {
	sfx.PreparedClip
	region    m.Maybe[sfx.LoopRegion]
	regionErr error
}

func (c loopRegionClip) LoopRegion() m.Maybe[sfx.LoopRegion] { return c.region }

func (c loopRegionClip) LoopRegionErr() error { return c.regionErr }

// The playhead wraps at the loop end and not at the duration. A voice that
// wrapped at the duration would report itself playing through a tail the
// backend stopped playing a bar ago, and the view would be describing a
// different sound from the one in the room.
func TestALoopingPlayheadWrapsAtTheLoopEndAndNotAtTheDuration(t *testing.T) {
	h := newHarness(t, nil, loopRegionPlugin{region: introLoop})

	voice := playWith(h, sfx.VoiceOptions{Loop: m.Some(true)})
	for step := 1; step < 48; step++ {
		if step > 1 {
			h.Step()
		}
		if got, want := readPlayhead(h, voice), time.Duration(step)*sfxtest.StepInterval; got != want {
			t.Fatalf("after flush %d the playhead is %v, want %v", step, got, want)
		}
	}
	h.Step()

	got, live := readLiveVoice(h, voice)
	if !live {
		t.Fatal("the voice ended at its loop end, and a looping voice never ends by itself")
	}
	if got.Playhead != 500*time.Millisecond {
		t.Fatalf("the wrap landed at %v, want the loop start 500ms", got.Playhead)
	}
	if got.Duration != clipDuration {
		t.Fatalf("the view reads a duration of %v, want the clip's whole %v: the region is not the length", got.Duration, clipDuration)
	}
	wantEndings(t, h)

	// And it keeps going round the region rather than round the clip.
	for range 16 {
		h.Step()
	}
	if replay := readPlayhead(h, voice); replay != 500*time.Millisecond {
		t.Fatalf("a second pass of the loop landed at %v, want the loop start 500ms", replay)
	}
}

// A seek past the end of a looping voice lands on the loop start, which is
// what the loop region is felt as on the game's face: a seek off the end of
// a track with an intro lands in the loop rather than replaying the intro.
// What crosses the seam is an ordinary VoiceStart carrying an Offset, and the
// Loop the voice plays with.
func TestASeekPastTheEndOfALoopingVoiceLandsOnTheLoopStart(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, loopRegionPlugin{region: introLoop}, seamPlugin{seam: seam})

	voice := playWith(h, sfx.VoiceOptions{Loop: m.Some(true)})
	h.Record(func(queue sfx.Queue) { queue.Seek(voice, 9*time.Second) })
	h.Step()

	got, live := readLiveVoice(h, voice)
	if !live {
		t.Fatal("a seek past the end ended a looping voice, and a looping voice never ends by itself")
	}
	if want := 500*time.Millisecond + sfxtest.StepInterval; got.Playhead != want {
		t.Fatalf("the seek left the playhead at %v, want the loop start and a step, %v", got.Playhead, want)
	}
	restart := lastChanges(seam).Starts
	if len(restart) != 1 || restart[0].Offset != 500*time.Millisecond || !restart[0].Loop {
		t.Fatalf("the seek emitted the starts %+v, want one looping from the loop start 500ms", restart)
	}
}

// A clip that declares no region loops the whole of itself, which is what an
// absent region means and what every clip said before the tags existed.
func TestAClipWithNoRegionStillLoopsWhole(t *testing.T) {
	h := newHarness(t, nil)

	voice := playWith(h, sfx.VoiceOptions{Loop: m.Some(true)})
	for range endsOnStep - 1 {
		h.Step()
	}
	if got, want := readPlayhead(h, voice), endsOnStep*sfxtest.StepInterval-clipDuration; got != want {
		t.Fatalf("an untagged clip wrapped to %v after a whole pass, want %v past its head", got, want)
	}

	h.Record(func(queue sfx.Queue) { queue.Seek(voice, 9*time.Second) })
	h.Step()
	if got := readPlayhead(h, voice); got != sfxtest.StepInterval {
		t.Fatalf("a seek past the end of an untagged looping clip landed at %v, want a step past its head", got)
	}
}

// A region the clip's own duration does not contain is not trusted into the
// wrap. The backend drops a malformed region itself and reports it, so this
// is not distrust of the backend: the wrap subtracts a span, and a span that
// is not inside the clip is a loop with no reason to stop.
func TestARegionOutsideTheClipLoopsTheWholeClipInstead(t *testing.T) {
	h := newHarness(t, nil, loopRegionPlugin{region: m.Some(sfx.LoopRegion{Start: 0.5, End: 4})})

	voice := playWith(h, sfx.VoiceOptions{Loop: m.Some(true)})
	for range endsOnStep - 1 {
		h.Step()
	}
	if got, want := readPlayhead(h, voice), endsOnStep*sfxtest.StepInterval-clipDuration; got != want {
		t.Fatalf("a region past the clip's end wrapped the playhead to %v, want the whole clip's %v", got, want)
	}
}

// The report half of the headless backend's drop: a backend answers a
// dropped region from LoopRegionErr with no path, since it has no name for a
// clip, and sfx reports it once for the clip, under the clip's path, however
// many voices play it. The clip prepares and plays whole. A release forgets
// the report, so the clip named again is reported again. The headless
// backend's own tests prove the drop; the plugin here answers what it does
// for an end past the clip.
func TestHeadlessDropsAMalformedRegionWholeAndReportsItOncePerClip(t *testing.T) {
	dropped := sfx.ErrLoopRegionIgnored{Start: 11025, End: 48705, Frames: 48704}
	h := newHarness(t, nil, loopRegionPlugin{regionErr: dropped})
	h.ExpectError(func(err error) bool {
		_, ignored := errors.AsType[sfx.ErrLoopRegionIgnored](err)
		return ignored
	})

	voice := playWith(h, sfx.VoiceOptions{Loop: m.Some(true)})
	playWith(h, sfx.VoiceOptions{})
	reported := dropped
	reported.Path = clipPath
	if got := h.Reports(); !slices.Equal(got, []error{reported}) {
		t.Fatalf("two plays of a clip whose region was dropped reported %v, want %v once", got, reported)
	}
	for range endsOnStep - 2 {
		h.Step()
	}
	if got, want := readPlayhead(h, voice), endsOnStep*sfxtest.StepInterval-clipDuration; got != want {
		t.Fatalf("the clip whose region was dropped wrapped to %v, want the whole clip's %v", got, want)
	}

	h.Record(func(queue sfx.Queue) { queue.Release(clipPath) })
	h.Step()
	playWith(h, sfx.VoiceOptions{})
	if got := h.Reports(); !slices.Equal(got, []error{reported, reported}) {
		t.Fatalf("the clip named again after its release reported %v, want the drop once more", got)
	}
}
