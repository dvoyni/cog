# otosound: design

The design of `github.com/dvoyni/cog/plugins/otosound`, the backend that
implements [sfx](../../sfx/docs/design.md)'s socket on desktop: a software
mixer of our own over `ebitengine/oto/v3`, in pure Go, decoding Ogg Vorbis
through our forks of `jfreymuth/oggvorbis` and `jfreymuth/vorbis`. It is v1's
otosound carried over whole and reshaped onto the v2 kernel, mapped in
[sfx: v1's sound, otosound and nosound carried onto the v2 kernel](https://github.com/dvoyni/cog/issues/945);
each section links the tickets that hold its decisions, v1's own among them.
The vocabulary is sfx's, in [`CONTEXT.md`](../../sfx/CONTEXT.md).

sfx's design owns the seam: the socket, `FlushChanges`, the prepared clip, and
the numbered list of what a backend owes. This document is how otosound meets
each item of that list, and how each is proved. Everything in it is invisible
through the socket: the residency tiers, the ring that carries a flush's changes
to the mixer, the per-block declick ramp, the offline resampler and the
read-ahead goroutines are properties of code rather than of anything a game
observes, and no test above the seam can see any of them.

Status: designed; nothing is built. v1's otosound, which this ports, was built
and tested on v1's kernel.

## Problem Statement

sfx computes everything a game's sound is: which voices play, their playheads,
each voice's 2×2 gain matrix from its volume, its bus, its falloff and cone and
the listener, its rate, whether it is paused, and when it ends. It hands a
backend one `FlushChanges` of starts, updates, stops and destroys per flush, and
asks it to prepare each clip from its encoded bytes. Something must turn that
into the samples a sound card consumes, on Windows, macOS and Linux:

- on a thread the sound card drives, outside the kernel, that may never wait,
  lock, allocate or decode, since a block it fills late is a click and one it
  fails to fill is silence;
- without losing an operation a flush handed over, even when that thread has
  not run for several flushes;
- decoding Ogg Vorbis, holding short clips decoded and streaming long ones,
  each converted to the device's rate without aliasing;
- without cgo, so every desktop target cross-compiles;
- without stopping a game from starting on a machine with no sound card, and
  without ever telling a game that has already heard sound that the machine
  has none.

v1's otosound did all of this on v1's kernel. v2's kernel composes it
differently: a backend is a plugin binding a socket, its config a pointer to
plain values with its tuning constants among them, its reports go through a
Logger it keeps, its device closes after every job has finished, and the oto
context it opens is shared by kernels that come and go. And v1's promise that a
lost device recovers is wider than oto allows: an oto context that has failed
stays failed for the process.

## Solution

otosound is a plugin, `otosound.NewPlugin()`, that binds `sfx.BackendSocket`.
It is v1's otosound with v2's composition:

- a **mixer** of our own is the `io.Reader` an oto player pulls, one read a
  block: it applies every flush's changes published to it, reads each live
  voice's frames, interpolates them for the voice's rate, multiplies them by its
  gain matrix, ramping from the last block's, and sums them;
- a **handoff ring**, preallocated, carries each flush's changes to it,
  published with one atomic store and drained whole at a block boundary; a full
  ring merges into the staging changes, never blocking and never dropping;
- **the mixer never frees**: a destroy travels inside the flush's changes, and
  the clip is let go on the flush side once the mixer has applied it;
- **preparing** runs on a goroutine: a clip whose decoded size fits under
  `Config.DecodedClipLimit` is decoded once and converted to the device rate
  with a windowed sinc; a longer one keeps its bytes and **streams**, each voice
  on it given a decoder and a read-ahead ring that a goroutine of its own fills;
- the device is **opened at `Voices(n)`**, which sfx calls from its `Start`,
  never blocking or failing the game, and **closed in `Destroy`**; on Windows a
  machine with no output device is asked about before the open; a device that
  never opened is a warning, logged once; a lost one is never reported, and the
  open is retried every `Config.DeviceRetry`, recovering what oto allows;
- the **oto context is per process** and never closed: each kernel takes its
  own player on it, and the first kernel's rate and buffer size hold for every
  kernel after.

The design is bound by three requirements, in this order:

1. **The device thread never stalls.** It may not allocate, lock, free, call
   into sfx or the kernel, or decode. A callback that waited on the scheduler
   is audible as a click, and a buffer it failed to fill is audible as silence.
2. **No cgo.** oto and oggvorbis are both pure Go, which is what keeps
   cross-compilation free.
3. **No flush is lost.** A flush's changes are a delta, not a frame, so the
   handoff must be lossless and ordered even when the device thread has not run
   for several flushes.

Two properties fall out. **The mixer never frees**, so a destroy travels in the
flush's changes and the memory comes back on the flush side behind an atomic
counter. And **the expensive work runs in `Prepare`**, on a goroutine, so the
device thread only ever copies, multiplies and interpolates.

## User Stories

The actors are a **game developer** composing a game with sound, a **player**
hearing it, a **test author** testing otosound, and a **maintainer** moving
otosound to a new oto or decoder.

### Hearing a game

1. As a player, I want no click, crackle or dropout from the mix, so that sound never tells me the game stalled.
2. As a game developer, I want every play a flush hands over to sound, even when several flushes land between two blocks, so that no sound is lost with nothing reporting it.
3. As a game developer, I want one flush's operations to become audible together, so that a play and a volume change recorded in one flush are heard as one sound.
4. As a player, I want a voice's volume, pan and position to move without zipper noise, so that a source passing by sounds smooth.
5. As a game developer, I want a stopped voice to fade over a block rather than cut, so that stopping a sound never clicks.
6. As a game developer, I want a voice to start at its full gain, so that a one-shot's attack is heard whole.
7. As a game developer, I want a paused voice to resume on the sample it stopped on, so that music comes back where it was.
8. As a game developer, I want a voice's rate to change its pitch cheaply, so that engine sounds and footsteps vary without a filter per voice.

### Clips

9. As a game developer, I want short clips decoded once and shared by every voice on them, so that playing a footstep costs an index.
10. As a game developer, I want long clips streamed, so that music doesn't cost twenty times its size in memory.
11. As a game developer, I want a clip at any rate converted without aliasing, so that a 96 kHz source doesn't fold its top octave into what I hear.
12. As a game developer, I want a streamed clip to sound exactly as it would held decoded, so that a clip sounds no different for being long.
13. As a game developer, I want a looping voice to repeat with no gap and no repeated or dropped frame, between the loop points my clip's tags name, so that an intro runs into a loop seamlessly.
14. As a game developer, I want a clip that isn't Ogg Vorbis, has no length or no format, or names a loop region it can't have, reported once under its path, so that I find the broken file.
15. As a game developer, I want to release a clip while it streams, so that a level's music goes with the level without a crash.

### The device

16. As a player, I want a game to start and run on a machine with no sound card, so that sound never stops me playing.
17. As a game developer, I want a device that could never be opened said once, as a warning, so that I know why a run is silent without the default logger ending it.
18. As a game developer, I want a device that was lost never reported, and readiness read from sfx's `Device`, so that a game that has heard sound is never told the machine has no sound card.
19. As a game developer on Windows, I want a machine with no output device reported rather than answered as ready, so that the one fact otosound promises to say is said.
20. As a game developer on Windows, I want a device plugged in after the game started to be heard, so that a late headset works.
21. As a game developer, I want the design to state what a lost device can and cannot come back from, so that I don't expect a recovery oto cannot give.

### Several kernels

22. As a game developer running several kernels in one process, I want each kernel's sound audible, so that a second simulation is heard.
23. As a game developer, I want a later kernel's ignored device settings reported, and its `Device` to answer the values in force, so that I know which rate I got.

### Configuring it

24. As a game developer, I want otosound's settings to be a plain `Config` whose zero values are stated defaults, its tuning counts among them, so that I tune the buffer, the residency limit and the read-ahead without code.
25. As a game developer, I want a setting out of its range, such as a negative buffer size, refused at once, so that a typo is named at composition.

### Testing it

26. As a test author, I want every obligation otosound owes proved without a sound card, so that the proofs run in CI.
27. As a test author, I want the device thread's allocations measured at zero over a full block, so that a change that allocates there fails at once.
28. As a test author, I want every piece of state shared between the flush and otosound's goroutines tested under the race detector, each test seen failing with its guard removed, so that a broken handoff is caught.
29. As a maintainer, I want oto's version, the forks' pins and oto's known issues stated, so that a bump checks exactly what otosound leans on.

## Implementation Decisions

### The module and the plugin

([sfx on the kernel: its socket, resources, Tasks and events, and what paces its flush](https://github.com/dvoyni/cog/issues/948), [sfx research: oto v3 and our Vorbis forks today](https://github.com/dvoyni/cog/issues/947))

- **A module of its own,** `github.com/dvoyni/cog/plugins/otosound`, in `go.work`, importing kernel, sfx, `ebitengine/oto/v3` at v3.5.1, `jfreymuth/oggvorbis` through our forks (see *oto and the Vorbis forks*), and `golang.org/x/sys/windows` for the Windows probe.
- **Desktop only.** Its files carry `//go:build !js`, so a web build composing otosound fails to compile rather than quietly ship a mixer that starves. Go's wasm target is single-threaded, so the mixer would compete with frame work, and oto requests each chunk by `postMessage` to the main thread and fills silence when the reply is late: v1's spike measured 1.880 s of audio produced for a 4.000 s window at a 20 ms buffer, on an idle page. v2 builds for no browser yet.
- **`otosound.PluginName`** is `"otosound"`. It is also the name its `Device` reports, so a game or a test reading sfx's `Device` learns which backend it got, and the `ApplicationName` it gives oto.
- **Its shape:**

  | Type | Kind | What it is |
  |---|---|---|
  | `Plugin` | plugin, `otosound.NewPlugin()` | Implements `kernel.Plugin` alone, and holds the state the backend works on: the handoff ring, the mixer and its voice table, the clip table, each slot's read-ahead, the releases waiting on the mixer, the finished prepares, the `Device` as it is now, and the Logger kept from `Init`. Its `Init` binds `sfx.BackendSocket` to a backend over it. |
  | `backend` | `sfx.BackendSocket`, private, in a file of its own | `backend{plugin: p}`: `Voices`, `Emit`, `Device`, `Prepare`, `TakePrepared` and `Install`, the flush side of the seam. |
  | `Config` | config, `*otosound.Config` | `SampleRate`, `BufferSize`, `DecodedClipLimit`, `RingDepth`, `ReadAhead`, `RefillWait` and `DeviceRetry`. |
  | errors | `err.go` | `ErrDeviceUnavailable`, `ErrDeviceConfigIgnored`, `ErrNotOggVorbis`, `ErrNoStreamLength` and `ErrNoStreamFormat`, v1's types; a dropped loop region is sfx's `ErrLoopRegionIgnored`. |

- **The device is reached through a private interface**, as v1 reached it: ask whether the machine has an output device, take or create the context, start a player over the mixer, poll for an error, and close the player. `NewPlugin` sets it to oto; the package's own tests set a fake device in its place (see *Testing Decisions*). No `Config` reaches it.
- **Kernel surface:**
  - resources: it installs none and reads none;
  - events: it publishes none and subscribes to none, so it has no `Handlers` resource;
  - tasks: it offers none, so it has no `Tasks` resource;
  - sockets: it binds `sfx.BackendSocket`, exclusive and not optional, so a kernel listing sfx and otosound has its backend; it declares none.
- **Dependencies: none.** Binding a socket is no dependency, since bindings resolve after every `Init`, and otosound's `Init` reads no other plugin's resource. v1's otosound named none either.
- **Nothing is pinned to the main thread.** No oto call otosound makes has to come from it: oto runs its own threads, darwin building its AudioQueue on a goroutine that locks an OS thread and WASAPI calling COM on a thread of its own, and otosound's Windows probe runs its COM calls on a thread of its own too.

### Config

([sfx on the kernel](https://github.com/dvoyni/cog/issues/948))

```go
// Config is otosound's part of the kernel's config, under PluginName. Its
// zero value is the default: a zero field takes the default its comment names.
type Config struct {
	// SampleRate is the rate the mixer produces and the device consumes. A
	// clip is converted to it once, in Prepare, so the device thread never
	// resamples a base rate. Zero means 48000.
	SampleRate int
	// BufferSize is how much audio the device buffers, and so how long a block
	// the mixer produces and how long the declick ramp is. Zero means 10 ms,
	// which v1's spike ran for 12 s on Windows with no underrun.
	BufferSize time.Duration
	// DecodedClipLimit is the largest decoded size, in bytes, a clip may have
	// and still be decoded once at load; a larger clip streams, with a decoder
	// and a read-ahead ring per voice.
	//   0  means 512 KiB, the default
	//  -1  means no limit: always decode
	//  -2  means always stream: never decode at load
	//   >0 is the limit in bytes
	DecodedClipLimit int
	// RingDepth is how many flushes' published changes the handoff ring
	// between the flush and the mixer holds. Zero means 8.
	RingDepth int
	// ReadAhead is how much audio a streamed voice's read-ahead ring holds.
	// Zero means 200 ms.
	ReadAhead time.Duration
	// RefillWait is how long a streamed voice's read-ahead sleeps when its
	// ring is full. Zero means 5 ms.
	RefillWait time.Duration
	// DeviceRetry paces both the retry of a device that could not be taken
	// and the poll of an open device for loss. Zero means 1 s.
	DeviceRetry time.Duration
}
```

- **`Init` reads it with `kernel.PluginConfigOrDefault[Config](config)`**, so a config of any other type, a `Config` by value included, panics there, and otosound never sees nil. It never writes the caller's `Config`: a default it fills in goes in a value of its own.
- **A value v1 refused panics,** as a composition error naming the field and its value, where v1 returned an `ErrInvalid…` error: a negative `SampleRate`, `BufferSize`, `RingDepth`, `ReadAhead`, `RefillWait` or `DeviceRetry`, and a `DecodedClipLimit` below -2, which names no tier. `ErrInvalidConfig` and the `ErrInvalid…` types go.
- **The last four were v1's constants,** each "a constant until something asks otherwise", and are fields because a count or size otosound's behaviour is tuned by is a field of its `Config`, its zero a stated default:
  - **`RingDepth`'s 8** is about 130 ms of flushes at 60 Hz: deep enough that an ordinary scheduling hiccup on either side is invisible, shallow enough that the whole ring is `RingDepth × (3 × MaxVoices)` fixed-capacity entries, made once at `Voices(n)`;
  - **`ReadAhead`'s 200 ms** is, at 48 kHz stereo, a 128 KB ring per streamed voice, its frames rounded up to a power of two, made once at the voice's start;
  - **`RefillWait`'s 5 ms** is a fraction of the read-ahead, so a ring just drained is refilled long before it runs dry;
  - **`DeviceRetry`'s 1 s** paces both the retry of a device that could not be taken and the poll of an open device for loss, as v1's constant did. A test sets it to milliseconds to run a loss and a recovery quickly, where v1 needed a field no config reached.
- **Channel count is not a knob.** The output is stereo because panning is: a mono output would make constant-power panning meaningless, and a surround output is a different mixer rather than a different config.
- **`SampleRate` and `BufferSize` belong to the oto context,** which is per process, so only the first kernel's are in force (see *Several kernels in one process*). The other five are each kernel's own.
- **One field with sentinels**, not a second `NoDecodedClips bool` beside the limit: two fields can disagree, and one cannot.

### Lifetime

([sfx on the kernel](https://github.com/dvoyni/cog/issues/948), [sfx: how its reports reach the kernel's logger without ending a game](https://github.com/dvoyni/cog/issues/952))

- **`Init`** reads the config, keeps the kernel's Logger from `Configurator.Logger()`, makes the backend's state, its `Device` not ready and named `"otosound"`, and binds `sfx.BackendSocket` to `backend{plugin: p}`. It opens nothing, so it never fails on a device.
- **`Voices(n)`**, which sfx calls once from its `Start`, before any `Emit`, sizes everything the mixer and the handoff need, once: the handoff ring's `RingDepth` slots and its staging changes, the voice table of `n` slots, and the table of each slot's read-ahead. Only then does it open the device (see *The device*). Sizing first is deliberate: the mixer must exist complete before anything can pull a block out of it. An `n` of zero or less is taken as one, as v1 took it.
- **`Start` and `Stop` do nothing.**
- **`Destroy`** halts every read-ahead, stops the device goroutine and waits for it, and closes this kernel's player with oto's `PauseAndStopReading`, so a destroyed kernel leaves nothing of otosound's running.
  - It is `Destroy` and not `Stop` because v2 finishes every job of the kernel between the two. A flush still in flight after `Stop` would emit into a closed player; none runs once `Destroy` begins.
  - A read-ahead ends with its voice, and a kernel being destroyed ends every voice there is, whether or not sfx got as far as emitting the stops.
  - The device goroutine logs through the kernel's Logger, and `Destroy` waits for it, so nothing logs after the kernel is gone.
- **The oto context is never closed.** oto has no `Context.Close` ([ebitengine/oto#149](https://github.com/ebitengine/oto/issues/149), open since 2021) and cannot make a second context in a process, so a context closed with one kernel could never be had by the next. A later kernel's otosound takes a player on the same context (see *Several kernels in one process*).

### oto, as measured

([sfx research: oto v3 and our Vorbis forks today](https://github.com/dvoyni/cog/issues/947); v1's spike on [audio: a plugin whose sounds are commanded, positioned, and mixed in pure Go](https://github.com/dvoyni/cog/issues/294))

The facts the rest of this design is built on, measured rather than assumed. v1's spike ran against oto v3.5.0 on Windows (WASAPI) on 2026-09-12:

| | |
|---|---|
| Builds at `CGO_ENABLED=0` | `windows/amd64`, `darwin/arm64`, `linux/amd64`, `js/wasm` |
| Desktop latency | **10 ms with zero underruns** over 12 s: 1200 reads, exactly 12.000 s produced |
| The pull path allocates per unit time, not per read | 41 reads and 400 reads in a fixed 4 s window both made about 458 allocations, about 114 a second |
| Context open | about 15 ms |
| Contexts per process | **one**, but a context makes several players |

The re-check against v3.5.1 and oto's unreleased `main`, on Windows 11 (WASAPI) and Go 1.27.4, at 48 kHz stereo float32 with a 10 ms buffer and a 480-frame player buffer, for 3 s, on 2026-11-27:

| | v3.5.1 | `main` |
|---|---|---|
| Ready after | 22 ms | 23 ms |
| A second `NewContext` | `oto: context is already created` | the same |
| Reads of the source | 301, every one 480 frames | 300, every one 480 frames |
| Audio produced | 3.010 s in 2.997 s | 3.000 s in 2.990 s |
| Worst gap between reads | 11.2 ms | 11.4 ms |

Both build at `CGO_ENABLED=0` for `windows/amd64`, `darwin/arm64` and `linux/amd64`. On macOS oto opens AudioToolbox at run time through purego; on Linux it talks to PulseAudio in pure Go and falls back to ALSA, loading `libasound` at run time through purego.

- **The API is the pull shape otosound wants:** `Context.NewPlayer(io.Reader)` with `FormatFloat32LE`. The mixer is the `io.Reader`, and oto never learns what a voice is.
- **The calls otosound makes** are `oto.NewContext` with its `SampleRate`, two channels, `FormatFloat32LE`, its `BufferSize` and the `ApplicationName`, a wait on the ready channel, `Context.Err`, `Context.NewPlayer`, `Player.SetBufferSize`, `Player.Play`, `Player.Err` and `Player.PauseAndStopReading`. Every one has the same signature in v3.5.0, v3.5.1 and `main`.
- **About 114 allocations a second are oto's own**, not otosound's. They are what the no-allocation obligation is measured against rather than included in: what otosound owes is that its own contribution is zero.
- **The player must stay reachable**, or oto stops it ([ebitengine/oto#293](https://github.com/ebitengine/oto/issues/293), documented by [#315](https://github.com/ebitengine/oto/pull/315)). otosound keeps each kernel's player in its state.
- **Reads are sequential across a context's players** ([ebitengine/oto#322](https://github.com/ebitengine/oto/pull/322)): a blocking `Read` would hold up every other kernel's. The mixer never blocks, so no kernel's holds up another's.

### The device

([The device's lifecycle: not ready, absent, and lost](https://github.com/dvoyni/cog/issues/299), [The Device lifecycle: absent, not ready yet, and lost](https://github.com/dvoyni/cog/issues/485), [sfx on the kernel](https://github.com/dvoyni/cog/issues/948))

**Opening it, and never failing the game:**

- **At `Voices(n)` otosound takes the device,** in one step: it asks whether the machine has an output device (see *Absence is asked about*) and, if it does, takes the process's context or creates it. The step is synchronous and cheap, since creating a context does not wait for the device, and it settles the rate the mixer produces and every clip is converted to. Settling it here is what keeps a clip from ever being prepared against a rate that is not the device's: the only case where the two differ is a later kernel, whose context already exists, so the answer is there at once. If the take fails, the rate the config asked for stands.
- **The device goroutine, one per kernel, does the rest.** When the take failed, it first retries it every `DeviceRetry` (see *Loss, the retry, and what it can recover*). Once it holds a context, it starts the player: it waits on the context's ready channel, checks `Context.Err`, makes a player over the mixer, gives it its buffer, starts it, and stores the `Device` as ready. oto takes the context's buffer as a duration and the player's as bytes, and both are said: the context sizes the device's buffer, and the player sizes the queue the mixer is pulled into, `BufferSize` at the rate in force, eight bytes a frame.
- **A ready `Device`** answers `Ready`, the name `"otosound"`, the rate in force, two channels, and as its `Latency` the length of the context's output buffer in force, which is all oto offers. Until it is ready, it answers not ready, named `"otosound"`.
- **`Device()` is a field read:** one atomic pointer load and a copy, cheap enough for sfx to poll every flush. The device side writes it and the flush side reads it. It changes under its reader with no notice, which is correct, because loss is silent.
- **A device that cannot be opened** is a warning, logged once (see *Reports*), after which otosound behaves exactly as the headless backend does: it accepts everything, plays nothing, and the game runs. It goes on retrying (see *Loss, the retry, and what it can recover*).
- **Readiness gates audibility, not residency.** `Prepare`, `Install` and `Emit` work whether or not a device is ready: the clip table is Go memory either way.
- **Rejected:** otosound falling back to silence without reporting when no device opens. That hides a real failure, which is why the headless backend exists as a deliberate composition instead.

**Absence is asked about; it is not inferred from a successful open:**

- **An open that succeeds is not evidence that a device exists.** On Windows, oto answers a machine with no endpoints by installing an unexported null context: a goroutine that drains the mux in real time forever, whose `Err` reports nil. The open returns no error, the poll on it never will, and the one condition otosound promises to say out loud is the one it cannot see: the game is told `Ready` on a machine that cannot make a sound.
- **So the question is asked of Windows rather than of oto,** the way oto's own fallback chain asks it, with both halves having to say no:
  - `waveOutGetNumDevs` counts zero WinMM output devices, which is what makes `waveOutOpen` on the wave mapper answer `MMSYSERR_BADDEVICEID`;
  - `IMMDeviceEnumerator::GetDefaultAudioEndpoint` for a render endpoint fails with a `FACILITY_WIN32` HRESULT: `E_NOTFOUND`, in the case that matters.

  Either half alone is a guess, and a false *absent* on a machine that can make a sound is a worse answer than the bug it would fix. A probe that cannot be carried out (COM refuses to start, the enumerator cannot be made, the call fails with an HRESULT that is not a Win32 one) reports nothing: *I could not find out* is not *there is no device*. On oto v3.5.1 the probe agrees with oto's rule exactly, since oto turns every `FACILITY_WIN32` failure of that call into its own "not found".
- **The cheap half runs first** and answers on every ordinary machine. The COM half runs on a thread of its own, locked and never unlocked, so the apartment it initializes goes with the thread when it ends: COM is per thread, and the thread `Voices` runs on is not otosound's to change, and may already be in an apartment of another model. It is reached only where WinMM counts no devices, so it costs a thread per retry on a machine with none.
- **The probe runs before the open, and that order is the load-bearing part.** oto's context is created once per process and is never rebuilt, so a context created while nothing exists holds the null context for the life of the process, and a device plugged in a minute later would never be heard however patiently otosound retried. Withholding the open is what leaves the retry something to find. Absence then needs no lifecycle of its own: *no device could be taken* is reported once, retried on the same cadence, and recovered from by the same machinery a refused open has.
- **Once a context exists, the probe answers nothing.** The context was created on a machine that had a device, and a device gone since is a loss; answering it here would give otosound a second opinion on the one thing it promises to stay silent on.
- **macOS and Linux need no probe.** On darwin a failure to build the AudioQueue is joined into the context's error and comes straight out of `Err`, which is what starting the player checks. On Linux oto tries PulseAudio and falls back to ALSA, and when both fail it joins their errors: no server and no card is exactly the case that reaches the join. On both, an open that succeeds means a device, and asking again would only add a way to be wrong.
- **Gap:** darwin has a different silent-but-ready state. An `AudioQueueStart` that fails because the audio session cannot be activated (the app is in the background, another app owns the session, media services are restarting) is retried by oto on a backoff rather than recorded, so the context stays quiet and error-free meanwhile. That is a session unavailable for a while, not a device absent, so it sits on the loss side of otosound's line, where silence is the contract. It is recorded because the line is a judgement and not a measurement, and because a probe here would misreport a backgrounded app as a machine with no sound card.
- **Rejected:**
  - reading oto's null context to tell a silent context from a working one: it is unexported, and reaching it would take `unsafe` against a field layout no version pins;
  - inferring absence from what the null context does, such as how fast it drains or how long a read takes: everything it does is what a working device does, at the same rate, by construction;
  - one half of the Windows probe alone: each names a machine where oto's other driver still starts and the game is audible;
  - reporting it upstream instead of fixing it here: oto substituting silence with no way to tell is a gap in its API worth filing, but a filed issue leaves every game shipping today silent with nothing said.

**Loss, the retry, and what it can recover:**

([sfx on the kernel](https://github.com/dvoyni/cog/issues/948), item 11; [sfx research: oto v3 and our Vorbis forks today](https://github.com/dvoyni/cog/issues/947))

- **Loss is ordinary and never reported.** Headphones come out; the default device changes. `Ready` goes false, and that is the whole of the notice.
- **The device goroutine polls an open device every `DeviceRetry`,** for an error on the context and then on the player. It polls because oto has nothing to push: a device that goes away shows only as an error, and only to whoever asks.
- **On an error, the order is contract:** `Ready` goes false; then the player is stopped; then the ring is detached (see *The mixer never frees*). The player is stopped with oto's `PauseAndStopReading`, not `Pause`: `Pause` keeps reading the source so that playback can resume without a gap, and a source still being read is a device thread that still exists. `PauseAndStopReading` blocks until a read in progress finishes and starts no other, which is the barrier the detach needs. Detaching before the player has stopped would claim nothing is mid-copy while something still might be.
- **The open is then retried every `DeviceRetry`, indefinitely,** until a device is taken or the kernel is destroyed. A retry never moves the rate: the rate is the context's, fixed when it was created, so a retry that succeeds opens at exactly the rate every clip was prepared against.
- **Recovery is a restart of every live voice, and sfx emits it.** sfx notices `Ready` going true on the flush and restates every live voice as a `VoiceStart` at its playhead now, because a start is the only operation carrying a position. To otosound each is an ordinary start: for a resident clip an index; for a streamed one, the read-ahead filling the old ring is halted and a new one opened at the offset. The decoder opens and seeks on the read-ahead's goroutine, with the mixer playing silence for the slot and holding its playhead at the head until the ring primes, so nothing of the clip is lost to the restart. Sixty-four streamed voices restarted together are sixty-four goroutines' work, never work on the thread that has a block to fill (see *Streaming*).
- **A lost device frees releases at once again.** A released clip is held until the mixer's applied counter passes the changes that destroyed it, and a lost device stops that counter for as long as the loss lasts. Detaching the ring puts it back in the state of a ring never pulled from, which means the same thing, that nothing is mid-copy out of anything, and the flush side goes back to freeing at once. Without it every release after a loss is held: a leak rather than a crash, bounded by what the game releases, and unbounded in time.
- **What the retry can recover is what oto allows.** An oto context's error only ever joins: nothing clears it, and the context cannot be made again. A retry after a context has recorded an error takes the same context, and starting a player on it fails while its error stands. So the retry recovers:
  - **a Windows machine with no device at start,** whose open the probe withheld: no context exists, and the retry creates one once a device appears;
  - **a device switch oto rides out by itself,** which on Windows is its own re-acquisition of an endpoint, up to 24 tries over about 10 s; otosound never sees it as a loss.

  Everywhere else, a context that recorded an error means no sound until the process restarts:
  - **Windows,** after about 10 s with no usable device, or on any error oto does not count as transient; on v3.5.1, a poll that finds no default endpoint is not counted transient, so one moment without a default endpoint is enough ([fixed on oto's `main`, #320](https://github.com/ebitengine/oto/pull/320));
  - **Linux on PulseAudio,** a server restart, since oto never reconnects; default-device changes are the server's and never reach oto;
  - **Linux on ALSA,** a write error `snd_pcm_recover` cannot recover;
  - **macOS,** a failed queue rebuild, a temporary failure included on v3.5.1 ([fixed on `main`, #329](https://github.com/ebitengine/oto/pull/329));
  - **any platform whose context's first initialization failed.** On macOS and Linux otosound opens without probing, so a machine that gets its device after the game started is never heard.

  In each, `Ready` goes false and stays false, the retry runs on and finds the same context, and the game plays on in silence, its voices still advancing. This replaces v1's promise that on recovery every live voice resumes. v1 shipped this way, so carrying it over changes nothing that works; what changes is the promise.
- **Not measured:** what oto does on a device switch was read from oto's source, never measured on hardware. v1's run that would settle it stands: on a machine with a real sound card, play a long looping clip, print `Device().Ready` once a second, then, one at a time, unplug the headphones it plays through, switch the default output device in the OS mixer, and sleep and wake the machine. For each: does `Ready` go false within a second or two, and does sound come back within a second or two of the device returning? A run where the audio stops and `Ready` stays true is the failure that would change the mechanism, from a poll to an explicit close and reopen.

### Several kernels in one process

([sfx on the kernel](https://github.com/dvoyni/cog/issues/948), item 10)

v2 runs several kernels under one supervisor, spawned and destroyed on demand: v1's "two Engines in one process", with kernels that also come and go.

- **The context is per process,** kept in package state under a mutex beside the rate and buffer size it was made with, because oto's is: creating a second one is an error. A kernel's otosound shares it and **takes its own player**, so every kernel is audible, and each mixes its own voices.
- **The first composition's `Config` owns the context,** for as long as the process lives, after its own kernel is destroyed too. A later kernel whose `SampleRate` or `BufferSize` differs from the context's, once defaults are filled in, has both ignored: that is reported once, as `ErrDeviceConfigIgnored` carrying the values asked for and those in force (see *Reports*), and its `Device` answers the values in force, never the ones it asked for.
- **Rejected:** a later kernel going silent because it cannot own the context. Which composition ran first can be a race, and silence decided by a race is worse than an audible kernel with a stated compromise.

### Reports

([sfx: how its reports reach the kernel's logger without ending a game](https://github.com/dvoyni/cog/issues/952))

- **One principle sets each report's level.** A mistake in the game's own files, composition or code is an error, `LogErr`, fatal under the default `StdLogger` in development and the application's `LogHandler`'s to judge in a shipped game. A fact about the machine the game runs on is a warning, `LogWarn`, because the game reads it from sfx's `Device` rather than handling it. That keeps "audio never stops a game from starting" under the default logger.
- **otosound reports only facts about the device,** through the Logger it keeps from `Configurator.Logger()` in `Init`. It logs where it learns each fact, in `Voices` or on its device goroutine, since v2's Logger is safe from any goroutine. v1's report subscription, which existed because a backend had no kernel of its own, goes.

  | What otosound learns | How it is logged |
  |---|---|
  | No device could be opened: the open was refused, or the Windows probe says the machine has no output device | a warning, `LogWarnOnce`, under a key of its own, carrying `ErrDeviceUnavailable`, which wraps what the open failed with |
  | A later kernel's `SampleRate` or `BufferSize` is ignored | `LogErrOnce`, under a key of its own, as `ErrDeviceConfigIgnored` |
  | A device that was ready is lost | never |

- **Only the first failure is kept:** the condition stays true every retry after, and is noise at that rate.
- **Once a device has been ready in this kernel, nothing the device goroutine meets is reportable again,** not a poll that errors and not the reopen after it refusing to play. Both are the same device being gone, and a game that has heard something must never be told the machine has no sound card.
- **Everything otosound learns about a clip crosses the seam back to sfx,** which reports it under the clip's path and forgets it on release:
  - a clip it cannot prepare comes back as the prepare's error, `ErrNotOggVorbis`, `ErrNoStreamFormat` or `ErrNoStreamLength`, in `TakePrepared` under the clip's token, which sfx wraps in `ErrClipFailed`, carrying the path;
  - a loop region it dropped comes back on the prepared clip, as `PreparedClip.LoopRegionErr()` answering sfx's `ErrLoopRegionIgnored` with no `Path`, which sfx fills in when it reports it, nil when the region is fine or absent.

  v1's queue of dropped regions, and the reports it made of them that could not name the clip, go.
- **A decoder that fails after the prepare,** in a streamed voice's read-ahead, ends the voice and reports nothing (see *Streaming*).
- **The error values** are v1's, in `err.go`:
  - `ErrDeviceUnavailable{Err}`, a device that could not be opened at all, unwrapping to what failed; a machine with no output device fails with "the machine reports no audio output device at all";
  - `ErrDeviceConfigIgnored{AskedSampleRate, SampleRate, AskedBufferSize, BufferSize}`;
  - `ErrNotOggVorbis{Err}`, bytes whose Ogg Vorbis headers would not parse, or that failed to decode;
  - `ErrNoStreamLength{}`, a stream that decoded to no frames at all. It is an error rather than a clip of no duration on purpose: a zero duration makes the playhead and the duration fiction, and silently removes `ReasonFinished` from every voice that names the clip;
  - `ErrNoStreamFormat{SampleRate, Channels}`, an identification header naming no rate, no channels, or more channels than the 2×2 gain matrix can address.

  A dropped loop region is sfx's `ErrLoopRegionIgnored{Path, Start, End, Frames, Err}`, declared once in sfx, and otosound's own copy goes: otosound fills in the span the tags asked for and the clip's true length in source frames, or the parse failure when a tag carried something that is not a count of frames, and leaves `Path` to sfx. It is not terminal: the clip plays, looping whole.

### The handoff ring

([Crossing the thread boundary: how a tick's writes reach the audio callback](https://github.com/dvoyni/cog/issues/302), [sfx on the kernel](https://github.com/dvoyni/cog/issues/948), item 7)

This is the sharpest part of otosound: the device thread runs outside the kernel, driven by the sound card, holding no lock and permitted to take none, while the flush writes the state it reads.

> **A handoff ring of each flush's changes, preallocated, published with one atomic store and drained whole at a block boundary.**

```
Emit        -> copy into the staging changes -> swap them into the ring's write slot -> atomic store of the write index
block start -> atomic load -> apply every pending slot's changes, in order, each whole -> atomic store of the read index
```

- **Wait-free on both sides.** The flush never waits on the device thread, which is the one thing it must never do, and the mixer never waits on the flush.
- **The synchronization is two monotonic sequence numbers and nothing else.** The write index is stored only by the flush side, after the changes at `write % RingDepth` are complete; the read index only by the mixer, after the changes of every slot below it have been applied whole. The read index is therefore also the applied counter the flush side frees behind.
- **`Emit` copies the `FlushChanges` it is handed,** which is sfx's and valid only for the call, into the staging changes, in the order the seam states: the stops first, each halting the slot's read-ahead; then the starts, each `ClipID` resolved to the clip it names while the clip table is still the flush side's own, and a start on a streamed clip given its read-ahead ring; then the updates; then the destroys. Stops come before starts because sfx stops a slot before it reuses one, and a steal puts both in one flush's changes: recorded the other way round, the stop would reach the mixer after the start and fade out the voice that had just replaced it.
- **otosound's changes are an ordered list of operations,** not the seam's four slices. Order is the point of it: a stop of a slot in one flush and a start of the same slot in the next are two operations whose order decides whether the second voice sounds at all, and four slices applied kind by kind cannot say that once two flushes have been merged into the staging changes.
- **A ring slot's changes hold three operations per voice slot,** a start, an update and a stop, so each is `3 × MaxVoices` entries, made once and never grown in the ordinary case. A publish is a pointer swap rather than a copy: the ring slot released becomes the next staging changes, keeping its capacity, so a warm handoff allocates nothing.
- **Why not a latest-wins buffer,** the way a renderer publishes frames. A frame is a complete description and may be replaced; a flush's changes are a delta, and sfx's contract requires that a play is never lost.

  > **The failing sequence.** Two flushes land between two blocks. Flush N's changes start a voice; flush N+1's update a different one. Latest-wins publishes N+1 over N, and the mixer never sees the start. **The voice never sounds, and nothing reports it.**

**A full ring merges; it never blocks and never drops:**

- A full ring means the device thread has not run for `RingDepth` flushes: a stalled or dying device, whose `Ready` is very likely already false.
- The flush side owns the staging changes, which are not published. When the ring is full, the publish publishes nothing, and the next `Emit` merges into staging instead: starts and stops append, and an update folds into the last start or update standing for its slot. That is the same coalescing sfx performs within a flush, widened over the flushes the merge covers, so it adds no new semantics. Since staging is unpublished, the merge touches nothing the mixer can be reading.
- **Rejected:** dropping the oldest flush's changes, which loses exactly the starts the contract promises to keep; blocking until the mixer drains, which makes a dead device stall the game; a seqlock, since the reader retries, and a retry in a device callback is a spin whose bound is the writer's schedule.

**There is no tearing, only latency:**

- A flush's changes are applied whole at a block start, so its operations become audible together: a play and a voice change carrying a position, in one flush, are indistinguishable from a play that carried the position. The list of tolerable inconsistencies is empty.
- What is real is bounded latency: changes published mid-block are applied at the next block start, **at most one block late, 10 ms at the default**.
- **Several flushes' changes can land in one block.** They are applied in order, not coalesced: two flushes' operations taking effect at one instant is correct, since they were ordered and stay ordered.
- **A play and a stop in one flush** arrive in the same changes, applied in order, so the voice is audible for zero samples. sfx still gives it a handle and an ending.
- **The block boundary is not the flush boundary,** and nothing tries to align them. A seek being block-accurate is the same admission from the other side.

### The mixer never frees

([Preload, Release, ReleaseAll, and asking about a Clip without loading it](https://github.com/dvoyni/cog/issues/484), [sfx on the kernel](https://github.com/dvoyni/cog/issues/948), item 7)

A release is stated on the flush, as a destroy in the flush's changes, and the mixer may be mid-copy out of that clip's samples. The seam has no other way to say it, because `FlushChanges` is the only route with an order.

```
mixer, at the end of each drain: atomic store of "applied up to sequence N"
flush side, each Emit:           atomic load; let go of the clips destroyed below sequence N
```

- **A destroy is an operation inside the flush's changes,** ordered after the stops that precede it, which are what drop the voice table's references. The mixer does nothing with it.
- **`Emit` marks each destroyed clip:** it moves the clip out of the clip table onto the list this `Emit` records, and marking one twice does nothing, so changes naming a clip twice are one release. Each destroy staged is tagged with the sequence its changes go out as, and held until the applied counter passes it; one whose changes have not been applied yet is simply let go a flush or two later.
- **Until a mixer has pulled its first block,** no device thread exists to be mid-copy out of anything, so a release is let go at once. That is what keeps otosound behaving as the headless backend does under a device that never opened, rather than holding every clip a game ever released. A lost device returns to that state when the ring is detached (see *Loss, the retry, and what it can recover*).
- **The lists of releases are cleared, not merely truncated:** a clip pointer left in a slice's spare capacity keeps its samples alive, which is the one thing those lists exist to let go of.
- The mixer never frees and never reads freed memory. The flush never waits. Freeing is the garbage collector's, once the last reference on the flush side goes.
- **Rejected:** a latest-wins snapshot of the mixer's complete state instead of deltas. It would restore losslessness, but the mixer owns each voice's playhead and advances it every frame, so a published complete state would either clobber the playhead or have to carry it, which means the flush reading state the mixer is writing. The delta is what keeps the playhead entirely on one side.

### The mixer

([Crossing the thread boundary](https://github.com/dvoyni/cog/issues/302), [What a sound Backend speaks: the seam beneath the sound Slot](https://github.com/dvoyni/cog/issues/376))

**What it may touch,** stated as a rule because it is the kind of invariant that erodes one convenient call at a time:

> **The mixer may touch:** its own voice table; the handoff ring, through atomics only; the prepared clip data it was given; its read-ahead rings, through their atomics.
>
> **The mixer may not:** allocate, take any lock, free anything, call into sfx or the kernel, or decode.

The allocation rule falls out of it rather than standing alone: the handoff ring, its slots, the staging changes, the voice table, the list of live slots and every read-ahead ring are made once, at `Voices(n)`, at `Prepare` or at a voice's start on the flush side.

**What it does per block.** One `Read` of the player is one block. The mixer first applies every flush's published changes, then gathers the slots that sound this block, fixing each one's target matrix (silence for a stopping or paused voice), so the loop over frames carries no decisions. A paused voice that has already ramped to silence is left out entirely. Then, for each frame, for each gathered slot:

1. Read the frame under the playhead: an index into the clip's shared samples for a resident voice, a copy out of its read-ahead ring for a streamed one.
2. Interpolate it for the voice's rate, if the playhead is between frames.
3. Multiply it by the 2×2 gain matrix, `Gains[source][output]`, ramping from the last block's matrix to this block's target; a mono clip uses row 0 alone.
4. Add it into the frame's output.
5. Advance the playhead by the voice's rate, unless it is paused or is a streamed voice waiting for its ring's first frames: wrapping at the loop region's end for a looping voice, and ending a one-shot at the clip's end.

Each output frame is clamped to [-1, 1] and written as two little-endian float32s: a mix of many voices can exceed unity, and a wrapped sample is far louder than a clipped one. At the end of the block every ramp lands exactly on its target, and a voice that was stopping is freed now that it has reached silence.

**It never decides anything.** It holds no notion of a bus, a priority, a position, the cap or a handle. A stolen voice reaches it as an ordinary stop.

**The voice table** is a fixed array of `MaxVoices` entries, indexed directly by the seam's `VoiceSlot`. sfx guarantees a slot is stopped before it is reused, so no generation is needed here, and otosound relies on that rather than defending against it. A slot holds a pointer to its clip, resolved on the flush side when the start was staged, so the device thread never reads the clip table's map; and, for a streamed voice, its read-ahead ring and never the stream that fills it. Its playhead is a float64 in converted frames, fractional because a rate is, and float64 because a five-minute track at 48 kHz is fourteen million frames, which a float32 cannot count one at a time. A play on a resident clip is a table entry and an entry in the staging changes, both made once, so it allocates nothing on either side; a play on a streamed clip allocates its read-ahead ring and goroutine on the flush side, which is the streamed tier's cost (see *Streaming*).

**How it treats each operation:**

- **A start begins at its target gain,** not ramping up from silence: a one-shot that faded in over a block would lose the transient that is the reason it was played. Its playhead starts at the start's offset in converted frames, an offset past the clip's end starting at its last frame; a streamed voice's starts at zero, since its read-ahead was opened at the offset. Its loop bounds are resolved once, here, so no block touches a conversion from seconds.
- **An update** restates the slot's target, rate and pause, which the next block ramps to. An update that reaches a slot a stop has already claimed is ignored, so a voice sfx has ended is never brought back mid-ramp.
- **A stop** ramps the voice to silence over the block, then frees the slot. A stop that reaches a voice no block has played yet takes it to silence outright, so a play and a stop in one flush are audible for zero samples.
- **A negative rate is taken as zero,** which is a legitimate freeze; sfx sends none, and a negative rate would run a voice backwards off the front of its buffer.
- **The backstop.** sfx computes a one-shot's ending from its clip's duration and its rate and emits a stop, which is declicked. A one-shot whose sample-exact playhead reaches the clip's end first is cut rather than ramped, since the samples to ramp with are what ran out.

### Preparing a clip: two tiers

([Assets: how a path becomes samples, and what streams](https://github.com/dvoyni/cog/issues/298), [The streamed tier, its read-ahead rings, and the offline resampler](https://github.com/dvoyni/cog/issues/482), [sfx: clips by path through platform's FileSystem, their cache, preparing and release](https://github.com/dvoyni/cog/issues/949))

- **`Prepare(token, encoded)` is handed the bytes sfx just read, and from then on they are otosound's;** sfx keeps none. It answers `done` false and spawns a goroutine, which keeps the decode off both the flush and the device thread. The goroutine's completion, the prepared clip or the error, lands under the token in a slice behind a mutex, and `TakePrepared` drains it on the next flush. That mutex is the only lock in the backend, and nothing the mixer runs can reach it.
- **The goroutine** reads over a `bytes.Reader` on the bytes it was handed, because the length comes from the stream's granule positions and needs an `io.Seeker`:
  1. It reads the identification header and the end granule position with `oggvorbis.GetLength`, without the setup header: the frame count, the source's rate and its channels. A header that will not parse is `ErrNotOggVorbis`; no rate, no channels or more than two is `ErrNoStreamFormat`, refused rather than truncated.
  2. It computes the decoded size, `frames × channels × 4`, and compares it with `DecodedClipLimit`.
  3. **Under the limit,** it decodes the whole clip, trims it to its granule end (see *Loop regions*), converts it to the device rate once (see *Resampling*), and hands back a prepared clip holding `[]float32`, interleaved. The encoded bytes are dropped behind it, since nothing will read them again.
  4. **Over the limit, or a frame count of 0,** it hands back a prepared clip holding the encoded bytes, the filter its voices will share when the rates differ, and the way to open decoders against them. A count of 0 has no computable decoded size, so it streams, and it is counted rather than believed: the clip is decoded once and its samples thrown away, so its duration is real. A stream that truly holds no frames is `ErrNoStreamLength`.
- **A failed decode hands sfx nil,** never a typed nil, which would be a non-nil prepared clip holding nothing and would be installed rather than reported.
- **`Install` mints the `ClipID`,** non-zero and never repeating, and keeps the clip in the clip table. It is `Install` and not `Prepare` that mints, so the ID comes into being on sfx's flush, where the destroy that pairs with it is always statable. A prepared clip otosound did not make is refused with an error of its own; sfx installs only what otosound's own `Prepare` produced, so it guards against a future caller.
- **The prepared value is garbage-collectable:** samples, or the bytes, a filter and a function, and nothing that needs an explicit release. The failing sequence it exists for is a clip released while its prepare is in flight: sfx drops the completion with nothing called, so anything native inside it would never be freed.
- **Both tiers answer the same facts,** the source's: duration, channels, sample rate and loop region, and `LoopRegionErr`. The duration is the clip's true length in frames over the source's rate, as the headless backend computes it, so the two backends report one clip identically. The rate is the file's, not the device's: a game asking a clip its rate is asking about the file. sfx cannot tell which tier it got, and neither can a game.
- **Where the bytes live** ([sfx: clips by path through platform's FileSystem](https://github.com/dvoyni/cog/issues/949), items 3 and 7). A resident clip's encoded bytes are freed once it is decoded. A streamed clip's are held by otosound for as long as it holds the clip: until the mixer has passed its destroy and no read-ahead still reads them, after which Go's collector frees them. Bytes a game gave through a memory mount are therefore held once by the mount, until the game removes them, and once by otosound while it keeps them, which for a streamed clip is as long as it plays.
- **Releasing a streamed clip while it plays is safe as built.** sfx stops its voices first, in the same flush's changes, which halts their read-aheads, and the bytes belong to otosound, which keeps them as long as a decoder reads them.
- **Why the tier is chosen here and not in sfx:** choosing it needs duration, channels and rate, which exist only once the headers are parsed, here. A knob in sfx's config would force a two-phase seam and put *streamed or resident* above a seam that speaks only of a clip's identity and bytes.
- **Rejected:** one residency tier. Always decoding cannot hold music (21×); always streaming charges a footstep a decoder for something smaller than the decoder itself.

**The measurements that set the default,** run on 2026-09-19 against upstream `jfreymuth/oggvorbis` on Windows, at `CGO_ENABLED=0`:

| | cost |
|---|---|
| Open a decoder (seekable source) | **460 µs**, 137 KB |
| Open a pooled or reused decoder | **74 µs** |
| Identification and comment headers only | **42 ns** |
| Setup header alone | **431 µs** (54% in the Huffman builder's linear search for an empty slot) |
| Decode one 10 ms block | allocated on every call |
| Decoded PCM against encoded, mono test clip | **51.6×** |
| Decoded PCM against encoded, 5-minute stereo track | **21×** (101 MiB against about 4.8 MB) |

- **Break-even,** where decoded samples are smaller than the decoder streaming them, is **0.78 s mono, 0.39 s stereo**.
- **The 512 KiB default sits deliberately above** the 137 KB break-even: between the two, memory is knowingly spent to make a play free. At 512 KiB that is **2.97 s mono at 44.1 kHz, 1.49 s stereo at 44.1 kHz, 1.37 s stereo at 48 kHz**.
- **Our forks moved two of those rows** ([vorbis: cog decodes through its own forks, with the setup parsed once per clip and nothing allocated per block](https://github.com/dvoyni/cog/issues/511)). Decoders of one encoding share one parsed setup inside the fork, with no API change, so a streamed voice opens and seeks in **96 µs and 75 KB** rather than 470 µs and 475 KB, and only the first voice on an encoding pays the parse ([The setup cache lands in vorbis](https://github.com/dvoyni/cog/issues/518)); and a block decodes without allocating. The table is what the tiers were chosen on, and stays as measured.
- **No pure-Go alternative exists.** Ebitengine wraps the same library, and every other option is cgo.

### Loop regions

([Where a music track's loop repeats from](https://github.com/dvoyni/cog/issues/471), [sfx: how its reports reach the kernel's logger](https://github.com/dvoyni/cog/issues/952))

A loop region is a fact about a clip, declared in its Ogg file's Vorbis comment header. otosound reads it in the same pass over the headers that gives the duration, channels and rate, and answers it on the prepared clip in seconds, absent meaning the whole clip.

| Tag | Meaning |
|---|---|
| `LOOPSTART` | the loop's start, in sample frames |
| `LOOPLENGTH` | the loop's length in sample frames; its end is the start plus the length |
| `LOOPEND` | the loop's end in sample frames, used when `LOOPLENGTH` is absent |

- **The tags are matched case-insensitively,** since a Vorbis comment's field name is case-insensitive by the format's definition and the tools in the wild disagree. They are the convention game-music tooling already writes, so a composer's export is the authoring surface.
- **`LOOPSTART` alone runs to the clip's end,** which is an intro running into a loop that carries on to the end of the file. **`LOOPLENGTH` wins over `LOOPEND`** where a file carries both. **A name repeated takes its first value**, so a re-tagged file plays the way the tool that tagged it last meant.
- **A malformed region is dropped whole, never clamped:** a start below zero, an end at or before the start, an end past the clip's frames, a value that is not an integer, or a comment header that will not parse. The clip then loops whole, and `LoopRegionErr` answers sfx's `ErrLoopRegionIgnored`. A clamped loop sounds like a working loop with the wrong loop point, which is the hardest audio bug to attribute, and clamping would make a tag's meaning depend on the clip it sits in.
- **The granule correction and the loop point are one pass.** A decoded buffer may carry a final packet's padding, so the frame count a decoder hands back is not always the stream's length: the true length is the last page's granule position when it is positive and below the decoded count. A resident clip's samples are trimmed to it; an absent region means looping to the granule end, never to the padded buffer's end; and a `LOOPEND` is in range only against that corrected count. sfx sees neither input: what crosses is a duration and a span, in seconds.
- **A looping resident voice** wraps at the region's bounds in converted frames, resolved once at its start. The frame after the last of the span is its first, which is what makes the loop gapless: no repeated frame at the wrap, and none dropped.
- **A one-shot on a clip with a region** plays the intro, the loop and whatever follows it, and ends at the clip's end: the region says where to repeat between, never where the clip stops.
- **A looping streamed voice** wraps in its read-ahead, in the source's own frames (see *Streaming*).

### Streaming

([The streamed tier, its read-ahead rings, and the offline resampler](https://github.com/dvoyni/cog/issues/482))

> **No clip's bytes are decoded on the thread that fills the device buffer.**

A streamed voice gets a read-ahead goroutine and a read-ahead ring. The device thread only ever copies from rings; the goroutine decodes ahead into them.

- **A read-ahead ring** is a fixed buffer of frames already decoded and already at the device rate, and two atomic counters: the frames the read-ahead has produced, stored by it only once the frames are in the buffer, and the frame the mixer's playhead has passed, stored by the mixer. Both are absolute frame numbers in the stream, which makes the wrap a mask, since the capacity is a power of two, and lets the mixer move on over an underrun without the two sides disagreeing about where they are. A third atomic holds the stream's length once the read-ahead has reached the clip's end, and an unknown length until then. No lock, no allocation, no signal to wait on.
- **The ring is sized once,** from `ReadAhead` at the device rate and the clip's channels, when the flush side stages the voice's start, and is never reallocated. It and the goroutine are the per-voice cost of the streamed tier, which is why the resident tier exists for everything short.
- **The goroutine** opens a decoder over the clip's bytes and seeks to the voice's offset in source frames, then decodes 4,096 source frames at a time, converts them, and pushes them into the ring. When the ring has no room for a chunk, it sleeps `RefillWait`: a sleep rather than a wait on a signal, since what would signal is the device thread, which may not take a lock or touch a channel. The decoder open and the seek happen here, so neither the flush nor the device thread ever pays for them.
- **A wrap at a loop region seeks while filling,** ahead of the wrap, not at it, so the page decode never lands on the wrap's own block. `oggvorbis.Reader.SetPosition` seeks the page before the target and skips the rest, so it is sample-exact; it errors only on a source that cannot seek, and the streamed tier reads bytes in memory. The read-ahead keeps feeding the same conversion across the wrap, so the filter's window reaches across it and the loop is gapless in the resampler as well as in the mixer. How the seek is placed is guidance; what is contract is that the loop is gapless.
- **A one-shot's read-ahead,** at the clip's end, drains what the resampler's window still holds and publishes the length, which ends the voice in the mixer at the sample its clip runs out on. A looping voice never publishes one, which is why it never ends by itself.
- **Read-ahead stops with its voice,** however the voice ended: finished, stolen, released or cut, all of which reach otosound as the same stop. The flush side halts it on the slot's stop, and again on a start that replaces the slot; `Destroy` halts every read-ahead there is. A halt never blocks, and halting twice is halting once. It does not free the ring: the mixer may still be ramping the voice to silence out of frames the ring holds, and the ring goes when the voice table drops it, which is the collector's business and never the device thread's.
- **An underrun plays silence for that slot,** not for the block: one voice that could not keep up must never take the rest of the mix with it. The frame just past the last one held is the edge of the read-ahead, not an underrun, and holds its sample for that frame, which is inaudible where dropping to silence would click.
- **An underrun advances the playhead; the head of a stream does not.** A voice whose ring has not yet had a frame holds its playhead rather than advancing through silence, because the first fill is a clip load, and a clip load starts from its offset with no catch-up. Once the voice has sounded, an underrun advances it: then it is the device's rule, a playhead advances whether or not anyone can hear it, and a stall that held would put the voice behind the world it is scored to for good. Holding the head is also what keeps a streamed one-shot from the backstop: a playhead a few milliseconds behind sfx's reaches the clip's end after the stop that declicks it, never before.
- **A decoder that will not open, or fails mid-clip, ends the voice and reports nothing.** A clip failure is terminal and reported once, at the prepare, where a clip that cannot be read is refused outright. A decoder that fails after that belongs to a clip that parsed, installed and has been playing, and the game has been told everything it is owed. It gets silence and an ending, which is what any voice that ran out gets.
- **What it costs.** v1 priced sixty-four streamed voices, as many as sfx's default `MaxVoices` allows, at about 8.7 MB of decoders and 64 goroutines before the setup cache; each voice also holds its read-ahead ring, 128 KB at the defaults. That worst case is stated, not legislated against: sfx cannot count what the seam hides from it. A recovery restarting sixty-four streamed voices is sixty-four goroutines opening decoders, about 6 ms of work in all with the setup cache and 29 ms before it, none of it on the thread that has 10 ms to fill a buffer.

### Resampling

([The streamed tier, its read-ahead rings, and the offline resampler](https://github.com/dvoyni/cog/issues/482), [Prototype: does hand-rolled panning and attenuation sound right?](https://github.com/dvoyni/cog/issues/304))

> **The expensive resampler runs offline, in `Prepare` and in a read-ahead. The cheap one runs on the device thread.**

- **A clip is converted to the device rate once,** with a Blackman-windowed sinc, lowpassed when decimating, which is what keeps a 96 kHz source from folding everything above 24 kHz into the audible band. `Prepare` is not on the device thread and can afford the good one; the device thread cannot, and does not need to.
- **The device thread only interpolates for a voice's rate,** linearly, between two frames. Linear and Catmull-Rom were both acceptable in listening, and pitch by resampling was judged acceptable by ear in v1's prototype.
- **The filter** keeps 24 zero crossings of the sinc on each side, which is 48 taps at unity, and tabulates 256 fractional delays between two input frames, blending the two either side of an output frame's fraction. Each row is normalized to sum to one, so the filter is unity gain at DC whatever the phase.
- **When decimating, the cutoff sits at 0.9 of the lower Nyquist** rather than on it, and the kernel is stretched to that cutoff rather than truncated at 48 taps. A windowed sinc has a transition band of real width, about 5.5/N of the input rate for a Blackman window over N taps, and a cutoff exactly on the new Nyquist puts half of it above, where whatever survives folds. The margin costs the top tenth of the new band, 21.6 kHz upwards against a 48 kHz device, above what anyone hears. Upsampling keeps the cutoff at the source's own Nyquist and the kernel at 48 taps, since there is nothing above it to fold.
- **Blackman,** not a truncated sinc or Hann: truncating leaves sidelobes at -21 dB, an alias folded 21 dB down and plainly audible, and Blackman's -74 dB stopband is what makes the assertion below true rather than nearly true.

  > **Measured** (2026-09-20): a 30 kHz tone in a 96 kHz source, decimated to 48 kHz, arrives at 18 kHz **106 dB down**, against a passband flat to **0.00 dB**. The same conversion by linear interpolation folds it at **0 dB**, full amplitude: at an exact 2:1 ratio every output frame lands on an input frame and the interpolation never runs.
- **The two tiers are one filter, and it is asserted rather than assumed.** The resampler is incremental: `Prepare` drives it in one call and a read-ahead drives it a chunk at a time, keeping the window's history across chunks and across a loop's wrap, so a streamed clip comes out frame for frame what the same clip held resident would have been. A clip must not sound different for being long enough to stream. A streamed clip keeps its filter, built once for its pair of rates and read-only after, and every voice on it shares it; a resident clip uses one once and drops it with its bytes. The read-ahead reuses the resampler's history buffer, so a streamed voice allocates for it once.
- **Nothing sfx keeps names the device rate.** The prepared clip answers the source's facts, and sfx's clip table keys on the path, so a clip is one clip whatever the device runs at; the rate in force is the context's, which never changes in a process.

### Declicking

([Prototype: does hand-rolled panning and attenuation sound right?](https://github.com/dvoyni/cog/issues/304))

- **sfx emits target values once per flush; otosound ramps to them across its own blocks,** a linear ramp per block from the matrix the last block ended on to the new target. Declicking is otosound's and not sfx's because it is a function of the output block rate, which only the backend knows: a flush at 60 Hz is 16.7 ms apart against a 10 ms block, so anything sfx emitted would be a step by the time it was heard.
- **The ramp runs to (f + 1) / frames** at frame f, so a block ends exactly on its target, not nearly on it, and a stop reaches real silence.
- **The block is one read, and on oto v3.5.1 a read is the player's whole buffer,** since oto's source read always asks for it, so the ramp is `BufferSize` long.

  > **Measured** (v1's prototype): without the ramp, the gain steps **0.078** of amplitude in a single sample at every 10 ms block boundary; with it, **0.00016**, smaller than the ramp's own per-sample motion. A difference of about **480×**, repeating at 100 Hz, which is what a zipper is. A linear ramp between two flush-rate matrices was judged clean for a fast-moving source, and nothing heard argues for anything smoother.
- **A stop is the same mechanism:** ramp to zero over the block, then free the slot. This is why sfx's ending of a voice precedes its silence by a few milliseconds, which sfx states as contract.

### Pause

([sfx: what pauses sound in v2, where v1 suspended on app's pause](https://github.com/dvoyni/cog/issues/950))

- **A pause crosses the seam as `VoiceParams.Paused` updates** for every live voice, which sfx emits when the game records `Queue.SetPaused`. otosound has no other path for it, and never calls oto's `Context.Suspend`.
- **A paused voice ramps to silence over a block and holds its playhead,** so it resumes on the same sample, ramping back up over a block. A paused voice already silent is left out of the block, frozen and inaudible.
- **The device stays open and is fed silence.** Closing it would risk a reopen that fails, and cost the open again.
- **Zeroing a gain does not suspend:** a voice keeps advancing whatever its gain is, so the mixer stops the playhead, not just the sound.
- **oto's open issues on `Suspend` are never reached:** on macOS it leaves up to four AudioQueue buffers queued, so audio from before a seek plays at `Resume` ([ebitengine/oto#336](https://github.com/ebitengine/oto/issues/336)), and on PulseAudio it does not cork the stream, so the sink stays active and draws power ([ebitengine/oto#292](https://github.com/ebitengine/oto/issues/292)).

### What otosound owes, and where it is met

([sfx on the kernel](https://github.com/dvoyni/cog/issues/948), item 8)

sfx's design lists what a backend owes, numbered. otosound meets each so:

| sfx's item | How otosound meets it |
|---|---|
| 1. Nothing decodes on the thread that fills the device buffer | decoding runs on the prepare goroutine and on each streamed voice's read-ahead goroutine; the mixer reads frames already decoded (*Preparing a clip*, *Streaming*) |
| 2. What the device thread may and may not touch | the mixer touches its voice table, the handoff ring through atomics, the prepared clip data and the read-ahead rings through their atomics, and never allocates, locks, frees, decodes or calls into sfx or the kernel (*The mixer*) |
| 3. No base-rate resampling on the device thread | the sinc runs in `Prepare` and the read-ahead; the device thread interpolates linearly for rate alone (*Resampling*) |
| 4. The device rate is not baked into what `Prepare` returns | the prepared clip answers the source's facts, sfx keys its clips by path, and the rate in force never changes in a process (*Resampling*) |
| 5. Every parameter change and every stop is declicked | a linear ramp per block to each target, a stop ramping to zero before its slot is freed (*Declicking*) |
| 6. A looping voice is gapless | the frame after a span's last is its first, and the read-ahead wraps ahead of the mixer through one conversion (*Loop regions*, *Streaming*) |
| 7. A prepared value is garbage-collectable | samples, or bytes, a filter and a function; nothing native (*Preparing a clip*) |
| 8. A slot is stopped before sfx reuses it | relied on: the voice table carries no generation, and `Emit` records a flush's stops before its starts (*The handoff ring*, *The mixer*) |
| 9. `Device()` is a field read | one atomic load and a copy (*The device*) |
| 10. A device that could never be opened is reported once, by the backend, which then behaves as the headless backend does; a lost device is reported never | the first failure is logged once with `LogWarnOnce`, nothing is reported once a device has been ready, and a release is let go at once while no mixer pulls (*The device*, *Reports*, *The mixer never frees*) |

Beside the list, otosound keeps three properties of its own that the seam relies on: no flush's changes are lost (*The handoff ring*), the mixer never frees and never reads freed memory (*The mixer never frees*), and the resampler does not alias (*Resampling*).

### oto and the Vorbis forks

([sfx research: oto v3 and our Vorbis forks today](https://github.com/dvoyni/cog/issues/947), [sfx on the kernel](https://github.com/dvoyni/cog/issues/948), item 12)

- **otosound requires oto v3.5.1,** the current release: v3.5.0, which v1 required, plus one fix, to the unsigned 8-bit conversion, which otosound's float32 output never reaches. The API is v3.5.0's. It requires purego v0.11.0, `jfreymuth/pulse` v0.1.3 and `golang.org/x/sys` v0.47.0.
- **Ogg Vorbis is decoded through our forks,** `dvoyni/vorbis` and `dvoyni/oggvorbis`, as v1 decoded: otosound's `go.mod` requires `jfreymuth/oggvorbis`, and the repository's `go.work` replaces both `jfreymuth` paths with the forks, once for every module in the workspace, at v1's pins on their `cog` branches, as the headless backend's ticket set it up:

  ```
  replace github.com/jfreymuth/vorbis => github.com/dvoyni/vorbis v1.0.3-0.20260921150454-ab3e6ce988a8
  replace github.com/jfreymuth/oggvorbis => github.com/dvoyni/oggvorbis v1.0.6-0.20260921150857-546236badfd4
  ```

  - The forks add the benchmarks, a Huffman builder that assigns codewords as it goes, allocation-free packet decoding, window and IMDCT tables built once per block size, the shared setup cache, pages read into buffers the reader keeps, and a packet table sized once. They add no public API: otosound calls only upstream's `NewReader`, `Reader.Read`, `SetPosition`, `SampleRate`, `Channels`, `Length`, `GetCommentHeader`, `GetLength` and `ReadAll`.
  - They build and decode on Go 1.27, which `go.work` names, at `CGO_ENABLED=0` for the three targets.
  - **The forks are permanent.** Upstream declined to review AI-generated code, archived `jfreymuth/vorbis`, moved the decoder into `jfreymuth/oggvorbis/vorbis` in v1.0.6, and its README refuses LLM-generated contributions; our pull requests are closed.
  - **The pair works only while the oggvorbis fork stays on its pre-1.0.6 base.** Rebased onto v1.0.6, the fork would decode with its own `vorbis/` subpackage, the vorbis `replace` would then do nothing, with no error, and the decoder work would have to move into that subpackage. A consumer that requires upstream v1.0.6 without our oggvorbis `replace` gets none of the work. The `replace` lines name no version on the left, so they also cover a build where something else requires oggvorbis v1.0.6.
  - **Upstream's one decoding change since the fork** is a floor 0 fix, an odd order starting at `1 / 4`, an integer zero, now `1. / 4`. Our fork still has the old line. No encoder since Xiph's own beta 4 emits floor 0.
  - **Two modules in one workspace with differing `replace` lines for the forks fail to build,** with `conflicting replacements for github.com/jfreymuth/oggvorbis`, which is why the pair is held once, in `go.work`, and in no module's `go.mod`. A module outside the repository repeats it, because Go ignores `replace` in a dependency ([Fork vorbis and oggvorbis to dvoyni and replace them in cog and its three consumers](https://github.com/dvoyni/cog/issues/512)).

### oto's open issues and known risks

([sfx research: oto v3 and our Vorbis forks today](https://github.com/dvoyni/cog/issues/947), [sfx: how sfx, otosound and the headless backend are tested on v2](https://github.com/dvoyni/cog/issues/951))

No open issue breaks otosound as it uses oto on macOS or Linux. These are known, and stated so a bump checks them:

| Issue | Platform | What a game sees | otosound |
|---|---|---|---|
| [jfreymuth/pulse#53](https://github.com/jfreymuth/pulse/issues/53) | Linux, PulseAudio | `PlaybackStream.Error` reads a field the library writes from another goroutine, unlocked | A data race: oto's `Err` reads it, and otosound polls `Err`. Only `-race` against a real PulseAudio server reports it, and no test opens one |
| [ebitengine/oto#229](https://github.com/ebitengine/oto/issues/229) | macOS reported, all | The context keeps filling the device with zeros while nothing plays, at a steady CPU cost | Always paid: otosound keeps one player playing for the kernel's life |
| [ebitengine/oto#54](https://github.com/ebitengine/oto/issues/54) | all | No output device selection; oto plays to the system default | By design, as in v1 |
| [ebitengine/oto#237](https://github.com/ebitengine/oto/issues/237) | all | `IsPlaying` turns false before the hardware has played the tail | Not reached: otosound never calls `IsPlaying` |
| [ebitengine/oto#336](https://github.com/ebitengine/oto/issues/336), [#292](https://github.com/ebitengine/oto/issues/292) | macOS, PulseAudio | `Suspend`'s bugs | Not reached: otosound never calls `Suspend` (*Pause*) |
| [jfreymuth/pulse#48](https://github.com/jfreymuth/pulse/issues/48) | mostly BSD | No PulseAudio server started on demand | Not on Linux desktops, which run PipeWire or PulseAudio per session; without one, oto falls back to ALSA |

Fixed on oto's `main` and still in v3.5.1:

- **ALSA accepts a changed rate** ([#326](https://github.com/ebitengine/oto/pull/326)): on Linux without a PulseAudio server, a card that cannot do the requested rate plays at the wrong speed.
- **darwin's temporary rebuild failures are fatal** ([#329](https://github.com/ebitengine/oto/pull/329)): a media-services restart during a rebuild ends sound for the process.
- **WASAPI's poll finding no default endpoint is fatal** ([#320](https://github.com/ebitengine/oto/pull/320)), and its sample wait has no timeout ([#323](https://github.com/ebitengine/oto/pull/323)), so an endpoint that stops signalling could hang oto's Windows loop.
- **WinMM gives each of its six headers the whole buffer** ([#327](https://github.com/ebitengine/oto/pull/327)): on the WinMM fallback, otosound's 10 ms becomes 60 ms of headers.

**Unmeasured: the 10 ms default on macOS and Linux.** v1 measured it on Windows alone. On macOS oto splits the buffer across four AudioQueue buffers, so 10 ms gives four buffers of 120 frames, 960 bytes each, an eighth of oto's own default of 1,536 frames (12,288 bytes) a buffer, which oto's comment calls necessary on some Apple hardware. On PulseAudio the 10 ms is passed as the latency requested, and the server may grant more.

## Testing Decisions

([sfx: how sfx, otosound and the headless backend are tested on v2](https://github.com/dvoyni/cog/issues/951))

**No test opens a real device.** The fake device stands in for oto behind otosound's private interface, set by the package's own tests on the plugin they build: it can refuse the open, report a machine with no output device whose open and play succeed anyway (oto's Windows null context, which is unexported and cannot be reached any other way), refuse to play, be unplugged, be plugged back in, gain a device, and settle a rate or buffer other than the one asked for, and it counts its probes, opens, plays and closes. A test needing a sound card could not run in CI, and a process has one oto context to spend: a test that spent it would spend it for every test after it in the binary.

**The obligation proofs are plain package tests, rendering into a buffer.** The mixer is an `io.Reader`, so a test reads blocks out of it, as oto would, and asserts on frames. Each obligation is proved in this package:

| What is proved | How |
|---|---|
| Item 1: nothing decodes on the device thread | structurally: the device thread's files (the mixer, the voice table, the handoff ring and its changes, the read-ahead ring) may import only `encoding/binary`, `math`, `sync/atomic`, `time` and sfx, for its types alone, so no package that could decode is reachable; and by a streamed voice whose decoder panics if called on the device goroutine |
| Item 2: the device thread allocates nothing, takes no lock and calls into nothing | `testing.AllocsPerRun` at **zero** over a full block, with a publish and a drain every block, for a resident voice and for a streamed one, the ring, the table and the read-ahead rings made beforehand; the import rule above for the lock and the calls |
| Item 3: no base-rate resampling on the device thread | the sinc is reachable only from `Prepare` and the read-ahead, since its file is not among the device thread's; the device thread is shown to interpolate for rate |
| Item 4: the device rate is not baked in | a clip prepared at a rate other than the device's is converted, and answers its source's rate |
| Item 5: declicking | the largest per-sample gain step across a block boundary and within the block is below the ramp's own step, and the block ends exactly on its target; a stopped voice ramps to silence before its slot is freed; a play and a stop in one flush's changes are audible for zero samples; a paused voice ramps to silence and holds its playhead |
| Item 6: a loop is gapless | a looping voice rendered to a buffer has no repeated or dropped frame at its wrap, resident and streamed, at a tagged region's bounds, and a real clip wraps at the same frame in both tiers |
| Item 8: stops before starts | a stolen slot's stop is recorded before the start that reuses it, and a start after a stop on one slot survives a merge |
| Item 10: reported once, lost never | over the fake device, with `DeviceRetry` in milliseconds: a device that never opens is reported once however often it is retried, and the game runs on; a machine with no output device is reported once and takes no player; a device appearing on a machine with none is picked up by the retry; a lost device is never reported and the open is retried until it comes back; a later kernel records that its config was ignored |
| No flush's changes are lost | every flush's published changes are applied whole and in order; a full ring merges into staging rather than blocking or dropping; a merge coalesces updates and keeps starts and stops |
| The mixer never frees | a released clip a voice is mid-copy from is held until the mixer has passed the changes that destroyed it; the applied counter moves only once a ring slot's changes are applied; a release is let go at once while no mixer has pulled, and again after a lost device, and is held against the counter once a device comes back |
| The resampler does not alias | the 96 kHz clip decimated to 48 kHz leaves the band above the new Nyquist empty; upsampling leaves the band it carries unchanged; a conversion in chunks is sample for sample the whole one |

Items 7 and 9 hold by the types they are: a prepared clip holds Go memory alone, and `Device()` is an atomic load.

**otosound's other package tests** are v1's, case by case, on the same fake device and buffers: prepare decodes off the flush and its completion arrives under its token; it refuses bytes that are not Ogg Vorbis; `Install` mints a non-zero ID that never repeats; a play on a resident clip allocates nothing in the backend, `Emit` and a block together; the device is not ready until one opens; a clip under the limit is resident and one over it streams; the limit's sentinels choose the tier outright; a stream whose length reads zero is counted rather than believed; a streamed clip is frame for frame what the resident one would have been; a streamed voice plays the frames its read-ahead decoded, holds its playhead until its ring primes, plays silence for its slot alone on an underrun, ends when its read-ahead says the stream ran out, seeks in its read-ahead when started at an offset, and its read-ahead stops with it; and the loop region's tags are read as *Loop regions* states, a malformed region dropped whole and answered by `LoopRegionErr`, a resident clip trimmed to its granule end. A benchmark times a streamed voice opening its decoder.

**A few kernel tests,** as v1's plugin tests did, compose sfx and otosound over the fake device. They ride on sfx's `sfxtest`: each calls `UsePluginBackend` before `Spawn`, so sfxtest binds no backend of its own, and spawns otosound's plugin, its fake device set, among the test's plugins, with the fixture put in sfxtest's memory mount by `Mount`. They assert that a game playing a real Ogg produces samples for the device, not silent, not clipping and in both channels; a device that cannot be opened is reported once and the game runs on, its voice alive and its `Device` not ready; a machine with no output device is reported once and the game runs on. `Init` panics on a config that is not a `*Config`, on a negative value in a field that takes none, and on a `DecodedClipLimit` below -2. What none of these can assert is that the samples sound right, which needs ears: v1's prototype judged the arithmetic, the ramp and pitch by resampling by ear.

**The fixture** is `testdata/pianoroll.ogg`, the first 48,704 frames (1.104399 s, stereo, 44.1 kHz) of *Pleasant Moments Piano Roll* from Wikimedia Commons, public domain, cut at an Ogg page boundary with nothing re-encoded, its attribution beside it. The headless backend holds its own copy, so a test asserts that the two backends report one clip identically. At 44.1 kHz against a 48 kHz device it also exercises the conversion.

**Concurrency under `go test -race`,** run in WSL on Windows. Each test is checked by breaking what it guards, an atomic store or a mutex replaced by a plain access, and seeing it fail. They replace v1's `-count=10`, which stood in for a detector v1's environment lacked, its tests written so that a broken protocol shows as a lost or reordered operation. They cover:

- the handoff ring between the flush and the device thread: no operation is lost or reordered while the flush side publishes and the mixer drains together;
- the read-ahead goroutines filling their rings while the mixer reads them, and their halting;
- the prepare goroutine, its completions drained through `TakePrepared` while others finish;
- the device goroutine storing the `Device`, recording its first failure and logging, while the flush reads `Device()`.

Under the detector, a test that counts allocations skips its count, since the race runtime allocates on its own; the counts run in a plain `go test`. jfreymuth/pulse#53 shows only against a real PulseAudio server, which no test opens, and is recorded as known (*oto's open issues*).

**No conformance suite.** v2 lifts v1's architectural ban on one, but v1's reason on the merits stands: the obligations are properties of the code, samples never cross the seam, and a black-box suite would certify what it can see while what matters went unchecked. otosound proves its own obligations in its own package.

**Test idiom:** same-package `_test.go` files, full-sentence test names, `<topic>bench_test.go` with no underscore, and `b.ReportAllocs()` in code.

## Out of Scope

- **Anything sfx owns:** the queue, voices and their handles, buses, the cap, stealing, the listener, the equations, the clip table, the endings, the pause flag, the restart on readiness, the seam's types and the list of what a backend owes, and every report about a clip.
- **The browser.** v1's jssound, the Web Audio backend, stays in v1 until v2 targets the web ([sfx: v1's sound, otosound and nosound carried onto the v2 kernel](https://github.com/dvoyni/cog/issues/945)).
- **Moving to oto v3.6.0** once it ships ([#945](https://github.com/dvoyni/cog/issues/945)). It changes three things otosound leans on: a source read asks only for the room left, so on drivers that drain in smaller pieces (macOS, from oto's source) the declick ramp is no longer one buffer long; WASAPI's "not found" narrows to `E_NOTFOUND`, which the Windows probe mirrors, so the probe would no longer agree with oto exactly; and `Player.UnplayedSize` appears, its Windows support still pending. It also fixes the Windows, ALSA and darwin bugs above. v2 forces none of it, so it is a later bump, with the re-check of what otosound leans on that v1 asked of every bump ([The Device lifecycle: absent, not ready yet, and lost](https://github.com/dvoyni/cog/issues/485)).
- **Measuring the 10 ms default on macOS and Linux** ([#945](https://github.com/dvoyni/cog/issues/945)). v2 forces no change to it, so it is tuning after the port.
- **HRTF.** It would redraw the seam: otosound receives a gain matrix and nothing a convolver could use.
- **Filters and reverb.** They would run here, and buses would have to cross the seam for them.
- **More than two output channels,** which is a different mixer rather than a different config.
- **Formats other than Ogg Vorbis.**
- **cgo,** in any form, in otosound. A cgo backend could be a plugin of its own that a game composes itself; it is not this one.
- **Choosing the output device** (ebitengine/oto#54): oto plays to the system default.

## Further Notes

- **Where v1's decisions were made,** each restated above: [audio: a plugin whose sounds are commanded, positioned, and mixed in pure Go](https://github.com/dvoyni/cog/issues/294), v1's map, with the spike's measurements; [Crossing the thread boundary: how a tick's writes reach the audio callback](https://github.com/dvoyni/cog/issues/302); [Assets: how a path becomes samples, and what streams](https://github.com/dvoyni/cog/issues/298); [The device's lifecycle: not ready, absent, and lost](https://github.com/dvoyni/cog/issues/299); [What a sound Backend speaks: the seam beneath the sound Slot](https://github.com/dvoyni/cog/issues/376); [Prototype: does hand-rolled panning and attenuation sound right?](https://github.com/dvoyni/cog/issues/304); [What a released clip does to the voices playing it](https://github.com/dvoyni/cog/issues/416); [Where a music track's loop repeats from, since Loop is a bool and an intro cannot run into it](https://github.com/dvoyni/cog/issues/471); [The streamed tier, its read-ahead rings, and the offline resampler](https://github.com/dvoyni/cog/issues/482); [Preload, Release, ReleaseAll, and asking about a Clip without loading it](https://github.com/dvoyni/cog/issues/484); [The Device lifecycle: absent, not ready yet, and lost](https://github.com/dvoyni/cog/issues/485); and, for the decoder, [otosound: the vorbis setup header costs 460 us and 137 KB per decoder, and every block allocates](https://github.com/dvoyni/cog/issues/466), [vorbis: cog decodes through its own forks](https://github.com/dvoyni/cog/issues/511), [Fork vorbis and oggvorbis to dvoyni and replace them](https://github.com/dvoyni/cog/issues/512) and [The setup cache lands in vorbis](https://github.com/dvoyni/cog/issues/518).
- **Research behind the port,** in sfx's `docs/research`: [oto v3 and our Vorbis forks today](../../sfx/docs/research/oto.md), and [what v1's sound plugins take from v1, and what v2 offers in its place](../../sfx/docs/research/v1-port.md).
- **Why oto.** gogpu's audio package was the first candidate for the backend, and was dropped: it plays on Windows only and decodes no Ogg. v1's own implementation was taken instead ([#945](https://github.com/dvoyni/cog/issues/945)).
- **The machinery keeps out of the glossary.** Of otosound's words only *Mixer* is a game's, and it is in sfx's `CONTEXT.md`. The handoff ring, the staging changes, the applied counter, a block, a voice slot, a read-ahead ring, the device thread and the device goroutine are machinery a game never meets; the glossary is a glossary, not a description of how the backend is built.
- **What is not foreclosed:**
  - per-clip decoder pooling: a pooled decoder opened in 74 µs against 460 µs, before the setup cache brought a streamed voice's open to 96 µs;
  - aligning a block boundary to the flush, which nothing needs and which sample-accurate music would be the first to want;
  - a smoother interpolation between blocks than linear, if a case ever turns up that the ramp does not serve;
  - buses in the mixer, the day filters or reverb land; nothing above the seam moves when they do.
- **What changed from v1, and why:**
  - **A plugin built by `NewPlugin()`,** configured by a `*Config` under `PluginName`, read with `PluginConfigOrDefault`: v1's constructor package, its `With…` builders and its root aliases over an `internal` package go, under v2's plugin rules ([#948](https://github.com/dvoyni/cog/issues/948)).
  - **It binds `sfx.BackendSocket` with a private `backend{plugin: p}`,** in place of providing an Adapter for sound's `BackendPort`; *Backend* replaces v1's *Adapter* as the word ([#945](https://github.com/dvoyni/cog/issues/945), [#948](https://github.com/dvoyni/cog/issues/948), item 3).
  - **Four constants became `Config` fields,** `RingDepth`, `ReadAhead`, `RefillWait` and `DeviceRetry`, under the rule that a count or size a plugin is tuned by is a config field; the test-only cadence field goes with them ([#948](https://github.com/dvoyni/cog/issues/948), item 9). `DeviceRetry` paces both the retry of a device that could not be taken and the poll of an open device for loss, as v1's constant did (decided at the map's handover, [#945](https://github.com/dvoyni/cog/issues/945)).
  - **A setting v1 refused panics** as a composition error, where v1 returned `ErrInvalidConfig` and three `ErrInvalid…` errors ([#948](https://github.com/dvoyni/cog/issues/948), item 9). `DecodedClipLimit` keeps v1's sentinels, -1 never streaming and -2 always streaming, so only a value below -2 panics, which is exactly what v1 refused (decided at the map's handover, [#945](https://github.com/dvoyni/cog/issues/945)).
  - **The player closes in `Destroy`, not `Stop`,** since v2 finishes every job between the two; `Voices(n)` from sfx's `Start` still opens it ([#948](https://github.com/dvoyni/cog/issues/948), item 10).
  - **The oto context is never closed,** and v1's two Engines become several kernels that come and go, the first kernel's settings winning after it is destroyed ([#948](https://github.com/dvoyni/cog/issues/948), item 10).
  - **The retry is stated for what it can recover,** replacing v1's promise that on recovery every live voice resumes, which oto's permanent context error cannot keep ([#948](https://github.com/dvoyni/cog/issues/948), item 11; [#947](https://github.com/dvoyni/cog/issues/947)).
  - **A device never opened is a warning, `LogWarnOnce`,** not an error, so a machine with no sound card does not end a game under the default logger, which panics on every error; a later kernel's ignored settings are `LogErrOnce` ([#952](https://github.com/dvoyni/cog/issues/952), item 1).
  - **otosound logs through the Logger it keeps from `Init`,** from its device goroutine, and its report subscription on v1's update event goes ([#952](https://github.com/dvoyni/cog/issues/952), item 3).
  - **A dropped loop region crosses back to sfx through `PreparedClip.LoopRegionErr()`,** reported under the clip's path, where v1 queued it and reported it unnamed ([#952](https://github.com/dvoyni/cog/issues/952), item 2).
  - **`Prepare` takes `[]byte`, which becomes otosound's,** where v1 took an asset Blob that referenced the asset cache's run; a resident clip's bytes are freed once decoded, and a streamed clip's are otosound's copy ([#949](https://github.com/dvoyni/cog/issues/949), item 3).
  - **A pause is the game's, through sfx's `Queue.SetPaused`,** where v1 followed the engine's pause; to otosound it is still `VoiceParams.Paused`, never oto's `Suspend` ([#950](https://github.com/dvoyni/cog/issues/950)).
  - **The tick is the flush,** which the game schedules on its update beat; the ring's depth and the latency are counted in flushes ([#948](https://github.com/dvoyni/cog/issues/948), item 1).
  - **oto v3.5.1,** in place of v3.5.0: one fix otosound does not use, and the same API ([#948](https://github.com/dvoyni/cog/issues/948), item 12; [#947](https://github.com/dvoyni/cog/issues/947)).
  - **The Vorbis forks are permanent,** where v1 expected the `replace` lines to come out once upstream merged; they hold only while the oggvorbis fork stays on its pre-1.0.6 base ([#947](https://github.com/dvoyni/cog/issues/947)).
  - **Concurrency tests run under `-race`, in WSL on Windows,** each checked by breaking what it guards, replacing v1's `-count=10`; and there is still no conformance suite, now for v1's reason on the merits rather than v1's architecture test ([#951](https://github.com/dvoyni/cog/issues/951), items 6 and 7).
  - **The seam's `Batch` is sfx's `FlushChanges`,** and otosound's batch ring and staging batch are its handoff ring and staging changes, since the kernel's glossary already defines *Batch* as the jobs one publication's handlers schedule (decided at the map's handover, [#945](https://github.com/dvoyni/cog/issues/945)).
  - **`ErrLoopRegionIgnored` is sfx's, declared once with a `Path`,** and otosound's copy goes: otosound answers it with no path, and sfx fills it in when it reports it. The Ogg header errors stay in otosound's `err.go`, and sfx wraps them in `ErrClipFailed`, which carries the path (decided at the map's handover, [#945](https://github.com/dvoyni/cog/issues/945)).
  - **otosound's kernel tests ride on sfxtest,** through `UsePluginBackend`, with otosound's plugin over its fake device among the test's plugins, where v1's plugin tests composed their engine on a harness of their own (decided at the map's handover, [#945](https://github.com/dvoyni/cog/issues/945)).
