package sfx_test

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/sfxtest"
)

// clipPath is where the tests mount the fixture clip, the headless backend's
// own: 48704 frames of stereo at 44.1 kHz.
const clipPath = "music/pianoroll.ogg"

// clipSeconds is the fixture's length as the seam carries it, in float32
// seconds, and clipDuration as the view carries it, converted the way sfx
// converts it.
var clipSeconds = float32(48704) / float32(44100)

var clipDuration = time.Duration(float64(clipSeconds) * float64(time.Second))

// endsOnStep is the step the fixture's one-shot ends on: its duration divided
// by the step, rounded up. 1.104399 s at a sixty-fourth of a second a step is
// 70.68 steps, so the voice is still playing after 70 and gone after 71.
const endsOnStep = 71

// newHarness answers a harness spawned with config and plugins, the fixture
// clip mounted at clipPath.
func newHarness(t *testing.T, config *sfx.Config, plugins ...kernel.Plugin) *sfxtest.Harness {
	t.Helper()
	clip, err := os.ReadFile("headless/testdata/pianoroll.ogg")
	if err != nil {
		t.Fatalf("reading the fixture clip: %v", err)
	}
	h := sfxtest.NewHarness(t)
	h.Spawn(config, plugins...)
	h.Mount(clipPath, clip)
	return h
}

// play records a play of the fixture clip and answers its handle.
func play(h *sfxtest.Harness) sfx.Voice {
	var voice sfx.Voice
	h.Record(func(queue sfx.Queue) { voice = queue.Play(clipPath, 0, sfx.VoiceOptions{}) })
	h.Step()
	return voice
}

// readLiveVoice answers voice's entry in the view, and whether it is live.
func readLiveVoice(h *sfxtest.Harness, voice sfx.Voice) (sfx.VoiceEntry, bool) {
	var entry sfx.VoiceEntry
	var live bool
	h.Read(func(voices sfx.LiveVoices, _ sfx.ClipTable) { entry, live = voices.Info(voice) })
	return entry, live
}

// readLiveCount answers how many voices the view holds.
func readLiveCount(h *sfxtest.Harness) int {
	var count int
	h.Read(func(voices sfx.LiveVoices, _ sfx.ClipTable) { count = voices.Len() })
	return count
}

// wantEndings fails the test unless the endings published so far are want.
func wantEndings(t *testing.T, h *sfxtest.Harness, want ...sfx.VoiceEndedEvent) {
	t.Helper()
	if got := h.Endings(); !slices.Equal(got, want) {
		t.Fatalf("the endings are %v, want %v", got, want)
	}
}

// seamPlugin wraps sfx's BackendSocket, whichever binds it, to keep what
// crosses the seam: the slot count and a copy of each flush's changes, and
// to answer seam's device in place of the bound backend's when it holds one.
type seamPlugin struct{ seam *seam }

func (seamPlugin) Name() kernel.PluginName { return "seam" }

func (seamPlugin) Dependencies() []kernel.PluginName { return nil }

func (p seamPlugin) Init(configurator *kernel.Configurator, _ any) error {
	configurator.WrapSocket[sfx.BackendSocket](func(bound sfx.BackendSocket) sfx.BackendSocket {
		return seamBackend{BackendSocket: bound, seam: p.seam}
	})
	return nil
}

func (seamPlugin) Start(*kernel.Kernel) error { return nil }

func (seamPlugin) Stop(*kernel.Kernel) error { return nil }

func (seamPlugin) Destroy() error { return nil }

// seam is what crossed the seam, kept by seamBackend.
type seam struct {
	mutex      sync.Mutex
	slotCounts []int
	changes    []sfx.FlushChanges
	// device is what the backend answers for its device, when present.
	device m.Maybe[sfx.Device]
}

// emitted answers a copy of each flush's changes so far.
func (s *seam) emitted() []sfx.FlushChanges {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return slices.Clone(s.changes)
}

// seamBackend is the bound backend, keeping what it is told in seam.
type seamBackend struct {
	sfx.BackendSocket
	seam *seam
}

func (b seamBackend) Voices(n int) {
	b.seam.mutex.Lock()
	b.seam.slotCounts = append(b.seam.slotCounts, n)
	b.seam.mutex.Unlock()
	b.BackendSocket.Voices(n)
}

func (b seamBackend) Emit(changes *sfx.FlushChanges) {
	b.seam.mutex.Lock()
	b.seam.changes = append(b.seam.changes, sfx.FlushChanges{
		Starts:   slices.Clone(changes.Starts),
		Updates:  slices.Clone(changes.Updates),
		Stops:    slices.Clone(changes.Stops),
		Destroys: slices.Clone(changes.Destroys),
	})
	b.seam.mutex.Unlock()
	b.BackendSocket.Emit(changes)
}

func (b seamBackend) Device() sfx.Device {
	if device, present := b.seam.device.Get(); present {
		return device
	}
	return b.BackendSocket.Device()
}

