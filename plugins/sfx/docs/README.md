# sfx guide

How to use `github.com/dvoyni/cog/plugins/sfx`, with examples: a first sound,
the flush a game schedules, voices and their options, endings and the live
view, buses, positional audio in 3D and in 2D, the pause, clips and their
memory, the device, stealing, and testing a game's sound on `sfxtest`. For the
reasons behind each choice, see [design.md](design.md); for the words it uses,
[CONTEXT.md](../CONTEXT.md). It assumes the
[kernel guide](../../../kernel/docs/README.md) and the
[platform guide](../../platform/docs/README.md).

A game that makes sound uses sfx alone: it names no device, no sample and no
mixer, and picks its backend once, in `main`.

- [Concepts](#concepts)
- [Quick start](#quick-start)
- [The plugin, its Config and its backend](#the-plugin-its-config-and-its-backend)
- [The flush](#the-flush)
- [Playing and changing a voice](#playing-and-changing-a-voice)
- [Endings](#endings)
- [The live view](#the-live-view)
- [Buses](#buses)
- [Positional audio](#positional-audio)
- [2D](#2d)
- [Pause](#pause)
- [Clips: Preload, Info and Release](#clips-preload-info-and-release)
- [Bytes that are not files](#bytes-that-are-not-files)
- [Failures and reports](#failures-and-reports)
- [The device](#the-device)
- [The cap and stealing](#the-cap-and-stealing)
- [Testing with sfxtest](#testing-with-sfxtest)
- [Rules](#rules)

## Concepts

```
 the game's handler of metronome.UpdateEvent
        │
        ├─ its recording tasks, each under kernel.Write[sfx.Queue]:
        │     Play, SetVoice, Seek, Stop · SetBus, StopBus · SetListener
        │     SetPaused · Preload, Release, ReleaseAll
        │
        └─ after them, sfx's Tasks.Flush, given beat.Delta + beat.Lost
              ├─ applies everything recorded, in order, a new clip read here
              ├─ advances every playhead, folds the buses, spatializes
              ├─ writes the views: LiveVoices, ClipTable, BusVolumes,
              │                    Listener, DeviceView
              ├─ hands the backend one FlushChanges (otosound, or headless)
              └─ publishes a VoiceEndedEvent for each voice that ended
```

| Type | What it is |
|---|---|
| `Plugin` | The kernel plugin, from `NewPlugin()`. |
| `Config` | sfx's part of the kernel's config: the voice cap and the bus count. |
| `Tasks` | The resource holding `Flush`, the task the game schedules on each update beat. |
| `Queue` | The resource every operation is recorded on, under `kernel.Write`. |
| `LiveVoices`, `VoiceEntry` | The view of the live voices, and one voice in it. |
| `ClipTable`, `ClipInfo`, `ClipState` | The view of what sfx knows of each clip, a clip's facts, and how far it has come. |
| `BusVolumes` | The view of each bus's volume. |
| `Listener` | The view of where the game is heard from. |
| `DeviceView`, `Device` | The view of the sound device, and the device as the backend reports it. |
| `Voice`, `NoVoice` | The handle a play answers, and the zero handle, which addresses nothing. |
| `VoiceOptions`, `ListenerOptions` | How a voice starts or what it becomes, and what the listener becomes: an absent field is the default, or unchanged. |
| `Bus`, `Master` | A group of voices the game declares, under the one bus sfx declares. |
| `Falloff`, `DistanceModel`, `Cone` | How a positional voice grows quieter with distance, and the directions it is loud in: W3C's parameters. |
| `VoiceEndedEvent`, `Reason` | The event of every ending, and why it ended. |
| `ErrClipUnreadable`, `ErrClipFailed`, `ErrLoopRegionIgnored`, `ErrClipNotPrepared` | What sfx logs about a clip, under its path. |
| `BackendSocket` | The socket a backend binds: otosound, or `headless.NewBackend`. |
| `sfxtest.Harness` | sfx in a real kernel over the headless backend, for tests. |

Words used throughout: a **clip** is a sound named by its path; a **voice**
is one playing instance of a clip, addressed by its handle; the **flush**
applies, at once and in the order they were recorded, everything recorded
since the last one; a **bus** is a group of voices sharing one volume; a
**positional voice** is one given a position, heard from the one
**listener**; a **backend** mixes what the flush hands it, and computes none
of it.

## Quick start

A game plays its music on a loop and the hero's footstep every half second,
on an update track of 60 beats a second. Its plugin mounts the files `main`
hands it and installs a resource of its own, what its recorder keeps between
beats. Each beat schedules its recorder, then sfx's flush with the beat's
delta and what the beat lost, both joined into the handler's out dependency.
The music is played once, on its own bus, looping and a band up in stealing;
each footstep is played and forgotten. otosound is the backend, listed beside
sfx. There is no load call: each clip is read at the flush that first names
its path.

```go
package main

import (
	"embed"
	"io/fs"
	"runtime"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/otosound"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/sfx"
)

const gameName kernel.PluginName = "game"

//go:embed assets
var embedded embed.FS

// The game's buses. sfx declares Master alone, and every other bus sits
// directly under it.
const (
	busMusic sfx.Bus = iota + 1
	busEffects
)

// beatsPerFootstep is how many update beats apart the hero's footsteps fall.
const beatsPerFootstep = 30

// soundState is the game's own resource: what its recorder keeps between
// beats.
type soundState struct {
	musicVoice sfx.Voice
	beatCount  int
}

// game plays its music and the hero's footsteps from files, its read mount.
type game struct {
	files fs.FS
}

func (*game) Name() kernel.PluginName { return gameName }

// Dependencies names platform and sfx: the game is listed after sfx, since
// its Init reads sfx.Tasks.
func (*game) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{platform.PluginName, sfx.PluginName}
}

func (p *game) Init(configurator *kernel.Configurator, _ any) error {
	configurator.BindSocket(platform.ReadMountSocket{FS: p.files})
	configurator.InitResource(soundState{})
	tasks, _ := configurator.Resource[sfx.Tasks]()
	flush := tasks.Flush
	record := configurator.RegisterTask2(recordSounds)
	// Each update beat records the game's sounds, then flushes them.
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, beat metronome.UpdateEvent) (metronome.UpdateEvent, kernel.JobHandle) {
		recorded := ex.Schedule(record, kernel.NoArg{})
		flushed := ex.Schedule(flush, beat.Delta+beat.Lost, recorded.Handle())
		return beat, ex.Join(in, flushed.Handle())
	})
	return nil
}

func (*game) Start(*kernel.Kernel) error { return nil }
func (*game) Stop(*kernel.Kernel) error  { return nil }
func (*game) Destroy() error             { return nil }

// recordSounds is the game's recorder, run on each beat before sfx's flush:
// the music at the first beat, and a footstep every beatsPerFootstep.
func recordSounds(k *kernel.Kernel, _ kernel.NoArg, queue kernel.Write[sfx.Queue], state kernel.Write[soundState]) kernel.NoResult {
	sounds := state.Get()
	if sounds.musicVoice == sfx.NoVoice {
		sounds.musicVoice = queue.Get().Play("music.ogg", 0, sfx.VoiceOptions{
			Bus:      m.Some(busMusic),
			Loop:     m.Some(true),
			Priority: m.Some(1),
		})
	}
	if sounds.beatCount%beatsPerFootstep == 0 {
		queue.Get().Play("step.ogg", 0, sfx.VoiceOptions{Bus: m.Some(busEffects)})
	}
	sounds.beatCount++
	state.Set(sounds)
	return kernel.NoResult{}
}

func init() { runtime.LockOSThread() }

func main() {
	assets, err := fs.Sub(embedded, "assets")
	if err != nil {
		panic(err)
	}
	supervisor := kernel.NewSupervisor(nil)
	config := map[kernel.PluginName]any{
		metronome.PluginName: &metronome.Config{
			Tracks: []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(60))},
		},
	}
	if _, err := supervisor.Spawn(config, runtime.GOMAXPROCS(0),
		platform.NewPlugin(),
		metronome.NewPlugin(),
		sfx.NewPlugin(),
		otosound.NewPlugin(), // the backend
		&game{files: assets},
	); err != nil {
		panic(err)
	}
	time.AfterFunc(10*time.Second, supervisor.Quit)
	for supervisor.WaitMainThread() {
		supervisor.TickMainThread()
	}
}
```

`assets/music.ogg` and `assets/step.ogg` are any Ogg Vorbis files. The
music's handle is minted when the play is recorded, before the flush that
applies it, so the game keeps it at once. A machine with no sound card runs
the game the same: the device reads not ready, and every playhead advances
all the same.

## The plugin, its Config and its backend

sfx is listed after platform, whose files its flush reads, and every plugin
that records into sfx or schedules its flush after sfx, since its `Init`
reads `sfx.Tasks`. Its `Config` sits under `sfx.PluginName`, and each field
has a default when zero:

```go
var soundConfig = map[kernel.PluginName]any{
	sfx.PluginName: &sfx.Config{
		// At most 32 voices at once; zero means 64.
		MaxVoices: 32,
		// Master and the game's two buses; zero means 32.
		MaxBuses: 3,
	},
}
```

A negative field panics in `Init`, as a composition error does.

sfx declares one socket, `sfx.BackendSocket`, exclusive and required: exactly
one backend binds it, and a kernel with none panics in `Spawn`. There is no
knob choosing one; the composition does:

- **otosound**, the plugin a game ships, listed beside sfx in either order.
  Its own `Config`, its buffer, sample rate and clip limits, is in
  [otosound's README](../../otosound/README.md).
- **The headless backend**, `headless.NewBackend(sampleRate)`, which plays
  nothing, for tests and for runs no one hears, such as a game's server. It
  is an adapter, not a plugin: the plugin that wants it binds it in its
  `Init`. A rate of zero means 48000.

```go
// silentServer is a build of the game that no one hears: it binds the
// headless backend in place of otosound, so its voices still play and end on
// schedule.
type silentServer struct{}

func (silentServer) Name() kernel.PluginName { return "silent server" }

func (silentServer) Dependencies() []kernel.PluginName { return nil }

func (silentServer) Init(configurator *kernel.Configurator, _ any) error {
	configurator.BindSocket[sfx.BackendSocket](headless.NewBackend(0))
	return nil
}

func (silentServer) Start(*kernel.Kernel) error { return nil }
func (silentServer) Stop(*kernel.Kernel) error  { return nil }
func (silentServer) Destroy() error             { return nil }
```

sfx subscribes to no event, so it has no `Handlers` resource to order
around.

## The flush

sfx schedules nothing of its own. The game schedules `Tasks.Flush` on each
update beat, as the quick start does:

- **After its recorders,** each a dependency of the flush, so what they
  record lands in this beat's flush. An operation recorded by a job not
  ordered before the flush is applied by the next one, never lost.
- **Given `beat.Delta + beat.Lost`,** so the playheads stay in step with what
  the device has played even after a stall.
- **Joined into the handler's out dependency,** so the beat finishes only once
  sfx has.
- **Once a beat.** The flush is the one verb that moves time: every playhead
  advances by the delta it is given, scaled by its voice's pitch. A game that
  stops scheduling it keeps hearing whatever plays, and nothing new applies.

What a flush applies, it applies at once, in the order it was recorded: a
`Play` followed by a `SetVoice` before one flush sounds exactly as a `Play`
that carried the change, and a play recorded before one flush is audible no
later than one recorded before the next. The numbers are reported, never
promised: there is no latency figure in the contract, only the one the device
reports.

A clip's bytes are read inside the flush that first names its path, through
platform's files: it is the one place sfx can stall, which is what
[`Preload`](#clips-preload-info-and-release) moves onto a loading screen.

## Playing and changing a voice

`Play(path, offset, options)` records a play of the clip at `path`, from
`offset` into it, and answers the voice's handle at once. `VoiceOptions` is
one value of `m.Maybe` fields: an absent one is the default. Its `Volume` is
linear, 1 being unity, and its `Pitch` the playback rate, 1 being the clip's
own:

```go
// playDoorCreak plays the door's creak a little quieter and higher, from a
// quarter of a second in.
func playDoorCreak(queue sfx.Queue) sfx.Voice {
	return queue.Play("door.ogg", 250*time.Millisecond, sfx.VoiceOptions{
		Bus:    m.Some(busEffects),
		Volume: m.Some[float32](0.8),
		Pitch:  m.Some[float32](1.1),
	})
}
```

The voice exists from the flush that applies the play. While its clip is
still being read and prepared it is addressable and stoppable, silent, and
then starts from its offset with no catch-up, so a footstep never plays its
tail.

`SetVoice(voice, options)` merges: each field it states replaces the voice's
own, and an absent one leaves it as it is. `Seek(voice, offset)` moves the
playhead, block-accurate. `Stop(voice)` ends it at once, with no tail.

A playhead is never outside its clip, so an offset that asks for more than the
clip holds is settled once the clip's length is known:

- a negative offset is zero;
- a one-shot at or past the end finishes, with `ReasonFinished`, paused or not;
- a looping voice past its loop end, by a `Play`, a `Seek` into the clip's tail
  or `Loop` turned on, is carried back into its loop by whole loops, where one
  that had been looping all along would be;
- a `Seek` at or past the clip's end moves a looping voice to its loop start.

```go
// replaySlowly plays a voice again from its start, at half its rate.
func replaySlowly(queue sfx.Queue, voice sfx.Voice) {
	queue.SetVoice(voice, sfx.VoiceOptions{Pitch: m.Some[float32](0.5)})
	queue.Seek(voice, 0)
}
```

- **Every operation on a voice that has ended does nothing,** with no error:
  a stop racing a sound that finished a flush ago is the most common race in
  audio code, and it is never a bug. `NoVoice` addresses nothing.
- **`Loop`** has a voice repeat the way its clip says to: over the clip's loop
  region, its `LOOPSTART`, `LOOPLENGTH` or `LOOPEND` comments, so an intro runs
  into the loop with no gap, or over the whole clip when it declares none. A
  looping voice never ends by itself, and a seek past its end lands on the
  loop's start. Changing `Loop` on a playing voice restarts it where it
  stands, a block-accurate step, as a seek is.
- **`Pitch`** scales the playhead: a voice at twice its rate ends in half its
  clip's duration. A negative pitch is 0, a freeze.
- **There is no fade.** A fade is the game changing a voice's `Volume`, or a
  bus's, on each beat; each change is declicked by the backend. A crossfade is
  two plays and a fade the game drives.
- **The clip and the offset are not options:** a voice on another clip is a
  new play, and a new offset a seek.

## Endings

`VoiceEndedEvent{Voice, Reason}` is published for every voice that ends,
from inside the flush, after its changes are handed to the backend:

| `Reason` | |
|---|---|
| `ReasonFinished` | the clip reached its end, and was not looping |
| `ReasonStopped` | `Stop`, or a `StopBus` covering it |
| `ReasonStolen` | the cap: see [The cap and stealing](#the-cap-and-stealing) |
| `ReasonFailed` | the clip could not be read or prepared, a flush or more after the play |
| `ReasonReleased` | the clip was released |

Its handlers run inline in the flush's job, while the flush holds its locks.
A handler answering an ending with a new play schedules a task, which waits
for the queue's lock and records into the next flush. A jukebox playing its
tracks one after another chains each on the ending of the one before it:

```go
// jukebox plays its tracks one after another on the music bus, each started
// at the flush after the one before it finished, back to the first after the
// last. A track stopped or released ends the jukebox.
type jukebox struct {
	tracks []string
}

// jukeboxState is what the jukebox keeps between tracks: its tracks, the
// voice playing and the next track's index.
type jukeboxState struct {
	tracks       []string
	playingVoice sfx.Voice
	nextTrack    int
}

// startJukeboxEvent is published by the jukebox's Start, for its first
// track.
type startJukeboxEvent struct{}

func (*jukebox) Name() kernel.PluginName { return "jukebox" }

// Dependencies names sfx, whose queue the jukebox records into.
func (*jukebox) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{sfx.PluginName}
}

func (j *jukebox) Init(configurator *kernel.Configurator, _ any) error {
	configurator.InitResource(jukeboxState{tracks: j.tracks})
	playNext := configurator.RegisterTask2(playNextTrack)
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event startJukeboxEvent) (startJukeboxEvent, kernel.JobHandle) {
		return event, ex.Schedule(playNext, sfx.NoVoice, in).Handle()
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, ending sfx.VoiceEndedEvent) (sfx.VoiceEndedEvent, kernel.JobHandle) {
		if ending.Reason != sfx.ReasonFinished {
			return ending, in
		}
		return ending, ex.Join(in, ex.Schedule(playNext, ending.Voice).Handle())
	})
	return nil
}

func (*jukebox) Start(k *kernel.Kernel) error {
	k.Publish(startJukeboxEvent{})
	return nil
}

func (*jukebox) Stop(*kernel.Kernel) error { return nil }
func (*jukebox) Destroy() error            { return nil }

// playNextTrack plays the next track when ended is the voice playing: the
// zero voice at the start, and then each track that finishes.
func playNextTrack(k *kernel.Kernel, ended sfx.Voice, queue kernel.Write[sfx.Queue], state kernel.Write[jukeboxState]) kernel.NoResult {
	box := state.Get()
	if ended != box.playingVoice || len(box.tracks) == 0 {
		return kernel.NoResult{}
	}
	box.playingVoice = queue.Get().Play(box.tracks[box.nextTrack], 0, sfx.VoiceOptions{
		Bus:      m.Some(busMusic),
		Priority: m.Some(1),
	})
	box.nextTrack = (box.nextTrack + 1) % len(box.tracks)
	state.Set(box)
	return kernel.NoResult{}
}
```

- **A track chained on an ending starts at the flush after it,** up to one
  flush of silence. That is a playlist's tolerance, not a loop's: two
  segments that must be gapless are one file with a loop region.
- **A stop is published at the flush that applied it,** not when the
  backend's declick ramp ends, so it never means the speaker is quiet.
- **There is no start event:** the game recorded the play and already knows.
- **Shutdown publishes nothing** for the voices still live.

## The live view

`LiveVoices`, read under `kernel.Read[sfx.LiveVoices]`, holds every live
voice as of the last flush: `Info(voice)` answers one, `All()` every one in
slot order, `Len()` how many are live and `Cap()` how many may be. A
`VoiceEntry` holds what the game commanded, `Path`, `Bus` and `Options`, and
what sfx derived: its `Playhead` and its clip's `Duration`, whether it is
`Paused`, by its own option or by the pause, its `Audibility`, the gain it is
heard at before panning, and its `Distance` from the listener.

```go
// loudestVoice answers the voice heard loudest now, for a debug overlay.
func loudestVoice(voices sfx.LiveVoices) (sfx.VoiceEntry, bool) {
	var loudest sfx.VoiceEntry
	found := false
	for entry := range voices.All() {
		if !found || entry.Audibility > loudest.Audibility {
			loudest = entry
			found = true
		}
	}
	return loudest, found
}

// atTheCap answers whether the next play would have to steal.
func atTheCap(voices sfx.LiveVoices) bool {
	return voices.Len() == voices.Cap()
}
```

Every voice that ever existed is observable exactly once: it is in the view
now, or its `VoiceEndedEvent` has fired. The playhead is flush-accurate, not
sample-accurate. The view carries no pan: a game checking that a sound is on
the player's right checks where it put the source.

## Buses

A bus is one of the game's own constants, as the quick start's `busMusic`
and `busEffects`. sfx declares only `Master`, the bus of a voice that names
none, so a game that declares no bus still has a working master slider.
Buses do not nest. A bus outside `Config.MaxBuses` is `Master` wherever it is
named, so a mis-declared constant plays on `Master` rather than silencing
anything.

`SetBus(bus, volume)` sets a bus's linear volume, which the flush folds into
every voice on it. It is coalesced: a slider dragged through a hundred values
before one flush costs one. `BusVolumes.Volume(bus)` reads it back as of the
last flush, every bus at unity before any `SetBus`, so a settings screen
draws its slider where the player left it:

```go
// setMusicVolume is the task a settings screen's slider schedules with the
// volume the player chose.
func setMusicVolume(k *kernel.Kernel, volume float32, queue kernel.Write[sfx.Queue]) kernel.NoResult {
	queue.Get().SetBus(busMusic, volume)
	return kernel.NoResult{}
}

// fadeTime is how long fadeMusicOut takes from unity to silence.
const fadeTime = 2 * time.Second

// fadeMusicOut lowers the music bus by one beat's share of fadeTime. The
// game schedules it beside its recorders, before the flush, on each beat of
// the fade.
func fadeMusicOut(k *kernel.Kernel, delta time.Duration, queue kernel.Write[sfx.Queue], buses kernel.Read[sfx.BusVolumes]) kernel.NoResult {
	volume := buses.Get().Volume(busMusic) - float32(delta.Seconds()/fadeTime.Seconds())
	queue.Get().SetBus(busMusic, max(volume, 0))
	return kernel.NoResult{}
}
```

`StopBus(bus)` ends every voice on the bus with `ReasonStopped`, `Master`
ending every voice. It is ordered rather than coalesced: a play recorded
before it is stopped, and one recorded after it is not. There is no mute: a
game keeps its own settings and sets the volume.

## Positional audio

A voice given a `Position` is positional for the rest of its life: quieter
with distance from the listener by its `Falloff`, quieter off its axis by its
`Cone` when it has an `Orientation`, and panned by W3C's equal-power
equations, which sfx computes. A voice with no position is heard centred,
which is what a UI click is. The axes are right-handed, forward −Z, up +Y and
right +X, for the listener and every voice; the units are the game's own.

```go
// makeMetreFalloff answers W3C's defaults with the reference distance at
// 2 metres, inside which an engine is heard at full volume.
func makeMetreFalloff() sfx.Falloff {
	falloff := sfx.MakeDefaultFalloff()
	falloff.Ref = 2
	return falloff
}

// playEngine plays a looping engine at, facing facing, loud ahead of it and
// at a third of its volume behind.
func playEngine(queue sfx.Queue, at m.Vec3, facing m.Quat) sfx.Voice {
	return queue.Play("engine.ogg", 0, sfx.VoiceOptions{
		Bus:         m.Some(busEffects),
		Loop:        m.Some(true),
		Position:    m.Some(at),
		Orientation: m.Some(facing),
		Falloff:     m.Some(makeMetreFalloff()),
		Cone:        m.Some(sfx.Cone{Inner: 90, Outer: 180, OuterGain: 0.3}),
	})
}

// moveEngine moves the engine; its orientation and falloff stay.
func moveEngine(queue sfx.Queue, engine sfx.Voice, at m.Vec3) {
	queue.SetVoice(engine, sfx.VoiceOptions{Position: m.Some(at)})
}

// hearFromCamera puts the listener where the camera is, facing its way.
func hearFromCamera(queue sfx.Queue, camera m.Transform) {
	queue.SetListener(sfx.ListenerOptions{
		Position:    m.Some(camera.Position),
		Orientation: m.Some(camera.Rotation),
	})
}
```

- **`Falloff` and `Cone` are W3C's `PannerNode` parameters,** each field named
  for its W3C one with the prefix dropped: `Ref` is `refDistance`, `Max`
  `maxDistance`, `Rolloff` `rolloffFactor`; a cone's angles are whole angles,
  so a cone of 90 is loud 45° either side. Each is given whole.
- **Set `Ref` to the world's scale.** W3C's default of 1 makes a source 10
  units away −20 dB, correct and far quieter than an author expects off metre
  scale.
- **An all-zero `Falloff` or `Cone` is W3C's defaults,** so a component nobody
  filled in is not silent; a partly filled one is taken literally, so start
  from `MakeDefaultFalloff()` or `MakeDefaultCone()` to move one field.
- **A cone needs a facing:** a positional voice with no `Orientation` is
  equally loud in every direction, whatever its `Cone` says.
- **`maxDistance` clamps only the linear model,** and never silences a voice
  under the other two, as W3C's does.
- **There is one listener,** at the origin and unrotated until the game moves
  it. `SetListener` merges field by field, so a job that moves it and one
  that turns it do not overwrite each other, and `Listener` reads it back as
  of the last flush. sfx never reads a camera: the listener follows one only
  because the game copies the camera across, as `hearFromCamera` does.
- **A stereo clip is panned, not mixed down:** one channel passes at unity
  and the other bleeds into it. Whether a positional clip should be mono is
  the game's judgement.
- **A source behind pans as its mirror image in front,** as two speakers
  cannot say behind.

## 2D

A 2D game uses the same model: a sprite's position is `m.Vec3{X: x, Y: y}`,
and the game **rotates its listener once**, so that, with Y down the screen,
the listener's forward is up the screen, its up out of the screen and its
right screen-right:

```go
// screenRotation is the 2D listener's rotation, which turns its forward up
// the screen.
var screenRotation = m.QuatRotationX(-math.Pi / 2)

// hearFromPlayer puts the 2D listener on the player, turned once for the
// screen.
func hearFromPlayer(queue sfx.Queue, x, y float32) {
	queue.SetListener(sfx.ListenerOptions{
		Position:    m.Some(m.Vec3{X: x, Y: y}),
		Orientation: m.Some(screenRotation),
	})
}

// pixelFalloff is a world measured in pixels: full volume within 64 px.
var pixelFalloff = sfx.Falloff{Ref: 64, Max: 10000, Rolloff: 1}

// playLoudspeaker plays a loudspeaker at x, y, turned angle radians from up
// the screen, loud ahead of it.
func playLoudspeaker(queue sfx.Queue, x, y, angle float32) sfx.Voice {
	return queue.Play("announcement.ogg", 0, sfx.VoiceOptions{
		Position:    m.Some(m.Vec3{X: x, Y: y}),
		Orientation: m.Some(m.QuatRotationZ(angle).Mul(screenRotation)),
		Falloff:     m.Some(pixelFalloff),
		Cone:        m.Some(sfx.Cone{Inner: 120, Outer: 240, OuterGain: 0.25}),
	})
}
```

- **The rotation is not optional.** Leaving it out is not a degraded pan but
  a broken one: panning projects the listener's up away before taking a
  bearing, an unrotated listener's up is +Y, and every sprite with a positive
  X is then hard right whatever its Y. Distances stay right either way, which
  is what makes the mistake quiet.
- **A 2D emitter's facing is the same rotation turned in-plane,**
  `m.QuatRotationZ(θ).Mul(screenRotation)`, facing up the screen at θ of 0.
- **Screen Y is front and back, not elevation,** and front and back fold to
  the same pan.

## Pause

`SetPaused(true)` suspends every voice at once, as when a menu is shown over
the game, and `SetPaused(false)` resumes each on the sample it stopped on.
The playheads stop, a voice played during the pause starts suspended,
`Stop`, `Seek` and the releases still apply, and the device stays open, fed
silence. The last value set before a flush wins.

v2 has no pause of the engine's for sfx to follow, so the pause is the
game's to say. A game pausing on focus loss says it from its own handler of
platform's `FocusEvent`:

```go
// setPaused records the pause of every voice, or its end.
func setPaused(k *kernel.Kernel, paused bool, queue kernel.Write[sfx.Queue]) kernel.NoResult {
	queue.Get().SetPaused(paused)
	return kernel.NoResult{}
}

// subscribePauseOnFocusLoss pauses every voice while the game's window has
// lost the focus.
func subscribePauseOnFocusLoss(configurator *kernel.Configurator) {
	pause := configurator.RegisterTask1(setPaused)
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event platform.FocusEvent) (platform.FocusEvent, kernel.JobHandle) {
		return event, ex.Join(in, ex.Schedule(pause, !event.Focused).Handle())
	})
}
```

- **A pause silences everything,** a menu's own clicks included. A game that
  wants its menu heard during a pause pauses its gameplay voices itself,
  through each voice's own `VoiceOptions.Paused`, and leaves `SetPaused` alone.
- **A voice's own `Paused` stays what the game said:** a voice is silent
  while either is set, and the voices it paused itself stay paused when the
  pause ends. `VoiceEntry.Paused` answers whether the playhead is stopped, by
  either.
- **A pause does not change the device,** and wins over a device that is not
  ready: nothing advances while it lasts.

## Clips: Preload, Info and Release

A clip is a path through platform's files. One path is one clip, read once
and shared by every voice that plays it, kept until it is released.
Releasing is optional.

- **`Preload(path)`** reads and prepares a clip without playing it, so a
  loading screen pays the read rather than the first shot fired. It promises
  the clip is resident, not that its next play is free: a long clip streams,
  and its first play still opens a decoder of its own. Preloading a clip
  already loading or playing changes nothing.
- **`ClipTable.Info(path)`** answers a clip's `ClipInfo`, its duration,
  channels and own sample rate, beside its `ClipState`: `ClipLoading`,
  `ClipReady` or `ClipFailed`. Every fact is zero until the clip is ready.
  Asking never starts a load, so a path nothing has named is `ClipLoading`
  for good.
- **`Release(path)`** gives a clip's memory back: every voice on it ends with
  `ReasonReleased`, one still waiting on it included, at the flush that
  applies the release, after which no voice plays it on any backend. A play
  of the path recorded after the release reads the clip again.
  **`ReleaseAll()`** releases every clip, which is what a teardown means.

```go
// forestClipPaths are the clips of the forest level.
var forestClipPaths = []string{"forest/ambience.ogg", "forest/birds.ogg", "forest/stream.ogg"}

// preloadLevel reads a level's clips while its loading screen shows.
func preloadLevel(queue sfx.Queue, paths []string) {
	for _, path := range paths {
		queue.Preload(path)
	}
}

// levelLoaded answers whether every clip of a level is ready, or failed, so
// the loading screen may leave.
func levelLoaded(clips sfx.ClipTable, paths []string) bool {
	for _, path := range paths {
		if _, state := clips.Info(path); state == sfx.ClipLoading {
			return false
		}
	}
	return true
}

// trackLength answers a track's length for a music player's display, once
// it is known.
func trackLength(clips sfx.ClipTable, path string) (time.Duration, bool) {
	info, state := clips.Info(path)
	return info.Duration, state == sfx.ClipReady
}
```

A level's end releases its clips as `preloadLevel` preloads them, one
`Release` a path. A track that should bridge two levels is kept by releasing
clip by clip and leaving it out, or by starting it again after `ReleaseAll`. Whether a clip is
decoded once or streamed is the backend's business, and a game cannot tell
which it got.

## Bytes that are not files

Bytes a game holds, generated or downloaded, play by path from a memory
mount the game binds and keeps as a resource of its own:

```go
// mountGenerated binds a memory mount for clips the game makes, under the
// prefix generated, and installs it as a resource, for a job to put bytes in.
func mountGenerated(configurator *kernel.Configurator) {
	generated := filesystems.NewMemoryFS()
	configurator.BindSocket(platform.ReadMountSocket{FS: generated, Priority: 2, Prefix: "generated"})
	configurator.InitResource(generated)
}

// playGenerated puts ogg in the mount as name and plays it, from a job
// holding the mount and the queue.
func playGenerated(generated *filesystems.MemoryFS, queue sfx.Queue, name string, ogg []byte) (sfx.Voice, error) {
	if err := generated.Put(name, ogg); err != nil {
		return sfx.NoVoice, err
	}
	return queue.Play("generated/"+name, 0, sfx.VoiceOptions{}), nil
}
```

The bytes are held twice while they are in use: once by the mount, and once
by the backend, which owns what the flush read. A game that wants the memory
back removes them from the mount once `ClipTable.Info` answers the clip
ready. New bytes put under a path whose clip is resident change nothing
until it is released.

## Failures and reports

No operation returns an error. A clip that cannot be read or prepared is
terminal: its voices end with `ReasonFailed`, a flush or more after the play
and never in the flush that applied it, and sfx logs it once under its path,
through the kernel's logger, as an error:

- `ErrClipUnreadable{Path, Err}`, a path the files could not read;
- `ErrClipFailed{Path, Err}`, a clip the backend refused, such as bytes that
  are not Ogg Vorbis, unwrapping to the backend's error;
- `ErrLoopRegionIgnored`, a loop region that does not fit its clip, dropped
  whole: the clip plays and loops whole, and this is the only notice;
- `ErrClipNotPrepared`, a backend that prepared nothing: a bug in the
  backend.

An error is a mistake in the game's own files or code, logged as the
[kernel guide](../../../kernel/docs/README.md#errors-and-logging) says errors
are. Only a release clears a failure: a
path that failed, was fixed, released and named again is read again, and
reported again if it fails again. There is no fallback clip.

## The device

`DeviceView`, read under `kernel.Read[sfx.DeviceView]`, answers the `Device`
the backend reported at the last flush, which polls it once:

```go
// describeDevice answers the line a settings screen shows under its volume
// sliders.
func describeDevice(view sfx.DeviceView) string {
	device := view.Device()
	if !device.Ready {
		return "No sound device"
	}
	return fmt.Sprintf("%s: %d Hz, %d channels, %v buffered", device.Name, device.SampleRate, device.Channels, device.Latency)
}
```

- **A device that is absent, not ready yet or lost reads not ready,** and a
  game cannot tell which, nor needs to. It is a fact to read, never an error
  to handle: a game starts with no sound card, and a lost device is reported
  by nobody. A device that could never be opened is the backend's to report,
  once, as a warning, which ends no game.
- **A voice's playhead advances whether or not anyone can hear it.** Music
  started before the device arrives is heard where the world is when it does,
  and a footstep played meanwhile has already ended, on schedule.
- **`Latency` is the length of the output buffer in force,** the only figure
  the contract reports, for a rhythm game to adapt to or refuse. Under the
  headless backend the device is ready, named `"headless"`, and its latency
  zero.
- Before the first flush it is the zero `Device`, not ready.

## The cap and stealing

At most `Config.MaxVoices` voices exist at once. A play arriving at a full
table steals from the voice in the lowest `Priority` band the least audible,
its volume through its bus, its falloff and its cone, ties going to the
oldest. The play is as stealable as anything already playing and ranks as the
newest: one quieter than every live voice is the one that loses. Either way
the loser ends with `ReasonStolen` in the flush that applies the play, and the
play still answers a real handle.

- **`Priority` is a hard band:** a voice one band up is never stolen by any
  number of plays below it. The quick start's music is at 1, so no crowd of
  footsteps at 0 cuts it. Protect a sound that matters by saying so up front.
- **A paused voice ranks at the audibility it would have unpaused,** and a
  voice waiting on its clip or on the device holds its slot.
- **A steal is final.** A game that wants a stolen sound back keeps a copy of
  the live view each flush, its path, playhead and options, and on
  `ReasonStolen` plays it again from the playhead plus the time since the
  copy. `Example_playAStolenVoiceAgain`, in
  [example_test.go](../example_test.go), runs that recipe.

## Testing with sfxtest

A good test of a game's sound asserts what sfx computed and published: that
a voice is live, on the right bus, at an audibility of 0.4 rather than 0, that
a one-shot ended and why, which reports a step raised. It never asserts the
pan, samples or anything below the seam: sfx's own tests pin the arithmetic
against W3C's numbers.

### The harness

`sfxtest.NewHarness(t)` runs sfx in a real kernel with no device: platform
with a memory mount, metronome with a fixed-step update track on a manual
clock, sfx, and a plugin of the harness's own that binds the headless
backend and, on each beat, schedules the test's records and then sfx's
flush, as a game schedules it, or under `UsePluginFlush` leaves the flush to
a game's own plugin among the test's.

| Call | What it does |
|---|---|
| `NewHarness(t)` | A harness whose kernel is not yet spawned; the test's end quits it and checks the errors logged. |
| `UseBackend(backend)` | Before `Spawn`: binds `backend` in place of the headless one, for a device that is not ready or a prepare that finishes later. |
| `UsePluginBackend()` | Before `Spawn`: binds no backend, so a plugin among the test's binds the socket, as otosound's kernel tests bind otosound's. |
| `UsePluginFlush()` | Before `Spawn`: schedules no flush, so a plugin among the test's that schedules `Tasks.Flush`, as a game's own does, flushes once a beat; the test's records run before each beat. |
| `Configure(name, config)` | Before `Spawn`: gives another plugin among the test's its config, such as otosound's `Config`; sfx's and metronome's come from `Spawn`. |
| `Spawn(config, plugins…)` | Spawns the kernel with `config`, nil for the defaults, the test's plugins listed after sfx. |
| `Mount(path, data)` | Puts a clip's bytes in the memory mount, at the root of the files, as the file `path`. |
| `Record(func(sfx.Queue))` | Records from the test into the next step, before its flush, so a test records without writing a plugin. |
| `Step()` | Steps one beat, `StepInterval` long, and returns once it has finished, every ending published. |
| `Read(func(LiveVoices, ClipTable, BusVolumes, Listener, DeviceView))` | Reads the views in a job holding their read locks, as the last step's flush left them. |
| `Endings()` | Every `VoiceEndedEvent` published so far, in order. |
| `Reports()` | Every error the kernel logged so far, expected or not, in order. |
| `Warnings()` | Every warning the kernel logged so far, in order; a warning fails no test. |
| `ExpectError(match)` | Names errors the test expects: an error logged that no match answers true of fails the test, at the step that logged it. |

The harness is strict: an error the test did not expect fails it. It waits
`sfxtest.Timeout` for the kernel before failing. Each step's delta is the
same, so everything above the seam is deterministic: which voices exist,
their playheads, the order stealing takes and the step each ending fires at.

### A game's own plugin

The harness schedules sfx's flush itself, so a game's plugin that schedules
the flush too, as the quick start's `game` does, would flush twice a beat.
`UsePluginFlush`, before `Spawn`, leaves the flush to it: the game is spawned
among the test's plugins whole, its recorder and its flush on each beat, and
what the test records through `Record` runs before each beat, so it lands in
the game's flush. The game is given no files of its own, and the test mounts
the clips it plays in the harness's memory mount. Its voice handles are kept
in its own resource, so a test finds a voice in the view. These tests sit
beside the quick start, in its package `main`:

```go
// spawnGame spawns the quick start's game under UsePluginFlush, with no
// files of its own.
func spawnGame(h *sfxtest.Harness) {
	h.UsePluginFlush()
	h.Spawn(nil, &game{files: fstest.MapFS{}})
}

// mountAssets mounts the quick start's clips from its embedded assets.
func mountAssets(t *testing.T, h *sfxtest.Harness, paths ...string) {
	t.Helper()
	for _, path := range paths {
		data, err := fs.ReadFile(embedded, "assets/"+path)
		if err != nil {
			t.Fatal(err)
		}
		h.Mount(path, data)
	}
}

// readMusic answers the music's entry in the live view, a zero entry while it
// is not playing.
func readMusic(h *sfxtest.Harness) sfx.VoiceEntry {
	var music sfx.VoiceEntry
	h.Read(func(voices sfx.LiveVoices, clips sfx.ClipTable, buses sfx.BusVolumes, listener sfx.Listener, device sfx.DeviceView) {
		for entry := range voices.All() {
			if entry.Path == "music.ogg" {
				music = entry
			}
		}
	})
	return music
}

func TestTheMusicLoopsOnItsBusABandUp(t *testing.T) {
	h := sfxtest.NewHarness(t)
	spawnGame(h)
	mountAssets(t, h, "music.ogg", "step.ogg")
	for range 3 {
		h.Step()
	}

	music := readMusic(h)
	if music.Voice == sfx.NoVoice {
		t.Fatal("the music is not playing")
	}
	if music.Bus != busMusic || !music.Options.Loop.Or(false) || music.Options.Priority.Or(0) != 1 {
		t.Errorf("the music is %+v, want it looping on busMusic at priority 1", music)
	}
	if music.Playhead != 3*sfxtest.StepInterval {
		t.Errorf("the music is %v in, want three steps, the flush that played it among them", music.Playhead)
	}
}

func TestAFootstepFallsEveryHalfSecond(t *testing.T) {
	h := sfxtest.NewHarness(t)
	spawnGame(h)
	mountAssets(t, h, "music.ogg", "step.ogg")
	for range beatsPerFootstep + 1 {
		h.Step()
	}

	h.Read(func(voices sfx.LiveVoices, clips sfx.ClipTable, buses sfx.BusVolumes, listener sfx.Listener, device sfx.DeviceView) {
		footstepCount := 0
		for entry := range voices.All() {
			if entry.Bus == busEffects {
				footstepCount++
			}
		}
		if footstepCount != 2 {
			t.Errorf("%d footsteps are playing, want the first beat's and the thirty-first's", footstepCount)
		}
	})
}

func TestAMissingFootstepFailsItsVoicesAndIsReportedOnce(t *testing.T) {
	h := sfxtest.NewHarness(t)
	h.ExpectError(isUnreadable)
	spawnGame(h)
	mountAssets(t, h, "music.ogg")
	for range beatsPerFootstep + 2 {
		h.Step()
	}

	failedCount := 0
	for _, ending := range h.Endings() {
		if ending.Reason == sfx.ReasonFailed {
			failedCount++
		}
	}
	if failedCount != 2 {
		t.Errorf("%d voices failed, want both footsteps", failedCount)
	}
	if reports := h.Reports(); len(reports) != 1 || !isUnreadable(reports[0]) {
		t.Errorf("the reports are %v, want step.ogg unreadable once", reports)
	}
}

// isUnreadable answers whether err is a clip's ErrClipUnreadable.
func isUnreadable(err error) bool {
	_, unreadable := errors.AsType[sfx.ErrClipUnreadable](err)
	return unreadable
}
```

### Plugins among the test's

A plugin that records into sfx without scheduling its flush, such as the
jukebox, which only answers endings, is spawned among the test's plugins and
driven by the harness's beats. A test reaches the end of a long track by
seeking near it rather than stepping through it:

```go
func TestTheJukeboxPlaysTheNextTrackWhenOneFinishes(t *testing.T) {
	h := sfxtest.NewHarness(t)
	h.Spawn(nil, &jukebox{tracks: []string{"one.ogg", "two.ogg"}})
	music, err := fs.ReadFile(embedded, "assets/music.ogg")
	if err != nil {
		t.Fatal(err)
	}
	h.Mount("one.ogg", music)
	h.Mount("two.ogg", music)
	h.Step()
	h.Step()

	var first sfx.VoiceEntry
	h.Read(func(voices sfx.LiveVoices, clips sfx.ClipTable, buses sfx.BusVolumes, listener sfx.Listener, device sfx.DeviceView) {
		if voices.Len() != 1 {
			t.Fatalf("%d voices are live, want the first track alone", voices.Len())
		}
		for entry := range voices.All() {
			first = entry
		}
	})
	if first.Path != "one.ogg" {
		t.Fatalf("the jukebox is playing %q, want the first track", first.Path)
	}
	h.Record(func(queue sfx.Queue) { queue.Seek(first.Voice, first.Duration-sfxtest.StepInterval) })
	h.Step()
	h.Step()

	if endings := h.Endings(); len(endings) != 1 || endings[0] != (sfx.VoiceEndedEvent{Voice: first.Voice, Reason: sfx.ReasonFinished}) {
		t.Errorf("the endings are %v, want the first track finished", endings)
	}
	h.Read(func(voices sfx.LiveVoices, clips sfx.ClipTable, buses sfx.BusVolumes, listener sfx.Listener, device sfx.DeviceView) {
		if voices.Len() != 1 {
			t.Fatalf("%d voices are live, want the second track alone", voices.Len())
		}
		for entry := range voices.All() {
			if entry.Path != "two.ogg" {
				t.Errorf("the jukebox is playing %q, want the second track", entry.Path)
			}
		}
	})
}
```

`UsePluginBackend` leaves the socket to such a plugin when it binds the
backend itself, as the silent server does:

```go
func TestTheSilentServerHearsTheHeadlessDevice(t *testing.T) {
	h := sfxtest.NewHarness(t)
	h.UsePluginBackend()
	h.Spawn(nil, silentServer{})
	h.Step()

	h.Read(func(voices sfx.LiveVoices, clips sfx.ClipTable, buses sfx.BusVolumes, listener sfx.Listener, device sfx.DeviceView) {
		if got := device.Device(); !got.Ready || got.Name != "headless" {
			t.Errorf("the device is %+v, want the headless one, ready", got)
		}
	})
}
```

### A backend of the test's own

The headless backend is always ready and finishes every prepare at once.
`UseBackend` binds one of the test's own for what it never does, here the
headless backend embedded with its device left out:

```go
// absentDevice is the headless backend on a machine with no sound card.
type absentDevice struct{ *headless.Backend }

func (absentDevice) Device() sfx.Device { return sfx.Device{Name: "absent"} }

func TestTheMusicPlaysOnWithNoDevice(t *testing.T) {
	h := sfxtest.NewHarness(t)
	h.UseBackend(absentDevice{headless.NewBackend(0)})
	spawnGame(h)
	mountAssets(t, h, "music.ogg", "step.ogg")
	for range 64 {
		h.Step()
	}

	h.Read(func(voices sfx.LiveVoices, clips sfx.ClipTable, buses sfx.BusVolumes, listener sfx.Listener, device sfx.DeviceView) {
		if device.Device().Ready {
			t.Error("the device is ready, want none")
		}
	})
	if music := readMusic(h); music.Playhead != time.Second {
		t.Errorf("the music is %v in, want a second whether or not anyone hears it", music.Playhead)
	}
}
```

## Rules

- Schedule `Tasks.Flush` once on each update beat, after your recorders,
  given `beat.Delta + beat.Lost`, and join it into the handler's out
  dependency.
- Record from tasks under `kernel.Write[sfx.Queue]`; read the views under
  `kernel.Read`, as of the last flush.
- Keep the handle a play answers; every operation on a voice that has ended
  does nothing.
- Declare your buses as constants from 1, under `Master`, and set
  `Config.MaxBuses` to cover them.
- Set `Falloff.Ref` to your world's scale; give a cone an `Orientation`.
- In 2D, rotate the listener once, as `screenRotation` does.
- Pause with `SetPaused` from your own handlers; sfx follows no pause of its
  own.
- Preload from a loading screen; release only to get memory back, knowing a
  release stops the clip's voices.
- Put a sound that must survive a crowd a `Priority` band up.
- Read the device as a fact; never wait on it being ready.
- Test on `sfxtest`, asserting what sfx computed and published, never the pan
  or the samples.
