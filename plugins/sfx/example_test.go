package sfx_test

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/platform/filesystems"
	"github.com/dvoyni/cog/plugins/sfx"
	"github.com/dvoyni/cog/plugins/sfx/headless"
)

// A steal is final: nothing in sfx resumes a stolen voice, and a game that
// wants the sound back plays it again as a new voice. VoiceEndedEvent
// carries only the handle and the reason, and an ended handle addresses
// nothing, so what the new Play needs, the clip's path, where it had got to
// and its options, the game keeps from the live view, once a flush, before
// the voice can be stolen.
//
// Here the cap is one voice. A looping ambience is playing when a footstep
// arrives in the same band at the same volume; the tie goes to the newcomer,
// so the footstep takes the slot and the ambience ends as ReasonStolen. The
// game wants looping sounds back and not one-shots, so it plays the ambience
// again from where it would have been by now. That play ties with the
// footstep in turn and takes the slot straight back; a play that ranked
// below everything live would lose instead and end as ReasonStolen again,
// which is the game's to retry on a later flush or let go.
//
// The example composes the kernel a game does, rather than sfxtest's
// harness, which needs a test: platform with the clip in a memory mount,
// metronome on a clock it steps, sfx, and the game's own plugin, which binds
// the headless backend and schedules its recorder and then sfx's flush on
// each beat.
func Example_playAStolenVoiceAgain() {
	clip, err := os.ReadFile("headless/testdata/pianoroll.ogg")
	if err != nil {
		fmt.Println(err)
		return
	}
	clips := filesystems.NewMemoryFS()
	if err := clips.Put(clipPath, clip); err != nil {
		fmt.Println(err)
		return
	}
	clock := metronome.NewManualClock()
	game := &retryingGame{clips: clips, kept: map[sfx.Voice]sfx.VoiceEntry{}}
	supervisor := kernel.NewSupervisor(nil)
	defer supervisor.Quit()
	configs := map[kernel.PluginName]any{
		sfx.PluginName: &sfx.Config{MaxVoices: 1},
		metronome.PluginName: &metronome.Config{
			Clock:  clock,
			Tracks: []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(64))},
		},
	}
	if _, err := supervisor.Spawn(configs, 2, platform.NewPlugin(), metronome.NewPlugin(), sfx.NewPlugin(), game); err != nil {
		fmt.Println(err)
		return
	}

	// update is one beat: the game's recorder, then sfx's flush.
	const step = time.Second / 64
	update := func(record func(sfx.Queue)) {
		game.script(record)
		clock.Advance(step)
	}

	var first sfx.Voice
	update(func(queue sfx.Queue) { first = queue.Play(clipPath, 0, sfx.VoiceOptions{Loop: m.Some(true)}) })
	for range 31 {
		update(nil)
	}
	fmt.Println("ambience at", game.keptEntry(first).Playhead)

	update(func(queue sfx.Queue) { queue.Play(clipPath, 0, sfx.VoiceOptions{}) })
	fmt.Println("ambience stolen:", game.wasStolen(first))

	// The retry lands in the next beat, and the beat after it takes the
	// copy, the view after the thirty-fourth flush, by which an ambience
	// started in the first and never stolen would stand thirty-four steps in.
	update(nil)
	update(nil)
	for _, entry := range game.keptEntries() {
		fmt.Println("ambience back at", entry.Playhead, "looping", entry.Options.Loop.Or(false))
		fmt.Println("in step with a voice never stolen:", entry.Playhead == 34*step)
	}

	// Output:
	// ambience at 484.375ms
	// ambience stolen: true
	// ambience back at 531.25ms looping true
	// in step with a voice never stolen: true
}