// The whole vertical path in one test: a game records a play, gets a voice,
// and the voice ends on the flush its duration says it should, not a flush
// early and not a flush late. The voice plays from flush 1, because the read
// and the prepare both happen inside that flush, and the playhead advances in
// the same flush that created it.
func TestAOneShotEndsOnTheFlushItsDurationSaysItShould(t *testing.T) {
	h := newHarness(t, nil)

	voice := play(h)
	for step := 2; step < endsOnStep; step++ {
		h.Step()
		if _, live := readLiveVoice(h, voice); !live {
			t.Fatalf("%v was gone after flush %d, and the clip is %v long", voice, step, clipDuration)
		}
	}
	wantEndings(t, h)

	h.Step()
	if _, live := readLiveVoice(h, voice); live || readLiveCount(h) != 0 {
		t.Fatalf("after flush %d the view still holds %v (%d live)", endsOnStep, voice, readLiveCount(h))
	}
	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonFinished})
}

// Under the headless backend the voice ends when its clip's header says it
// does, and the view carries its path, its playhead flush by flush, and the
// header's duration.
func TestAOneShotUnderHeadlessEndsOnTheFlushItsDurationSaysItShould(t *testing.T) {
	h := newHarness(t, nil)

	voice := play(h)
	for step := 1; step < endsOnStep; step++ {
		if step > 1 {
			h.Step()
		}
		entry, live := readLiveVoice(h, voice)
		if !live {
			t.Fatalf("%v was gone after flush %d, and the clip is %v long", voice, step, clipDuration)
		}
		want := sfx.VoiceEntry{Voice: voice, Path: clipPath, Playhead: time.Duration(step) * sfxtest.StepInterval, Duration: clipDuration}
		if entry != want {
			t.Fatalf("after flush %d the view holds %+v, want %+v", step, entry, want)
		}
	}

	h.Step()
	if _, live := readLiveVoice(h, voice); live {
		t.Fatalf("after flush %d the view still holds %v", endsOnStep, voice)
	}
	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonFinished})
}

// A stop ends a voice at the flush that applies it, and the ending says who
// ended it, so a subscriber never has to tell "it ended" from "I ended it".
func TestAStoppedVoiceEndsAsStopped(t *testing.T) {
	h := newHarness(t, nil)

	voice := play(h)
	h.Record(func(queue sfx.Queue) { queue.Stop(voice) })
	h.Step()

	if _, live := readLiveVoice(h, voice); live {
		t.Fatalf("%v outlived its stop", voice)
	}
	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonStopped})
}

// The handle is minted when the play is recorded, so it addresses the voice
// in the flush that recorded it. A play and a stop in one flush is the
// sharpest case: the stop names a voice that does not exist yet, and by the
// time the flush applies it, it does. Nothing crosses the seam, because
// nothing was ever audible: a voice that began and ended inside one flush has
// no start to emit and no stop to pair with one.
func TestAHandleIsUsableInTheFlushThatRecordedIt(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, nil, seamPlugin{seam: seam})

	var voice sfx.Voice
	h.Record(func(queue sfx.Queue) {
		voice = queue.Play(clipPath, 0, sfx.VoiceOptions{})
		queue.Stop(voice)
	})
	h.Step()

	if voice == sfx.NoVoice {
		t.Fatal("Play handed back NoVoice")
	}
	if _, live := readLiveVoice(h, voice); live || readLiveCount(h) != 0 {
		t.Fatalf("the view holds %v (%d live) after a play and a stop in one flush", voice, readLiveCount(h))
	}
	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonStopped})
	if changes := seam.emitted()[0]; len(changes.Starts) != 0 || len(changes.Stops) != 0 {
		t.Fatalf("a voice that never sounded crossed the seam: %d starts, %d stops", len(changes.Starts), len(changes.Stops))
	}
}

// The surface is total. Every operation is a no-op on a voice that is gone,
// and on NoVoice, with no error and no response field to check, which is what
// a stop racing a clip that finished a flush ago needs.
func TestEveryOperationIsANoOpOnAVoiceThatIsGone(t *testing.T) {
	h := newHarness(t, nil)

	voice := play(h)
	h.Record(func(queue sfx.Queue) { queue.Stop(voice) })
	h.Step()
	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonStopped})

	h.Record(func(queue sfx.Queue) {
		queue.Stop(voice)
		queue.Stop(sfx.NoVoice)
	})
	h.Step()

	if _, live := readLiveVoice(h, voice); live || readLiveCount(h) != 0 {
		t.Fatalf("a stale handle addressed something: live=%v, %d live", live, readLiveCount(h))
	}
	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonStopped})
}

