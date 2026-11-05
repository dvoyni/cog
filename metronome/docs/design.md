# metronome: design

`github.com/dvoyni/cog/metronome` paces the work a kernel does over time. An
application declares **tracks**; each track publishes its own event type as
**beats**, on a fixed or a variable step, with its own settings. metronome
knows nothing about rendering, windows, input or the ECS: a server declares an
update track, a client adds a render track carrying gfx's `RenderEvent`, and
gfx's draw chain hangs off it. Nothing in `main` drives frames (see the
kernel's *The main thread and pacing*); metronome is what keeps the beat.

Status: built. The decisions come from the
[metronome map](https://github.com/dvoyni/cog/issues/609); each section links
the ticket that holds its detail.

## Constraints

- **Render-agnostic.** metronome imports the kernel and nothing else of cog.
  gfx, system and the ECS hang off its events; it never imports them.
- **Alloc-free per beat.** Once started, publishing a beat, waiting for the
  next one and tracking what is in flight allocate nothing: timers and
  signals are made once per track and reused.
- **One clock.** metronome reads time only through its `Clock`, never the
  standard library's clock, so a test or an agent can replace time entirely.
- **No pause, no time scale.** A track ticks from `Start` to `Stop`. A scaled
  or paused clock is a game clock, above metronome.

## Shape

| Type | Role |
|---|---|
| `Plugin` | The kernel plugin, from `NewPlugin()`. Runs one goroutine per track between `Start` and `Stop`. It installs no resource: nothing else reaches it. |
| `Config` | metronome's part of the kernel's config, under `PluginName`: its `Clock` and its `Tracks`. |
| `NewTrack[E]` | Declares a track publishing `E`, with its `Settings`, for `Config.Tracks`. |
| `Beat` | The header every track event embeds: `Tick`, `Delta`, `Lost`, `TimeFromStart`, `Alpha`, `InterpolatedTick`. |
| `Settings` | A track's pacing: step or interval, in-flight limit, debt cap, delta clamp, reference track. Presets `FixedStep(hz)` and `Variable()`. |
| `UpdateEvent` | The one event metronome ships: a plain `struct{ Beat }` for an update track. |
| `Clock`, `Timer` | Where time comes from, how a track waits, and how a clock waits for the tracks to settle. `WallClock` is the default. |
| `ManualClock` | A `Clock` driven by hand, for tests and deterministic runs: `Advance`, `Jump`, `Wait`. |

## Tracks

([Update and draw event shapes](https://github.com/dvoyni/cog/issues/627))

- **No built-in tracks.** A track exists only when the application declares
  it in metronome's `Config`, as `NewTrack[E](settings)` in `Tracks`. A
  server declares one track, a client two or more.
- **Tracks are config.** No plugin reaches metronome to add one: the
  application, which spawns the kernel, decides every track and its
  settings, as it decides everything else in the config.
- **The event is the user's.** `E` embeds `metronome.Beat`, and whatever else
  it holds is the payload:

  ```go
  type RenderEvent struct {
  	metronome.Beat
  	Ready kernel.JobHandle // gfx's first handler sets it
  }
  ```

  metronome stamps the header through an unexported method on `*Beat`,
  promoted through the embedding, so `NewTrack` is constrained to
  `[E any, P interface{ *E; beater }]`. A track's event type is its identity:
  two tracks of one event type are a composition error and panic.
- **Tracks are independent.** Each runs on its own goroutine and publishes on
  it, so its handlers run there and a slow handler delays no other track.
  Locks order the work: a render task that reads the world waits for the
  update's write scheduled before it.
- **gfx ships only `RenderEvent`,** no track and no preset: the application
  declares the render track and picks its settings.

## Beat

([Update and draw event shapes](https://github.com/dvoyni/cog/issues/627),
[Catch-up cap](https://github.com/dvoyni/cog/issues/628),
[Clock source](https://github.com/dvoyni/cog/issues/629))

| Field | Type | Holds |
|---|---|---|
| `Tick` | `uint64` | This beat's number on its own track, from 1, never reset. Dropped steps get no number, so it stays contiguous. |
| `Delta` | `time.Duration` | On a fixed-step track, the step. On a variable track, the clock time since the previous beat, raw and unsmoothed, clamped to `MaxDelta` when set. |
| `Lost` | `time.Duration` | Time thrown away just before this beat, by dropped steps or the `MaxDelta` clamp; 0 when on pace. On a fixed-step track, a whole multiple of the step. |
| `TimeFromStart` | `time.Duration` | On a fixed-step track, the tick's deadline from the epoch; on a variable track, the publish time from the epoch. |
| `Alpha` | `float32` | In [0,1): how far past `InterpolatedTick` this beat falls, for blending its predecessor into it. |
| `InterpolatedTick` | `uint64` | The last tick of the reference track published before this beat. |

- **Every field is zero** on an event published by hand, not by metronome.
- **One invariant on every track:** `TimeFromStart` = Σ (`Delta` + `Lost`)
  over its beats. On a fixed-step track that is `Tick` × step plus the
  `Lost` so far.
- `Delta` shares a word with the ECS **Delta**; they belong to different
  contexts, and the clash is accepted.

## Pacing

### Fixed-step tracks

([Catch-up cap](https://github.com/dvoyni/cog/issues/628),
[Clock source](https://github.com/dvoyni/cog/issues/629))

- **Deadlines are computed:** tick n is due at epoch + n × step, never summed
  from sleeps, so timer jitter never becomes drift.
- **Debt.** Being past the next deadline is debt: steps owed but not yet
  published. `MaxDebt` caps it, 4 by default; a step owed past the cap is
  dropped, and its time shows in the next beat's `Lost`.
- **Owed steps publish back-to-back,** as soon as the in-flight limit allows:
  a track behind has no deadlines left to wait for.
- **`FixedStep(hz)`** sets the step to 1/hz (hz zero is 60) and
  `MaxInFlight: 1`, so a step publishes only once the previous one has
  finished, and time piles up only in the debt, never as queued jobs.
- At stop, debt is discarded: there is no final beat.

### Variable tracks

([Clock source](https://github.com/dvoyni/cog/issues/629),
[The os pump and the tracks](https://github.com/dvoyni/cog/issues/632))

- **`MinInterval`** is the least time between two beats; zero is none.
- **`MaxDelta`** clamps `Delta` after a stall; zero is no clamp. The clamped
  time is that beat's `Lost`.
- `MaxDebt` means nothing here.

### In flight

([Update and draw event shapes](https://github.com/dvoyni/cog/issues/627),
[Clock source](https://github.com/dvoyni/cog/issues/629))

- **A beat is in flight** until its publication's out dependency finishes:
  the job the last handler hands back. metronome arms a reused
  `kernel.Signal` on it with `Kernel.Notify`.
- **`MaxInFlight N`:** a new beat starts only while fewer than N of the
  track's beats are in flight. Zero, the default, is no limit.
- **A track is settled** when no beat of it is in flight and it owes no debt.
- **The draw modes are setting combinations,** documented, not built in:

  | Mode | Settings |
  |---|---|
  | vsync | `MaxInFlight: 2`, no `MinInterval`: a FIFO present blocks, the out dependency waits on it, and the display paces the track |
  | limit(fps) | `MinInterval: 1/fps`, plus `MaxInFlight` |
  | uncapped | `MaxInFlight` alone |

- **A handler that schedules a pinned job returns it in its out
  dependency** (`ex.Join(in, job)`), so the in-flight limit sees it.
  Otherwise each beat schedules another such job, and they pile up behind a
  blocked main thread. system's docs say the same of `ProcessEvents`.

### Waiting

([Clock source](https://github.com/dvoyni/cog/issues/629),
[Manual clock for tests and deterministic replays](https://github.com/dvoyni/cog/issues/631))

- **Every beat waits on its track's timer.** A fixed-step track resets it to
  the next deadline, a variable track to the previous beat plus
  `MinInterval`, or to now when it has none. The clock decides when a
  deadline counts as reached, so any clock can hold back any track.
- The track goroutine `select`s on its timer, on the signal of its oldest
  beat in flight, and on stop. It publishes once the deadline is reached (or
  debt is owed) and a slot is free.

## Interpolation

([Update and draw event shapes](https://github.com/dvoyni/cog/issues/627),
[Clock source](https://github.com/dvoyni/cog/issues/629))

- **`Settings.InterpolateAgainst`** names a fixed-step track by its event
  type, the **reference track**. Unset, `Alpha` and `InterpolatedTick` are
  zero.
- Tracks share only the reference track's last published tick and its
  deadline, through atomics.
- At a beat's publish, `InterpolatedTick` is that tick T, and `Alpha` is
  (now − deadline(T)) / step. While the reference track is in debt, `Alpha`
  is clamped to the largest `float32` below 1, so the beat shows T, the
  newest state.
- `InterpolatedTick`'s state is the one the beat's tasks read: locks order
  them after the writes T's publication scheduled. It also lets agents match
  a frame to the tick it shows.

## Clocks

([Clock source](https://github.com/dvoyni/cog/issues/629),
[Manual clock for tests and deterministic replays](https://github.com/dvoyni/cog/issues/631))

```go
type Clock interface {
	Now() time.Duration // since the epoch
	NewTimer() Timer    // one per track
	// metronome sets wait at Start and nil at Stop; wait returns once every
	// track has settled
	SetWaitCallback(wait func(ctx context.Context) error)
}

type Timer interface {
	Reset(at time.Duration) // fire once Now has reached at; the clock decides when that is
	C() <-chan struct{}
	Stop()
}
```

- **The epoch belongs to the clock:** it is where `Now` reads zero. Every
  track measures from it.
- **`WallClock`,** the default and `NewWallClock()`, reads monotonic time
  with the epoch at the `Start` of the plugin it is given to, backs each
  `Timer` with one reused `time.Timer`, and fires a deadline already past at
  once. No spin-wait.
- **`Config{Clock: c}`** uses `c` instead. A custom clock may fire on time,
  late, on an outside event such as an agent's MCP call, or never; the game
  sees its time and cannot tell.
- **`ManualClock`** starts at 0 and moves only when told:
  - `Advance(d)` walks deadline by deadline across every track, letting each
    due beat publish and settle before moving on. Work takes no manual time,
    so no debt builds and nothing drops.
  - `Jump(d)` moves to the target at once, as a stall would, so debt,
    `MaxDebt` and `Lost` can be tested.
  - A variable track beats at most once per call, at the target time.
- **Waiting belongs to the clock.** metronome hands its clock a wait
  callback at `Start` and takes it back at `Stop`; it returns once every
  track has settled. Only a clock that moves time when told needs it, to see
  the work of a deadline it fired done: `ManualClock` waits on it in
  `Advance` and exposes it as `Wait(ctx)`, a custom clock driven by an agent
  does the same, and `WallClock` ignores it. A clock serves one metronome.
- **Replays need nothing from metronome.** A replay is deterministic through
  fixed-step tracks, whose `Delta` is always the step, plus inputs recorded
  per `Tick` outside metronome. It runs on either clock.

## Lifecycle

([Clock source](https://github.com/dvoyni/cog/issues/629),
[The os pump and the tracks](https://github.com/dvoyni/cog/issues/632),
[Quitting](https://github.com/dvoyni/cog/issues/633))

- **`Start`** starts the clock's epoch and one goroutine per track, and
  hands the clock its wait callback; `Stop` takes it back first.
- **`Stop`** signals every track goroutine and joins them, so no beat is
  published after it returns. It does not wait on beats in flight: the
  kernel finishes the jobs it has, and blocking on a signal there could
  deadlock when `Destroy` is called from a job.
- **No quit API.** Ending the application is the supervisor's: a window
  close is a system event, and an application handler calls
  `Supervisor.Quit` or `Destroy`.
- **Teardown belongs in `Destroy`.** metronome is listed before the plugins
  that handle its beats, so it stops last, and up to `MaxInFlight` + 1 beats
  per track may publish while their `Stop`s run. A plugin's `Stop` therefore
  only halts its own activity; gfx and system release surfaces, the device
  and windows in `Destroy`, after the kernel's last job.
- **A stall holds a track in flight.** During a Windows drag the pinned
  `ProcessEvents` job does not finish, so the beat it was joined into stays
  in flight. A fixed-step track owes steps up to `MaxDebt` and catches up
  back-to-back after the drag; a render track's first beat after it has the
  stall as `Delta`, unless `MaxDelta` clamps it. A long stall has no special
  case.

## Testing

- **One seam: the public API in a real kernel.** A test spawns a kernel
  with metronome on a `ManualClock` and a test plugin that declares tracks
  and records the beats its handlers see. It drives time with `Advance` and
  `Jump`, waits with `Wait`, and asserts on the recorded beats: ticks, the
  `Delta`/`Lost`/`TimeFromStart` invariant, debt and `MaxDebt`,
  `MaxInFlight` (with handlers that hold their out dependency open),
  `MinInterval`, `MaxDelta`, `Alpha` and `InterpolatedTick`, and that
  nothing publishes after `Stop`.
- **No tests of internals,** such as the deadline arithmetic or the signal
  ring: they are reached through the seam.
- **`WallClock`** gets a few thin tests against real time with loose
  tolerances.
- **Concurrency** runs under `go test -race`, checked by breaking the code
  it guards and seeing it fail.
- **Alloc-free:** `testing.AllocsPerRun` is zero for a beat's publish and
  wait, as the kernel asserts for its own paths.
- Prior art: the kernel's and the ECS's tests, which drive a spawned kernel
  through its public API.

## Out of scope

- Animation timelines, a time scale, pause and single-step, and any game
  clock above ticks.
- Recording or replaying input.
- Overriding track settings from the config part; it can be added later
  without breaking anything.
- v1's hold for agent snapshots and its `QuitEvent`, `PauseChangeEvent` and
  `Last` flag.