// retryingGame is the recipe, as a game's plugin. Each beat its recorder
// plays again the stolen voices it wants back, from its copy advanced by the
// time since the copy was taken, records what the example scripted, and then
// takes a fresh copy of the live view. The copy is a beat old by the time
// the ending arrives: it was taken before the flush that stole the voice,
// the last moment the voice was in the view, so the elapsed time is that one
// beat's delta, and the new voice lands where the stolen one would have been.
type retryingGame struct {
	clips *filesystems.MemoryFS
	// mutex guards what the recorder, the ending handler and the example
	// share: scripted, the record for the next beat; kept, the copy of the
	// live view; and stolen, the voices stolen since the last beat.
	mutex    sync.Mutex
	scripted func(sfx.Queue)
	kept     map[sfx.Voice]sfx.VoiceEntry
	stolen   []sfx.Voice
}

func (*retryingGame) Name() kernel.PluginName { return "retryinggame" }

func (*retryingGame) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{sfx.PluginName}
}

func (g *retryingGame) Init(configurator *kernel.Configurator, _ any) error {
	configurator.BindSocket[sfx.BackendSocket](headless.NewBackend(0))
	configurator.BindSocket(platform.ReadMountSocket{FS: g.clips})
	tasks, _ := configurator.Resource[sfx.Tasks]()
	flush := tasks.Flush
	record := configurator.RegisterTask2(func(_ *kernel.Kernel, delta time.Duration, queue kernel.Write[sfx.Queue], voices kernel.Read[sfx.LiveVoices]) kernel.NoResult {
		g.update(queue.Get(), voices.Get(), delta)
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(_ *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, beat metronome.UpdateEvent) (metronome.UpdateEvent, kernel.JobHandle) {
		recorded := ex.Schedule(record, beat.Delta+beat.Lost)
		flushed := ex.Schedule(flush, beat.Delta+beat.Lost, recorded.Handle())
		return beat, ex.Join(in, flushed.Handle())
	})
	configurator.Subscribe(func(_ *kernel.Kernel, _ *kernel.Executor, in kernel.JobHandle, ending sfx.VoiceEndedEvent) (sfx.VoiceEndedEvent, kernel.JobHandle) {
		if ending.Reason == sfx.ReasonStolen {
			g.mutex.Lock()
			g.stolen = append(g.stolen, ending.Voice)
			g.mutex.Unlock()
		}
		return ending, in
	})
	return nil
}

func (*retryingGame) Start(*kernel.Kernel) error { return nil }

func (*retryingGame) Stop(*kernel.Kernel) error { return nil }

func (*retryingGame) Destroy() error { return nil }

// script has the next beat's recorder call record, when it is not nil.
func (g *retryingGame) script(record func(sfx.Queue)) {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.scripted = record
}

// keptEntry answers the copy of voice the game holds.
func (g *retryingGame) keptEntry(voice sfx.Voice) sfx.VoiceEntry {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	return g.kept[voice]
}

// keptEntries answers every voice in the copy the game holds.
func (g *retryingGame) keptEntries() []sfx.VoiceEntry {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	entries := make([]sfx.VoiceEntry, 0, len(g.kept))
	for _, entry := range g.kept {
		entries = append(entries, entry)
	}
	return entries
}

// wasStolen answers whether voice was stolen since the game's last beat.
func (g *retryingGame) wasStolen(voice sfx.Voice) bool {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	for _, stolen := range g.stolen {
		if stolen == voice {
			return true
		}
	}
	return false
}

// update is the recorder's beat: the retries, the scripted record, then the
// fresh copy of the live view.
func (g *retryingGame) update(queue sfx.Queue, voices sfx.LiveVoices, elapsed time.Duration) {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	for _, voice := range g.stolen {
		if was, kept := g.kept[voice]; kept && was.Options.Loop.Or(false) {
			queue.Play(was.Path, was.Playhead+elapsed, was.Options)
		}
	}
	g.stolen = g.stolen[:0]
	if g.scripted != nil {
		g.scripted(queue)
		g.scripted = nil
	}
	clear(g.kept)
	for entry := range voices.All() {
		g.kept[entry.Voice] = entry
	}
}
