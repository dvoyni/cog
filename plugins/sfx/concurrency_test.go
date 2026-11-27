package sfx_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/sfxtest"
)

// The tests in this file are sfx's concurrency tests: run under
// go test -race, in WSL on Windows, each fails once the guard its comment
// names is taken away. The allocation gate's beats run under the detector
// too, counting nothing.

// concurrentBeatCount is how many beats each test's tasks record in.
const concurrentBeatCount = 96

// concurrentRecord is what one of concurrentGame's recorders is scheduled
// with: the beat it runs in, from 0, and which recorder it is. A reader is
// scheduled with the beat alone.
type concurrentRecord struct {
	beat  int
	index int
}

// concurrentGame is a test plugin whose tasks use sfx together on each beat,
// none ordered after another nor against the harness's flush, so each runs
// before the flush, waits on its locks or runs after it, as the kernel finds
// them: recorderCount recording tasks under the queue's write lock, and
// readerCount readers of each of the live voices, the bus volumes with the
// listener, the clip table and the device, each under the read locks of
// those views alone, so a reader waits on the flush only for what it reads.
// The beat's handler joins them into its out dependency, so a Step returns
// once they finish.
//
// Each recorder, until stopped is set, plays two voices of the resident
// clip, one a few beats from its end and one from its head, moves the first,
// seeks the second, stops the first voice it played on the beat before, and
// sets the effects bus's volume and the listener's X to one value of its
// own, which no other record sets; every tenth beat it pauses or resumes
// every voice. Each reader checks that what it reads is one flush's: the
// live voices count what they yield, none past its clip's end; the bus's
// volume and the listener's X are one record's value, or both as sfx
// started; the clip, once ready, has its duration; and the device is the
// headless one.
type concurrentGame struct {
	recorderCount int
	readerCount   int
	// stopped is whether the recorders have stopped recording: the test sets
	// it before the beats that let the last records land.
	stopped atomic.Bool
	// beatCount counts the beats handled; only the beat's handler, run once
	// a beat in turn, reads and writes it.
	beatCount int
	// mutex guards minted, every handle the recorders minted; lastPlayed, the
	// first voice each recorder played on its last beat, by its index; and
	// inconsistencies, what the readers found that no one flush would leave.
	mutex           sync.Mutex
	minted          []sfx.Voice
	lastPlayed      []sfx.Voice
	inconsistencies []string
}

func (*concurrentGame) Name() kernel.PluginName { return "concurrentgame" }

func (*concurrentGame) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{sfx.PluginName}
}

func (g *concurrentGame) Init(configurator *kernel.Configurator, _ any) error {
	g.lastPlayed = make([]sfx.Voice, g.recorderCount)
	record := configurator.RegisterTask1(g.record)
	reads := []*kernel.Task[int, kernel.NoResult]{
		configurator.RegisterTask1(g.readVoices),
		configurator.RegisterTask2(g.readBusesAndListener),
		configurator.RegisterTask1(g.readClip),
		configurator.RegisterTask1(g.readDeviceView),
	}
	configurator.Subscribe(func(_ *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, beat metronome.UpdateEvent) (metronome.UpdateEvent, kernel.JobHandle) {
		beatIndex := g.beatCount
		g.beatCount++
		out := in
		for index := range g.recorderCount {
			out = ex.Join(out, ex.Schedule(record, concurrentRecord{beat: beatIndex, index: index}).Handle())
		}
		for range g.readerCount {
			for _, read := range reads {
				out = ex.Join(out, ex.Schedule(read, beatIndex).Handle())
			}
		}
		return beat, out
	})
	return nil
}

func (*concurrentGame) Start(*kernel.Kernel) error { return nil }

func (*concurrentGame) Stop(*kernel.Kernel) error { return nil }

func (*concurrentGame) Destroy() error { return nil }

// record is one recorder's beat.
func (g *concurrentGame) record(_ *kernel.Kernel, at concurrentRecord, queueHandle kernel.Write[sfx.Queue]) kernel.NoResult {
	if g.stopped.Load() {
		return kernel.NoResult{}
	}
	queue := queueHandle.Get()
	value := float32(at.beat*g.recorderCount+at.index+1) / 1024
	first := queue.Play(clipPath, clipDuration-time.Duration(2+at.index)*sfxtest.StepInterval, sfx.VoiceOptions{Bus: m.Some(busEffects), Position: m.Some(m.Vec3{X: value})})
	second := queue.Play(clipPath, 0, sfx.VoiceOptions{Priority: m.Some(at.index % 2)})
	queue.SetVoice(first, sfx.VoiceOptions{Position: m.Some(m.Vec3{X: value, Z: -1})})
	queue.Seek(second, time.Duration(at.beat%4)*sfxtest.StepInterval)
	queue.SetBus(busEffects, value)
	queue.SetListener(sfx.ListenerOptions{Position: m.Some(m.Vec3{X: value})})
	if at.beat%10 == 0 {
		queue.SetPaused(at.beat%20 == 0)
	}
	g.mutex.Lock()
	defer g.mutex.Unlock()
	queue.Stop(g.lastPlayed[at.index])
	g.lastPlayed[at.index] = first
	g.minted = append(g.minted, first, second)
	return kernel.NoResult{}
}

