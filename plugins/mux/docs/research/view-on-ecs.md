# mux research: a view on top of ecs

Question ([#782](https://github.com/dvoyni/cog/issues/782)): does a view written as an ecs system work with mux as [#780](https://github.com/dvoyni/cog/issues/780) decided? That decision gives each model two resources, `M` and `mux.Timeline[M, D]`. An update writes both, the AI reads `M`, `Step` writes both and a view reads both. The game schedules `Step` from its render-beat handler and makes its views depend on it.

Sources, read at `main` 5d52f0b:

- ecs: `ecs/docs/README.md`, `ecs/docs/design.md`, `ecs/CONTEXT.md`, `ecs/*.go`.
- kernel: `kernel/access.go`, `kernel/configurator.go`, `kernel/configurator_gen.go`, `kernel/executor.go`, `kernel/job.go`, `kernel/combiner.go`, `kernel/lockstate.go`, `kernel/task.go`, `kernel/CONTEXT.md`, `kernel/docs/design.md`.
- The render beat: `gfx/events.go`, `gfx/framepool.go`, `metronome/doc.go`.

Names such as `Game`, `Depth`, `Scene` and `Sprite` below are illustrative.

## 1. `kernel.Read` beside ecs parameters

This works today.

- **A system is a kernel task.** It is registered with `RegisterTask0` … `RegisterTask8`, and each parameter may be any `kernel.Parameter[P]`: a type whose zero value's `Declare(*kernel.Access) P` declares its locks (kernel/configurator_gen.go:30-40, kernel/access.go:97-104). ecs has no wrapper of its own (ecs/docs/design.md:252-261).
- **Both kinds are Parameters.** `kernel.Read[T]` and `kernel.Write[T]` are (kernel/access.go:111-113, 124-125), and so is every ecs handle: `Query1.Declare` (ecs/query_gen.go:33-39), `Store` and `Lookup` (ecs/store.go:121-124, 160-163), `World` (ecs/world.go:106-108). ecs lists `kernel.Read[R]` and `kernel.Write[R]` among a system's parameters (ecs/docs/README.md:263, ecs/docs/design.md:226, 237-246).
- **It is tested.** `move(k, t tick, q Query2[game, *position, velocity], s kernel.Read[settings])` is registered with `RegisterTask2` and runs as a job (ecs/system_test.go:14-42). Calling it allocates nothing (ecs/system_test.go:44-55).
- **The lock set holds every parameter's locks.** Each `Declare` sets bits in the read and write masks of one shared `Access` (kernel/access.go:12-16, 21-42). Those masks become the task's `taskLocks` (kernel/configurator.go:123-136). Take this view:

  ```go
  func drawUnits(k *kernel.Kernel, event gfx.DrawEvent,
  	model kernel.Read[Game], timeline kernel.Read[mux.Timeline[Game, Depth]],
  	sprites ecs.Query2[Scene, *Sprite, UnitIndex]) kernel.NoResult
  // p.drawUnits = configurator.RegisterTask3(drawUnits)
  ```

  It reads `Game`, the timeline and world `Scene`. It writes `Sprite`'s store and reads `UnitIndex`'s.
- **Limit: eight parameters.** `RegisterTaskN` takes at most eight (kernel/configurator_gen.go:184), so mux's two leave six for ecs handles. Beyond eight, `RegisterTask` with each handle's `Declare` called by hand has no limit (ecs/docs/design.md:262-283).
- **Limit: registration order.** Every resource must already exist when the view registers, or registration panics (kernel/access.go:18-20, 69-80). `mux.RegisterModel` therefore runs first: in an earlier plugin, or earlier in the same `Init`. ecs has the same rule: `ecs.RegisterWorld` comes before a world's systems (ecs/docs/design.md:92-98).

## 2. Scheduling, and depending on `Step`

- **ecs schedules nothing.** Its plugin subscribes to nothing and publishes nothing (ecs/plugin.go:31-32, ecs/docs/design.md:99-101). No ecs code outside the docs publishes, schedules or waits. The guide says: "your handlers schedule systems as jobs, in whatever order your game needs" (ecs/docs/README.md:49-53). The game calls `ex.Schedule` from its handler of any event (ecs/docs/README.md:135-141). "A system is a task ... Ordering systems is ordering jobs" (ecs/docs/design.md:287-288).
- **The game schedules the view system, as it would a plain task.** It does so from its `gfx.DrawEvent` handler. gfx publishes `DrawEvent` inside its render-beat handler. A handler "schedules its recording tasks after Ready ... and returns ex.Join(in, own)" (gfx/events.go:17-21).
- **A dependency on `Step` works.** With `Executor.Schedule(task, arg, deps ...JobHandle)`, the job "starts once every job in deps has finished and task's locks are free" (kernel/executor.go:27-39). A job reaches the lock combiner only when its last dependency finishes (kernel/job.go:124-127, 161-169), so the view cannot start before `Step`. Both jobs are in one publication's batch, and nothing in a batch starts before the last handler returns (kernel/executor.go:8-13).
- **Dependencies take handles.** `Schedule` returns a typed `Job[B]`, but dependencies take a `JobHandle`, which comes from `Job[B].Handle()` (kernel/job.go:26-34, kernel/docs/design.md:228-233). #780's snippet passes `step` and `view` where handles go. To compile, it would be `ex.Schedule(p.drawUnits, event, step.Handle(), event.Ready)` and `ex.Join(in, view.Handle())`.
- **A later handler sees an earlier handler's jobs only through `in`.** `in` is the out dependency handed on from the handler before (kernel/configurator.go:15-18). `DrawEvent` is gfx's type, so it cannot carry a `Step` handle. Suppose a view in another plugin's handler schedules after `in`. It then waits for `Step` and also for every job the earlier handlers handed on, their views included.
- **`Step` after `in` cannot be overtaken.** In a `DrawEvent` handler, the first `in` is the frame's acquire (gfx/framepool.go:377-379). That acquire waits for the previous frame's present (gfx/framepool.go:372-377), which waits for the previous `DrawEvent`'s out (gfx/framepool.go:379-381). So `Step` scheduled after `in`, as in #780, runs after the previous frame's views even with several render beats in flight. It also waits for the acquire, which under FIFO blocks on the display (metronome/doc.go:35-37). `Step` with no dependency would run beside the acquire. With more than one beat in flight, though, it could then advance the timeline before the previous frame's views had read it.

## 3. Locks

| Job | mux locks | ecs locks |
|---|---|---|
| update | write `M`, write `Timeline` | only the ecs handles it takes, if any |
| AI | read `M` | none |
| `Step` | write `M`, write `Timeline` | none |
| view on ecs | read `M`, read `Timeline` | read world `W`, plus the store locks of its terms and handles |

- **No ecs lock conflicts with `Step` or an update.** ecs locks only resources of its own private types:
  - `*worldResource[W]` (ecs/world.go:102, 106-108, 263-264)
  - `*storeOf[W, T]` (ecs/store.go:92-104)
  - `*refsResource[W, T]` (ecs/refs.go:159-161, 223-225, 246-248, ecs/keyedrefs.go:57-59, 143-145)
  - `*commandRecorder[W, Tag]` (ecs/commands.go:155-162, 274-276)
  - its `*registry` (ecs/plugin.go:27)

  A resource is named by its Go type and owns one bit of the lock masks (kernel/configurator.go:52-58, kernel/kernel.go:69-75, kernel/access.go:91-95). `M` and `Timeline[M, D]` are types of mux or the game, so no ecs handle shares a bit with them.
- **What does serialise is mux's own table, as #780 intends.** The view waits for `Step` and for any update, and the AI and the views read side by side.
- **ecs's own locks also serialise, among ecs systems.** A view's store write excludes the other systems on that store (ecs/docs/design.md:169-170, 219-226). The barrier, `World[W]`, excludes every handle of `W` (ecs/docs/design.md:198-204). It is taken by:
  - `CommandSink` (ecs/commands.go:274-276)
  - `ObservationResetSystem` and `ShrinkSystem` (ecs/systems.go:16-28)
  - saves (ecs/docs/README.md:501)

  A view that took `World[W]` to spawn presentation entities would run alone in its world. A command buffer records the spawns instead, and a commit applies them later (ecs/docs/design.md:421-457).
- **No deadlock.**
  - The combiner grants a job all its locks at once or none (kernel/lockstate.go:33-53, kernel/combiner.go:94-103), so no job holds one lock while waiting for another.
  - A job asks for locks only once its dependencies have finished (kernel/job.go:133-139, 161-169).
  - A waiting writer reserves what it writes, so a stream of readers cannot starve it (kernel/lockstate.go:16-19, 70-76).
  - Conflicting jobs with no dependency between them run one at a time, in no guaranteed order, with ecs or without (kernel/task.go:40, kernel/docs/design.md:238-242).
  - The kernel's one documented deadlock is a task or handler blocking on a `Signal` (kernel/docs/design.md:398-400). ecs never does that.
- **ecs's internal synchronisation stays inside its own calls.** A lane's mutex is held around one append (ecs/commands.go:353-361, 373-380, ecs/docs/design.md:449-453). Mask bits (ecs/mask.go:50-61) and reservations (ecs/entitytable.go:35, ecs/refs.go:94) use atomics. None is held across a job, and none touches a mux resource.

## What this means for mux

What works as it is:

- An ecs view takes `kernel.Read[M]` and `kernel.Read[Timeline[M, D]]` beside its queries and stores. Its lock set holds both reads and its ecs locks, and the call allocates nothing.
- The game schedules the view from its `DrawEvent` handler, after `step.Handle()`, as #780 shows for a plain view. ecs adds nothing to schedule or order.
- No ecs lock shares a resource with `M` or the timeline, and the kernel cannot deadlock on lock order. A frame serialises only on mux's own lock table and on ecs's store and barrier locks.

What it leaves open, for mux, ecs or the game:

- #780's snippet needs `.Handle()` on `step` and `view` to compile.
- Views in several plugins' `DrawEvent` handlers see `Step` only through `in`, which also carries the earlier handlers' views. #780 avoids this by having one handler schedule `Step` and every view. If views are spread across plugins, something would have to hand `Step`'s handle to them.
- `kernel.Read[T].Get()` returns a copy of `T` (kernel/access.go:116). A view reaching `&model` through `Read[M]` gets the address of a shallow copy of `M`'s top-level struct, made on each `Get`. A timeline resource that is a struct would be copied whole. ecs avoids both: its resources are private pointer types reached through its own Parameter handles (ecs/store.go:92-124). platform makes its resource an interface of reads instead (platform/resources.go:39-46). Which shape the timeline's read side takes is not yet decided, and the question is the same with ecs or without.
- A view that writes current values into components every frame through a `*T` term marks every row it yields as changed (ecs/docs/design.md:389-391). In an observed store, each such row is listed, and past an eighth of the rows the observers and the reset walk every row (ecs/docs/design.md:376-382). Only rows whose bytes moved report a delta (ecs/docs/design.md:364-366). ecs's advice for a pass that writes few rows is a `T` term plus `Store.Write(e)` (ecs/docs/README.md:318-319): here, only the entities with an active override.
- Not ecs's, but it bears on #780's "time advanced only by `Step`": gfx publishes no `DrawEvent` while its device is lost or released (gfx/framepool.go:363-371). No `Step` runs then, and the timeline's time stands still until recovery.
