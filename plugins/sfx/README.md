# sfx

`github.com/dvoyni/cog/plugins/sfx` is the plugin that gives a game sound:
clips played as voices, grouped on buses, positioned in the world and heard
from one listener, with all of the arithmetic computed in sfx and none of it
below. It is an interface plugin, as [gfx](../gfx) is: a backend binds its
socket and mixes what it is handed, [otosound](../otosound) in a game, or the
headless backend in `sfx/headless` in tests and runs no one hears. It is its
own Go module (Go 1.27), importing [kernel](../../kernel),
[platform](../platform) and [libs/m](../../libs/m), with
[metronome](../metronome) in `sfxtest` and the Vorbis decoder, through cog's
forks, in `headless`. It knows nothing of rendering, cameras or the ECS.

## Features

- **A sound is one line.** A play names its clip by path, with no load call
  and no handle to a clip, and answers its voice's handle at once, usable
  before the flush that applies it.
- **Recorded, then flushed.** Every operation is recorded on one queue by the
  game's tasks and applied at once, in the order recorded, by the flush the
  game schedules on its update beat; none is ever lost, and every operation
  on a voice that has ended does nothing.
- **Voices retained, not re-declared.** Volume, pitch, loops over a clip's
  loop region with no gap at the loop point, seeks and stops, each change
  declicked by the backend; a view of every live voice with its playhead,
  its audibility and its distance; and an event for every ending, with why.
- **Buses.** Groups of voices the game declares under `Master`, each one
  volume, readable for a settings screen, and stopped at once.
- **Positional audio.** W3C Web Audio's distance models, cones and
  equal-power panning, transcribed, one model for 3D and for 2D, around one
  listener the game places.
- **A pause** of every voice, each resuming on the sample it stopped on.
- **Clips by path,** read once and shared, preloaded from a loading screen,
  asked about without loading, released to get memory back; bytes the game
  holds play from a memory mount.
- **A cap with stealing:** the least audible voice loses, ties going to the
  oldest, and a voice put a priority band up survives any crowd.
- **Never stopped by a device.** A device that is missing, late or lost is a
  fact to read, playheads advance whether or not anyone hears them, and a
  game starts with no sound card.
- **Tested without a device.** `sfxtest` runs sfx in a real kernel over the
  headless backend, which reads each clip's length from its headers so its
  voices still end on schedule.
- **Zero allocations per steady flush** once warm, which the tests assert,
  but for a first play or preload of a path, its read, its prepare and its
  reports.

## Shape

- **`Plugin`**, from `NewPlugin()`, is sfx. **`Config`** is its part of the
  kernel's config: `MaxVoices`, zero meaning 64, and `MaxBuses`, zero meaning
  32.
- **`Tasks.Flush`** is the task the game schedules on each update beat, after
  its recorders, given the beat's `Delta + Lost`.
- **`Queue`** is the resource every operation is recorded on: `Play`,
  `SetVoice`, `Seek`, `Stop`, `SetBus`, `StopBus`, `SetListener`,
  `SetPaused`, `Preload`, `Release` and `ReleaseAll`.
- Five read-only **views**, written by the flush alone: **`LiveVoices`** of
  `VoiceEntry`s, **`ClipTable`** answering a clip's `ClipInfo` and
  `ClipState`, **`BusVolumes`**, **`Listener`** and **`DeviceView`**
  answering the `Device`.
- **`Voice`** is a voice's handle, `NoVoice` none. **`VoiceOptions`** and
  **`ListenerOptions`** are how a voice starts or what it becomes and what
  the listener becomes, fields of `m.Maybe`. **`Bus`** is the game's own
  constant, `Master` sfx's. **`Falloff`**, **`DistanceModel`** and **`Cone`**
  are W3C's parameters, `MakeDefaultFalloff()` and `MakeDefaultCone()` their
  defaults.
- **`VoiceEndedEvent`** is every ending, its **`Reason`** why.
- **`ErrClipUnreadable`**, **`ErrClipFailed`**, **`ErrLoopRegionIgnored`**
  and **`ErrClipNotPrepared`** are what sfx logs about a clip, once, under its
  path.
- **`BackendSocket`** is the seam, with `FlushChanges`, `VoiceStart`,
  `VoiceUpdate`, `VoiceParams`, `VoiceSlot`, `PreparedClip`, `Prepared`,
  `LoopRegion` and `ClipID`, which only a backend sees.
- **`headless.NewBackend(sampleRate)`**, in the `headless` subpackage, is the
  backend that plays nothing, an adapter a plugin binds.
- **`loopregion.Read`**, in the `loopregion` subpackage, reads a clip's loop
  tags into a `LoopRegion`, for every backend to run one arithmetic.
- **`sfxtest.Harness`**, in the `sfxtest` subpackage, runs sfx in a real
  kernel over the headless backend, for tests.

## Kernel surface

- **Resources it installs:** `Tasks` (`Flush`), read in a later plugin's
  `Init`, for the game to schedule; `Queue`, written under
  `kernel.Write[sfx.Queue]` by every recorder; `LiveVoices`, `ClipTable`,
  `BusVolumes`, `Listener` and `DeviceView`, read under `kernel.Read` and
  written by sfx's flush alone; and a private flush state, the bound backend
  and the flush's scratch.
- **Resources it reads from other plugins:** `platform.FS`, for read, in the
  flush, as a clip's bytes are read at the first play or preload of its path.
- **Tasks it offers:** `Tasks.Flush`, which sfx schedules nowhere.
- **Events it publishes:** `VoiceEndedEvent`, one per ending, from inside the
  flush.
- **Events it subscribes to:** none, so it has no `Handlers` resource.
- **Sockets it declares:** `BackendSocket`, exclusive and not optional, so a
  kernel no backend binds panics in `Spawn`: bound by otosound, or by a
  plugin binding `headless.NewBackend`.
- **Sockets it binds:** none.

sfx is listed after platform, and a plugin that records into sfx or
schedules its flush is listed after sfx, since its `Init` reads `sfx.Tasks`.
otosound is listed beside sfx, in either order; the plugin binding the
headless backend may be listed anywhere.

See [docs/README.md](docs/README.md) for the guide with examples,
[docs/design.md](docs/design.md) for the design and why it is this way, and
[CONTEXT.md](CONTEXT.md) for its words.