// readVoices is one reader's beat of the live voices alone.
func (g *concurrentGame) readVoices(_ *kernel.Kernel, beat int, voicesHandle kernel.Read[sfx.LiveVoices]) kernel.NoResult {
	voices := voicesHandle.Get()
	yieldedCount := 0
	for entry := range voices.All() {
		yieldedCount++
		if entry.Duration > 0 && entry.Playhead > entry.Duration {
			g.noteInconsistency(fmt.Sprintf("beat %d: %v stands at %v of a %v clip", beat, entry.Voice, entry.Playhead, entry.Duration))
		}
	}
	if length := voices.Len(); yieldedCount != length {
		g.noteInconsistency(fmt.Sprintf("beat %d: the live voices yielded %d and count %d", beat, yieldedCount, length))
	}
	return kernel.NoResult{}
}

// readBusesAndListener is one reader's beat of the bus volumes and the
// listener together.
func (g *concurrentGame) readBusesAndListener(_ *kernel.Kernel, beat int, busesHandle kernel.Read[sfx.BusVolumes], listenerHandle kernel.Read[sfx.Listener]) kernel.NoResult {
	volume := busesHandle.Get().Volume(busEffects)
	listenerAt := listenerHandle.Get().Position().X
	if volume != listenerAt && (volume != 1 || listenerAt != 0) {
		g.noteInconsistency(fmt.Sprintf("beat %d: the effects bus is at %v and the listener's X at %v, two records' values", beat, volume, listenerAt))
	}
	return kernel.NoResult{}
}

// readClip is one reader's beat of the clip table alone.
func (g *concurrentGame) readClip(_ *kernel.Kernel, beat int, clipsHandle kernel.Read[sfx.ClipTable]) kernel.NoResult {
	if info, state := clipsHandle.Get().Info(clipPath); state == sfx.ClipReady && info.Duration != clipDuration {
		g.noteInconsistency(fmt.Sprintf("beat %d: the clip is ready at %v, want %v", beat, info.Duration, clipDuration))
	}
	return kernel.NoResult{}
}

// readDeviceView is one reader's beat of the device alone.
func (g *concurrentGame) readDeviceView(_ *kernel.Kernel, beat int, deviceHandle kernel.Read[sfx.DeviceView]) kernel.NoResult {
	if device := deviceHandle.Get().Device(); device.Ready && device.Name != "headless" {
		g.noteInconsistency(fmt.Sprintf("beat %d: the device reads %+v, want the headless one", beat, device))
	}
	return kernel.NoResult{}
}

// noteInconsistency keeps what a reader found that no one flush would
// leave.
func (g *concurrentGame) noteInconsistency(inconsistency string) {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.inconsistencies = append(g.inconsistencies, inconsistency)
}

// stepConcurrentGame steps h concurrentBeatCount beats with game recording,
// and then two more without, for the records of the last beat, which may
// have run after its flush, to land, and answers every handle the game's
// recorders minted.
func stepConcurrentGame(h *sfxtest.Harness, game *concurrentGame) []sfx.Voice {
	for range concurrentBeatCount {
		h.Step()
	}
	game.stopped.Store(true)
	h.Step()
	h.Step()
	game.mutex.Lock()
	defer game.mutex.Unlock()
	return game.minted
}

// Recording tasks write the queue on every beat while the flush drains it,
// none ordered before it, so each record lands in the flush that next takes
// the queue's lock. None is lost: every voice they played is observable
// exactly once, in the live view or through its one ending, at a cap the
// plays overrun, so stealing mints handles that name no slot. Run under
// -race, it fails with Queue taken out of the flush's locks, the flush
// draining the queue through a copy of it kept in its private resource.
func TestRecordersBesideTheFlushLoseNoOperation(t *testing.T) {
	game := &concurrentGame{recorderCount: 4}
	h := newHarness(t, &sfx.Config{MaxVoices: 8}, game)

	minted := stepConcurrentGame(h, game)

	if want := concurrentBeatCount * game.recorderCount * 2; len(minted) != want {
		t.Fatalf("the recorders minted %d handles, want %d", len(minted), want)
	}
	endingCounts := map[sfx.Voice]int{}
	for _, ending := range h.Endings() {
		endingCounts[ending.Voice]++
	}
	h.Read(func(voices sfx.LiveVoices, _ sfx.ClipTable, _ sfx.BusVolumes, _ sfx.Listener, _ sfx.DeviceView) {
		for _, voice := range minted {
			_, live := voices.Info(voice)
			observedCount := endingCounts[voice]
			if live {
				observedCount++
			}
			if observedCount != 1 {
				t.Errorf("%v is observable %d times, live %t, want exactly once", voice, observedCount, live)
			}
		}
	})
}

// Reading tasks read the views on every beat, beside recorders and the
// flush, none ordered against another: what each reads is one flush's, never
// half of one, since the flush holds every view's write lock at once. Run
// under -race, it fails with the flush holding LiveVoices under kernel.Read
// rather than kernel.Write.
func TestReadersOfTheViewsNeverSeeHalfAFlush(t *testing.T) {
	game := &concurrentGame{recorderCount: 2, readerCount: 4}
	h := newHarness(t, &sfx.Config{MaxVoices: 8}, game)

	stepConcurrentGame(h, game)

	game.mutex.Lock()
	defer game.mutex.Unlock()
	for _, inconsistency := range game.inconsistencies {
		t.Error(inconsistency)
	}
}
