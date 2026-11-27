# What v1's sound plugins take from v1, and what v2 offers in its place

Research for [cog#946](https://github.com/dvoyni/cog/issues/946), on the map
[cog#945](https://github.com/dvoyni/cog/issues/945). Gathered 2026-11-27.

The question: what do v1's `sound` Slot, `otosound` and `nosound` take from
v1's kernel, `slots/app`, `slots/storage` and `libs/assets`, and what does v2
offer in place of each? Where does v2 offer nothing? Which passages of v1's
specs are tied to v1's kernel and must be restated?

It records facts and decides nothing. Each finding names the ticket of the map
it feeds: [#948](https://github.com/dvoyni/cog/issues/948) kernel shape and
flush pacing, [#949](https://github.com/dvoyni/cog/issues/949) clips, cache
and preparing, [#950](https://github.com/dvoyni/cog/issues/950) pause,
[#951](https://github.com/dvoyni/cog/issues/951) testing.

**Sources.** v1's code and specs under `_legacy/`, and v2's code and docs in the
working tree at `330dba2`. A path under `_legacy/` is v1, and every other path
is v2, from the repository root. `L` is a line in the file named. I read v1's
three plugins' non-test code in full, `sound.md` in full, `otosound.md` by
section, and the v1 kernel, app, storage and assets code each use reaches. On
the v2 side I read `kernel/docs/design.md` in full and the code and docs of
metronome, platform, gfx, render, canvas and mux where they answer a use.

## Answer in short

| v1 use | What it does there | v2 counterpart | Status |
|---|---|---|---|
| `kernel.Subscription`, `Lock`, `Observe` | The flush, the pause response, and the backends' report handlers, each locking resources in a handler | A handler that locks nothing and schedules a task, which declares `kernel.Read`/`kernel.Write` | Reshaped |
| `Subscribe(...).Last()`, exported `FlushOnUpdate` type | The flush runs after every recorder of the tick, and ecsaudio orders itself before it | `OrderSubscriptions` on a `Sub[E]` kept in a `Handlers` resource, plus the `in` dependency chain | Reshaped; no "last" |
| `RequiredPort`, `RequireAdapter`, `RequiredAdapter.Get`, `Adapter[P]`, `ProvideAdapter` | sound requires one Backend, which otosound or nosound provides | `DeclareExclusiveSocket[T](false)`, `ExclusiveSocket.Get` from `Start`, `BindSocket[T]` | Direct |
| `ErrMissingAdapter` | A composition without a backend fails | `Spawn` panics on a non-optional socket left unbound | Direct |
| `ReportErrorOnce`, `ForgetReportedError`, `ForgetReportedErrors`, `ReportError` | Clip failures once per Clip, forgotten at release; device and config reports | `Logger.LogErrOnce(key string)`, `ForgetLogOnce(key)`, `LogErr`; no forget-by-predicate | Partial |
| `Register` / `Start` / `Stop`, `Dependencies`, config by value | Lifecycle, `Voices(n)` at Start, device closed at Stop, config validated with errors | `Init` / `Start` / `Stop` / `Destroy`, `Dependencies`, `PluginConfigOrDefault` on a pointer, composition errors panic | Reshaped |
| `HandleCommand` | The MCP listing alone | None: jobs replace commands | Out of scope with MCP |
| `app.UpdateEvent` (`Dt`, `Tick`) | Paces the flush once per fixed step and advances every playhead by `Dt` | A metronome track's beat (`Beat.Delta`, `Tick`, `Lost`): a track event sfx ships, `metronome.UpdateEvent`, or a task the application schedules | Choice for #948 |
| `app.PauseChangeEvent` | Suspends every Voice when the engine's tick source pauses | Nothing: no engine pause, no pause event | **Gap** (#950) |
| Focus, minimizing, the application pausing | v1 used none of them | `platform.FocusEvent` only; nothing for minimizing or backgrounding | **Gap** (#950) |
| `storage.FileSystem` | The flush reads a Clip's bytes by path | `platform.FS` (`fs.FS`), under `kernel.Read[platform.FS]`, files valid only in the job | Direct |
| `storage.ReadMount`, `ReadMountPort`, `PermanentFSPort` | Tests mount clip bytes; every composition needs a permanent FS | `platform.ReadMountSocket`; `WriteFSSocket` optional | Direct, and lighter |
| `assets.Blob` | `ClipWithBytes`, a clip's identity by bytes, the bytes crossing to the backend | None. Bytes that are not files go into a memory mount and load by path | **Gap**, forced change (#949) |
| `assets.Descr`, `assets.Cache` and its Library | The read, report-once under the descriptor, terminal failure, and `Free` beneath sound's clip table | None shared: each plugin keeps its own table (render's `Inventory`, canvas's `fileTable`) | **Gap**, with precedent (#949) |

**The gaps where v2 offers nothing:**

1. **No engine pause.** v2 has no `TimeCmd`, no `PauseChangeEvent` and no
   paused tick source. metronome rules pause out: *"A scaled or paused clock is
   a game clock, above metronome."* Pause and minimizing are also absent
   from platform. See section 2.
2. **No minimized, occluded or backgrounded event.** platform publishes
   `FocusEvent` and nothing else about a window's attention, and gogpu does not
   track minimizing. See section 2.3.
3. **No shared asset Library.** `libs/assets` was not carried to v2. There is no
   `Cache`, no `Descr`, no loader and no family-keyed report-once. See section 4.
4. **No Blob, so no clip named by bytes.** `ClipWithBytes` has nothing to stand
   on, and v2's rule is a memory mount and a path. The memory mount copies the
   bytes in and copies them out, which v1's zero-copy Blob did not do. See
   section 4.
5. **No typed report-once keys and no forget-by-predicate.** v2's Once keys are
   strings, and `ForgetLogOnce` forgets one key. v1 keyed by `clipFailureKey`
   and `assets.Descr` values and could forget a whole family. See section 1.3.
6. **A default LogHandler that ends the process on `LogErr`.** v1's default
   handler logged and carried on, so *"audio never stops a game from
   starting"* held under it. v2's `StdLogger` panics on every `LogErr`. A
   machine with no sound card, reported as v1 reports it, would end a v2 game
   run under the default logger. See section 1.3.

v2 also offers three things v1 lacked:

- A backend can keep the kernel's `Logger` from `Init` and log from its own
  goroutine. otosound's and nosound's report subscriptions are then no longer
  needed.
- The writable filesystem is optional, so a test composes no empty permanent
  filesystem.
- Test harness packages are allowed (`rendertest`, `canvastest`), where v1's
  `archtest` forbade a `soundtest`.

---

## 1. v1's kernel

### 1.1 Subscription, Observe and Lock

**What v1 does.** A v1 subscription is a factory answering a `Lock`, which
binds the handler's resource handles and so declares its locks, and an
`Observe`, its body (`_legacy/kernel/contract.go` L58, L72, L92). sound has two
subscriptions, and each backend has one:

- **The flush,** `flushOnUpdate` on `app.UpdateEvent`
  (`_legacy/slots/sound/internal/plugin-flush.go` L48-126). Its `Lock` takes
  write handles on eight resources (`Queue`, `Voices`, `Clips`, `Buses`,
  `Listener`, `Device`, the private `flushScratch` and `lastFlush`) and a read
  on `storage.FileSystem` (L49-67). The body drains the prepares, applies the
  operations, computes and emits, then publishes one `VoiceEndedEvent` per
  ending with `k.PublishEvent` (L123). It is registered with `.Last()`
  (`_legacy/slots/sound/internal/plugin.go` L67).
- **The pause response,** `suspendOnPauseChange` on `app.PauseChangeEvent`
  (`_legacy/slots/sound/internal/plugin-pause.go` L29-48). It locks `Voices` and
  the scratch only, and emits one batch of updates.
- **Each backend's report,** `reportOnUpdate` on `app.UpdateEvent`, locking
  nothing (`_legacy/extensions/otosound/internal/plugin.go` L98-112,
  `_legacy/extensions/nosound/internal/plugin.go` L69-75). It exists because *"a
  Backend has no Kernel of its own"* (nosound `plugin.go` L14-17). It drains
  atomics and a queue the backend's goroutines wrote, and reports them.

The subscription's type is its identity, and sound exports it: `FlushOnUpdate`
and `SuspendOnPauseChange` (`_legacy/slots/sound/id.go` L8-22) let *"a recording
System - ecsaudio's, or a game's own - order itself Before it"*, through
`Ordering.Before[T]` (`_legacy/kernel/subscription.go` L21, `Last` L36).

**What v2 offers.**

- **Handlers lock nothing.** A handler's work is *"to decide what to schedule,
  and to adjust the event"* (`kernel/docs/design.md` L441-442). Locks belong to
  tasks. `access.Read[T]()` and `access.Write[T]()` declare them, or a plain
  function takes `kernel.Read`/`kernel.Write` parameters (`RegisterTask0`…`8`,
  L207-220; `kernel/access.go` L102-131). So each v1 subscription becomes a
  handler plus a task. The root `AGENTS.md` asks the task body to be a named
  function in `tasks.go`, and state kept between runs to be a resource, as
  `flushScratch` already is (`plugin-flush.go` L12-19).
- **The flush's arity.** `RegisterTask8` is the largest plain form
  (`kernel/configurator_gen.go` L184). v1's flush holds nine handles, and v2
  adds a tenth: the backend binding has to live in a resource, because *"no task
  reaches an implementation but through what the declaring plugin offers, in
  the resources it installs"* (design L163-166). gfx keeps its instance inside
  its `Device` resource, read in `Start` (`plugins/gfx/plugin.go` L64, L112). If
  `lastFlush` leaves with MCP, and the scratch and the backend share one
  private resource, the flush fits eight. Otherwise it is written with
  `RegisterTask` and a constructor (#948).
- **"Last" has no counterpart.** v2 sorts an event's handlers by
  `OrderSubscriptions(before, after)` declarations, keeping plugin order where
  none decides (design L417-424; `kernel/configurator.go` L205). A plugin that
  lets others order around its handlers publishes their `Sub[E]` in a
  `Handlers` resource (`kernel/AGENTS.md`, *Plugins*; `plugins/platform/resources.go`
  L20-30). A handler is ordered after the ones before it by its `in`
  dependency (design L425-435). Jobs whose locks conflict and have no
  dependency between them run *"in no guaranteed order"* (L246-250). So "the
  flush sees every play recorded this tick" holds only if every recorder's job
  is in the flush's `in` chain or is ordered before it. That is an ordering a
  game states, where v1 stated it once with `.Last()`. A game plugin is listed
  after sfx when its `Init` reads sfx's resources, and then its handlers run
  after sfx's by default (#948).
- **Publishing from the flush.** *"A task may publish. Its handlers run inline
  in the job while the job's locks are held"* (design L456-457).
  `VoiceEndedEvent{Voice uint64, Reason uint8}` is pointer-free, as every v2
  event must be (L261-282; `_legacy/slots/sound/internal/voice.go` L20,
  `ending.go` L12). An event cannot carry a `ClipRef` holding a path (L280-282),
  and v1's carries none.
- **The backends' report subscriptions go.** `Configurator.Logger()` answers a
  Logger *"for a plugin's own values to keep from Init: it stays valid after
  Init returns"* (`kernel/configurator.go` L45-48), and it *"is safe from any
  goroutine"* (`kernel/logger.go` L10-14). otosound's device goroutine and
  nosound's `Prepare` can then log directly, so neither needs an `UpdateEvent`
  subscription. render keeps its Logger in a `reporter` value the same way
  (`plugins/render/reporter.go` L10-17).

### 1.2 Ports, Adapters and `ErrMissingAdapter`

**What v1 does.** `BackendPort kernel.RequiredPort[Backend]`
(`_legacy/slots/sound/internal/ports.go` L8; shape at `_legacy/kernel/contract.go`
L103). sound calls `registrar.RequireAdapter[BackendPort]()` in `Register` and
keeps the `RequiredAdapter[Backend]` in a plugin field (`plugin.go` L14, L40),
read with `.Get()` from `Start` on (L76-78) and in each flush (`plugin-flush.go`
L69). Each backend declares an identity type
`SoundBackend kernel.Adapter[sound.BackendPort]` and calls `ProvideAdapter`
(`_legacy/extensions/otosound/internal/adapters.go` L10, `plugin.go` L74;
nosound `adapters.go` L9, `plugin.go` L55). A composition with no Adapter, or
two, fails with `ErrMissingAdapter` or `ErrDuplicateAdapter`
(`_legacy/kernel/registrar.go` L125-129, `err.go` L141-150). sound also provides
an `McpProvider` Adapter to the mcp broker (`plugin.go` L69), which is out of
scope.

**What v2 offers.** v2 renames v1's Ports and Adapters as sockets, which *"bind
plugins to each other, and are no resources"* (design L158-184):

- `DeclareExclusiveSocket[T](optional)` answers an `ExclusiveSocket[T]` whose
  `Get` *"panics in Init, before the bindings resolve"*
  (`kernel/configurator.go` L84; `kernel/socket.go` L11-16). A backend calls
  `BindSocket[T](impl)` (L101). No identity type such as `SoundBackend` is
  needed: the socket is identified by `T`.
- `T` is named with the suffix `Socket` (`kernel/AGENTS.md`, *Code*), as gfx's
  `BackendSocket` is (`plugins/gfx/sockets.go`).
- **`ErrMissingAdapter` becomes a panic in `Spawn`.** *"none of a socket that
  is not optional ... [is a] composition error, and `Spawn` panics naming the
  socket and the plugins"* (design L171-176). Two bindings of an exclusive
  socket panic the same way.
- The socket's interface lives in the declaring package's `sockets.go`, *"beside
  nothing else"* (root `AGENTS.md`, *Plugins*). So `Batch`, `VoiceStart`,
  `VoiceUpdate`, `VoiceParams`, `PreparedClip`, `Prepared`, `ClipID`,
  `VoiceSlot`, `LoopRegion` and the `Device` value, which v1 declared beside
  `Backend` in `seam.go` (`_legacy/slots/sound/internal/seam.go` L10-184), go in
  files of their own.
- v1's `internal/` package and the friend functions it needed
  (`_legacy/slots/sound/internal/friends.go` L11-15) existed because v1's Slot
  layout aliased internal types from its root. v2 has one package, so
  unexported methods do that work. The potential rule *"a resource type ... is a
  private type in a file named after it, and `resources.go` names it with a
  public alias"* (root `AGENTS.md`, *Potential rules*, [4]) is v2's spelling of
  v1's read-only aliased views (`_legacy/slots/sound/resources.go` L12-71).

### 1.3 Report-once, and what an error does

**What v1 does.**

- `kernel.ReportErrorOnce[T comparable](key T, errs ...error)` keys by any
  comparable value, and distinct key types never collide
  (`_legacy/kernel/kernel.go` L25-54). `ForgetReportedError(key)` clears one key,
  and `ForgetReportedErrors[T](match)` clears a family (L56-79).
- sound keys a Clip's prepare failure by `clipFailureKey(ClipRef)`, a type of
  its own (`_legacy/slots/sound/internal/clips.go` L109-112, L165, L336-339). It
  forgets the key at release (L243-249), *"which is what makes a release the one
  thing that clears a terminal failure"* (L209-211).
- The Library keys a read failure by the `assets.Descr` itself
  (`_legacy/libs/assets/cache.go` L65-71), forgets it on `Free` (L91-100), and
  `FreeAll` forgets the whole family with `ForgetReportedErrors` (L118).
- otosound reports `ErrDeviceUnavailable` and `ErrDeviceConfigIgnored` once,
  under singleton key types (`_legacy/extensions/otosound/internal/plugin.go`
  L17-24, L100-105). Both backends report each dropped Loop Region with
  `ReportError`, because the drained queue is the dedupe (otosound L106-110;
  nosound L60-74).
- **v1's default error handler logs and carries on** for everything but a
  plugin panic (`_legacy/kernel/engine-reporting.go` L108-115).

**What v2 offers.**

- `Logger.LogErr`, `LogErrOnce(key string, err)`, `LogWarnOnce`,
  `ForgetLogOnce(key string)` (`kernel/logger.go` L26-80). **Keys are strings,
  and there is no forget-by-predicate.** A key built at run time allocates, and
  logging may (design L484-494). canvas, the v2 precedent for forgetting reports
  by path, builds `logKey + path` keys and records them under the path in its
  own file table, so unloading the path forgets them
  (`plugins/canvas/filetable.go` L153-160; `plugins/canvas/reporter.go` L30).
  render reports each failure *"once per key and forgotten when the asset is
  freed"* (`plugins/render/docs/design.md` L227). sfx's clip table would keep
  the key the same way. With clips named by path only (section 4), the path is
  the key (#949).
- **v2's default handler panics on `LogErr`.** *"A nil LogHandler means a new
  `StdLogger`: `LogErr` panics ... Every error is fatal under it, which suits
  development"* (design L496-499). render logs its reports with `LogErr`
  (`plugins/render/reporter.go` L19-25) and canvas through `LogErrOnce`, so a
  missing texture is fatal under the default too. That suits a mistake in the
  game's files. It does not suit two of v1's reports:
  - `ErrDeviceUnavailable`, a machine with no sound card. v1 says *"Audio never
    stops a game from starting"* (`sound.md` L1110-1111) and rejects *"Failing
    the composition when no Device opens"* (L1797-1798).
  - `ErrLoopRegionIgnored`, which *"is not terminal: the Clip plays"*
    (`_legacy/extensions/otosound/internal/err.go` L100-111).

  Under v2's default, reporting either through `LogErr` ends the process.
  `LogWarnOnce` exists. **Gap:** v2 has no "error that is not fatal" level, so
  which level each v1 report takes is a decision the map does not hold yet
  (section 8).

### 1.4 The plugin lifecycle and configuration

**What v1 does.**

- **Dependencies:** sound depends on storage only. Subscribing to an event
  needs no dependency (`_legacy/slots/sound/internal/plugin.go` L27-33), and the
  backends depend on nothing (otosound `plugin.go` L45-49).
- **Register:** sound validates its config with errors, `ErrInvalidConfig` and
  `ErrInvalidMaxVoices` (L42-56; `internal/err.go` L39-52), and initializes its
  resources (L58-65). otosound validates three fields with errors
  (`_legacy/extensions/otosound/internal/plugin.go` L56-72; `err.go` L8-39).
- **Start:** sound calls `backend.Voices(n)` (`plugin.go` L76-79), which is when
  otosound takes the device (otosound `plugin.go` L51-54).
- **Stop:** otosound closes the device and joins its watcher goroutine (L79-86).
- **Constructors:** each plugin has a constructor package
  (`_legacy/slots/sound/soundplugin/soundplugin.go`,
  `_legacy/extensions/otosound/otosoundplugin/otosoundplugin.go`). Configs are
  values with `With*` builders (`_legacy/slots/sound/internal/config.go` L22-26).

**What v2 offers.**

- `Plugin` has `Init`, `Start`, `Stop` and `Destroy` (`kernel/plugin.go`
  L19-51).
- The constructor is `NewPlugin()` in the plugin's own package (root
  `AGENTS.md`). The config is `*Config`, read with `PluginConfigOrDefault`,
  which panics on any other type (`kernel/plugin.go` L55-75). So
  `ErrInvalidConfig` disappears.
- **A negative `MaxVoices`, `SampleRate`, `BufferSize` or `DecodedClipLimit`
  becomes a panic.** *"A composition error (registering something invalid, a
  bad Spawn argument) panics. A runtime failure goes to LogErr"*
  (`kernel/AGENTS.md`, *Code*). metronome panics on a negative setting the same
  way (`plugins/metronome/track.go` L91-93).
- **Dependencies:** registering a task that reads `platform.FS` in `Init` needs
  platform's resource to exist already: *"A resource not yet initialized ...
  panics at registration"* (design L225-229). So sfx names platform in
  `Dependencies` and is listed after it, as render does (`render/docs/design.md`
  L166). Binding a socket is no dependency (`kernel/plugin.go` L23-29).
- **Stop and Destroy:** `Stop` runs in reverse plugin order. *"Jobs its
  publications schedule still run before the kernel is removed"*, and `Destroy`
  runs *"after Stop and once every job of the kernel has finished"*
  (`kernel/plugin.go` L41-50; design L327-331). metronome's `Stop` *"does not
  wait on beats in flight, which the kernel finishes"*
  (`plugins/metronome/plugin.go` L127-135). A flush still in flight after
  otosound's `Stop` would therefore `Emit` to a closed device. Whether otosound
  closes in `Stop` or in `Destroy` depends on its place in the plugin list
  (#948).
- **Several kernels per process.** v2 runs several kernels under one
  supervisor, spawned and destroyed on demand (design L59-104). This is v1's
  *"Two Engines in one process"* (`sound.md` L1238-1254), with kernels that also
  come and go. oto's context is per process, so the first kernel's `Config`
  still owns it after that kernel is destroyed (#948).

### 1.5 Commands

`registrar.HandleCommand[voicesCmd]` (`_legacy/slots/sound/internal/plugin.go`
L66; `mcpprovider.go` L241-260) and the `lastFlush` ring it reads
(`lastflush.go` L3-56) serve the MCP provider alone. v2 has no commands: *"jobs
replace commands and resource-holding subscribers"* (design L5-6). MCP is out of
the map's scope, so both go, and `endingsHeld` with them.

---

## 2. `slots/app`

### 2.1 `UpdateEvent` paces the flush

**What v1 does.** `app.UpdateEvent{Dt float64, Last bool, Tick int64}` is the
fixed-step tick. app publishes it once per step, catching up when frames run
long and dropping steps past `MaxPending` (`_legacy/slots/app/internal/events.go`
L7-24; `loop.go` L78-135). sound flushes on every step, not only the `Last`
one, and advances each playhead by `Dt` scaled by the Voice's rate
(`plugin-flush.go` L79; `_legacy/slots/sound/internal/voices.go` L742-748).
`Tick` is read only for the MCP ring (`plugin-flush.go` L113-120). The backends
use `UpdateEvent` only as a moment to report (section 1.1).

**What v2 offers.** *"Kernels pace themselves. ... A plugin with a timer runs
its own goroutine and publishes its kernel's update event on each tick"*
(design L367-369). That plugin is metronome:

- **Tracks.** The application declares each track as
  `metronome.NewTrack[E](settings)` in metronome's `Config`, and *"the event is
  the user's"*, a struct embedding `metronome.Beat`
  (`plugins/metronome/docs/design.md` L39-62; `plugin.go` L28-36; `track.go`
  L86-113).
- **The beat.** `Beat` carries `Tick uint64` from 1, `Delta time.Duration`,
  `Lost` (steps dropped past the debt cap, or a clamped variable delta),
  `TimeFromStart`, `Alpha` and `InterpolatedTick` (`plugins/metronome/beat.go`
  L5-35). A fixed-step track is `FixedStep(hz)`, and a variable one is
  `Variable()` (`track.go` L48-66).
- **metronome ships one event,** `metronome.UpdateEvent{Beat}` (`beat.go`
  L36-42).

v2 has three precedents for how a plugin is paced, and choosing among them is
#948's:

1. **The plugin ships its own track event**, which the application declares as
   a track. gfx does this: `gfx.RenderEvent{metronome.Beat}` is *"the beat of
   the render track the application declares with
   `metronome.NewTrack[gfx.RenderEvent]`"* (`plugins/gfx/events.go` L10-15;
   `plugins/gfx/README.md` L126-141).
2. **The plugin offers a task, and the application schedules it** from a
   handler of its choice. platform's `Tasks.ProcessEvents` works this way
   (`plugins/platform/resources.go` L9-18). So does mux's `Tasks.Step`, which
   takes the delta as its arg, so its caller decides the pacing
   (`plugins/mux/resources.go` L9-20).
3. **The plugin subscribes to `metronome.UpdateEvent`.** No plugin does this
   today.

Facts a choice meets:

- **v1's `Dt` is always fixed.** On a variable track `Delta` varies, and a beat
  can stand for several dropped steps through `Lost`. Whether a playhead
  advances by `Delta` or by `Delta + Lost` decides whether sfx's world time
  keeps up with the mixer's real time after a stall. v1 dropped excess steps
  silently, so its playheads lagged then (`loop.go` L109-135).
- **Coalescing has a scope.** v1 coalesces parameters *"within a tick"*
  (`sound.md` L174-175). If recorders run on one track and sfx flushes on
  another, the flush's scope is no longer the recorders' tick.
- **A task arg is pointer-free** (design L261-282), so a task sfx offers takes
  the beat or a duration. It cannot take a path.

### 2.2 `PauseChangeEvent` suspends sound

**What v1 does.**

- **The event.** app publishes `PauseChangeEvent{Paused bool}` when its tick
  source starts or stops publishing ticks. It publishes on the first frame that
  sees the change, and waits for it before that frame's ticks
  (`_legacy/slots/app/internal/events.go` L34-55; `loop.go` L78-83). The tick
  source is paused by `TimeCmd`: *"it pauses the update loop, resumes it, steps
  it"*, from gameplay, a test harness or a frame-step debugger
  (`_legacy/slots/app/internal/commands.go` L59-82). Nothing in v1 pauses on
  focus or minimizing.
- **sound's response.** It sets `enginePaused` over the whole Voice table and
  emits one batch restating the Voices it moved
  (`_legacy/slots/sound/internal/plugin-pause.go` L8-48; `voices.go` L346-350,
  L571-590). A suspended Voice's playhead does not advance (`voices.go`
  L745-748). It is a subscription of its own because *"pause stops
  app.UpdateEvent, and an Adapter left holding the last batch it was given keeps
  mixing it"* (`_legacy/slots/sound/id.go` L14-22).

**What v2 offers: nothing.**

- metronome: *"No pause, no time scale. A track ticks from `Start` to `Stop`. A
  scaled or paused clock is a game clock, above metronome"*
  (`plugins/metronome/docs/design.md` L24-25). Its *Out of scope* names *"pause
  and single-step"* and *"v1's ... `QuitEvent`, `PauseChangeEvent` and `Last`
  flag"* (L294-300).
- v2's one pause is a game clock: mux's `WriteTimeline.Pause()` and `Resume()`,
  *"as when a menu is shown over the game"*, per timeline, from the game's own
  task (`plugins/mux/writetimeline.go` L205-215). It is a precedent for a pause
  a game states through a plugin's resource rather than one the engine
  announces.
- Because no v2 track ever stops, v1's reason for a separate subscription
  (ticks stop, so the flush cannot carry the pause) does not arise in v2 in
  that form (#950).
- v1 rejected one shape already: a `SuspendAll()` verb **at the seam**, *"a
  second way to say what `VoiceParams.Paused` already says"* (`sound.md`
  L1538-1549, L1809-1810). That rejection speaks of the Backend, not of a verb a
  game records on the Queue.

### 2.3 Focus, minimizing and the application pausing in v2

- **Focus:** `platform.FocusEvent{Window, Focused bool}`, published from
  `ProcessEvents` when a window gains or loses the keyboard focus
  (`plugins/platform/events.go` L67-72; README *Kernel surface*, L84-87).
- **Minimizing:** nothing. platform's events are window opened, closed, close
  requested, resize, key, text, focus, layout, pointer and scroll
  (`plugins/platform/events.go` L1-106). gogpu's HostLoop *"does not track
  whether a window is minimized"* (`submodules/gogpu/hostloop.go` L235-240).
  gogpu's plugin only sees a minimized window as an acquire timeout
  (`plugins/gogpu/device.go` L652-655).
- **The application pausing or going to the background:** nothing in platform
  or metronome. otosound's spec records one OS-level case: on macOS an audio
  session that cannot be activated *"the app is in the background"* is retried
  silently, on the loss side of the line (`otosound.md` L160-170).

---

## 3. `slots/storage`

**What v1 does.**

- **The read.** `storage.FileSystem` is *"the single storage resource"*, a
  read-only overlay of every mount (`_legacy/slots/storage/resources.go`
  L5-11; `internal/filesystem.go` L17-79). The flush reads it under
  `kernel.Read[storage.FileSystem]` and boxes it as an `fs.FS` only on the
  first Play of a tick, to save 32 bytes (`plugin-flush.go` L57, L132-166).
- **No goroutine can hold it.** The resource *"must not be retained past the
  handler that declared it, so there is no filesystem to hand a goroutine"*
  (`clips.go` L140-147; `sound.md` L881-895). That is why v1 reads synchronously
  in the flush and only the prepare moves off the tick.
- **Seeking.** `storage.FileSystem.Open` promises no `Seek`, so nosound
  header-parses a `bytes.Reader` over the bytes it was handed
  (`_legacy/extensions/nosound/internal/backend.go` L83-88; `sound.md`
  L1680-1686).
- **Mounts.** Tests contribute clip bytes as a read mount through
  `ReadMountPort` (`_legacy/slots/storage/ports.go` L36-51;
  `internal/mounts.go` L18-22). Every composition also needs a
  `PermanentFSPort` Adapter, so each sound and backend test carries an empty
  `permanentAdapter` (`_legacy/slots/sound/internal/permanentadapter_test.go`
  L12-47; `example_test.go` L33-37). The spec counts *"seven identical copies"*
  (`sound.md` L1639-1641).

**What v2 offers.**

- **`platform.FS`** is `fs.FS` itself, read under `kernel.Read[platform.FS]` with
  `fs.ReadFile` and the rest, and *"files opened from it are valid only while the
  job runs"* (`plugins/platform/resources.go` L32-39). The interface needs no
  boxing, so v1's 32-byte economy has nothing to save.
- **v1's constraint stands.** The root `AGENTS.md` says *"A resource is never
  kept outside the function that reached it: not in a plugin's field, another
  resource or a closure."* So the read is still in a job holding the lock. What
  v2 adds is that a job runs on a worker, beside other readers of `FS`, rather
  than on v1's dispatch. The read could be a job of its own that the flush
  waits on, instead of a stall inside the flush (#949).
- **Writers.** `platform.FS` has writers: a job holding its write lock writes
  through `ElevateFileAccess` (L41-45), and platform's `FlushValues` does so on
  `WindowClosedEvent` (L20-30). A flush declaring `Read[platform.FS]` waits for
  them.
- **Mounts.** `platform.ReadMountSocket{FS, Priority, Prefix}` is v1's
  `ReadMount`, bound by any plugin (`plugins/platform/sockets.go` L63-75).
  `WriteFSSocket` is optional (L52-61; README L90-93), so a v2 test composes no
  empty writable filesystem.

---

## 4. `libs/assets`: Blob, Descr, Cache and the Library beneath the clip table

**What v1 does.**

- **The cache.** sound's `Clips` holds an
  `assets.Cache[clipParams, struct{}, assets.Blob]` beside its own entries
  (`_legacy/slots/sound/internal/clips.go` L114-138).
- **The loader is the identity:** `Load` returns the bytes the Library read,
  `Default` returns the zero `Blob` (read as "the read failed"), and `Free` does
  nothing (L82-99).
- **What the Library gives,** *"in exchange"*:
  - the read, `fs.ReadFile` on a `Name`, skipped when a `Blob` payload is given
    (`_legacy/libs/assets/cache.go` L54-82);
  - report-once under the descriptor key, *"the entry is the once"* (L22-24,
    L65-74);
  - terminal failure until a `Free`, and the forget (L84-100).
- **Who reports what.** sound reports only what the Library cannot: bytes that
  are not there (`clips.go` L157-167).
- **Identity.** `ClipRef{name string; blob assets.Blob}` names a Clip by path,
  or by encoded Ogg the caller holds: `ClipWithResource`, `ClipWithBytes`
  (`_legacy/slots/sound/internal/clipref.go` L17-42). Identity is the path
  when there is one, else the Blob's pointer and length, so `ClipRef` is
  compared with `Equal` (L44-66; `_legacy/libs/assets/blob.go` L14-36;
  `descr.go` L3-49).
- **The bytes cross the seam as a Blob:**
  `Prepare(token any, encoded assets.Blob)`
  (`_legacy/slots/sound/internal/seam.go` L169-174). otosound keeps a streamed
  Clip's Blob and opens decoders over it
  (`_legacy/extensions/otosound/internal/clipdata.go` L109, L301-426;
  `stream.go` L64-71). A Blob *"references the same run the cache holds"*
  (`otosound.md` L439-441).
- **The Library holds every Clip's encoded bytes** until the Clip is released,
  decoded ones included. otosound drops its own reference once it has decoded,
  but the cache entry stays (`clips.go` L93-95; `cache.go` L79-81). v1's
  residency therefore has a part the spec does not state.

**What v2 offers: no Library, no Blob.**

- `libs/` holds `m` and `meshfile` only. Each v2 plugin keeps its own table of
  loaded files.
- **render's `Inventory` is the precedent for a cache of loaded files**
  (`plugins/render/inventory.go` L14-52; `docs/design.md` L194-230):
  - **The parameter.** A task parameter declares read locks on render's asset
    table, gfx's `Device` and `platform.FS`, and the table is synchronized
    inside by a mutex per table (`design.md` L181-191).
  - **Loading.** It loads by path through `platform.FS` in the caller's job: a
    texture's header at the acquire, the body in render's own jobs before the
    submit (L212-217, L474-488).
  - **Sharing.** It shares by a count per comparable key, `Acquire…` and
    `Release…`, and frees at the end of the frame the count reached zero
    (L218-222). sfx's model differs here: in v1 nothing a game must release, and
    a release that stops Voices (`sound.md` L20-23, L984-1022).
  - **Failure.** Each failure is reported once per key and forgotten when the
    asset is freed (L223-227).
  - **Bytes.** *"Bytes that aren't files go into a memory mount ... and load
    from there by path. There are no blobs"* (L230). canvas says the same:
    *"Every resource is a path"* (`plugins/canvas/docs/design.md` L396, L848).
- **The memory mount** is `filesystems.NewMemoryFS()`, which its owner binds
  as a read mount and keeps as a resource. `Put(name, data)` and `Remove(name)`
  run under the mount's own lock (`plugins/platform/filesystems/memoryfs.go`
  L18-37, L98-104; `plugins/platform/docs/design.md` L396-407). *"A put keeps a
  copy of the bytes, and a read answers one"* (`plugins/platform/docs/README.md`
  L454-482). A clip given as bytes is therefore held twice, once in the mount
  and once in the copy sfx reads, where v1's `ClipWithBytes` copied nothing.
- **Consequences for #949:**
  - `ClipWithBytes` has no v2 counterpart, and a clip is a path.
  - With no Blob, `ClipRef` becomes comparable, so `Equal` loses its reason
    (`sound.md` L219-221).
  - The bytes cross the seam as a `[]byte` that sfx read.
  - Whether sfx keeps a decoded Clip's bytes, as v1's Library did, is a choice
    v1 never made explicitly.

---

## 5. Other v2 rules the port meets

None of these is a v1 dependency, but each is a v2 rule that changes how v1's
code lands. Each is listed so that a ticket can claim it.

- **The headless backend as an adapter.** v2's silent and recording
  implementations of a socket are adapters, not plugins:
  `platform/headless.NewWindows()` is *"for a test's plugin to bind"*
  (`plugins/platform/headless/windows.go` L10-27), and gfx's recording
  instance is `recording.NewInstance()` (`plugins/gfx/recording/instance.go`
  L28-44). A potential rule of the root `AGENTS.md` ([3]) says *"a filesystem,
  or any other layer a plugin's socket takes, is an adapter, not a plugin: a
  constructor in a subpackage of the declaring plugin that the application's own
  plugin calls and binds"*. The map's `plugins/sfx/headless` sits where such an
  adapter would. v1's nosound is a plugin with a `Config` (`SampleRate`) and a
  report subscription (`_legacy/extensions/nosound/internal/plugin.go` L19-75;
  `config.go`). As an adapter it has no `Configurator` and so no Logger. Its one
  report, a dropped Loop Region, then has to leave through what `Prepare`
  answers or through a Logger its binder hands it. gogpu, the real backend, is
  a plugin, so otosound as a plugin has precedent.
- **Tuning constants become `Config` fields.** *"A count or size a plugin's
  behaviour is tuned by is a field of its `Config`, its zero value meaning a
  stated default, not a constant left for a later measurement to set"* (root
  `AGENTS.md`). v1 keeps five such values as constants, each saying *"a constant
  until something asks otherwise"*:
  - otosound's batch ring depth of 8 (`_legacy/extensions/otosound/internal/ring.go`
    L7-11);
  - otosound's read-ahead of 200 ms and refill wait of 5 ms (`stream.go`
    L28-39);
  - otosound's device retry of 1 s (`backend-device.go` L17-21);
  - sound's 32 Buses (`_legacy/slots/sound/internal/buses.go` L5-9).
- **File layout.** Each of these has a file of its own: `handles.go` for the
  `Voice` handle, `resources.go` with `Tasks` and `Handlers` first, `tasks.go`
  for every task body and subscribed handler, `plugin.go` holding the plugin
  alone, and `sockets.go` holding the socket alone (root `AGENTS.md`;
  `kernel/AGENTS.md`). The README has a **Kernel surface** section
  (`kernel/AGENTS.md`).
- **Tests.** *"A test of concurrent behaviour runs under `go test -race`"*
  (`submodules/rules/GO-RULES.md`, *Tests*). otosound's spec ran its concurrency
  tests with `-count=10` instead, for want of a race detector (`otosound.md`
  L652-654).
- **Upstream forks.** v1 reaches `dvoyni/vorbis` and `dvoyni/oggvorbis` by
  `replace` directives on the `jfreymuth` paths (`_legacy/go.mod` L41-43). The
  new modules need the same.

---

## 6. Gaps where v2 offers nothing

1. **Engine pause**: no event, no command, no paused track (section 2.2). #950.
2. **Minimizing and backgrounding**: no platform event; focus alone is
   published (section 2.3). #950.
3. **A shared asset Library**: no `Cache`, `Descr` or loader. sfx's clip table
   keeps the read, the once and the forget itself, as render and canvas do
   (section 4). #949.
4. **Bytes named as a clip**: no `Blob`, so no `ClipWithBytes`. A memory mount
   stands in, at the price of two copies (section 4). #949.
5. **Typed report-once keys and family forgetting**: string keys and one-key
   forgetting only (section 1.3). #949.
6. **An error that is not fatal under the default logger**: `LogErr` panics
   under `StdLogger`, where v1's handler logged and carried on (section 1.3). No
   ticket holds it.

"Last" ordering (section 1.1) and a detachable filesystem reader (section 3) are
not gaps. v2 has the first in another shape, and v1 lacked the second too.

---

## 7. Passages of v1's specs tied to v1's kernel

`otosound.md` is the only backend spec; nosound has none. Each passage below
speaks in v1's kernel terms, or rests on a v1 dependency v2 lacks, and must be
restated in `plugins/sfx/docs/design.md` (or otosound's). The vocabulary swap
the map already settled (Slot, Port, Adapter, Extension and Bundle becoming
plugin, socket, backend and resource) is not repeated per line.

### `_legacy/slots/sound/docs/specs/sound.md`

| Lines | Passage | Tied to |
|---|---|---|
| L3-12, L24-30 | "is a Slot ... Its Adapters are"; ecsaudio "drives the same queue"; "an opt-in Extension a game composes" | Slot, Adapter, Extension, Bundle |
| L113-121 | **Backend** on the glossary's avoid list | Reversed by the map: *Backend* is v2's term |
| L123-162 | *The shape of the package*: the Slot/Extension/Bundle table; "one Adapter per platform, in build-tagged files"; "no vocabulary package", `ports.go`; four resources and their locks; `storage.FileSystem` as precedent | v1's plugin layout, storage |
| L164-177 | The queue "written under its lock by every recorder and flushed once per tick, atomically" | "tick" is `app.UpdateEvent` (#948) |
| L179-203 | *The flush, in order*: "A `Play` ... reads its bytes and calls `Prepare` here"; the Library | The tick, storage, libs/assets |
| L290-306 | Ordering and causality "in tick N" | The tick (#948) |
| L313-314 | A musical fade from "an `anim` timeline in its own handler" | v1's anim; v2's is mux |
| L324-340 | *One event*: `VoiceEndedEvent` | v1's synchronous publish; restate as published from the flush task |
| L352-353 | "There is no `Update` or `Tick` verb" | Holds, but a task sfx offers (section 2.1) would be one |
| L553-555 | "Shutdown publishes nothing" | v1's lifecycle; v2's `Stop` may publish (design L327-331) |
| L562-575 | The live view "read the way `gfx.Viewport` is, as `kernel.Read[*sound.Voices]`"; "a resource and not a command", `gfx.FrameSnapshot` | v1 gfx, v1 commands |
| L789-835 | *Where each half lives*: the Library, the identity loader, `kernel.ReportErrorOnce` under the descriptor key, "A Blob is a pointer and a length" | libs/assets, report-once (#949) |
| L881-895 | *The read is on the tick*: storage's resource "must not be retained" | storage; still true of `platform.FS`, restated (#949) |
| L900-917 | `Preload` and streamed Clips: "the encoded bytes belong to the asset cache" | libs/assets (#949) |
| L933-948 | *Failure*: "the Library's own report for a read failure, `kernel.ReportErrorOnce` under the key for a prepare failure" | libs/assets, report-once (#949) |
| L950-982 | *Asking about a Clip*: `ClipInfoOf(handle kernel.Read[*Clips], ...)`; "never the Library"; v1 gfx's `TextureDescr` | libs/assets; v1 gfx |
| L1015-1018 | "A release forgets the Clip's failure, in sound's report-once table and in the Library's both" | report-once, libs/assets (#949) |
| L1113-1133 | *Registration never fails*: "`Startup` neither blocks nor fails", "`kernel.ReportErrorOnce`", v1 gfx's `ErrBackendNotReady` | v1 lifecycle; v2's fatal `LogErr` (section 1.3) |
| L1154-1160 | "No `ReportError`, not even once" | report vocabulary |
| L1166-1172 | "the Slot notices `Ready` going true on the tick it polls" | The tick (#948) |
| L1236 | "A paused engine does not change `Device`" | Engine pause (#950) |
| L1238-1254 | *Two Engines in one process* | v2's several kernels, which also come and go (section 1.4) |
| L1256-1290 | *Configuration*: configs follow v1 gogpu's `config.go`, "`With*` builders"; `Config` given by value | v2: `*Config`, `PluginConfigOrDefault`, negative values panic |
| L1292-1310 | *Pause suspends; Hold is not audio's*: "An engine Pause", `slots/app/commands.go` | app's TimeCmd and PauseChangeEvent: the gap (#950) |
| L1405-1443 | *One call per tick*: "`gfx.Backend.Execute`'s shape" | The tick; v1 gfx |
| L1445-1491 | The `Backend` interface: `Prepare(token any, encoded assets.Blob)`; `BackendPort kernel.RequiredPort[Backend]`; `kernel.ErrMissingAdapter` | libs/assets, Ports (sections 1.2, 4) |
| L1493-1497 | "a poll at flush, which is `gfx.TakeCapture`'s shape. A callback would arrive ... where there is no handler and no lock" | v1 gfx; v1 handlers |
| L1538-1549 | *Pause is a field*: "an engine Pause is `sound` emitting an update for every live Voice" | Engine pause (#950) |
| L1551-1600 | *What an Adapter owes*: `kernel/archtest/tiers_test.go`, no shipped `soundtest`; item 2 "call into `sound` or `kernel`"; item 10 "reported once" | v1 archtest; report-once (#951) |
| L1602-1641 | *Testing*: "a fixed `TimeStep`"; a probe plugin; seven copies of `permanentadapter_test.go` | v1 app, storage; v2 tests through the Supervisor and metronome's `ManualClock` (#951) |
| L1680-1686 | `storage.FileSystem.Open` returns `fs.File` | storage; `platform.FS` is the same |
| L1698-1701 | "`nosound` is cog's first shipped no-op Adapter ... There is no `nogfx` or `nostorage`" | v2 has `gfx/recording` and `platform/headless` (section 5) |
| L1755-1756 | **Rejected: "A Port named `sfx`"**: *"In game audio SFX means effects as opposed to music, and it is the Bus name most games declare"* | Naming; see section 8 |
| L1818-1865 | *Required work*: the Slot layout, `soundplugin`, `internal/types`, `archtest`'s `ruleSlotPort` and `ruleRootFiles`, `kernel.ReportErrorOnce` at `kernel/kernel.go:52` | v1 layout and archtest |

### `_legacy/extensions/otosound/docs/specs/otosound.md`

| Lines | Passage | Tied to |
|---|---|---|
| L3-9 | "an Extension that fills `sound`'s `BackendPort`"; "invisible through the Port" | Extension, Port |
| L18-19, L373-374 | The device thread may not "call into `sound` or `kernel`" | Holds; restate with v2's names |
| L109-115 | "`Startup` neither blocks nor fails — `gfx`'s shape one Slot over"; "`kernel.ReportErrorOnce`, as `otosound.ErrDeviceUnavailable`" | v1 lifecycle; v2's fatal `LogErr` (section 1.3) |
| L203-205, L526-528 | "The Slot restates every live Voice ... on the tick it sees `Ready` go true" | The tick (#948) |
| L229-237 | *Two Engines in one process* | Several kernels (section 1.4) |
| L239-258 | *Config*: "`extensions/gogpu/config.go`'s convention ... `With*` builders" | v2 Config rules; tuning constants (section 5) |
| L263-334 | *The handoff*: "the tick", "one batch per tick" | The tick (#948) |
| L336-353 | *The Mixer never frees*: "the tick side" frees | The tick (#948) |
| L426, L439-441 | "the encoded `assets.Blob`"; "references the same run the cache holds" | libs/assets (#949) |
| L623-655 | *How the obligations are proved*: `kernel/archtest/tiers_test.go`; the storage Extensions' tests; "no race detector ..., so concurrency tests run with `-count=10`" | v1 archtest; v2's `-race` rule (#951) |
| L720-745 | *Required work*: root declarations, `otosoundplugin` exporting `New()`, "`github.com/jfreymuth/oggvorbis`" | v1 layout; v2 `NewPlugin()`; the forks |

---

## 8. For the map: decisions the open tickets may not hold

The map's open tickets are #948 (kernel shape and flush pacing), #949 (clips,
cache and preparing), #950 (pause) and #951 (testing). These findings ask for a
decision none of them names outright:

1. **Which level each v1 report takes under v2's logger.** v2's default
   `StdLogger` makes every `LogErr` fatal, and v1's `ErrDeviceUnavailable` and
   `ErrLoopRegionIgnored` were reports a game survives (section 1.3). It touches
   #948 and #951, and could be its own ticket.
2. **Whether `sfx/headless` is an adapter or a plugin, and how a backend
   reports.** v2's precedents (`platform/headless`, `gfx/recording`) and
   potential rule [3] point to an adapter, while v1's nosound is a plugin with a
   `Config` and a subscription. A backend that keeps a Logger needs no report
   subscription at all (sections 1.1, 5). It fits #948.
3. **Which of v1's tuning constants become `Config` fields** under the root
   `AGENTS.md` rule: ring depth, read-ahead, refill wait, retry cadence, Bus
   count (section 5). It fits #948.
4. **Where otosound closes the device,** `Stop` or `Destroy`, given that v2
   finishes the kernel's jobs between the two (section 1.4). It fits #948.
5. **The name `sfx`.** v1's spec rejected *"a Port named `sfx`"* because SFX is
   the Bus most games declare (`sound.md` L1755-1756). The map's *"v1's design
   questions are not reopened"* and its choice of `plugins/sfx` meet here. A
   game declaring a Bus called `SFX` beside a package called `sfx` is the case
   v1 named.
