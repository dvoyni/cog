# sfx: design

The design of `github.com/dvoyni/cog/plugins/sfx`, the interface plugin that
gives a game sound: clips played as voices, grouped on buses, positioned in the
world and heard from one listener, with all of the arithmetic computed in sfx
and none of it below. A **backend** binds sfx's socket and mixes what it is
handed: otosound, the plugin over `ebitengine/oto/v3` that a game ships, whose
mixer, device thread and streaming are in [otosound's design](../../otosound/docs/design.md),
or the **headless backend** in `plugins/sfx/headless`, which plays nothing, for
tests and for runs no one hears. This document owns sfx, the headless backend
and the seam between sfx and every backend: the socket, `FlushChanges`, the
prepared clip and the numbered list of what a backend owes.

It is v1's sound specification carried over whole onto v2's kernel, mapped in
[sfx: v1's sound, otosound and nosound carried onto the v2 kernel](https://github.com/dvoyni/cog/issues/945).
v1's API and behaviour are kept wherever v2 forces no change; each section links
the v1 tickets it came from as provenance, and the map's tickets that changed
it. *Further Notes* ends with every change from v1 and the ticket that decided
it. The vocabulary is in [`CONTEXT.md`](../CONTEXT.md).

Status: built, ticket by ticket on the [map](https://github.com/dvoyni/cog/issues/945), from [sfx: the seam and the headless backend](https://github.com/dvoyni/cog/issues/953) to [sfx: README, guide and AGENTS.md](https://github.com/dvoyni/cog/issues/964); each section links the tickets that decided it.

## Problem Statement

A game needs sound: a footstep played and forgotten, music that runs from an
intro into a loop with no gap, a source that is quieter the further away it is
and heard from its side, volume groups a settings screen moves, and a cap on
what plays at once that never cuts the music for a crowd of footsteps. It needs
all of that without handling a device, a thread, a decoder or a sample. The
device may be missing, late or lost; a music track decoded whole costs twenty
times its file; and the mixer runs on the device's own clock, outside the
kernel, where nothing that takes a lock may reach.

v1's sound was that layer: a Slot over the otosound and nosound Adapters,
specified against v1's kernel, and every piece it stood on is gone from v2 or
changed. Its flush ran last on app's fixed-step `UpdateEvent` through `.Last()`,
and v2 has no "last". It paused on app's `PauseChangeEvent`, and v2 has no
engine pause. It read clips through storage, cached them in libs/assets'
Library and named bytes as a Blob, none of which v2 carries. Its reports were
keyed by typed values, and its default handler logged an error and carried on,
where v2's `StdLogger` panics on every `LogErr`, so a machine with no sound card,
reported the v1 way, would end a v2 game. v1's design questions were settled and
are not reopened. What remains is to restate v1 on v2's kernel, changed only
where a v2 rule or a missing v1 dependency forces it.

## Solution

sfx is the machine, not a convenience layer.

- A game **records operations** into one `Queue` resource from its own tasks:
  plays, seeks, stops, a voice's options, a bus's volume, the listener, a pause,
  and preloads and releases of clips. A play hands back the voice's handle at
  once.
- The game **schedules sfx's flush**, `Tasks.Flush`, on each update beat, after
  its recorders, passing the beat's delta and what the beat lost. The flush
  applies everything recorded since the last one at once and in the order it was
  recorded, computes every voice's gain matrix and rate, and hands the backend
  the flush's changes, one `FlushChanges`.
- It reads **views** that the flush alone writes: the live voices, the clip
  table, the bus volumes, the listener and the device.
- It names **clips by path** through platform's filesystem, and bytes it holds
  go into a memory mount. The flush reads a clip's bytes at its first play or
  preload, and the backend prepares it.
- **Positional audio** is the W3C Web Audio specification's arithmetic,
  transcribed: three distance models, the cone gain and equal-power panning, one
  model for 2D and 3D.
- A **device** that is missing, late or lost is a fact the game reads, never an
  error it handles, and a voice's playhead advances whether or not anyone can
  hear it.
- One **event**, `VoiceEndedEvent`, is published for every ending, with its
  reason.
- The **backend socket** speaks in slots, a 2×2 gain matrix, a rate and clip
  IDs, and knows nothing of handles, buses, priorities, positions or the cap.
- **`sfxtest`**, a harness over the headless backend, runs a game's sound in a
  real kernel with no device.

The design is bound by v1's four requirements, in this order, and every
decision below was taken against them:

1. **One machine, shaped for two faces.** A game records into sfx's queue
   directly. A later ECS face (v1's ecsaudio, out of scope here) would drive
   that same queue and reimplement nothing, which is why the queue is shaped the
   way it is.
2. **Nothing a game must load or release.** There is no `LoadSound` and no handle
   to a clip. A caller names a path; loading and caching live inside sfx.
   Releasing is optional, for a game that wants the memory back sooner.
3. **2D and 3D.** A position per voice and one listener, with distance
   attenuation, panning and cones. The same model serves a 2D game and a 3D one.
4. **Desktop, with no cgo.** Windows, macOS and Linux, through backends that
   need no cgo, which keeps cross-compiling free. cog ships no cgo backend; one
   is allowed only as a backend a game composes itself.

Three properties fall out of taking those in that order, and most of what
follows is a consequence of one of them. **The queue speaks in position and
listener, never in gain and pan**, which keeps a later HRTF effort from
redrawing what a game says, and makes the arithmetic one implementation rather
than one per backend. **A voice is retained, not re-declared**: a music track
seeked partway through cannot be derived from a frame's declarations, which is
why sfx holds a table. And **the backend is a dumb sink**: it is handed a gain
matrix and a rate, and knows nothing of handles, buses, priorities, positions,
the cap or the equations.

## User Stories

The actors are a **game developer** composing a game that makes sound, a
**plugin author** building a face on sfx (an ECS plugin of sound components), a
**backend author** binding sfx's socket, and a **test author** testing sfx, a
backend or a game's sound.

### Playing voices

1. As a game developer, I want to play a sound by naming its file's path, with no load call and no handle to a clip, so that a sound is one line.
2. As a game developer, I want a play to hand back its voice's handle at once, usable before the next flush, so that I stop, move or seek it without waiting.
3. As a game developer, I want every operation on a voice that has ended to do nothing, with no error, so that a stop racing a sound that just finished is never a bug.
4. As a plugin author, I want a voice's options to be one value whose absent fields mean the default at a play and unchanged at a change, so that an ECS component can be exactly that value.
5. As a game developer, I want to seek a voice, and to stop one without a tail.
6. As a game developer, I want a play followed by a change in the same flush to sound exactly as a play that carried the change.
7. As a game developer, I want a play recorded before one flush audible no later than one recorded before the next, so that sounds keep the order my game made them in.
8. As a game developer, I want pitch to change the playback rate, the voice ending when its rate says it would, so that a sped-up sound is not cut short.
9. As a game developer, I want a looping voice to repeat the way its file says, an intro running into a loop with no gap, so that music loops as its composer exported it.
10. As a game developer, I want to fade a voice or a bus by changing its volume on each beat myself, with every change declicked, so that no change clicks and no fade verb hides state.

### Clips

11. As a game developer, I want a clip read once per path and shared by every voice that plays it, so that ten footsteps cost one file.
12. As a game developer, I want to preload a clip from a loading screen, so that its read lands there rather than on the first shot fired.
13. As a game developer, I want to release one clip, or every clip, and have the voices on it stopped and told why, so that a level's sounds give their memory back at the level's end.
14. As a game developer, I want to ask a clip's duration, channels and rate, and whether it is loading, ready or failed, without starting a load, so that a UI shows a track's length.
15. As a game developer, I want bytes I hold played by putting them in a memory mount and playing their path, so that generated or downloaded audio plays like a file.
16. As a game developer, I want long music streamed and short effects decoded once without choosing which, so that memory follows what I play.
17. As a game developer, I want a clip that cannot be read or decoded to end its voices with a reason and be reported once under its path, so that a missing file is named and nothing else breaks.
18. As a game developer, I want a malformed loop tag to make the clip loop whole and be reported, never clamped, so that a broken loop is found rather than heard as the wrong loop point.

### Buses and volume

19. As a game developer, I want to declare my own buses, with master already there, so that a game that declares none still has a working master slider.
20. As a game developer, I want a bus's volume readable, so that a settings screen draws its slider where the player left it.
21. As a game developer, I want to stop every voice on a bus at once, master stopping everything.
22. As a game developer, I want a bus outside the range to play on master, so that a mis-declared constant never silences my game.
23. As a game developer, I want the voice cap and the bus count in sfx's `Config`, so that I tune them without code.

### Positional audio

24. As a game developer, I want to give a voice a position in my own units and hear it quieter with distance and from its side.
25. As a game developer, I want each voice to carry its own falloff and cone, in W3C's parameters, so that their meaning is a published document's.
26. As a game developer, I want one listener I place and turn myself, so that it follows a camera only because I copy the camera across.
27. As a game developer, I want a 2D game to use the same model by rotating its listener once, so that 2D needs no mode of its own.
28. As a game developer, I want a voice with no position heard centred, so that a UI click is just a play.
29. As a game developer, I want a voice with a cone but no facing to be equally loud in every direction, so that no voice is directional along an axis nobody chose.
30. As a game developer, I want stereo clips panned rather than mixed down to mono.

### Stealing

31. As a game developer, I want a cap on voices, the least audible voice losing when a play arrives at a full table, so that the sounds that matter most are the ones heard.
32. As a game developer, I want a priority band a voice can be put in, so that music is never stolen by any number of footsteps.
33. As a game developer, I want ties broken by age, oldest first, so that stealing is the same on every run.
34. As a game developer, I want a stolen voice to end with its own reason, and a recipe to play it again in step, so that a sound I need back comes back where it would have been.

### Pause

35. As a game developer, I want to pause every voice at once and have each resume on the same sample, with the device kept open, so that a pause menu silences the game and gives it back as it was.
36. As a game developer, I want a voice played during a pause to start paused.
37. As a game developer, I want to pause on focus loss from my own handler of platform's focus event, so that the choice is my game's.

### The flush and endings

38. As a game developer, I want to schedule sfx's flush on my update beat after my recorders, passing the beat's delta and what it lost, so that playheads stay in step with what the device has played even after a stall.
39. As a game developer, I want an operation recorded by a job I did not order before the flush applied by the next flush, never lost.
40. As a game developer, I want an event for every voice that ends, with why, so that dialogue and playlists chain on it.
41. As a game developer, I want a read-only view of every live voice, with its clip's path, bus, playhead, duration, options, whether it is paused, its audibility and its distance, so that I read what my game commanded and what sfx derived.
42. As a game developer, I want every voice to be observable exactly once, in the view or through its ending, so that my bookkeeping never loses one.

### The device and reports

43. As a game developer, I want my game to start with no sound card, and a device that is missing, late or lost to be a fact I read, so that sound never stops my game.
44. As a game developer, I want a voice's playhead to advance whether or not the device is ready, so that music started before the device arrives is heard where the world is.
45. As a game developer, I want the device's name, rate, channels and latency, the length of its output buffer in force, readable, so that a rhythm game adapts or refuses.
46. As a game developer, I want a mistake in my own files or code reported as an error, fatal under the default logger while I develop, and a fact about the player's machine as a warning, so that a missing sound card never ends my game.
47. As a game developer, I want each clip's failure reported once under its path, and again only after a release, so that the log is not flooded and a repaired file that breaks again is heard from.

### Backends

48. As a backend author, I want a small socket (slots told once, one `FlushChanges` per flush, a gain matrix and a rate), so that I compute none of the arithmetic.
49. As a backend author, I want what I owe written as a numbered list, so that I know what my own package must prove.
50. As a backend author, I want the bytes handed to my prepare to be mine, so that I stream from them without a copy.
51. As a backend author, I want what I learn about a clip to go back to sfx through what I answer, so that I report nothing about clips and sfx names each one.

### Testing and performance

52. As a test author, I want a harness that runs sfx in a real kernel over the headless backend, with clip bytes mounted from the test, so that a game's sound is tested with no device.
53. As a test author, I want to record from my test and step one beat, so that a test needs no plugin of its own.
54. As a test author, I want the endings and the reports a step raised, in order, so that a test asserts which voice ended why.
55. As a test author, I want the headless backend to end voices when their clips' headers say they end, so that a one-shot's ending is asserted.
56. As a test author, I want the arithmetic pinned once by sfx's own tests against W3C's numbers, so that my tests assert audibility and never the pan.
57. As a game developer, I want a steady flush to allocate nothing once warm, so that sound does not feed the garbage collector.

## Implementation Decisions

### The module and the plugin

([sfx on the kernel: its socket, resources, Tasks and events, and what paces its flush](https://github.com/dvoyni/cog/issues/948), from v1's [sound: a Slot beneath audio, the way gfx sits beneath canvas](https://github.com/dvoyni/cog/issues/373))

- **A module of its own,** `github.com/dvoyni/cog/plugins/sfx`, importing kernel, platform and libs/m.
  - Its `headless` package also requires the Vorbis decoder, `jfreymuth/oggvorbis`, through cog's forks `dvoyni/vorbis` and `dvoyni/oggvorbis`, by the same `replace` pair otosound carries. The two modules keep identical pins, or `go.work` settles them: two differing replacements in one workspace fail to build. The forks are permanent, since upstream archived `jfreymuth/vorbis` and takes none of cog's changes ([sfx research: oto v3 and our Vorbis forks today](https://github.com/dvoyni/cog/issues/947)).
  - Its `loopregion` package reads a clip's `LOOPSTART`, `LOOPLENGTH` and `LOOPEND` comments into a `LoopRegion`, or the `ErrLoopRegionIgnored` of a region dropped whole (see *The loop region*), working on the plain comments and the clip's length in frames. The headless backend and otosound both import it, so they run one arithmetic.
  - Its `sfxtest` package also imports metronome.
  - It imports no ECS, no renderer and no camera.
- **An interface plugin,** as gfx is. sfx holds the queue, the voice table, the clip table, the buses, the listener and every equation, and declares the socket a backend binds.
  - otosound is a plugin of its own, as gogpu is.
  - The headless backend is an adapter, as gfx's recording adapter and platform's headless windows are: a constructor a test's plugin, or an application's own, calls and binds.
- **`sfx.PluginName`** is `"sfx"`. sfx names platform in its `Dependencies` and is listed after it, since its flush reads `platform.FS`. A plugin that records into sfx or schedules its flush is listed after sfx, since its `Init` reads `sfx.Tasks`.
- **A game composes one backend:** otosound, or the headless backend in tests and in runs no one hears. There is no knob selecting a backend, and a composition with none panics in `Spawn`.
- **`Config`,** under `PluginName`, given as a pointer and read with `kernel.PluginConfigOrDefault`:
  - `MaxVoices`, how many voices may exist at once; zero means 64 (see *The voice: its handle, the cap and stealing*);
  - `MaxBuses`, how many buses exist, `Master` among them; zero means 32. v1 fixed the count at 32; the root `AGENTS.md` rule that a count a plugin is tuned by is a field of its `Config` makes it one.
  - A negative value in either panics in `Init` as a composition error, where v1 returned `ErrInvalidMaxVoices` or `ErrInvalidConfig`.
- **Resources it installs,** `Tasks` first in `resources.go`, each other one a private type in a file of its own named by a public alias there:

  | Resource | Lock | What it is |
  |---|---|---|
  | `Tasks` | read in a later plugin's `Init` with `configurator.Resource[sfx.Tasks]()` | `Flush`, the one task sfx offers, which it schedules nowhere |
  | `Queue` | `kernel.Write[sfx.Queue]`, taken by every recorder | Where every operation is recorded |
  | `LiveVoices` | `kernel.Read[sfx.LiveVoices]` | The live voice view (v1's `Voices`) |
  | `ClipTable` | `kernel.Read[sfx.ClipTable]` | What sfx knows of each clip, asked with `Info(path)` (v1's `Clips`) |
  | `BusVolumes` | `kernel.Read[sfx.BusVolumes]` | Each bus's volume as of the last flush (v1's `Buses`) |
  | `Listener` | `kernel.Read[sfx.Listener]` | Where the game is heard from, as of the last flush |
  | `DeviceView` | `kernel.Read[sfx.DeviceView]` | What the device is now, as the `Device` the backend answered |
  | a private one | the flush's alone | The bound backend, read from the socket in `Start`, and the flush's scratch: its `FlushChanges` and its endings |

  - The five views have no mutators, and the flush alone writes them. They are several, and apart from the queue, so that a job asking a question never contends with the jobs recording operations. v1's `Voices`, `Clips` and `Buses` are renamed because a type name is not a bare plural.
  - There is no `Handlers` resource, since sfx subscribes to nothing.
- **Events it publishes:** `VoiceEndedEvent{Voice, Reason}`, from inside the flush (see *Endings*).
- **Events it subscribes to:** none.
- **Sockets:** it declares `BackendSocket`, exclusive and not optional, so a kernel with no backend panics in `Spawn`, as v1's composition failed with `ErrMissingAdapter`. It binds none.
- **Where things live:** `sockets.go` holds `BackendSocket` alone. `FlushChanges`, `VoiceStart`, `VoiceUpdate`, `VoiceParams`, `PreparedClip`, `Prepared`, `VoiceSlot` and `LoopRegion`, which v1 declared beside its `Backend` interface, are in files of their own. The flush's body is a named function in `tasks.go`. The handles, `Voice` and the backend's `ClipID`, are collected in `handles.go`, as the root `AGENTS.md` collects a package's handle types. v1's `internal/` package and the friend functions it needed go: v2 has one package, and unexported methods do their work.
- **Lifetime:**
  - sfx reads the bound backend from its socket in `Start`, keeps it in its private resource beside the flush's scratch, and calls `Voices(n)` there, before any flush. That is when otosound opens its player.
  - sfx's `Stop` and `Destroy` do nothing of their own. otosound closes its player in its own `Destroy`, not its `Stop`, since v2 finishes every job in flight between the two, so no flush emits into a closed player (otosound's design).
  - **Shutdown publishes nothing.** No ending is published for the voices still live when the kernel is destroyed: an event nobody can receive is not an ending worth modelling, and N events at shutdown are N chances to touch a half-torn world.

### The flush on the kernel

([sfx on the kernel](https://github.com/dvoyni/cog/issues/948), from v1's [sound: a Slot beneath audio](https://github.com/dvoyni/cog/issues/373) item 5)

```
the game's handler of its update beat:
  its recording tasks, each under Write[sfx.Queue]
  after them, sfx's Tasks.Flush, given beat.Delta + beat.Lost:
    1. drain TakePrepared into the clip table
    2. apply the operations in recorded order, a new clip's bytes read here and
       handed to Prepare, then the coalesced bus volumes, listener and pause
    3. compute: bind voices to clips that became ready, end those whose clips
       failed, advance every playhead, fold the buses, spatialize
    4. poll Device, and restate every live voice if it just became ready
    5. Emit the flush's changes
    6. end the flush: rebuild the free slots, the steal order and the ranking
       context the next flush's plays are minted against
    7. publish one VoiceEndedEvent per ending, in order
  joined into the handler's out dependency
```

- **sfx offers `Tasks.Flush`, a `*kernel.Task[time.Duration, kernel.NoResult]`, and schedules it nowhere,** as mux's `Step` and platform's pump are offered.
  - The game schedules it on each update beat, after its recorders, passing `beat.Delta + beat.Lost`. Passing the lost time keeps the playheads in step with what the mixer has played even after a stall, where v1, which dropped excess steps silently, let them lag.
  - The flush advances every playhead by the delta it is given, scaled by each voice's rate.
  - It was chosen over a track event sfx would ship, `sfx.FlushEvent`, and over a subscription to `metronome.UpdateEvent`. Both would order the flush against recorders that v2 cannot order, since v2 has no "last": a game would have to order every recording handler before sfx's, and one that forgot would record into a flush already run.
- **How a game schedules it,** recording first:

  ```go
  func (p *gamePlugin) Init(c *kernel.Configurator, config any) error {
  	tasks, _ := c.Resource[sfx.Tasks]()
  	flush := tasks.Flush
  	recordSounds := c.RegisterTask2(p.recordSounds)
  	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, beat metronome.UpdateEvent) (metronome.UpdateEvent, kernel.JobHandle) {
  		recorded := ex.Schedule(recordSounds, kernel.NoArg{})
  		flushed := ex.Schedule(flush, beat.Delta+beat.Lost, recorded.Handle())
  		return beat, ex.Join(in, flushed.Handle())
  	})
  	return nil
  }

  func (p *gamePlugin) recordSounds(k *kernel.Kernel, _ kernel.NoArg, queue kernel.Write[sfx.Queue], game kernel.Read[*Game]) kernel.NoResult {
  	q := queue.Get()
  	for _, shot := range game.Get().ShotsFired {
  		q.Play("sounds/shot.ogg", 0, sfx.VoiceOptions{Bus: m.Some(busEffects), Position: m.Some(shot.At)})
  	}
  	return kernel.NoResult{}
  }
  ```

- **v1's guarantees hold per flush:**
  - The operations recorded before a flush apply at it atomically, in the order they were recorded: never partly applied, and audible together at the mixer's next block boundary.
  - An operation recorded by a job the game did not order before the flush is applied by whichever flush next takes the queue's lock: if the flush holds it, the operation waits and lands in the next flush. It is never lost.
  - `Flush` is the one verb that moves time, in place of v1's "there is no `Update` or `Tick` verb". Audio runs on its own clock, so a game that stops scheduling the flush keeps hearing whatever plays, and nothing new applies.
  - The device's readiness is noticed on the flush, which polls it once.
- **The flush is one named body in `tasks.go` over eight handles,** registered with `RegisterTask8`: writes of `Queue`, `LiveVoices`, `ClipTable`, `BusVolumes`, `Listener`, `DeviceView` and the private resource, and a read of `platform.FS`, through which a new clip's bytes are read.
  - It holds every one of sfx's resources at once, which is what "applied atomically" means in lock terms: no recorder is mid-append while the queue is drained, and no reader sees half a flush's voices.
  - v1's flush held a ninth, `lastFlush`, and its `endingsHeld` and `voicesCmd` beside it, all of which served the MCP provider alone and go with it. The backend, which in v2 must live in a resource, shares the private one with the scratch.
- **The flush in order,** as v1 settled it:
  1. **Drain `TakePrepared()`** into the clip table: install each completion whose entry is still live at the epoch its token names, and drop the rest. A completion that carries an error fails its entry, and a clip that installs with a dropped loop region has it reported, each once under the clip's path (see *Reports*).
  2. **Apply the operations,** in the order they were recorded. A `Play` or `Preload` naming a path the table does not hold reads its bytes and calls `Prepare` here; a `Release` stops the voices on that clip and queues its destroy. The coalesced values land where the ordered operations end, since none of them is one: the bus volumes, the listener and the pause, each the last value recorded.
  3. **Compute:** bind each voice waiting on a clip that is now ready, end with `ReasonFailed` each voice whose clip has failed, unless the voice's play was applied in this flush, advance every playhead, fold each bus's volume into each voice's gain, and run the W3C equations over every positional voice against the one listener. Stealing is *applied* here, not resolved: the victim was chosen when the play was recorded, because that is where the handle naming its slot was minted (see *Stealing*).
  4. **Poll `Device()`** once, writing the `DeviceView` resource. A device that became ready since the last flush has every live voice restated in this flush's changes (see *The device*). Ready going false is not acted on at all.
  5. **`Emit(changes)`** once.
  6. **End the flush:** clear the slots whose voices ended, and rebuild the free slots, the steal order and the ranking context the next flush's plays are minted against. The slots come back only now, after the changes carrying their stops have been handed over, which is what "a slot is stopped before sfx reuses it" costs: one pass, once a flush.
  7. **Publish** one `VoiceEndedEvent` per ending, in the order they happened.

  Draining first is what makes a release recorded against an in-flight `Prepare` cost nothing: by the time the completion arrives its entry is gone, so no `ClipID` is ever minted only to be destroyed.

### The queue

([sfx on the kernel](https://github.com/dvoyni/cog/issues/948), from v1's [sound: a Slot beneath audio](https://github.com/dvoyni/cog/issues/373), [The command surface: what gameplay says to make a sound](https://github.com/dvoyni/cog/issues/296) and [what pauses sound in v2](https://github.com/dvoyni/cog/issues/950))

- **One `Queue` resource,** written under its write lock by every recorder and flushed atomically by each flush. It is deliberately not a latest-wins buffer, as a renderer's frame may be: a frame is a complete description and may be replaced, while an operation is a delta and may not.
  - **Never lost, applied in order:** `Play`, `Seek`, `Stop`, `StopBus`, `Preload`, `Release`, `ReleaseAll`.
  - **Coalesced between flushes, last value wins:** a voice's options, a bus's volume, the listener (field by field), and the pause.
    - A voice's options coalesce by being applied in order: the last `SetVoice` recorded for a voice before a flush is the last one applied, which says "last value wins" while keeping `Play` and `SetVoice` in one list, and is what makes a `Play` followed by a `SetVoice` indistinguishable from a `Play` that carried the same options.
    - A bus's volume, the listener and the pause are tables rather than places in the list: each is read by every voice at the one moment the flush computes, so a place in the ordered list would mean nothing.
  - **A handle is minted when the play is recorded,** so it is usable before the flush that applies it. There is no command to wait on.
- **A recorder holds the queue's write lock and nothing else.** That is why the bus volumes and the listener a play is ranked against are copied into the queue at the end of each flush rather than read where the handle is minted: a play that had to lock `BusVolumes` and `Listener` to rank itself would widen every recorder's lock set, and sfx's resources are several precisely so that they do not contend.
- **Recorded by tasks,** under `kernel.Write[sfx.Queue]`, as the example in *The flush on the kernel* records. A face on sfx, such as an ECS plugin's recording system, writes the same queue and reimplements nothing.

### What a game says

([The command surface: what gameplay says to make a sound](https://github.com/dvoyni/cog/issues/296), with [Positional audio](https://github.com/dvoyni/cog/issues/297)'s fields, [The voice](https://github.com/dvoyni/cog/issues/301)'s `Priority`, [sound on assetcache](https://github.com/dvoyni/cog/issues/412)'s `Preload`, and the v2 names from [sfx on the kernel](https://github.com/dvoyni/cog/issues/948), [clips by path](https://github.com/dvoyni/cog/issues/949) and [what pauses sound](https://github.com/dvoyni/cog/issues/950))

There are **three addressable things**, a voice, a bus and the listener, and
clips beside them, and the verbs fall out of them rather than being chosen.

```go
type Bus int
const Master Bus = 0 // the only bus sfx declares; a game declares the rest

// VoiceOptions is "start like this" to Play and "become this" to SetVoice. An
// absent field is the default to Play and unchanged to SetVoice.
type VoiceOptions struct {
	Bus         m.Maybe[Bus]
	Volume      m.Maybe[float32] // linear, 1 is unity
	Pitch       m.Maybe[float32] // playback rate, 1 is the clip's own
	Loop        m.Maybe[bool]
	Paused      m.Maybe[bool]
	Priority    m.Maybe[int]     // a hard band above audibility in stealing
	Position    m.Maybe[m.Vec3]
	Orientation m.Maybe[m.Quat]
	Falloff     m.Maybe[Falloff] // replaced whole
	Cone        m.Maybe[Cone]    // replaced whole
}

// ListenerOptions is "become this" to SetListener. An absent field is unchanged.
type ListenerOptions struct {
	Position    m.Maybe[m.Vec3]
	Orientation m.Maybe[m.Quat]
}
```

**Eleven operations**, recorded on the queue:

| Addressed to | Operation |
|---|---|
| a voice | `Play(path string, offset time.Duration, options VoiceOptions) Voice` · `SetVoice(voice, options)` · `Seek(voice, offset)` · `Stop(voice)` |
| a bus | `SetBus(bus, volume)` · `StopBus(bus)`, `Master` stopping everything |
| the listener | `SetListener(options ListenerOptions)` |
| every voice | `SetPaused(paused bool)` |
| a clip | `Preload(path)` · `Release(path)` · `ReleaseAll()` |

and the questions are asked of the views rather than the queue: a bus's volume,
the listener, a clip's facts and state (`ClipTable.Info`), the live voices, and
the device.

- **`VoiceOptions` is one value of `m.Maybe` fields rather than a set of narrow setters,** because a face such as an ECS plugin must reconcile a whole component in one operation, and because `m.Maybe` is libs/m's spelling of a set mask. It stays a legal component value: comparable when its fields are, pointer-free, copyable into a store. It and `ListenerOptions` are named `…Options`, as a struct that is only a call's parameter is, in place of v1's `Params` and `ListenerParams`.
- **The clip and the offset are not options.** Changing a voice's clip is a new `Play`, and a changed offset is a `Seek`.
- **Every time a game gives or reads is a `time.Duration`:** `Play`'s and `Seek`'s offsets, a voice's playhead and its clip's duration in the live view, and a clip's duration in `ClipInfo`. The seam's values, seen only by sfx and its backends, stay as v1 had them: a `VoiceStart`'s offset is a `time.Duration`, and a prepared clip's duration and loop region are `float32` seconds. The conversion is sfx's.
- **Each verb's rules:**
  - `Play` ranks the play against the table when it is recorded (see *Stealing*), so its options are read there as well as applied in order at the flush. A play that finds no slot still gets a real handle. The voice exists from the flush that applies the play, silent until its clip is ready.
  - `Stop`, `SetVoice` and `Seek` are no-ops on a voice that no longer exists, and on `NoVoice`.
  - `SetVoice` merges: each field it states replaces the voice's own. Changing `Loop` on a voice the backend already holds restarts it where it stands, since `Loop` is a fact of a `VoiceStart` at the seam and a `VoiceUpdate` has no field for it; it costs a block-accurate discontinuity at the moment the game toggles looping, as a `Seek` does.
  - `Seek` clamps a negative offset to zero. An offset at or past the clip's end ends a one-shot with `ReasonFinished` and moves a looping voice to its loop start. On a voice still waiting on its clip it moves the offset the voice will start from. It is block-accurate, never sample-accurate.
  - `Pitch` is the playback rate. A negative one is clamped to zero, a legitimate freeze, since it would run a voice backwards off the front of its buffer.
  - `SetBus` and `StopBus` resolve their bus as `Play` does, an out-of-range one being `Master`. `StopBus` is ordered rather than coalesced, since it interacts with the plays around it: a play recorded before it is stopped, and one recorded after it is not.
  - `SetListener` merges field by field, so a job that moves the listener and one that turns it do not overwrite each other.
  - `SetPaused` sets one flag over the whole voice table (see *Pause*).
  - `Preload` and `Release` take a path, and they and `ReleaseAll` are ordered (see *Clips*): a `Play` recorded after a `Release` of its path loads the clip again, and one recorded before it is cut. A `Release` of a path nothing has named does nothing.

#### Ordering and causality, and no figure anywhere

The contract promises **ordering and causality**, never a latency figure
([the web backend](https://github.com/dvoyni/cog/issues/300) §2):

- **A flush's operations apply atomically:** ordered by recording, audible together at the next block boundary, never partly applied. A `Play` followed by a `SetVoice` carrying a position, before one flush, is indistinguishable from a `Play` that carried the position.
- A `Play` recorded before flush *N* is audible no later than one recorded before flush *N+1*.
- `Stop` silences without a tail.
- A position set before flush *N* governs everything mixed after it.

The numbers are **reported, never promised**: the device's name, sample rate and
latency, the length of its output buffer in force, are readable, so a game that
genuinely needs the figure, a rhythm game, reads it and adapts or refuses.

#### No fades, and no errors

- **Every parameter change is declicked** by a short internal ramp with no knob: a gain step on a running voice clicks, and that is physics. The ramp is the backend's, over its own blocks (see *The seam*).
- **A fade is the game's** (see `CONTEXT.md`): the game changing a voice or a bus on each beat, from its own task or a mux timeline it steps. A crossfade is two plays and a fade the game drives. This is a deliberate non-feature, stated so nobody later reads it as an omission.
- **The surface is total.** Every operation is a no-op on a voice that no longer exists, with no error and no response field to check: a `Stop` racing a clip that finished a flush ago is the most common race in audio code. **No operation returns an error.** What goes wrong is reported through the kernel's logger (see *Reports*), and the only failure a game observes in its own code is a clip that ended its voices with `ReasonFailed`.

### What is never said

([The command surface](https://github.com/dvoyni/cog/issues/296) item 12, [where a music track's loop repeats from](https://github.com/dvoyni/cog/issues/471) §3)

- **No type names, and no caller can state:** sample rate, channel count, buffer or block size, a device's name or index, a backend's name, a file format or decoder, PCM, samples or frames, gain or pan (positions only), a thread, callback or mixer, a latency figure, a voice slot's index, or whether a clip is streamed or resident.
- **The rule governs a caller, and a file is not a caller.** A clip's own Vorbis comments carry its loop region in sample frames, read by the backend from bytes it already parses and converted to seconds before they cross the seam. No signature in sfx names a sample. The rule delimits the API, not the asset format.

### The voice: its handle, the cap and stealing

([The voice: its handle, its lifetime, and the cap](https://github.com/dvoyni/cog/issues/301), [Priority on emitters](https://github.com/dvoyni/cog/issues/502), [Should a stolen Voice resume when a slot frees?](https://github.com/dvoyni/cog/issues/503), [How a game tests its sound](https://github.com/dvoyni/cog/issues/467) §6, [the prototype](https://github.com/dvoyni/cog/issues/304))

#### The handle

```go
// Voice is an opaque handle to one playing sound. It is comparable, copyable
// and usable as a map key, and the zero value means no voice.
type Voice uint64

const NoVoice Voice = 0

func (v Voice) String() string // "Voice(7v2)" is index 7 at generation 2
```

- **An index in the low 32 bits and a generation in the high 32, neither exported.** Generations start at 1, so `Voice(0)` unambiguously means no voice while index 0 stays an ordinary slot. A struct of two `uint32`s is the same eight bytes and as comparable, but it leaks its layout into every call site and can never be re-cut.
- **Staleness is one compare inside sfx and is never surfaced.** A handle whose generation does not match its slot's addresses nothing, which is exactly what "every verb is a no-op on a voice that is gone" needs to be built without an error path. A game holding a handle across a steal, a stop and a reuse of that index never touches the voice that took the slot.
- **The table is fixed and generational,** so sfx needs no free list beyond the slots themselves and no map from handle to voice: the handle is the index.
- `String` shows both halves, since a log that cannot tell a reused index from the handle before it is useless; reading them back in code is what the type refuses.

#### The cap

- **`Config.MaxVoices`, zero meaning 64.** One number.
- **Two caps, a total and a smaller one for streamed voices, are foreclosed, not rejected.** A streamed voice was measured at 137 KB plus a goroutine and a ring buffer, against an index for a resident one ([Assets: how a path becomes samples, and what streams](https://github.com/dvoyni/cog/issues/298)). But the tier lives entirely inside the backend and sfx never learns it, so sfx cannot count what it cannot see, and a second cap would put "streamed or resident" back above the seam.
- **The worst case is stated instead:** 64 streamed voices is about 8.7 MB and 64 goroutines. The number a game tunes is the one it can reason about, how many sounds it wants at once, and the memory follows from what it plays.

#### Stealing

- **When a play arrives at a full table, the loser is the voice with the lowest (priority, audibility), ties broken by age, oldest first.**
- **Audibility is the final computed gain, volume × bus × falloff × cone,** the scalar before panning, never anything read off the gain matrix. That one number makes "quietest" and "furthest from the listener" the same policy rather than two that disagree, and gives a non-positional voice a rank with no special rule.
  - **Measured** in [the prototype](https://github.com/dvoyni/cog/issues/304): taking audibility as `max(L, R)` off the matrix scores a centred voice 0.1414 against a hard-panned one's 0.2000 at the same distance, 3 dB invented out of nothing, because equal-power panning preserves power and so cannot make a voice louder, only move it between the ears. Left that way, stealing would discard the sound in front of you for the one beside you.
- **`Priority` is a hard band:** a lower-priority voice always loses to a higher-priority one, whatever the gains say. Music at priority 1 is not stolen by a crowd of footsteps at 0, however loud the crowd.
  - **That is the recipe, and a game can rely on it: a voice one band up is never stolen by any number of plays below it.** "Keep my music playing" is answered by putting the music at priority 1, and "the gun I just fired must be heard" by putting the player's own weapon there too. A sound that matters is protected by saying so up front, never by anything sfx infers.
  - `Priority` never crosses the seam: a backend knows nothing of priorities, and a stolen voice reaches it as an ordinary stop.
- **The tie-break is contract, not an implementation detail.** Two voices tie on both terms exactly whenever one clip is played twice at one position before one flush: a double footstep, two shell casings, a burst weapon. Left unstated, the tie would fall to whatever order the table was walked in, which makes every stealing test flaky on a schedule nobody controls. Age is a counter of starts, not a tick, so two voices started before one flush are still ordered.
- **An incoming play ranks as the newest,** so within a band a strictly quieter play loses, and one that ties exactly steals the oldest.
- **A paused voice ranks at the audibility it would have unpaused.** Pausing neither protects a voice nor puts it first on the block; ranking it at zero would make "pause the music for a cutscene" a reliable way to lose the music. A pause of every voice is the same.
- **A voice waiting on its clip, or on a device that is not ready, occupies a slot,** because its clock is already running. A cold-started game reaches the cap with nothing audible yet, and that is correct: those voices are going to be audible.
- **The incoming play can be the one that loses.** It still gets a real handle, and its `VoiceEndedEvent` arrives in the flush that applies it. The alternative, a play always stealing something, means the hundredth footstep of a bug silences the music, which is the failure the cap exists to prevent. The game's code is the same either way, since there is nowhere to put a "your play was refused" case.
- **A steal is final: nothing resumes a stolen voice.** A sound that matters is kept by putting it a band up beforehand. A sound that must come back in step is the game's call, and the game holds everything it needs:
  - **The retry recipe.** Keep a copy, each flush, of each voice from the live view: its clip's path, playhead and options. On `ReasonStolen`, for the sounds the game wants back, record `Play(path, playhead + elapsed, options)`, where *elapsed* is the time since the copy was taken.
  - The copy has to be the game's, since `VoiceEndedEvent` carries only `{Voice, Reason}` and an ended handle addresses nothing. It is taken before the flush that steals the voice, the last moment the voice is in the view, so a retry recorded as the ending arrives has *elapsed* of one flush's delta and lands where the stolen voice would have been. `Play` already takes an offset, so no verb is added, and the game decides which sounds deserve it, since only the game knows.
  - `Example_playAStolenVoiceAgain`, in sfx's tests on sfxtest, runs the recipe and checks the new voice lands in step, so the recipe cannot quietly stop compiling.
  - The retry is an ordinary play and ranks like one: one that ranks below everything live loses and ends as `ReasonStolen` in the flush that applies it, never having reached the view. Whether to try again later is again the game's call.
- **The victim is chosen when the play is recorded.** Three things this design asserts cannot all hold at a full table: the handle is minted when the play is recorded, the handle is the index, and stealing resolves in the compute phase. The minter has no slot to hand out at record time, and the victim is not chosen until the flush.
  - **The third bends.** The victim is chosen as the play is recorded, against the table as it stood at the end of the last flush; compute only applies what the handle already says. The other two are visible to a game, which holds the handle and resolves every verb through it, while "stealing resolves in compute" is visible to nobody: what a game can observe is that the victim is in the view until the flush that steals it, and that stays true either way.
  - Deferring the mint would bend what a game can see: a play that might lose would hand back a handle addressing nothing until the next flush, `Play` would have two return shapes, and a `Stop` recorded beside it would silently miss.
  - Choosing the slot does not disturb the voice still in it. The slot holds the victim's whole handle, so every operation recorded against the victim before the flush still finds it, and is applied in order, before the start that takes the slot over.
- **Two consequences, stated:**
  - **A play can only steal a voice that was live at the end of the last flush.** It cannot steal one started by a play recorded before the same flush, so a hundred plays before one flush consume at most the slots that existed, and the rest lose outright.
  - **The ranking context is one moment.** Every candidate, the incoming play included, is ranked against the same bus volumes and the same listener, those of the end of the last flush, so no two candidates are compared across a flush.

#### Endings

| `Reason` | |
|---|---|
| `ReasonFinished` | the clip reached its end and was not looping |
| `ReasonStopped` | `Stop`, or `StopBus` covering it |
| `ReasonStolen` | the cap, whether the voice played for a second or never sounded |
| `ReasonFailed` | the clip could not be read or prepared |
| `ReasonReleased` | the clip it was playing was released |

- **`VoiceEndedEvent{Voice, Reason}` is published for every ending,** so a subscriber never has to tell "it ended" from "I ended it" to keep its own bookkeeping straight. Dialogue sequencing and playlists land on it. It is pointer-free, as every v2 event is, and carries no clip: a game holds the handle it played with and already knows.
- **It is published from inside the flush,** after the flush's changes are emitted, so its handlers run inline in the flush's job while the flush's locks are held. A handler that answers an ending with a new play schedules a task, which waits for the queue's lock and runs after the flush, so the play lands in the next flush, as in v1.
- **There is no `VoiceStartedEvent`** ([How a game tests its sound](https://github.com/dvoyni/cog/issues/467) §2), and the reason is what an event is for: an event exists to tell a game something it did not do. Finishing, being stolen, failing and being released are none of the game's doing. A start is: the game recorded the `Play` and already knows.
- **`StopBus` gets no member of its own.** It is `Stop` over a set; splitting it later is additive, and a reader switching on `ReasonStopped` keeps working.
- **`Release` does get one.** It is not a stop at all, stopping being its side effect, and it is typically recorded by a teardown task nowhere near the one that started the sound. Folding it into `ReasonStopped` would hide the one release-related bug behind the one cause that is always innocent ([What a released clip does to the voices playing it](https://github.com/dvoyni/cog/issues/416) §2).
- **A looping voice never ends by itself** and publishes nothing until one of the other four reaches it.
- **The delays differ, and a game reading only the reason would assume they do not.** `ReasonStolen` from a full table arrives in the flush that applies the play. `ReasonFailed` arrives one or more flushes later, never in the flush that applied the play, because preparing is asynchronous. A game cannot write "play it, and if it fails this frame, do X".
- **A stop is published at the flush that applied it,** not when the backend's declick ramp finishes, so the event precedes the silence by a few milliseconds. It never meant "the speaker is now quiet".
- **Chaining two distinct tracks stays on `VoiceEndedEvent`,** and its cost is not zero: a track chained on an ending starts at the flush after the ending, up to one flush of silence, 16.7 ms on a 60 Hz update track and more on a slow frame. That is the tolerance a playlist gets, not the tolerance a loop gets. Two segments that must be gapless are one file with a loop region.

#### The live view, and the closure property

- **`LiveVoices` is one read-only view of the live voices:** `Info(voice) (VoiceEntry, bool)`, `All() iter.Seq[VoiceEntry]` in slot order, `Len()`, how many are live, and `Cap()`, the table's size, beside `Len` so that a reader asking whether the game is at its cap gets both numbers under one read lock.
- **A `VoiceEntry` holds:** the `Voice`; `Path`, its clip's path; its `Bus`; its `Playhead` and the clip's `Duration`, each a `time.Duration`, the duration zero while the voice waits on its clip; `Options`, the `VoiceOptions` it is set to now; `Paused`, whether its playhead is stopped, by its own `Options.Paused` or by the pause, so a reader asking whether the playhead moves is answered about the playhead and not about who stopped it; its `Audibility`, the scalar stealing ranks on; and its `Distance` from the listener, in the game's units, zero on a non-positional voice.
- **It is a resource, not a request.** Voices are retained, so they sit still between flushes and a reader reads them there.
- **The reader can rely on this: every voice that ever existed is observable exactly once. It is in the live view now, or its `VoiceEndedEvent` has fired.**
  - It holds in the hard cases. A play stolen in the flush that applies it never appears in the view, but fires `ReasonStolen` in that flush. A clip that cannot be prepared fires `ReasonFailed` a flush or more later.
  - **Its cost, stated:** a voice stolen at once leaves behind only its reason; the options it would have played with are gone, since the view is where options live and it never appeared there. A test that must assert on the options of a voice that may be stolen asserts on them before the flush that steals it.
- **The playhead is flush-accurate, not sample-accurate.** sfx computes it from the clip's duration and the voice's rate, the same way it computes endings, and the device is somewhere inside its current block whenever anyone reads it. `Seek` being block-accurate is the same admission from the other side, so a reader meets the granularity once.
- **The view carries audibility and distance, never the pan.** A game's test asserts what it commanded and what sfx derived from it: that the alarm is playing, on the right bus, audible at 0.4 rather than 0. The pan is sfx's arithmetic, pinned once by sfx's own tests against W3C's published numbers, not re-asserted by every game that makes a noise. A game checking that its sound is on the player's right checks where it put the source, which is its own data.

### Buses

([The command surface](https://github.com/dvoyni/cog/issues/296), [What a sound Backend speaks](https://github.com/dvoyni/cog/issues/376) §3, and [sfx on the kernel](https://github.com/dvoyni/cog/issues/948) item 9)

- **`type Bus int`, declared by the game.** sfx predeclares **`Master = 0` alone**, so an unset bus lands on `Master`, and a game that declares no buses still has working audio and a working master slider.
- **Flat, no nesting.** Every bus sits directly under `Master`. A tree is where a DSP graph starts, and that is out of scope.
- **`Config.MaxBuses` of them, zero meaning 32,** `Master` among them.
- **An out-of-range bus is `Master` everywhere a bus is named,** a `Play`, a `SetBus`, a `StopBus` and a read, rather than an error, so a mis-declared constant does not silence a game. One rule cannot disagree with itself: a game whose constant is wrong is wrong about it consistently, its sounds playing on `Master` and its slider moving `Master`.
- **A bus's volume is readable,** `BusVolumes.Volume(bus) float32`, as of the last flush, every bus at unity before any `SetBus`, so a settings screen draws its slider where the player left it. A `SetBus` recorded before a flush is visible after it, which is true of every operation.
- **There is no `Mute`.** A game persists its settings anyway, and a shadow copy inside sfx invites "muted by whom".
- **A bus is coalesced, `StopBus` is not.** A bus's volume is read by every voice on it at the one moment the flush computes, so the last value recorded wins and a slider dragged through a hundred values before one flush costs one. `StopBus` interacts with the plays around it, so it is an ordered operation.
- **A backend does not know buses exist.** sfx folds a bus's volume into each voice's gain matrix, and there is no bus at the seam.
  - **The cost, stated:** a bus volume change re-emits every voice on that bus, at most `MaxVoices` entries in one flush's changes. Voices already at that volume are not re-emitted, since a fold that did not move is not a change. That is nothing against the alternative, a second place volume is decided.
  - Filters and reverb would genuinely want buses in the backend, and they are out of scope. Adding the concept now to serve a feature ruled out is how a seam ossifies around machinery nobody asked for.
- **Nothing about spatialization is per bus.** A game that wants every footstep to fall off alike passes the same `Falloff` on each play. A node-graph backend would otherwise have to copy bus state onto every node, and a bus would stop being a volume.

### Positional audio

([Positional audio: the listener, the units, and what 2D means beside 3D](https://github.com/dvoyni/cog/issues/297), as amended by [where a 2D game's listener stands](https://github.com/dvoyni/cog/issues/470), with the measurements of [the prototype](https://github.com/dvoyni/cog/issues/304))

**One model serves both.** A position is always an `m.Vec3`, a facing always an
`m.Quat`, and a 2D sound is a 3D sound lying on the `Z=0` plane. sfx has no
`Vec2`, no 2D mode, no pan, and no notion of what a unit is.

#### The axes, stated outright

- **Right-handed, forward −Z, up +Y, right +X,** for the listener and for every voice. This is what `m.LookAt` already does, and it is the W3C listener's default forward `(0,0,−1)` and up `(0,1,0)`, so an unrotated listener is the W3C default listener, and a speaker and a spotlight on one transform point the same way. This design states it rather than inheriting it from any renderer.

#### The arithmetic is W3C's, transcribed

- **The distance models (`linear`, `inverse`, `exponential`), the cone gain, the azimuth and elevation of a source, and `equalpower` panning from azimuth are the W3C Web Audio API specification's published equations, adopted verbatim and normatively.** The reason is structural: a future `PannerNode` backend agrees with sfx's own arithmetic by construction instead of by careful matching. The cost is inheriting W3C's parameter names and two quirks: `maxDistance` clamps only the linear model and never silences a voice, and the linear formula is odd.
- **The implementation transcribes them from the W3C document itself,** down to the order of operations and the degrees-then-radians round trip, so that a reader checks the code against the document; the equations below restate them so that this design stands on its own. What is added is named where it is added, and is only ever a guard against a value that must not reach a device: a NaN gain, or the normalization of a zero vector.
- **Precision.** sfx promises run-to-run determinism on one build and explicitly not bit-equality across architectures: `math.Acos` and `math.Cos` carry no such guarantee, so a gain computed on `windows/amd64` may differ in its last bits from the same gain on `darwin/arm64`. The arithmetic runs in `float64` and lands in `float32`, and **every test of it compares with a tolerance.** This sits here beside the arithmetic, not in a testing appendix, because it is contract.

The equations, with `d` the distance from the listener to the source and the
falloff's fields under their W3C names (`dRef` is `Ref`, `dMax` is `Max`,
`rolloffFactor` is `Rolloff`):

```
distance gain
  inverse      dRef / (dRef + rolloffFactor × (max(d, dRef) − dRef))
  linear       1 − rolloffFactor × (max(min(d, dMax), dRef) − dRef) / (dMax − dRef)
               with rolloffFactor clamped to [0, 1]
  exponential  pow(max(d, dRef) / dRef, −rolloffFactor)
  rolloffFactor is clamped below at 0 for every model; a gain that comes out NaN,
  a divisor the falloff's own fields made zero, is 1: an undefined attenuation is
  no attenuation

cone gain (W3C angles are whole angles, so they are halved)
  1 when the voice has no facing, or both angles are 360
  angle = the angle, in degrees, between the voice's forward and the direction
          from the voice to the listener; 1 when the voice stands on the listener
  angle ≤ |Inner| / 2                      1
  angle ≥ |Outer| / 2                      OuterGain
  between, x = (angle − |Inner|/2) / (|Outer|/2 − |Inner|/2)
                                           (1 − x) + x × OuterGain

azimuth and elevation, in degrees, from the listener's front and up
  v      = normalize(source − listener); 0 and 0 when the source is on the listener
  right  = normalize(front × up);  ahead = normalize(front);  upright = right × ahead
  p      = v − upright × (v · upright)
  when p is not zero:
    azimuth = acos(normalize(p) · right)
    if normalize(p) · ahead < 0: azimuth = 360 − azimuth
    if 0 ≤ azimuth ≤ 270: azimuth = 90 − azimuth, else azimuth = 450 − azimuth
  when p is zero (straight overhead or underfoot), azimuth is 0
  elevation = 90 − acos(v · upright), folded into [−90, 90]
  every cosine is clamped into [−1, 1] before acos, since two vectors normalized
  in float32 can dot a hair past 1

equal-power panning, into Gains[source channel][output channel]
  clamp azimuth into [−180, 180], then fold it into [−90, 90]:
    below −90: −180 − azimuth;  above 90: 180 − azimuth
  mono:               x = (azimuth + 90) / 180
  stereo, azimuth ≤ 0: x = (azimuth + 90) / 90
  stereo, azimuth > 0: x = azimuth / 90
  gainL = cos(x × π/2),  gainR = sin(x × π/2)
  mono:               outL = in × gainL;          outR = in × gainR
  stereo, azimuth ≤ 0: outL = inL + inR × gainL;   outR = inR × gainR
  stereo, azimuth > 0: outL = inL × gainL;         outR = inR + inL × gainR
  the matrix is then scaled by the voice's audibility
```

- **Landmarks:** a source ahead is azimuth 0, to the right +90, to the left −90, behind −180; elevation is +90 overhead and −90 underfoot. Elevation is computed and kept, and no equal-power gain reads it; it is what an HRTF effort would want.
- **A non-positional voice runs the same equations at azimuth 0,** since "heard centred" is a place on the circle and not a second rule. That makes a mono clip 0.707 in each ear rather than unity in both, and leaves a stereo clip on its own diagonal.

#### Falloff and cone are per voice

```go
type Falloff struct {
	Model   DistanceModel // W3C distanceModel: DistanceInverse (the default, zero), DistanceLinear, DistanceExponential
	Ref     float32       // W3C refDistance,   default 1
	Max     float32       // W3C maxDistance,   default 10000
	Rolloff float32       // W3C rolloffFactor, default 1
}

type Cone struct {
	Inner     float32 // degrees, W3C coneInnerAngle, default 360
	Outer     float32 // degrees, W3C coneOuterAngle, default 360
	OuterGain float32 // W3C coneOuterGain,           default 0
}
```

- **Each field's doc comment names the W3C parameter it corresponds to.** The prefixes are dropped, `Ref` and not `RefDistance`, because the struct already says which distance.
- **Both are comparable and pointer-free,** so a component can carry one, and each is **replaced whole** by `SetVoice`: seven separate `Maybe` fields would buy finer updates and cost a fourteen-field `VoiceOptions`, for a group nobody changes piecemeal.
- **An all-zero `Falloff` or `Cone` is the W3C defaults,** as `m.Transform`'s zero scale is the identity: a component an author never filled in must not be silent, and an all-zero cone read literally is a voice silent everywhere. A partly filled one is taken literally, so `Falloff{Ref: 64}` is a reference distance of 64 with no rolloff and no maximum. An author who wants the defaults with one field moved starts from `MakeDefaultFalloff()` or `MakeDefaultCone()`, which answer W3C's defaults.
- **Units are the game's.** sfx never learns a scale: there is no world-scale setting and no default-falloff verb. A game measured in pixels passes its own `Falloff`, typically a constant it declares once.
- **Set `Ref` to the world's scale.** W3C's default of 1 is a trap off metre scale: measured in [the prototype](https://github.com/dvoyni/cog/issues/304), on the defaults a source at radius 4 is −12 dB and at radius 10 is −20 dB. Correct, and far quieter than an author expects. The package's doc and `Falloff`'s say so.

#### Positional is one-way

- **A voice with no position is non-positional:** no falloff, no cone, no panning, heard centred. A UI click is one.
- **The first position a voice receives, at `Play` or by `SetVoice`, makes it positional for the rest of its life.** No operation is silently ignored and no clearing verb is needed; a voice that should stop being positional is a new `Play`. A merge cannot unset a `Maybe` that has been set, so nothing else has to be remembered.
- **A `Falloff` or `Cone` given to a voice with no position is kept,** and takes effect when it gets one.
- **A cone needs a facing.** A positional voice with no `Orientation` is equally loud in every direction whatever its `Cone` says, so a voice is never accidentally directional along an axis nobody chose. W3C's `(1,0,0)` default `PannerNode.orientation` is not inherited.
- **A voice the listener moved relative to is re-emitted** with no operation naming it: a player turning on the spot re-emits the sources around them, and a voice whose bearing, distance and gains did not move is not.

#### One listener

- **Set by `SetListener` with a `ListenerOptions`,** absent fields unchanged, readable back from `Listener.Position()` and `Listener.Orientation()` as of the last flush, and sitting at the origin with no rotation before any call.
- **The zero `m.Quat` is read as the identity,** since `(0,0,0,0)` is no rotation, so a game that fills in only a position is not left facing nowhere.
- **sfx never reads a camera.** A game copies a camera's transform across; it is one value copy, since `m.Transform` carries `Position` and `Rotation`, and `Scale` means nothing here. A listener that followed a camera would make sfx import a renderer and pick one of two.

#### Stereo pans as W3C pans it

- **A stereo clip keeps both channels and shifts weight between them;** it is not mixed down to mono first, because mixing down is a departure a `PannerNode` backend would have to imitate by hand. What W3C's stereo arm does is pass one channel at unity and bleed the other into it: a stereo source panned hard right is its left channel folded into its right, not its left channel silenced.
- **Whether a positional clip should be mono is the game's judgement, not sfx's.** Both were judged fine by ear ([the prototype](https://github.com/dvoyni/cog/issues/304) item 6), so the package doc carries a note describing what stereo does when panned, not a recommendation. sfx pans what it is given.

#### 2D, precisely

A 2D game passes `m.Vec3{X: x, Y: y}` and **rotates its listener**, once:

```go
queue.SetListener(sfx.ListenerOptions{
	Orientation: m.Some(m.QuatRotationX(-math.Pi / 2)),
})
```

That is forward `(0,−1,0)`, up `(0,0,−1)`, right `(1,0,0)`: with a 2D game's
Y-down, ahead is up the screen, up is out of the screen, and right is
screen-right.

- **This is not optional, and leaving it out is not a degraded pan but a broken one.** Panning projects the listener's up axis away before taking a bearing, and an unrotated listener's up is +Y. The `Z=0` plane contains +Y, so projecting it away leaves X and nothing else: every sprite with positive X is hard right and every one with negative X hard left, whatever its Y. Measured on an 800×600 screen with the player at its centre:

  | Source | Unrotated listener | Rotated listener |
  |---|---|---|
  | 20 px right, level | **+90.0°** | +90.0° |
  | 200 px right, 200 px up the screen | **+90.0°** | +45.0° |
  | 20 px right, 400 px up the screen | **+90.0°** | +2.86° |
  | straight up the screen | 0.0° | 0.0° |

  Distances are right in every row either way, which is what makes the failure so quiet: three pans exist, the arithmetic is right, and nothing reports anything.
- **Nothing 2D-specific is added:** no constant in sfx, no mode, no listener in canvas. The value is an ordinary rotation handed to the `SetListener` that already exists.
- **Screen Y is front and back, not elevation.** Under the rotation, elevation is identically zero in a 2D game, and front and back fold to the same pan under equal-power panning.
- **The 2D cone is the same quaternion, turned in-plane.** `m.QuatRotationZ(θ).Mul(base)` puts an emitter's forward at `(0,−1,0)`, `(1,0,0)` and `(0,1,0)` for θ of 0°, 90° and 180°, so a 2D author learns one rotation and spends it on the listener and on every directional emitter. This is the one-line recipe the package doc gives, because nobody writes it by hand correctly the first time.
- **The degenerate case** is a source at the listener's exact position: the projection is zero-length, the azimuth is 0, and it is heard centred. **The implementation never normalizes a zero vector.**

#### What is accepted rather than fixed

Judged by ear and accepted as known properties of equal-power stereo, not
defects ([the prototype](https://github.com/dvoyni/cog/issues/304)):

- **The fold behind.** A source passing behind pans the same as its mirror image in front, because two speakers cannot say behind and W3C folds rather than inventing a cue. This is why keeping HRTF out of scope is honest rather than a deferral.
- **The zenith snap.** The azimuth is 0 by definition where the horizontal projection vanishes.

### Clips

([sfx: clips by path through platform's FileSystem, their cache, preparing and release](https://github.com/dvoyni/cog/issues/949), from v1's [Assets: how a path becomes samples, and what streams](https://github.com/dvoyni/cog/issues/298), [sound on assetcache](https://github.com/dvoyni/cog/issues/412) and [What a released clip does to the voices playing it](https://github.com/dvoyni/cog/issues/416))

#### A clip is a path

- **Every verb, `Play`, `Preload` and `Release`, and every view entry takes or holds a plain `path string`,** read through platform's filesystem, as render's `Inventory` and canvas's `Assets` name their files. v1's `ClipRef`, `ClipWithResource`, `ClipWithBytes` and `ClipRef.Equal` are gone: with no Blob, a clip is comparable as the string it is.
- **Identity is v1's: one path is one clip.** Two plays of one path play the same clip, read once and shared.
- **Bytes a game holds go into a memory mount** and play by path. The game makes a `filesystems.NewMemoryFS()` in its `Init`, binds it as a read mount and keeps it as a resource of its own, puts the bytes from any job, then plays the path:

  ```go
  generated := filesystems.NewMemoryFS()
  c.BindSocket(platform.ReadMountSocket{FS: generated, Priority: 2, Prefix: "generated"})
  c.InitResource(generated)
  // later, in a job: generated.Put("voice.ogg", ogg), then queue.Play("generated/voice.ogg", 0, sfx.VoiceOptions{})
  ```

- **New bytes put under a path whose clip is resident change nothing** until the clip is released. v1 had no reload, and none is added.

#### The clip table

- **`ClipTable` replaces v1's asset Library,** which v2 does not carry, as render's `Inventory` and canvas's file table keep their own. Per path it keeps the clip's state, the `ClipID` its backend minted, its `ClipInfo`, its loop region, the epoch its prepare was started at, and its failure.
- **The table is in front of the read:** a path it holds is answered from it, and only a path it does not hold is read. There is no fourth state for "never asked" (see *Asking about a clip*).
- **The key is the path alone; nothing composes into it.** render composes a texture's colour space and mipmaps into its key; a clip has no variant dimension, because rate is a voice's parameter applied at playback, stereo is never downmixed, and nothing is baked. That holds only because a backend may not bake the device's rate into what it prepares (obligation 4): resampling is at playback, so a device change costs the table nothing.
- **The epoch** is what makes a release recorded against an in-flight prepare cost nothing. The token handed to `Prepare` carries the path and the epoch its entry held when the prepare began; a completion whose entry has gone, or moved to another epoch, is dropped with nothing called. That is also why a prepared value must be garbage-collectable: there is no destroy for something never installed.
- **Its failures are reported once each, under keys built from the path** (see *Reports*), and forgotten when the path leaves the table, as canvas's file table forgets its keys. v2's one-key `ForgetLogOnce` is therefore enough where v1 forgot a family of typed keys.
- **Destroys wait for the flush's changes.** A release takes the clip's `ClipID` out of the table into the `Destroys` of the flush's `FlushChanges`, after the flush has ended the clip's voices, and a backend applies a flush's stops before its destroys, since `FlushChanges` is the only route with an order.

#### The read is in the flush, and the design says so

- **A clip's bytes are read synchronously inside sfx's flush,** through the `platform.FS` read the flush holds, at the flush that applies the `Play` or `Preload` that first names the path. This is not an implementation accident left implicit: **it is the one place sfx can stall, and a reader deserves to know where.**
- **v2 forces no change.** A resource is never kept outside the function that reached it, so there is no filesystem to hand a goroutine: the read stays in a job holding the lock, and the prepare is what moves off the flush. What v2 adds is that the flush is a job of its own on a worker, so the stall holds up sfx's flush and the jobs waiting on its locks, not the whole frame.
- A flush that names no new path reads nothing.
- **Rejected:**
  - a per-flush byte budget, reading N bytes each flush: it makes a cold play's latency unbounded and untestable, and is strictly worse against the causality promise than one honest stall;
  - a read job of its own that the flush would wait on: a task's argument cannot carry a path, so it would need a private list resource and a handler or event to schedule it;
  - changing platform to offer a detachable reader, a redesign this effort has no business starting.

#### Preparing

- **`BackendSocket.Prepare(token any, encoded []byte)` is handed the slice sfx just read.** From then on the bytes are the backend's, and sfx keeps none.
- **Preparing stays where v1 had it:** otosound decodes in a goroutine of its own and answers `done=false`, and the result comes back through `TakePrepared` and `Install` at a later flush; the headless backend parses the headers inline and answers `done=true`. One interface covers both with no branch in sfx.
- **This drops v1's unstated residency,** in which the asset library kept every clip's encoded bytes until release, decoded clips included. A decoded clip's encoded bytes are freed once it is decoded, and a streamed clip's live as long as its backend holds them.
- **`Install` mints the `ClipID`, at the flush,** where the destroy that pairs with it is guaranteed to be statable (see *The seam*).

#### Two tiers, and which is invisible

([Assets](https://github.com/dvoyni/cog/issues/298))

- **A path becomes samples in two tiers, and which tier is the backend's business.** A clip whose decoded size fits under a limit the backend's config names is decoded once and its voices index one shared buffer; a longer clip **streams**, a decoder per voice.
- **Neither tier alone is defensible:**
  - always decoding: a five-minute stereo 44.1 kHz track is 101 MiB of `float32` against about 4.8 MB encoded, 21×, and the measured mono test clip came out at 51.6×. Music cannot be resident.
  - always streaming: 460 µs to open a decoder and 137 KB resident per live decoder. A footstep would pay half a millisecond and 137 KB for a clip whose whole decoded form is smaller than its decoder. The break-even is 0.78 s mono and 0.39 s stereo.
- **otosound's knob is `DecodedClipLimit`,** the largest decoded size, in bytes, a clip may have and still be decoded once: 0 means 512 KiB, the default; −1 means no limit, always decode; −2 means always stream; above 0 is the limit in bytes.
  - The limit is on **decoded** size, because decoded size is what costs memory, and it is computable before decoding, as `Length() × Channels × 4`, from the identification header and the end granule position. At 512 KiB that is 2.97 s mono at 44.1 kHz, 1.49 s stereo at 44.1 kHz, and 1.37 s stereo at 48 kHz. It sits deliberately above the 137 KB memory break-even: between the two, memory is knowingly spent to make a `Play` free.
  - **A clip whose length reads 0,** truncated or from a source that cannot seek, has no computable decoded size and therefore streams.
  - **The knob is the backend's, not sfx's,** because choosing the tier needs duration, channels and rate, which exist only once the backend has parsed the headers. A knob in sfx's `Config` would force a two-phase socket and would put "streamed or resident" above a seam that speaks only of clip identity and bytes. How otosound streams is its own design's.
- **The tier is invisible to the game,** which is the rule of *What is never said* holding, and it is why `Preload` promises what it does.

#### `Preload` promises residency, not a free `Play`

- **`Preload(path)` promises that the clip is resident and will not fail. It does not promise that the next `Play` is free.** It cannot: for a short clip the next play is free; for a long one the first play still opens a decoder, 460 µs; and the game cannot tell which it has. A guarantee the caller cannot verify and sfx cannot keep is worse than the weaker one that is always true.
- **For a streamed clip, stated rather than implied:** `Preload` reads the bytes and parses the headers, and that is all it can do. The first `Play` still opens a decoder of its own, and the voice is silent until its read-ahead ring primes: residency, with a visible cost, and not a free play ([#482](https://github.com/dvoyni/cog/issues/482)'s handover).
- **`Preload` is the lever for choosing which frame eats the read:** a loading screen rather than the first shot fired.
- **It is idempotent where it matters:** preloading a clip already loaded, loading or playing changes nothing, and a `Play` and a `Preload` of one path name one entry and cost one prepare.

#### A play before its clip is ready

- **The voice exists from the flush that applies the play.** It is addressable, stoppable and seekable, silent until its clip is ready, and then starts **from its offset, with no catch-up.** Catch-up would serve music sync and ruin one-shots, the overwhelming majority: a 300 ms load would play a footstep's tail.
- **This is deliberately unlike the device rule** in *The device*, where a playhead advances whether or not anyone hears it. A clip's load is a bounded hiccup sfx is actively fixing, measured in milliseconds, so starting at the head is honest; a device's readiness may never come at all. The two rules sit together here rather than letting a reader discover the difference.

#### Failure

- **A clip that cannot be read or prepared is reported once,** under its path (see *Reports*), and is **terminal:** the entry holds the failure, and only a release clears it. Voices already recorded against it end with `ReasonFailed`, a flush or more later.
- **There is no retry and no "try again".** Every clip failure is terminal: a bad path, a file that is not Ogg Vorbis, a corrupt stream. otosound's table is ordinary Go memory whether or not its device is ready. **Readiness gates audibility, not residency.**
- **There is no fallback clip.** A silence-shaped stand-in would be a lie with a duration; a pending voice is not playing anything at all.

#### Asking about a clip

```go
func (t ClipTable) Info(path string) (ClipInfo, ClipState)

type ClipInfo struct {
	Duration   time.Duration
	Channels   int // 1 or 2; a stereo clip is never downmixed
	SampleRate int // the clip's own, never the device's
}

type ClipState uint8 // ClipLoading, ClipReady, ClipFailed
```

- **`ClipTable.Info(path)` replaces v1's free function `ClipInfoOf`.** `ClipInfo` carries no ID: nothing a game must load or release is the requirement, so there is no handle to a clip, and exporting one invites a game to hold it. What gameplay legitimately wants of a clip is a question, not a handle. The state is spelled `ClipState` throughout, now that there is one package.
- **`ClipLoading` is a clip whose prepare is in flight, and also a path nothing has named.** There is no fourth state for "never asked": nothing failed and nothing is resident, which is what `ClipLoading` says. A game that only ever asks is told `ClipLoading` forever; `Preload` is the verb that changes the answer.
- **Asking never starts a load.** It reads sfx's own table; a question that loaded would be the second read sfx does not have.
- **The state beside the facts** is what stops a zero duration being read as an answer: every fact is zero until the clip is ready, and a duration of zero and a state of `ClipLoading` are different answers to different questions. A descriptor that reports a size of zero forever cannot tell "not yet" from "never", and this does not repeat that.

#### Release

([What a released clip does to the voices playing it](https://github.com/dvoyni/cog/issues/416))

- **A release stops every voice playing the clip.** A deferral until the last voice ends was retired, not moved up a layer, and the failing sequence is what decides it: a game releases a level's clips, one of which is a looping ambience. Under deferral the last voice on that clip never ends, so the release is never forwarded, the memory never comes back, and nothing is reported. `Release`, called only to get memory back, did nothing at all, silently, for the rest of the process. A release that quietly does not release is worse than one that stops a sound, and "releasing something still in use is the caller's mistake" is already render's rule, where a freed texture a material binds draws as unbound.
- **Those voices end with `ReasonReleased`.** A pending voice is cut the same way, and ends `ReasonReleased` rather than `ReasonFailed`: nothing failed, the game changed its mind. The rule that holds a failure back a flush does not apply, since a release is the game's own verb.
- **`ReleaseAll()` cuts everything,** which is what a teardown call means. A game that wants one track to bridge a transition releases clip by clip and keeps the bridging one, or lets `ReleaseAll` cut it and starts the bridge afterwards; naming a clip again reloads it.
- **Requirement 2 is the backstop.** Release is optional, so a game that never records it never meets any of this, which is what makes the cheap, honest answer good enough.
- **It is a guarantee, not a reported property:** after the flush that applies a release, no voice is playing that clip, on any backend. Stopping a voice needs no cooperation from a buffer's lifetime.
- **The stops precede the destroy in the same flush's changes,** so a release is atomic within its flush and the mixer never applies a destroy for a clip it is still mixing. The flush does the two in that order, the voices first and then the clip table, and `FlushChanges` is the only route a destroy takes.
- **A release forgets the clip's failure,** both of its report keys, which is the whole of "only a release clears it": a path that failed, was fixed and is named again is read again, and can be reported again. `ReleaseAll` forgets the keys of every path the table holds.
- **A streamed voice's read-ahead stops with it.**
- **Releasing a streamed clip while it plays is safe as built,** and for a reason worth saying: sfx stops the voices first, which halts their read-aheads, and the encoded bytes belong to the backend, which keeps them as long as a decoder reads them, and Go's collector frees them. Nothing the destroy frees is anything a decoder still holds.

#### What a clip from bytes costs

- **Bytes given through a memory mount are held once by the mount,** a copy its `Put` keeps until the game removes them, **and once by the backend while it keeps them,** the copy the flush's read answered: for a decoded clip until it is decoded, and for a streamed clip as long as it plays. v1's Blob copied nothing; v2's rule of a memory mount and a path costs the copy.
- **A game that wants the memory back removes them from the mount once the clip is ready,** as `ClipTable.Info` answers. A path removed from the mount and then released and named again no longer reads, and fails as any missing file does.

#### The loop region

([where a music track's loop repeats from](https://github.com/dvoyni/cog/issues/471))

- **A loop point is a fact about a clip, not a parameter of a voice.**

  ```go
  // LoopRegion is the span a looping voice repeats between, in seconds.
  type LoopRegion struct{ Start, End float32 }
  ```

- **It rides in the Ogg file's Vorbis comment header,** is parsed by the backend in the same pass that already reads duration, channels and rate, and comes back up on `PreparedClip.LoopRegion() m.Maybe[LoopRegion]`. **Absent means the whole clip**, following the rule that an absent `m.Maybe` is the default.
- **`VoiceOptions.Loop` stays `m.Maybe[bool]` and `VoiceStart.Loop` stays `bool`.** `Loop` simply stops meaning "repeat the whole clip" and starts meaning **"repeat the way this clip says to"**. A clip with no tag says whole, which is what every untagged clip says, so no behaviour moves.
- **The tags,** read case-insensitively, since a Vorbis comment's field name is case-insensitive by the format's own definition and tools disagree:

  | Tag | Meaning |
  |---|---|
  | `LOOPSTART` | loop start, **in sample frames** |
  | `LOOPLENGTH` | loop length in sample frames; `End = Start + Length` |
  | `LOOPEND` | loop end in sample frames, used when `LOOPLENGTH` is absent |

  - This is the de-facto convention game-music tooling already writes, which is the whole point of choosing it: a composer's export is the authoring surface, and no Go code has to agree with a file it cannot see.
  - `LOOPSTART` alone, an intro running into a loop that carries on to the end of the file, ends the region at the clip's own end. `LOOPLENGTH` wins over `LOOPEND` where a file carries both. A file that repeats a tag is read by its first.
- **A malformed region is dropped whole, never clamped.** If `Start < 0`, or `End <= Start`, or `End` is past the clip's end, or a value does not parse, or the comment header cannot be read, the backend drops the region entirely and the clip loops whole. A clamped loop sounds like a working loop with the wrong loop point, the single hardest audio bug to attribute, and clamping would make a tag's meaning depend on the clip it sits in.
  - **The drop crosses the seam back to sfx** on the prepared clip, through `PreparedClip.LoopRegionErr()`, an `ErrLoopRegionIgnored` with no `Path`, nil when the region is fine or absent. sfx fills in its `Path` and reports it once for the clip, under the clip's path (see *Reports*). The clip prepares and plays; this is the only notice that it plays without the loop point its author wrote.
  - sfx also checks the region against the duration when the clip installs, and treats a region the clip does not contain as the whole clip. That is not distrust of the backend; it is that the playhead's wrap subtracts a span in a loop, and a span outside the clip is a loop with no reason to stop.
- **sfx needs the region for two reasons of its own,** which is why it comes up rather than staying below:
  - **the playhead:** a looping voice wraps at the loop end, not at the duration;
  - **`Seek`:** a seek past the clip's end wraps **to the loop start**, not to zero, so a seek off the end of a track with an intro lands in the loop rather than replaying the intro.

  Endings are unaffected: a looping voice never ends by itself. A looping voice starts at the head of its clip and runs into its loop: a playhead before the loop start is carried forwards into it, never lifted up to it.
- **The granule correction and the musical loop point are one parse in one place.** The true final sample of a stream, against a decoded buffer that may carry codec padding, and the `LOOPSTART` tag both come out of the backend's one pass over the headers. They compose in one function, and sfx sees neither input. An absent region still means loop to the granule end, never to the padded buffer's end.
- **The contract promises a loop is gapless: a looping voice produces no gap, and no repeated or dropped sample frame, at its loop point,** on every backend that makes sound.
  - This is a third kind of promise beside ordering and causality, admitted for one reason: the backends hold the sample-exact mechanism, so the promise costs nothing to keep and something real to lose. Written down, it stops a later optimisation from spending it quietly.
  - It is deliberately narrow: it says nothing about underruns, or anything the device's lifecycle governs.
  - It is tested **at the backend,** which can render to a buffer and assert on frames, not through the game's face, which by design cannot see a sample.

### Pause

([sfx: what pauses sound in v2, where v1 suspended on app's pause](https://github.com/dvoyni/cog/issues/950))

- **The game pauses sound.** It records `Queue.SetPaused(paused bool)`, which sets one flag over the whole voice table. v2 has no pause of the engine's for sfx to follow, no paused track and no pause event, so v1's "games do nothing" cannot hold. This follows mux's precedent: a game that pauses its own clock, with mux's `Pause` "as when a menu is shown over the game", records `SetPaused` beside it.
- **What a pause does is v1's, word for word:**
  - **every live voice is suspended, and resumes on the same sample.** A paused game that comes back to its music thirty seconds in is a bug in every game that has ever paused;
  - **playheads stop;**
  - **a voice played during the pause starts suspended,** from its first flush, without anybody having said so about it;
  - **`Stop`, `Seek` and the release verbs still apply;**
  - **the device stays open and is fed silence.** Closing it risks a reopen that fails, and costs the open again;
  - **a pause wins over a device that is not ready:** a pause while the device is not ready suspends everything, the playhead rule included, and nothing advances;
  - **it crosses the seam as `VoiceParams.Paused` updates for every live voice whose pause it changes,** at most `MaxVoices` entries in the changes of the flush that applies it, with no special path, never as oto's `Context.Suspend`; a voice whose params it leaves as they were, one the game already paused itself, is not restated, as a voice at a bus volume it already had is not. oto's open bugs in its suspend, old audio played at resume on macOS (ebitengine/oto#336) and a sink kept active on PulseAudio (ebitengine/oto#292), are therefore never reached;
  - **set twice before one flush, the last value wins,** as `SetBus` and `SetListener` do. It is applied where the coalesced values land, so the flush that applies a pause advances no playhead.
- **A pause does not change `Device`.** A pause is not a device state.
- **The name is `SetPaused`,** after its neighbours `SetVoice`, `SetBus` and `SetListener`, not mux's `Pause` and `Resume`. A voice's own `VoiceOptions.Paused` stays, and a voice is silent while either is set, as in v1. `VoiceEntry.Paused` answers whether the playhead is stopped by either, while the voice's own `Options.Paused` stays what the game asked for, so the two are never confused.
- **A pause suspends everything, a click a pause menu plays included,** as in v1. A game that wants menu sounds during its pause pauses its gameplay voices itself, by their own `Paused`. No per-bus pause is added.
- **Focus and minimizing pause nothing.** v1 paused on neither, so nothing is ported. A game that wants to pause on focus loss records `SetPaused` from its own handler of `platform.FocusEvent`. Minimizing has no platform event, and this effort adds none.
- **Zeroing a gain does not suspend:** a resident buffer started from its head keeps advancing whatever its gain is, so a resume would land thirty seconds in. That is why the backend stops the source rather than silencing it, and why pause is a field of `VoiceParams` and not a verb of the socket (see *The seam*).

### The device

([The device's lifecycle: not ready, absent, and lost](https://github.com/dvoyni/cog/issues/299), with [sfx on the kernel](https://github.com/dvoyni/cog/issues/948) items 10 and 11, [sfx research: oto v3](https://github.com/dvoyni/cog/issues/947) and [sfx: how its reports reach the kernel's logger](https://github.com/dvoyni/cog/issues/952))

> **Audio never stops a game from starting,** and a device that is missing, late
> or lost is a fact the game can read rather than an error it must handle.

#### Spawning never fails on a device

- **A backend binds sfx's socket whether or not a device exists.** Its `Init` and `Start` neither block nor fail on one.
- **A device that cannot be opened at all,** no sound card, a locked device, a CI machine, **is reported once by the backend, as a warning**, after which that backend accepts everything and plays nothing, as the headless backend does, its device not ready. The game runs, under the default logger too, since a warning ends nothing (see *Reports*).
- **The report belongs to the backend, never to sfx.** sfx cannot tell "a device was tried and failed" from "a device is not ready yet"; only the backend knows which. A silence nobody asked about is not a bug.
- **Rejected:** failing the composition when no device opens. Sound is the one subsystem whose total absence costs a game nothing.

#### Three states, and one rule about time

- **The device is absent** (never opened), **not ready yet** (for the milliseconds an open takes on desktop) **or lost** (unplugged, the default device changed). A game sees one thing in all three: `Ready` is false.
- **A voice's playhead advances whether or not anyone can hear it.** A voice recorded while the device is not ready exists from the flush that applied its play, advances silently, and is simply mid-clip when the device arrives, or has already ended, its `VoiceEndedEvent` delivered on schedule. A footstep played thirty seconds before the device arrives is over by then; music started then is heard thirty seconds in.
- **Rejected:** suspending time until the device is ready. It makes the queue and the first audible moment both unbounded, thirty seconds of accumulated footsteps arriving as one wall, and it breaks the causality promise in the direction nobody checks: a `Play` from flush *N* becoming audible after minutes of world time.

#### Loss is ordinary, and is not reported

- **Headphones come out; the default device changes.** This is common, not a fault.
- **Nothing is reported,** not even once. `Ready` going false is the whole notice.
- **Playback keeps being simulated:** playheads run, voices end on time, options keep coalescing.
- **The backend retries the open on a slow cadence,** once a second by default, indefinitely, and polls an open device for loss on the same cadence: otosound's `DeviceRetry` paces both, as v1's constant did.
- **When the device becomes ready, voices resume where the world is now,** not where it was when the device went away.
  - **How it is said:** sfx notices `Ready` going true at the flush that polls it, and restates every live voice as a `VoiceStart` at the playhead it has now. A start is the only operation that carries a position, so a restart is the resync, and it carries that flush's options with it, so no update is owed beside it. Its cost is bounded by `MaxVoices`, the bound a bus volume change already pays, and it is paid once per arrival, never per flush.
  - **This is sfx's job, not the backend's.** A backend has no playheads at all: sfx computes them, which is what makes a test's timeline world time rather than the device's. A backend left to resync itself could only resume its own mixer table where that table stopped, which is where the world was.
  - **It covers every arrival, not just a recovery.** A device that was never ready becoming ready is the same transition and gets the same restart: every voice played before it is re-pinned to the world, and those whose clips ran out meanwhile have already ended.
  - **A consequence:** resuming a streamed voice at an arbitrary playhead costs a decoder open and a seek, about 460 µs, where a resident voice costs an index. An arrival that restarts many streamed voices at once pays that many times.
- **What otosound can actually recover is narrower than v1 promised.** oto's context records an error it never clears, and a context cannot be made again in one process, so the once-a-second retry finds the same dead context. It recovers a Windows machine that had no device at start, and a device switch oto rides out by itself in under about ten seconds. Everywhere else, a failed device means no sound until the process restarts. This replaces v1's promise that every live voice resumes on recovery; otosound's design states it with its cases.

#### `sfx.Device`

```go
// Device reports the sound device as it is now. It is read-only: a game cannot
// choose a backend, only see which one it got.
type Device struct {
	// Ready reports whether anything is audible. False means absent, not opened
	// yet, or lost; a game cannot tell which, and does not need to.
	Ready bool
	// Name is the backend's own name: "otosound", or "headless".
	Name string
	// SampleRate and Channels are the device's, not a clip's. Zero until Ready.
	SampleRate int
	Channels   int
	// Latency is the length of the output buffer in force, which is all oto
	// offers, not the figure a Config asked for. Zero until Ready. The contract
	// promises ordering and causality and never a latency figure, so this is
	// the only place one is reported.
	Latency time.Duration
}
```

- **Its resource is `DeviceView`, which the flush writes from `BackendSocket.Device()`,** polled once per flush, a field read on the backend's side. A backend builds the `Device` value; a reader under `kernel.Read[sfx.DeviceView]` gets a copy, asks it `Device()`, and can change nothing. Before the first flush it answers the zero `Device`, not ready, since nothing has polled the backend yet.
- **What a game does with it is the justification, not a nicety:** on the web, which v1's jssound served and v2 does not target yet, a game draws its "click to enable sound" prompt from it, since `Ready` is the only way to know the gesture is still owed; a rhythm game reads the latency from it. Nothing else in the contract can say either.
- **Routine answers, recorded rather than re-asked:**
  - **the headless backend reports `Ready` true.** It is a working device that plays nothing, not a missing one: a game gating anything on `Ready` must not wait forever under it, and a test must not special-case it;
  - **`Latency` is zero under the headless backend,** which is true rather than a placeholder: nothing is buffered;
  - **a pause does not change `Device`.**
- **Several kernels in one process.** v2 runs several kernels under one supervisor, spawned and destroyed on demand, which is v1's "two Engines in one process" with kernels that also come and go. oto allows one context per process, so otosound's context is never closed: a later kernel's otosound takes a player of its own on it, both kernels are audible, the first kernel's device settings win, a later kernel's are reported as ignored, and its `Device` reports the actual values in force, never the ones it asked for. Which kernel ran first is a race, and silence decided by a race is worse than an audible kernel with a stated compromise. otosound's design holds the detail.
- **Channel count is not a knob.** The output is stereo because panning is: a mono output would make constant-power panning meaningless, and a surround output is a different mixer.

### Reports

([sfx: how its reports reach the kernel's logger without ending a game](https://github.com/dvoyni/cog/issues/952), with the keys of [clips by path](https://github.com/dvoyni/cog/issues/949))

- **One principle sets each report's level.**
  - **A mistake in the game's own files, composition or code is an error,** `LogErr`, as render reports a missing texture: fatal under the default `StdLogger` while the game is developed, and the application's `LogHandler`'s to judge in a shipped game.
  - **A fact about the machine the game runs on is a warning,** `LogWarn`, because the game reads it from `Device` rather than handling it. That keeps "audio never stops a game from starting" true under the default logger, where every `LogErr` panics.
  - A composition error, such as a negative `Config` field or a missing backend, panics, as every v2 composition error does.

  | Report | Who | Level and key |
  |---|---|---|
  | A clip's bytes unreadable, `ErrClipUnreadable{Path, Err}`: the failed read of a path, carrying the path and the filesystem's error | sfx | `LogErrOnce` under the clip's key, a prefix plus the path |
  | A clip that is not Ogg Vorbis, has no length or no format, or fails to install: the backend's error, wrapped with the path in `ErrClipFailed` | sfx | `LogErrOnce` under the clip's key |
  | A loop region ignored, the clip playing whole: the `ErrLoopRegionIgnored` `PreparedClip.LoopRegionErr()` answered, its `Path` filled in by sfx | sfx | `LogErrOnce` under a key of its own for the clip, a second prefix plus the path |
  | A backend answering a prepare with neither a prepared clip nor an error, or installing under the zero ID, `ErrClipNotPrepared`: a bug in the backend | sfx | `LogErr`, the entry failing for good |
  | No device could be opened | the backend (otosound) | `LogWarnOnce` |
  | A later kernel's device settings ignored | otosound | `LogErrOnce` |
  | A device that was open and is lost | nobody | `Device` going not ready is the whole notice |

- **Everything a backend learns about a clip crosses the seam back to sfx,** which reports it under the clip's path and forgets it on release:
  - a prepare failure comes back as `Prepare`'s error, or as a completion's `Err` through `TakePrepared`, or as `Install`'s error;
  - an ignored loop region comes back on the prepared clip, through `PreparedClip.LoopRegionErr() error`, an `ErrLoopRegionIgnored` with no `Path`, nil when the region is fine or absent, read when the clip installs.

  v1's reports of a dropped loop region could not name the clip, since a backend had no name for one; v2's do.
- **Backends report only facts about the device.** otosound reports through the Logger it keeps from `Configurator.Logger()` in `Init`, from its own goroutine that opens and watches the device, since v2's Logger is safe from any goroutine; v1's report subscriptions in otosound and nosound go. **The headless backend reports nothing,** and needs no Logger.
- **The keys are strings, built from a prefix of sfx's own and the path,** and the clip table forgets both of a path's keys with `ForgetLogOnce` when the path leaves it: on `Release`, and on `ReleaseAll` for each path the table holds. A key built at run time allocates, which is a first play's cost, outside the steady flush's gate.
- **The error types** are kept from v1, each in its plugin's `err.go`:
  - sfx's: `ErrClipUnreadable`, now the failed read of a path with the path and the filesystem's error; `ErrClipNotPrepared`; `ErrClipFailed`, a backend's prepare or install failure wrapped with the path it happened to, unwrapping to what the backend said; and `ErrLoopRegionIgnored{Path, Start, End, Frames, Err}`, declared once, here, where v1 declared a copy in each backend. The backend knows what went wrong and sfx knows which clip it was, and a report carrying only one of the two names a condition nobody can find: a backend answers `ErrLoopRegionIgnored` with no `Path`, and sfx fills it in when it reports it, `LogErrOnce` under the clip's loop key.
  - the headless backend's: the Ogg header errors `ErrNotOggVorbis`, `ErrNoStreamFormat` and `ErrNoStreamLength` (see *The headless backend*), which sfx wraps in `ErrClipFailed`, carrying the path.
  - otosound's are its design's, the same three Ogg header errors among them.
  - v1's `ErrInvalidConfig` and `ErrInvalidMaxVoices` go: a config that is not a `*Config` panics in `kernel.PluginConfigOrDefault`, and a negative field panics in `Init`.

### The seam

([What a sound Backend speaks: the seam beneath the sound Slot](https://github.com/dvoyni/cog/issues/376), with [sfx on the kernel](https://github.com/dvoyni/cog/issues/948) item 3, [clips by path](https://github.com/dvoyni/cog/issues/949) item 3 and [reports](https://github.com/dvoyni/cog/issues/952) item 2)

> **The backend is a dumb, allocation-free sink with a fixed table of slots.**

It is told how many slots exist once, receives one `FlushChanges` per flush,
multiplies a gain matrix it did not compute, and is asked two questions. It knows nothing of
handles, buses, priorities, positions, the cap or the equations.

#### The socket

```go
// BackendSocket is the backend seam. Exactly one plugin or adapter binds it in
// an Init, with configurator.BindSocket[sfx.BackendSocket]: otosound, or the
// headless backend. A kernel with none panics in Spawn.
type BackendSocket interface {
	// Voices tells the backend how many slots exist. Called once, in sfx's
	// Start, before any Emit. Slots are numbered [0, n), and sfx stops a slot
	// before it reuses one.
	Voices(n int)

	// Emit applies one flush's operations. changes is owned by sfx and is
	// valid only for the duration of the call.
	Emit(changes *FlushChanges)

	// Device reports the device as it is now, Ready included. Polled once per
	// flush; it is a field read, not a query.
	Device() Device

	// Prepare turns encoded Ogg bytes into something voices can be started
	// from: samples for a short clip, the bytes and a way to stream them for a
	// long one. Which is the backend's business. The bytes are the backend's
	// from the call on; sfx keeps none. done=true means prepared is valid now;
	// done=false means it will appear in TakePrepared under the same token. The
	// prepared value must be garbage-collectable.
	Prepare(token any, encoded []byte) (prepared PreparedClip, done bool, err error)

	// TakePrepared answers the prepares that finished since the last call, and
	// clears them.
	TakePrepared() []Prepared

	// Install mints the ID of a prepared clip.
	Install(clip PreparedClip) (ClipID, error)
}
```

- **`BackendSocket` is v1's `Backend` interface,** declared with `DeclareExclusiveSocket[BackendSocket](false)`, alone in `sockets.go`. gfx's backend seam was renamed `BackendSocket` to match.
- **sfx calls it from its `Start` and from the flush alone, one call at a time,** since the flush holds its private resource's write lock. A backend needs no lock of its own against sfx's calls; whatever else it runs, such as otosound's mixer and goroutines, is its own to guard.

#### A slot, not a handle

```go
// VoiceSlot identifies a voice to a backend. It is always in [0, MaxVoices),
// minted and reused by sfx, which is what lets a backend's voice table be a
// fixed array indexed directly by it.
type VoiceSlot int
```

- **`Voice`'s index and generation are not public contract,** so they cannot cross into a backend without exporting the accessors the handle refuses; so they do not cross at all.
- **sfx guarantees a slot is stopped before it is reused,** so a backend never sees an ambiguous ID and needs no generation of its own. A steal is where the two land in one flush's changes, which is why `Stops` are applied before `Starts`. The map between the two is sfx's, and it is indexable because the handle's index half is the slot.
- **A backend allocates nothing per play.** Its table is `MaxVoices` long, made once at `Voices(n)`.

#### A 2×2 gain matrix, computed above the seam

```go
// VoiceParams is everything about how a voice sounds, as a backend sees it.
type VoiceParams struct {
	// Gains maps source channels to output channels: Gains[source][output].
	// Row 0 is a mono clip's only row. sfx computes it from the voice's volume,
	// its bus, its falloff and cone and the W3C panning equations; the backend
	// multiplies and nothing else.
	Gains [2][2]float32
	// Rate is the playback rate, 1 being the clip's own.
	Rate float32
	// Paused stops the voice advancing without ending it.
	Paused bool
}
```

- **Sixteen bytes, and no equation below the seam.** A scalar pan cannot express W3C's stereo rule, `outL = inL + inR·cos x`, `outR = inR·sin x` on one side and its mirror on the other, and mixing a stereo clip down to dodge it was refused. Since a stereo positional voice is a supported choice rather than a discouraged one, all four entries are ordinary traffic.
- **Rejected:** a scalar pan with a separate stereo rule, which puts half of a W3C equation in every backend and lets two backends disagree about one clip.
- **`Rate` is the cheap part of resampling:** the interpolation for a voice's rate runs on the device thread, and the expensive base-rate resampler runs in `Prepare` (obligation 3).

#### A backend declicks

- **sfx emits target values once per flush; the backend ramps to them across its own blocks.** A stop is the same mechanism: ramp to zero over a few milliseconds, then free the slot.
- **The reason** is that declicking is a function of the output block rate, which only the backend knows. sfx runs at its flush's rate, 16.7 ms on a 60 Hz update track, against otosound's 10 ms buffer, so anything sfx emitted would be a step by the time it was heard.
- **Measured** in [the prototype](https://github.com/dvoyni/cog/issues/304): without declicking, the gain steps 0.078 of amplitude in a single sample at every 10 ms block boundary; with it, 0.00016, smaller than the ramp's own per-sample motion. About 480× worse, repeating at 100 Hz, which is what a zipper is.

#### One call per flush

```go
// FlushChanges is the changes one flush hands the backend.
type FlushChanges struct {
	Starts   []VoiceStart
	Updates  []VoiceUpdate
	Stops    []VoiceSlot // applied first, before the starts that reuse their slots
	Destroys []ClipID    // ordered after the stops that precede them
}

// VoiceStart begins one voice in one slot.
type VoiceStart struct {
	Slot   VoiceSlot
	Clip   ClipID
	Offset time.Duration // where to begin; 0 is the head
	Loop   bool
	Params VoiceParams
}

// VoiceUpdate restates one slot's parameters.
type VoiceUpdate struct {
	Slot   VoiceSlot
	Params VoiceParams
}
```

- **A `FlushChanges` is owned by sfx and valid only for the duration of `Emit`.** sfx resets it for the next flush keeping its capacity, so a warm flush builds it with no allocation; a backend that needs it after the call copies it. Its starts, updates and stops are made `MaxVoices` long in `Init`, since a flush states each slot at most once in each, so the flush where the device arrives, restarting every live voice at once, grows none of them.
- **A backend applies the stops first, then the starts, then the updates.** The fields are not commutative on one slot, and a steal states both about it in one flush's changes: the victim's stop, and the start that takes the slot over. That is the whole of what "sfx stops a slot before it reuses one" asks of a backend, and why stealing needs no verb at the seam.
- **A `Seek` is a `VoiceStart` with an `Offset`,** which is also how a sample-accurate start on a node-graph backend would be reached without the seam naming a sample.
- **A voice played and ended inside one flush appears in neither starts nor stops,** which is the atomic-per-flush promise seen from below: it was never audible, so there is nothing to start and nothing to stop.
- **One handoff point per flush** is what lets otosound hand a whole flush to its mixer atomically. otosound's handoff is v1's: a handoff ring of each flush's changes, preallocated, published with one atomic store and drained whole at a block boundary, a mixer that never frees and takes no lock, and the frees done on the flush's side once the mixer has passed the changes that destroyed a clip. That is otosound's design.
- **Rejected:**
  - one call per operation, which makes the atomic-per-flush guarantee something each backend must reconstruct;
  - a latest-wins publish between the flush and the mixer: a flush's changes are a delta, so latest-wins silently drops a `Play` whenever two flushes land in one block's gap.

#### Nothing is pushed; two things are polled

```go
type PreparedClip interface {
	Duration() float32
	Channels() int
	SampleRate() int
	LoopRegion() m.Maybe[LoopRegion] // absent: loop the whole clip
	LoopRegionErr() error            // why a declared region was dropped; nil when fine or absent
}

// Prepared is one finished prepare, drained at the top of a flush.
type Prepared struct {
	Token any
	Clip  PreparedClip
	Err   error
}

// ClipID is an opaque handle minted by the backend. Zero means none.
type ClipID uint32
```

- **Every return path is a poll at the flush.** A callback would arrive on the device thread or a prepare goroutine, where there is no task and no lock.
- **There is nothing to poll about voices at all.** sfx computes endings from `Duration()` and each voice's rate, and all five reasons are things sfx decides, so **a backend never reports a voice.** `Device()` answers the whole struct rather than sitting beside a separate `Ready()`: one call, polled where every other answer is polled, and no thread question to answer.
- **`PreparedClip` stays opaque about samples and honest about facts.** It carries its facts because `VoiceEndedEvent` fires for every ending: if it were a bare `any`, only the backend would know when a clip runs out, the headless backend would never end a voice, and a game tested under the headless backend would behave differently from the same game under otosound. `LoopRegionErr` joins them so that what a backend learns about a clip reaches the one place that can name it.
- **`Prepared.Token`** is the value sfx handed to `Prepare`, and is how a completion finds the entry it belongs to, or fails to, when that entry was released while the prepare was in flight.
- **There is no `Destroy` on the socket.** A clip is released through `FlushChanges.Destroys` and nowhere else, because only `FlushChanges` carries an order: a destroy inside it is guaranteed to arrive after the stops that precede it, and a backend frees the clip once its mixer has passed those changes, which is what makes "the mixer never frees" true. A bare call outside `FlushChanges` has no such order, and two ways to say one release are what a later reader tries to unify and gets wrong ([#484](https://github.com/dvoyni/cog/issues/484)).
- **`Install` mints the ID rather than `Prepare`,** so that the handle comes into existence at sfx's flush, where the destroy that pairs with it is guaranteed to be statable. That is the same reason as the garbage-collectable rule, and the failing sequence is: a clip is released while its prepare is in flight; the entry goes; the completion arrives, sfx sees a stale epoch and drops the prepared value without calling anything, since there is no destroy for something never installed; anything native inside it would never be freed. Both backends satisfy the rule: otosound's prepared value is Go memory, its samples or its bytes, and the headless backend's is four facts.
- **One interface covers both backends with no branch in sfx:** otosound spawns a goroutine and answers `done=false`, and the headless backend parses the headers inline and answers `done=true`.

#### Pause is a field, not a verb

- **A pause crosses as `VoiceParams.Paused`,** and the pause of every voice is sfx emitting an update for each live voice, at most `MaxVoices` entries, with no special path.
- **Rejected:** a `SuspendAll()` verb on the socket, a second way to say what `FlushChanges` already says; two ways to say one thing can disagree.

### What a backend owes

([What a sound Backend speaks](https://github.com/dvoyni/cog/issues/376), [How a game tests its sound](https://github.com/dvoyni/cog/issues/467) §4, restated in v2's names by [sfx on the kernel](https://github.com/dvoyni/cog/issues/948) item 8, with [reports](https://github.com/dvoyni/cog/issues/952) item 2)

The obligations are a numbered list a backend author reads, and **each backend
proves its own, in its own package** (see *Testing Decisions*). otosound's
design proves each of them for otosound; the headless backend has no device
thread and no samples, and satisfies most of them vacuously, which is the right
outcome rather than an exemption.

1. **Nothing decodes on the thread that fills the device's buffer.** This is the difference between a glitch-free mixer and an unexplained crackle.
2. **The device thread may touch** its own voice table, whatever carries the flushes' changes to it, through atomics only, the prepared clip data it was given, and its read-ahead rings. **It may not** allocate, take any lock, free anything, call into sfx or the kernel, the kernel's Logger included, or decode. A backend's own goroutines that do not fill the device's buffer, such as otosound's that opens and watches the device, are not the device thread, and may log. A backend with no device thread satisfies this vacuously.
3. **Do not resample a clip's base rate on the device thread.** The expensive resampler runs in `Prepare`; the cheap interpolation for `Rate` runs on the device thread.
4. **Do not bake the device's rate into what `Prepare` answers,** so that a device change costs the clip table nothing.
5. **Declick every parameter change and every stop,** over the backend's own blocks.
6. **A looping voice is gapless** at its loop point: no gap, and no repeated or dropped frame.
7. **A prepared value is garbage-collectable.** Anything needing explicit release goes behind a `ClipID`, never into the value handed to `Install`.
8. **A slot is stopped before sfx reuses it,** so no generation is needed below the seam. This is sfx's guarantee, and a backend may rely on it rather than defend against it.
9. **`Device()` is a field read,** cheap enough to poll every flush.
10. **A device that could never be opened is reported once, by the backend, as a warning,** and the backend then accepts everything and plays nothing, as the headless backend does. **A lost device is reported never.** A backend reports nothing else: what it learns about a clip goes back to sfx, through `Prepare`'s error, a completion's `Err`, `Install`'s error, or `PreparedClip.LoopRegionErr()`.

### The headless backend

([sfx on the kernel](https://github.com/dvoyni/cog/issues/948) item 4, [reports](https://github.com/dvoyni/cog/issues/952), [how sfx, otosound and the headless backend are tested](https://github.com/dvoyni/cog/issues/951), from v1's [How a game tests its sound](https://github.com/dvoyni/cog/issues/467))

- **An adapter, not a plugin:** `headless.NewBackend(sampleRate int) *headless.Backend` in `plugins/sfx/headless`, a `sfx.BackendSocket` for a test's plugin, or an application's own, to bind, as platform's headless windows and gfx's recording adapter are bound. It answers its concrete type, as `headless.NewWindows()` and `recording.NewInstance()` do. `sampleRate` is what v1's nosound `Config` held, the rate its `Device` reports, so a test can assert a rate with no device; zero means 48000, and a negative rate panics, as a composition error.

  ```go
  func (p *headlessPlugin) Init(c *kernel.Configurator, config any) error {
  	c.BindSocket[sfx.BackendSocket](headless.NewBackend(0))
  	return nil
  }
  ```

  v1's nosound was a plugin with a `Config` and a subscription to app's `UpdateEvent` that reported its dropped loop regions. With clip facts crossing back to sfx and nothing left to report, the adapter needs neither.
- **It records nothing.** It is a pure sink: every start, update, stop and destroy is accepted and kept nowhere, and `Voices(n)` keeps the number nowhere, since there is no table to size when nothing is mixed. The live voice view is sfx's and is the same whatever backend was composed; putting a view in the silent backend would give it only to the composition that needs it least.
- **But it cannot install nothing,** because a voice waiting on its clip starts when the clip installs: if the headless backend installed nothing, no voice under it would ever play.
- **`Prepare` reads the Ogg headers and the stream's length, and decodes no samples.** It keeps the sample rate, channels, duration and loop region, and nothing else, and answers `done=true`, so nothing ever appears in `TakePrepared`. `Install` mints a non-zero ID from a counter that never repeats; nothing else about a clip is kept.
  - **Without a real duration** `ReasonFinished` never fires, the closure property has a hole, and a test cannot assert that a one-shot ended, which is most of what a test of sound wants to say. A real duration also keeps `ReasonFailed` reachable, a path no game can otherwise exercise on purpose. A header is not a recording of what played, so "records nothing" survives intact.
  - **Measured** against `jfreymuth/oggvorbis` v1.0.5 on [the prototype](https://github.com/dvoyni/cog/issues/304)'s clips:

    | Clip | Header only | Full decode | Ratio |
    |---|---|---|---|
    | `bellsmall.ogg` (mono, 44.1 kHz, 6.5 s) | 526 µs | 5.83 ms | 11× |
    | `engine5cyl.ogg` (mono, 96 kHz, 32.6 s) | 523 µs | 60.4 ms | 115× |
    | `pianoroll.ogg` (stereo, 44.1 kHz, 176 s) | 525 µs | 312 ms | **595×** |

    **The header's cost is flat,** about 525 µs whatever the clip's length, which is the property that matters: a test suite's cost stops scaling with the audio it names. `Reader.Length()` matched the decoded frame count exactly on all three, so the duration is exact, not an estimate.
  - **`Length()` needs an `io.Seeker`,** because it reads the last page's granule position, and on a source that cannot seek it answers 0. So the headless backend parses a `bytes.Reader` over the bytes it was handed: O(file size) in I/O, still O(1) in CPU. **A duration of zero is never silently accepted as a clip's length.**
  - **`NewReader` decodes the first audio packet,** not only the three header packets, because Vorbis data need not start at position zero. "Header only" is therefore constant work, not zero samples, and the design says so rather than promising more than the library does.
- **Its refusals are `Prepare`'s errors,** each terminal like every clip failure, the voices recorded against the clip ending `ReasonFailed`, and sfx reporting them wrapped in `ErrClipFailed`, which carries the path. They stay in the headless backend's `err.go`:
  - `ErrNotOggVorbis{Err}`: bytes whose Ogg Vorbis headers would not parse;
  - `ErrNoStreamFormat{SampleRate, Channels}`: an identification header naming no sample rate or no channels;
  - `ErrNoStreamLength`: a stream whose length reads zero, a truncated file or one whose last page carries no granule position. It is an error rather than a clip of no duration on purpose: a zero duration makes the playhead and the duration fiction, and silently removes `ReasonFinished` from every test that names that clip.
- **A malformed loop region** is answered by its prepared clip's `LoopRegionErr()`, sfx's `ErrLoopRegionIgnored` with its `Start`, `End`, `Frames` and `Err` and no `Path`, which sfx fills in when it reports it: the span the tags asked for and the clip's true length in frames, or the parse failure. A comment header that would not read costs the clip only its tags, and the clip still plays.
- **Its loop region and duration are the same arithmetic otosound runs,** over the same bytes, reaching the same answer, the loop region read by `loopregion.Read`, which both backends import: a clip reports one duration and one loop region whichever backend a game composed, or a test that passes under the headless backend says nothing about the game under otosound.
- **Its `Device` is constant:** `Ready` true, `Name` `"headless"`, the sample rate it was made with, 2 channels and a `Latency` of zero, built once since nothing is opened and nothing can be lost.
- **It reports nothing,** and needs no Logger.
- **Rejected:**
  - instant success with a zero duration, which makes the playhead and the duration fiction and silently removes `ReasonFinished` from every test;
  - decoding fully and discarding, which buys real durations that parsing the header already gives exactly, and charges a test suite 595× for them on a music-length clip.
- **It is a precedent as v1's nosound was:** fill the socket, keep what the contract must be able to report, keep nothing else, as gfx's recording adapter and platform's headless windows do for their sockets.

## Testing Decisions

([sfx: how sfx, otosound and the headless backend are tested on v2](https://github.com/dvoyni/cog/issues/951), from v1's [How a game tests its sound](https://github.com/dvoyni/cog/issues/467))

> **A test composes the headless backend, steps beats, and asserts against
> sfx's own flush-computed state and its events: never against samples, and
> never below the seam.**

**The determinism line is the seam.** Above it everything is deterministic on a
fixed step: which voices exist, their playheads, their audibility, the order
stealing takes, which endings fire and at which step. It stays deterministic
because a playhead advances whether or not anyone can hear it, so a test's
timeline is world time and never the device's. **Below it nothing is,** and a
test asserts nothing there: not `Device.Latency`, and not `Device.Ready` on a
real backend. Under the headless backend `Device` is constant by construction,
so a test may assert it there and nowhere else. That is not a caveat on the
testing story; it is the testing story.

**Four seams, each tested at its own cost:**

- **`plugins/sfx/sfxtest`, a harness after canvastest,** serving sfx's own tests and a game's:
  - `NewHarness(t)` and `Spawn(config, plugins...)`, composing platform with a memory mount, sfx with the given `*sfx.Config`, the headless backend unless `UseBackend` or `UsePluginBackend` says otherwise, and metronome on a `ManualClock`, the test's plugins after them;
  - `UseBackend(backend sfx.BackendSocket)`, before `Spawn`, binding a fake backend instead of the headless one, for the tests of a device not ready, a device arriving and prepares answering `done=false`;
  - `UsePluginBackend()`, before `Spawn`, binding no backend, so a plugin among the test's binds the socket, after rendertest's `UseSoftwareAdapter`: otosound's kernel tests use it with otosound's plugin over its fake device;
  - `Configure(name kernel.PluginName, config any)`, before `Spawn`, putting a plugin of the test's config into the kernel's config: otosound's kernel tests give otosound's `*Config` with it, a short `DeviceRetry` or another rate;
  - `Mount(path, bytes)`, putting clip bytes into the memory mount;
  - `Record(func(sfx.Queue))`, recording from the test for the next step, so a test records without writing a plugin;
  - `Step()`, which advances a fixed-step track on the `ManualClock`, its handler scheduling the recorders and then `Tasks.Flush` with the beat's `Delta + Lost`, as a game schedules it, so a plugin of the test's that schedules the flush too flushes twice a beat: a game's own plugin stays out, its recorders driven through `Record`;
  - `Read(func(sfx.LiveVoices, sfx.ClipTable, sfx.BusVolumes, sfx.Listener, sfx.DeviceView))`, run in a job holding their read locks after a step, so a test reads the views;
  - `Endings()`, the `VoiceEndedEvent`s in the order they were published;
  - `Reports()`, the reports logged, from a `LogHandler` of the harness's own;
  - `Warnings()`, the warnings logged, each its message and its args, which fail no test: a backend's device that could not be opened is one;
  - `ExpectError(match func(error) bool)`, naming an error the test expects: an error the test did not expect fails it, as rendertest's does.

  v1's probe plugins, its fixed `TimeStep` and its seven identical copies of an empty permanent-filesystem adapter go: v2's writable filesystem is optional, and harness packages are allowed where v1's architecture tests forbade a `soundtest`.
- **Plain package tests of the arithmetic,** with no kernel, as `kernel/AGENTS.md` asks of a module with an interface of its own: the distance models, the cone gain, azimuth and elevation, equal-power panning and the 2D rotation, pinned against W3C's published numbers, every one with a tolerance.
- **otosound's own,** plain package tests in otosound that render to a buffer through v1's fake device, so they run in CI with no sound card, proving each obligation for otosound, and its kernel tests on sfxtest through `UsePluginBackend`. No test opens a real device. They are otosound's design's.
- **The headless backend's package tests,** proving its few obligations: that durations come from the headers, and that `Device` is constant. v1's nosound tests move here.

**What a test asserts, and never asserts.**

- **It asserts** that a clip is playing, on the right bus, at an audibility of 0.4 rather than 0; that a one-shot ended, and why; that the right voice was stolen; which ending fires at which step; what a clip's state and facts are; which reports a step raised.
- **It never asserts** the gain matrix, the azimuth, samples, or anything below the seam. sfx's arithmetic is pinned once by its own package tests against W3C's numbers.
- **History is `VoiceEndedEvent`,** read through `Endings()`: a test can subscribe, so it reads the event stream, which is unbounded and ordered. v1's endings ring served the MCP provider, a reader that could not subscribe, and goes with it.

**v1's test cases move by case.** Each is carried over with its name and its
assertion, and its setup is rewritten onto sfxtest; v1's harness code is not
ported. By v1's file:

- **The flush and the voice:** `TestAOneShotEndsOnTheFlushItsDurationSaysItShould`, `TestAHandleIsUsableInTheFlushThatRecordedIt`, `TestEveryOperationIsANoOpOnAVoiceThatIsGone`, `TestAFailedClipEndsItsVoicesLaterAndNeverInTheSameFlush`, `TestAPlayBeforeItsClipIsReadyStartsFromItsOffsetWithNoCatchUp`, `TestTheFlushEmitsOneFlushChangesPerFlush`, `TestTheViewCarriesWhatTheGameCommandedAndWhatSfxDerived`, `TestAPausedVoiceSuspendsAndResumesWhereItStopped`, `TestAPlayAndASetVoiceInOneFlushReachTheSeamAsOne`, `TestAPlayThatFindsNoSlotEndsInTheFlushThatRecordedIt`, `TestTheDeviceIsPolledEveryFlush`, `TestASeekMovesThePlayheadAndCrossesTheSeamAsAStart`, `TestASeekClampsBelowZeroAndEndsAOneShotPastTheEnd`, `TestALoopingVoiceWrapsAndNeverEndsByItself`, `TestChangingLoopRestartsTheVoiceWhereItStands`, `TestPitchCrossesTheSeamAsRateAndScalesThePlayhead`.
- **The pause,** now recorded with `SetPaused`: `TestAPauseSuspendsEveryVoiceAndResumesItOnTheSameSample`, `TestAPauseIsAnUpdatePerLiveVoiceAndLeavesWhatTheGameSaid`, `TestAPauseBeatsADeviceThatIsNotReady`.
- **The handle:** `TestAHandleIsMintedWhenThePlayIsRecorded`, `TestASlotIsHandedOutAgainAtTheNextGeneration`, `TestAPlayThatFindsNoSlotStillGetsARealHandle`, `TestVoiceRendersBothHalves`.
- **Stealing:** `TestACrowdOfFootstepsCostsTheCapAndTheMusicSurvives`, `TestATieOnBothTermsBreaksByAgeOldestFirst`, `TestPriorityIsABandAndNotAWeight`, `TestAPausedVoiceRanksAtTheAudibilityItWouldHave`, `TestAVoiceWaitingOnItsClipOccupiesASlot`, `TestPriorityIsRestatedByTheSameOrderedListEveryOtherOptionIs`, `TestAStolenVoiceReachesTheBackendAsAnOrdinaryStop`, and `Example_playAStolenVoiceAgain`.
- **Buses:** `TestABusVolumeFallsOnItsOwnVoicesAndNoOthers`, `TestABusVolumeIsReadableAndCoalescedWithinOneFlush`, `TestAnAbsentBusAndAnOutOfRangeOneBothLandOnMaster`, `TestStopBusEndsItsVoicesWithReasonStoppedAndMasterStopsEverything`, `TestStopBusStopsThePlaysBeforeItAndNotTheOnesAfter`.
- **Positional audio, on sfxtest:** `TestANonPositionalVoiceIsHeardCentredAtEqualPower`, `TestTheFirstPositionMakesAVoicePositionalForTheRestOfItsLife`, `TestAudibilityDoesNotMoveOverAConstantRadiusOrbit`, `TestAPositionalVoiceWithNoOrientationIsEquallyLoudInEveryDirection`, `TestTheListenerIsReadableAndCoalescedFieldByField`, `TestMovingTheListenerReEmitsTheVoicesItMovedRelativeTo`.
- **The arithmetic, as plain package tests:** `TestTheAzimuthLandmarksAreExact`, `TestElevationIsPlusNinetyOverheadAndMinusNinetyUnderfoot`, `TestASourceAtTheListenersExactPositionIsHeardCentred`, `TestEqualPowerHoldsAcrossTheSweep`, `TestAFullOrbitFavoursNeitherEar`, `TestASourceBehindPansAsItsMirrorImageInFront`, `TestAMonoClipAtTheCentreIsEqualPowerRatherThanUnity`, `TestAStereoClipPansByBleedingRatherThanByMixingDown`, `TestTheDistanceModelsAreW3CsOwnNumbers`, `TestMaxClampsTheLinearModelAndSilencesNothingElse`, `TestAnAllZeroFalloffAndConeAreTheW3CDefaults`, `TestAnUndefinedDivisorIsNoAttenuationRatherThanNaN`, `TestTheConeGainIsW3CsThreeBands`, `TestTheTwoDimensionalListenerRotationIsTheSpecsTable`, `TestTheTwoDimensionalConeIsTheSameQuaternionTurnedInPlane`.
- **The loop region:** `TestALoopingPlayheadWrapsAtTheLoopEndAndNotAtTheDuration`, `TestASeekPastTheEndOfALoopingVoiceLandsOnTheLoopStart`, `TestAClipWithNoRegionStillLoopsWhole`, `TestARegionOutsideTheClipLoopsTheWholeClipInstead`.
- **Clips and release:** `TestAfterTheFlushThatRecordsAReleaseNoVoiceIsPlayingThatClip`, `TestAPendingVoiceIsCutByAReleaseAndEndsReleasedRatherThanFailed`, `TestPreloadMakesAClipResidentWithoutPlayingIt`, `TestAskingAboutAClipNeverStartsALoad`, `TestOnlyAReleaseClearsAClipThatFailed`, `TestReleaseAllCutsEveryVoiceAndDestroysEveryClip`, `TestAReleasedClipIsReadAgainWhenItIsNamedAgain`, `TestTheClipSurfaceIsTotal`.
- **The device:** `TestAVoiceAdvancesWhileTheDeviceIsNotReadyAndIsMidClipWhenItArrives`, `TestAVoiceThatOutlivesNoDeviceEndsOnSchedule`, `TestSfxNeverReportsANotReadyDevice`, `TestARecoveryRepinsEveryLiveVoiceToWhereTheWorldIsNow`, `TestAPausedVoiceDoesNotChangeTheDevice`, `TestARecoveryRepinsAPausedVoiceAtTheSampleItSuspendedOn`, `TestARecoveryKeepsALoopingVoiceLooping`. These, and the cases of a prepare still in flight, need a device that is not ready or that arrives, and a prepare that answers `done=false`, which the headless backend never gives. They run over a fake backend written in sfx's own tests, as v1's was, test code rather than harness code, bound with sfxtest's `UseBackend`.
- **v1's nosound, in the headless backend's package tests:** `TestPrepareReportsTheHeaderExactly`, `TestPrepareRefusesAStreamWithNoLength`, `TestPrepareRefusesBytesThatAreNotOggVorbis`, `TestTheDeviceIsReadyAndSilent`, `TestInstallMintsANonZeroIdPerClip`, `TestEverythingElseIsAcceptedAndKeptNowhere`, `TestHeadlessReportsTheLoopRegionFromTheHeaderPass`, `TestHeadlessSaysNothingAboutAnUntaggedClip`, and the drop half of `TestHeadlessDropsAMalformedRegionWholeAndReportsItOncePerClip`, asserting `LoopRegionErr()`.
- **v1's nosound, on sfxtest:** `TestAOneShotUnderHeadlessEndsOnTheFlushItsDurationSaysItShould`, `TestAStoppedVoiceEndsAsStopped`, `TestAClipThatIsNotThereEndsItsVoicesAsFailed`, `TestSfxReportsTheHeadlessDevice`, and the report half of `TestHeadlessDropsAMalformedRegionWholeAndReportsItOncePerClip`, asserting through `Reports()` that sfx reports the drop once for the clip, since the report is sfx's now.
- **Going with what they tested:** `TestEqualIsClipIdentity`, `TestBytesAreIdentifiedByTheirRunAndNotTheirContents` and `TestAReleaseCutsOnClipIdentityRatherThanFieldIdentity`, with `ClipRef`; `TestTheRingNamesTheClipOfAReleasedVoice` and the eleven tests of the MCP provider, with the provider and its ring.

**The allocation gates.**

- **The mixer allocates nothing over a full block,** as v1 required, gated in otosound's package tests.
- **New: a steady flush allocates nothing once warm,** checked with `testing.AllocsPerRun(...) == 0` over sfxtest's `Step`, as canvas gates its frame. A steady flush holds only what is not a first: voices advancing and ending, plays, stops and seeks of clips already resident, options, bus volumes, the listener and the pause changing, positional voices moving, and the endings published. **Outside it, by nature:** a first play or preload of a path, its read, its prepare, and its reports, whose once-keys allocate. v1 never measured its flush, so this is an addition, not a port.
  - `TestASteadyFlushAllocatesNothingOnceWarm` steps a game's beats of all of these after its warm beats, over a table that never fills and one at its cap, every play stealing; `TestTheFlushWhereTheDeviceArrivesAllocatesNothingOnceWarm` counts the one flush where a device arrives and every live voice restarts, more starts than any flush before it.
- Under `go test -race` the gates step their beats for the detector and check no count: the race runtime allocates on its own.

**Concurrency under `go test -race`,** each test checked by breaking what it
guards, run in WSL on Windows. They replace v1's `-count=10`, run for want of a
race detector:

- sfx's recorders against the flush: recording tasks writing the queue while a flush drains it, none ordered before it, losing no operation (`TestRecordersBesideTheFlushLoseNoOperation`, failing with the queue taken out of the flush's locks), and tasks reading the views beside them, never seeing half a flush (`TestReadersOfTheViewsNeverSeeHalfAFlush`, failing with the flush holding `LiveVoices` under a read lock);
- otosound's handoff ring between the flush and its device side, its read-ahead goroutines, its prepare goroutine drained through `TakePrepared`, and its logging from its own goroutine: otosound's design's.

The PulseAudio data race jfreymuth/pulse#53, inside the `Err` that otosound polls,
shows only against a real PulseAudio server, which no test opens; it is recorded
as known.

**No conformance suite.** v2 lifts v1's architectural ban on a shared suite, but
v1's reason on the merits stands: the obligations are properties of the code,
not of observable behaviour, and samples never cross the socket, so a black-box
suite would certify what it can see while what matters went unchecked. That is
worse than no suite, because it would read like one. Each backend proves its
own obligations in its own package.

## Out of Scope

- **Ruled out by the map** ([sfx: v1's sound, otosound and nosound carried onto the v2 kernel](https://github.com/dvoyni/cog/issues/945)):
  - **v1's jssound,** the Web Audio backend. v2 has no web build yet, so it stays with v1 until v2 targets the web. The arithmetic stays W3C's so that it, or a `PannerNode` backend, agrees by construction when it comes.
  - **v1's ecsaudio,** the ECS face. It would be the first ECS face in v2 and would set the convention for every other, so it is an effort of its own. sfx's queue and `VoiceOptions` stay shaped for it.
  - **v1's MCP provider,** its capability listing what plays, its endings ring and its command. v2 has no mcp plugin to contribute it to.
  - **Moving to oto v3.6.0** once it ships, and **measuring otosound's 10 ms default buffer on macOS and Linux**: otosound's.
- **Ruled out by [what pauses sound](https://github.com/dvoyni/cog/issues/950):** a pause on focus or minimizing, which is the game's own; an event for minimizing; a pause per bus.
- **Ruled out by v1, and still out:**
  - **HRTF and binaural spatialization.** Nothing permissive in the Go ecosystem provides it, and it would mean a convolver plus an HRIR dataset. The queue speaks in position and listener, so a later effort would not redraw what a game says, but it would redraw the seam, which receives only a gain matrix. The fold behind is the honest price of that.
  - **Filters and reverb.** Low-pass occlusion and a reverb are the named candidates, and buses are where they would attach. The filtering would run in the backend, with sfx computing only its parameters.
  - **An arbitrary DSP or effects node graph.** A general graph ossifies a contract around machinery nobody has asked for.
  - **Doppler, and a velocity in the model.** Nothing is foreclosed: `VoiceOptions` carries `Pitch` and `VoiceParams` carries `Rate`, so a later velocity is an additive `m.Maybe` feeding a path that already exists, and no backend changes. A speed of sound needs a unit, and that question belongs to Doppler's own effort.
  - **More than one listener.** Split screen is the case, and nothing has asked for it. `SetListener` can later take an optional listener whose absence means the only one, without breaking a caller.
  - **MP3, FLAC, Opus and WAV.** Ogg Vorbis only. MP3's one Go decoder is archived, Opus has no mature pure-Go decoder, and the rest earn nothing sfx needs.
  - **cgo backends shipped by cog,** such as miniaudio, OpenAL Soft, FMOD or Steam Audio. One is allowed as a backend a game composes itself; cog ships none.
  - **A live, growing audio stream:** voice chat, a synthesizer, a video's audio track. A function cannot be a component and is not declarative.
  - **Caller-supplied PCM clips.** Kept out because nothing asks for synthesis, and it would put a sample rate back into the request types.
  - **An agent making sound.** Input is synthesized because the player is an input, whereas sound is an output, and a tool that injected it would make every later observation describe a world the game did not produce.

## Further Notes

- **Research behind the port,** in `docs/research`:
  - [what v1's sound plugins take from v1's kernel, app, storage and assets, and what v2 offers in their place](research/v1-port.md), whose section 7 lists every passage of v1's spec tied to v1's kernel, each restated here;
  - [oto v3 and our Vorbis forks today](research/oto.md).
- **Provenance.** v1's sound specification was assembled from the eighteen resolved tickets of v1's audio map, [audio: a plugin whose sounds are commanded, positioned, and mixed in pure Go](https://github.com/dvoyni/cog/issues/294), which holds the oto spike's measurements, and from [sound on assetcache](https://github.com/dvoyni/cog/issues/412). Its decoding decisions are v1's [vorbis: cog decodes through its own forks](https://github.com/dvoyni/cog/issues/511). otosound's half is in [otosound's design](../../otosound/docs/design.md): the mixer, the device thread, the handoff ring, streaming, resampling, its `Config` and lifetime, and how it proves the obligations.
- **What is not foreclosed,** each additive:
  - **buses at the seam,** the day filters or reverb land; nothing above the seam moves when they do;
  - **more than two output channels,** which widens `Gains`; a different mixer, not a different config;
  - **a backend-reported voice ending,** if a streamed clip ever turns out to end at a length `Duration()` did not predict;
  - **a loop region in `VoiceOptions`,** for dynamic music, two loops in one file switched at run time: a new `m.Maybe` field and a wider `VoiceStart`, the absent case being exactly today's behaviour;
  - **a `ReleaseUnused()`,** releasing only clips no voice is playing, if a game ever wants the memory without the silence; sfx already knows which clips have voices;
  - **splitting `ReasonStopped`** into voice-scoped and bus-scoped members;
  - **a cap per bus,** if a game ever wants a budget rather than a volume group;
  - **exposing `Voice`'s index and generation,** which the opaque `uint64` keeps re-cuttable;
  - **pooling decoders per clip** in a backend: a pooled decoder opens in 74 µs rather than 460 µs;
  - **the headless backend's cheaper header path:** the identification header gives rate and channels in 42 ns and an end-scan gives the duration in about 28 µs without touching the setup header, at the cost of the backend owning an Ogg page parser. 50 clips in a test is 23 ms today, which is not worth the code yet;
  - **a device-changed event,** if a game turns out to want to act on an arrival rather than poll `Device`;
  - **a shared conformance suite,** if a third-party backend ever appears and something turns out to be checkable through the socket;
  - **azimuth or the gain matrix on the live view,** if a game turns out to want to assert the pan itself;
  - **a Web Audio `PannerNode` backend,** which the arithmetic being W3C's verbatim lets agree by construction.
- **Rejected shapes not already named above:**
  - **one software mixer on every platform,** which v1's [the web backend](https://github.com/dvoyni/cog/issues/300) settled and [sound: a Slot beneath audio](https://github.com/dvoyni/cog/issues/373) reversed: it ships the web delayed and starved when the browser's own audio thread is there, and the browser's main thread cannot be escaped. It is why the seam is shaped for a node-graph backend too;
  - **per-voice narrow setters** (`SetVolume`, `SetPitch`, `SetPosition`, and a per-voice `SetPaused`): five more operation types, and a face reconciling a component would dispatch four times. `Queue.SetPaused` is the pause of every voice, not one of these;
  - **wholesale replacement plus a mandatory voice query,** which makes reading a voice's state mandatory machinery;
  - **buses by hash, or a fixed `SFX`/`Music`/`UI` enum,** which forces cog's taxonomy on every game; `Master` is the only name sfx owns;
  - **`Stop` taking either a voice or a bus,** a union in disguise;
  - **a clip hash with a name table,** retired when v1's ecs let a component hold a string;
  - **a baked clip handle** (`BakeClip`/`ReleaseClip`), which is `LoadSound`/`UnloadSound` and which requirement 2 forbids;
  - **`Position2D m.Maybe[m.Vec2]`** beside the 3D field: two position fields that can disagree, and the second model requirement 3 forbids;
  - **a world-scale setting, and a `SetDefaultFalloff` verb:** the first would make sfx learn a unit, and the second hides state from each play;
  - **oldest-first stealing on its own,** which steals the music for a footstep, and **priority as the only key,** which makes a game rank every sound it owns before it can play two;
  - **the newest always winning** within a band: a crowd of distant sounds would cut off the loud one beside you, and "quietest" and "furthest away" would stop being one policy ([#502](https://github.com/dvoyni/cog/issues/502) §2);
  - **resuming a stolen voice when a slot frees,** either way ([#503](https://github.com/dvoyni/cog/issues/503) §3): anything short is over before a slot frees, suspending the voice keeps its entry alive and breaks the closure property, and replaying it as a new voice is the game's retry moved into sfx as a heuristic;
  - **an error from sfx for a device that is not ready:** not-ready is normal for some backends, and only the backend knows which kind it is;
  - **the voice keeping its samples past a release,** which needs a count in the layer kept free of lifetime rules and makes `Release` mean two things on two backends;
  - **a queue-next verb on a voice,** for gapless chaining: a second scheduling mechanism beside `Play` for a case nothing has raised;
  - **a tier enum** (`Tight`/`Relaxed`) **in place of a reported latency,** which forces the design to defend category boundaries it would get wrong;
  - **pausing through oto's `Context.Suspend`,** which meets ebitengine/oto#336 and #292 ([what pauses sound](https://github.com/dvoyni/cog/issues/950)).
- **Names avoid other contexts' terms, and keep v1's where v1 had one:**
  - the kernel's glossary already defines *Batch* as the jobs one publication's handlers schedule, so the seam's v1 `Batch` is `FlushChanges`, the changes one flush hands the backend, decided at the map's handover ([#945](https://github.com/dvoyni/cog/issues/945));
  - mux owns `Pause` and `Resume` for a timeline; sfx's verb is `SetPaused`, after its neighbours;
  - render owns *Inventory* for its table of loaded assets; sfx's is the `ClipTable`;
  - gfx's backend seam was renamed `BackendSocket` to match sfx's ([sfx on the kernel](https://github.com/dvoyni/cog/issues/948) item 3);
  - **the name `sfx` is one v1 rejected for a Port:** in game audio "SFX" means effects as opposed to music, and it is the bus name most games declare. The map named the plugin `sfx` all the same; a game's own bus constant for its effects sits beside the package, as `busEffects` does in the examples here.

### What changed from v1, and why

Every change below is forced by a v2 rule or a missing v1 dependency, or was the owner's choice at the handover, and each
names the map ticket that decided it. Everything else is v1's.

- **v1's sound Slot is the interface plugin sfx,** its Port and Adapters a socket and backends: `Backend` and `BackendPort` are `sfx.BackendSocket`, exclusive and required, so a missing backend panics in `Spawn` where v1 failed with `ErrMissingAdapter`. otosound is a plugin; v1's nosound is the headless backend, an adapter made with `headless.NewBackend(sampleRate)`, which takes what nosound's `Config` held ([#948](https://github.com/dvoyni/cog/issues/948), from [#946](https://github.com/dvoyni/cog/issues/946)). The glossary's *Backend* replaces v1's *Adapter* ([#945](https://github.com/dvoyni/cog/issues/945)).
- **The flush is a task the game schedules,** `Tasks.Flush` given `Delta + Lost`, where v1's ran last on app's `UpdateEvent` through `.Last()`, which v2 has no counterpart for. v1's per-tick guarantees hold per flush, and an operation recorded outside the flush's order lands in the next one. Passing the lost time keeps playheads in step after a stall, where v1's lagged ([#948](https://github.com/dvoyni/cog/issues/948)).
- **The resources are `Queue`, `LiveVoices`, `ClipTable`, `BusVolumes`, `Listener` and `DeviceView`,** v1's `Voices`, `Clips` and `Buses` renamed because a type name is not a bare plural; each a private type aliased in `resources.go` after `Tasks`, with no `Handlers` since sfx subscribes to nothing. `Params` and `ListenerParams` are `VoiceOptions` and `ListenerOptions`, since a struct that is only a call's parameter is named `…Options` ([#948](https://github.com/dvoyni/cog/issues/948)).
- **The flush is one task over eight handles,** the backend kept in a private resource beside the scratch; v1's `lastFlush`, `endingsHeld` and `voicesCmd` go with the MCP provider ([#948](https://github.com/dvoyni/cog/issues/948)).
- **`Config` gains `MaxBuses`,** zero meaning 32, in place of v1's fixed `MaxBuses` constant, by the rule that a count a plugin is tuned by is a field of its `Config`; and a negative field panics where v1 returned `ErrInvalidMaxVoices`, a composition error panicking in v2. v1's `ErrInvalidConfig` goes with config by value ([#948](https://github.com/dvoyni/cog/issues/948)).
- **v1's promise that every live voice resumes on recovery** is replaced by what otosound can recover, since oto's failed context never comes back ([#948](https://github.com/dvoyni/cog/issues/948), from [#947](https://github.com/dvoyni/cog/issues/947)).
- **A clip is a plain path.** `ClipRef`, `ClipWithResource`, `ClipWithBytes` and `Equal` go with libs/assets' Blob, which v2 lacks; bytes a game holds go through a memory mount, at the cost of a copy held by the mount, which v1's Blob did not pay ([#949](https://github.com/dvoyni/cog/issues/949)).
- **`ClipTable` replaces the asset Library** beneath v1's clip table, keeping per path the state, ID, facts, epoch and failure, and reporting under string keys of a prefix and the path that a release forgets, so v2's one-key `ForgetLogOnce` suffices where v1 forgot a family of typed keys ([#949](https://github.com/dvoyni/cog/issues/949)).
- **`Prepare(token any, encoded []byte)` hands the backend the bytes sfx just read,** and sfx keeps none, which drops v1's unstated residency of every clip's encoded bytes in the Library until release ([#949](https://github.com/dvoyni/cog/issues/949)).
- **`ClipTable.Info(path) (ClipInfo, ClipState)` replaces `ClipInfoOf`,** and `State` is `ClipState` throughout, now that there is one package ([#949](https://github.com/dvoyni/cog/issues/949)).
- **The read stays in the flush,** which in v2 is a job of its own on a worker, so the stall holds up only sfx's flush and the jobs waiting on its locks ([#949](https://github.com/dvoyni/cog/issues/949)).
- **The game pauses sound with `Queue.SetPaused`,** since v2 has no engine pause for sfx to follow: v1's subscription to app's `PauseChangeEvent`, and its exported `SuspendOnPauseChange` and `FlushOnUpdate` types, go, and the pause lands in the flush with the other coalesced values. What a pause does is v1's word for word; v1's note on app's `Hold` goes with app ([#950](https://github.com/dvoyni/cog/issues/950)).
- **A voice is restated only when its params differ from those the backend last had of it,** one comparison whatever moved them, where v1 restated every live voice on a pause: a pause therefore leaves a voice the game already paused itself unrestated, and v1's pause test counts updates only of the voices it suspends (the owner's choice at the merge of [#959](https://github.com/dvoyni/cog/issues/959)).
- **Each report's level follows one principle:** a mistake in the game's files, composition or code is an error, and a fact about the machine is a warning, so a missing sound card does not end a game under v2's `StdLogger`, which panics on every error. Clip facts cross the seam back to sfx through `Prepare`'s error and the new `PreparedClip.LoopRegionErr()`, and are reported under the clip's path; backends report only the device, through a Logger kept from `Init`, and the headless backend reports nothing. v1's report subscriptions in both backends go, and `ErrClipUnreadable` carries the path and the filesystem's error ([#952](https://github.com/dvoyni/cog/issues/952)).
- **Tests run on `sfxtest`,** a harness over the headless backend and a `ManualClock`, where v1 wrote probe plugins and seven permanent-filesystem fixtures; v1's cases move by name. `-race` replaces `-count=10`, a steady flush gains an allocation gate v1 never had, and there is still no conformance suite, on v1's merits rather than its architecture tests ([#951](https://github.com/dvoyni/cog/issues/951)).
- **The seam's `Batch` is `FlushChanges`,** the changes one flush hands the backend, and `Emit` takes `changes *FlushChanges`, since the kernel's glossary already defines *Batch* as the jobs one publication's handlers schedule (decided at the map's handover, [#945](https://github.com/dvoyni/cog/issues/945)).
- **`ErrLoopRegionIgnored` is declared once, in sfx,** with a `Path` beside v1's `Start`, `End`, `Frames` and `Err`: a backend answers it from `PreparedClip.LoopRegionErr()` with no path, and sfx fills the path in when it reports it, so the copy each backend declared goes. The Ogg header errors stay in each backend's `err.go`, and sfx wraps them in `ErrClipFailed`, which carries the path (decided at the map's handover, [#945](https://github.com/dvoyni/cog/issues/945)).
- **The headless backend is made by `headless.NewBackend(sampleRate int)`,** answering its concrete type as `platform/headless` and `gfx/recording` do; zero means 48000, a negative rate panics as a composition error, and its `Device.Name` is `"headless"`, where nosound's was `"nosound"` (decided at the map's handover, [#945](https://github.com/dvoyni/cog/issues/945)).
- **`sfxtest` gains `Read`, `UseBackend`, `UsePluginBackend` and `ExpectError`** beside `NewHarness`, `Spawn`, `Mount`, `Record`, `Step`, `Endings` and `Reports`: a test reads the views in a job holding their read locks after a step; binds a fake backend before `Spawn`, for a device not ready, a device arriving and prepares answering `done=false`; or binds none, so a plugin among its own binds the socket, as otosound's kernel tests do; and an error it did not expect fails it, as rendertest's does (decided at the map's handover, [#945](https://github.com/dvoyni/cog/issues/945)).
- **Carried-over test names keep their case, with v1's dropped words replaced:** `Nosound` by `Headless`, `Tick` by `Flush`, `Adapter` by `Backend`, `EnginePause` by `Pause`, `Sound` and `Slot`, where they mean v1's sound Slot, by `Sfx`, and the seam's `Batch` by `FlushChanges`; `Slot` meaning a voice slot stays (decided at the map's handover, [#945](https://github.com/dvoyni/cog/issues/945)).
- **The device's resource is `DeviceView`,** a private `deviceView` aliased in `resources.go` whose `Device()` answers the `Device` the backend built, as the root `AGENTS.md` asks of a resource type, while `Device` stays the exported value type `BackendSocket.Device()` answers (decided during the burst, [#962](https://github.com/dvoyni/cog/issues/962)).
- **An element of the `LiveVoices` view is a `VoiceEntry`,** where v1's was a `VoiceInfo`, since an element of a view is an entry (decided at the map's handover, [#945](https://github.com/dvoyni/cog/issues/945)).
- **`MakeDefaultFalloff()` and `MakeDefaultCone()` replace v1's `DefaultFalloff` and `DefaultCone`,** a constructor answering a value being `make*` (decided at the map's handover, [#945](https://github.com/dvoyni/cog/issues/945)).
- **Every time a game gives or reads is a `time.Duration`:** `Play`'s and `Seek`'s offsets, a `VoiceEntry`'s `Playhead` and `Duration`, and `ClipInfo.Duration`, where v1 gave the game `float32` seconds. The seam's values, `VoiceStart.Offset`, `LoopRegion` and `PreparedClip.Duration()`, seen only by sfx and its backends, stay as v1 had them (decided at the map's handover, [#945](https://github.com/dvoyni/cog/issues/945)).
- **`Device.Latency` is the length of the output buffer in force,** which is all oto offers, where v1 promised the device's own output latency (decided at the map's handover, [#945](https://github.com/dvoyni/cog/issues/945)).
- **jssound, ecsaudio and the MCP provider are out of scope** ([#945](https://github.com/dvoyni/cog/issues/945)).