// One flush, one Emit, whatever the flush held, and the backend is told how
// many slots exist once, before any of them.
func TestTheFlushEmitsOneFlushChangesPerFlush(t *testing.T) {
	seam := &seam{}
	h := newHarness(t, &sfx.Config{MaxVoices: 16}, seamPlugin{seam: seam})

	if !slices.Equal(seam.slotCounts, []int{16}) {
		t.Fatalf("the backend was told the slot counts %v, want 16 once", seam.slotCounts)
	}

	voice := play(h)
	for range endsOnStep - 1 {
		h.Step()
	}
	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: voice, Reason: sfx.ReasonFinished})

	changes := seam.emitted()
	if len(changes) != endsOnStep {
		t.Fatalf("%d flushes emitted %d changes", endsOnStep, len(changes))
	}
	if starts := changes[0].Starts; len(starts) != 1 || starts[0].Slot != 0 || starts[0].Clip == 0 || starts[0].Offset != 0 {
		t.Fatalf("the first flush's starts are %+v, want one in slot 0 from the head of a real clip", starts)
	}
	for _, quiet := range changes[1 : endsOnStep-1] {
		if len(quiet.Starts) != 0 || len(quiet.Stops) != 0 {
			t.Fatalf("a quiet flush carried %d starts and %d stops", len(quiet.Starts), len(quiet.Stops))
		}
	}
	if stops := changes[endsOnStep-1].Stops; !slices.Equal(stops, []sfx.VoiceSlot{0}) {
		t.Fatalf("the ending flush's stops are %v, want slot 0 alone", stops)
	}
}

// A play that finds every slot taken still gets a real handle, and its ending
// arrives in the flush that recorded it, never having been live.
func TestAPlayThatFindsNoSlotEndsInTheFlushThatRecordedIt(t *testing.T) {
	h := newHarness(t, &sfx.Config{MaxVoices: 1})

	var held, overflow sfx.Voice
	h.Record(func(queue sfx.Queue) {
		held = queue.Play(clipPath, 0, sfx.VoiceOptions{})
		overflow = queue.Play(clipPath, 0, sfx.VoiceOptions{})
	})
	h.Step()

	if overflow == sfx.NoVoice {
		t.Fatal("the play that lost got no handle at all")
	}
	if _, live := readLiveVoice(h, overflow); live {
		t.Fatal("the play that lost is in the view")
	}
	if _, live := readLiveVoice(h, held); !live || readLiveCount(h) != 1 {
		t.Fatalf("the view holds %d voices, want the one that kept its slot", readLiveCount(h))
	}
	wantEndings(t, h, sfx.VoiceEndedEvent{Voice: overflow, Reason: sfx.ReasonStolen})
}

// Every voice the view holds is in it once, with its path, and Len and Cap
// answer the live voices and the cap under one read.
func TestTheViewHoldsEveryLiveVoiceWithItsPath(t *testing.T) {
	h := newHarness(t, nil)

	var first, second sfx.Voice
	h.Record(func(queue sfx.Queue) {
		first = queue.Play(clipPath, 0, sfx.VoiceOptions{})
		second = queue.Play(clipPath, 0, sfx.VoiceOptions{})
	})
	h.Step()

	h.Read(func(voices sfx.LiveVoices, _ sfx.ClipTable) {
		var held []sfx.Voice
		for entry := range voices.All() {
			if entry.Path != clipPath {
				t.Errorf("%v plays %q, want %q", entry.Voice, entry.Path, clipPath)
			}
			held = append(held, entry.Voice)
		}
		if want := []sfx.Voice{first, second}; !slices.Equal(held, want) {
			t.Errorf("All yields %v, want %v", held, want)
		}
		if voices.Len() != 2 || voices.Cap() != 64 {
			t.Errorf("Len is %d and Cap %d, want 2 live under the default cap of 64", voices.Len(), voices.Cap())
		}
	})
}

// A composition with no backend is a composition error: sfx's socket is
// exclusive and not optional, so Spawn panics naming it.
func TestASpawnWithNoBackendPanics(t *testing.T) {
	supervisor := kernel.NewSupervisor(nil)
	t.Cleanup(supervisor.Quit)
	defer func() {
		if recovered := recover(); recovered == nil || !strings.Contains(fmt.Sprint(recovered), "BackendSocket") {
			t.Fatalf("a spawn with no backend panicked with %v, want a panic naming the BackendSocket", recovered)
		}
	}()
	supervisor.Spawn(nil, 1, platform.NewPlugin(), sfx.NewPlugin())
}

// A negative cap is a composition error, and panics in Init.
func TestANegativeMaxVoicesPanics(t *testing.T) {
	supervisor := kernel.NewSupervisor(nil)
	t.Cleanup(supervisor.Quit)
	defer func() {
		if recovered := recover(); recovered == nil || !strings.Contains(fmt.Sprint(recovered), "MaxVoices") {
			t.Fatalf("a negative MaxVoices panicked with %v, want a panic naming MaxVoices", recovered)
		}
	}()
	supervisor.Spawn(map[kernel.PluginName]any{sfx.PluginName: &sfx.Config{MaxVoices: -1}}, 1, platform.NewPlugin(), sfx.NewPlugin())
}
