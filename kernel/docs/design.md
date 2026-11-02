# kernel v2: design

The v1 kernel lives in `_legacy/kernel`. v2 keeps its resource model and lock
semantics and changes three things: several kernels run under one supervisor,
Ports and Adapters give way to plain resources, and jobs replace commands and
resource-holding subscribers.

## Alloc-free

Once a kernel is spawned, the kernel allocates nothing: not to publish, not to
schedule, not to run a job. Every API shape is chosen to keep that true, and a
shape that cannot is wrong.

- **Events travel by value.** A pointer passed through a function value
  escapes, whatever the event's size, so a handler takes the event and returns
  it (see Events).
- **Task arg and result are pointer-free**, so a job's arg and result can be
  kept without boxing (see Pointer-free data).
- **The Executor is handed by pointer** and is valid only during the
  publication it was handed for, so the kernel can reuse it.
- **Dependencies are a concrete `JobHandle`**, not an interface. Converting a
  `Job[T]` to an interface allocates once it escapes (measured: 1 per
  conversion), so `Job[T].Handle()` is explicit.
- **`deps ...JobHandle`** is never kept past the call, so the variadic slice
  stays on the caller's stack.
- **`Broadcast`** allocates nothing, so an event published into other
  kernels is never boxed into an `any`.
- **Exempt:** `Spawn`, `Destroy` and plugin `Init`/`Start`/`Stop`/`Destroy` are cold
  paths and allocate freely. Logging is too: `LogWarn`/`LogDebug` take
  `...any`, and boxing those args allocates at the call site, so they do not
  belong on a per-frame path.

The kernel's own tests should assert `testing.AllocsPerRun == 0` for
`Publish`, the `Executor` methods and job execution.

## Shape

```
Supervisor ── lifetime, GetKernel and Broadcast, LogHandler
  ├─ Kernel 1  ── resources · tasks · handlers
  └─ Kernel 2  ── resources · tasks · handlers
```

| Type         | Role                                                              |
|--------------|-------------------------------------------------------------------|
| `Supervisor` | Spawns and destroys kernels, runs pinned jobs on the main thread, ends the application (`Spawn`, `GetKernel`, `Destroy`, `Broadcast`, `TickMainThread`, `WaitMainThread`, `Quit`, `Done`); owns the one `LogHandler`. |
| `Kernel`     | Runtime handle given to handlers, tasks and the main loop: `ID`, `Publish`, `Notify`, `Logger`. It manages no kernels and schedules nothing directly. |
| `Logger`     | A kernel's way to the `LogHandler`, from `Kernel.Logger()`: `LogErr`, `LogWarn`, `LogDebug`, their `Once` forms and `ForgetLogOnce`. |
| `Signal`     | Reusable completion signal for code outside the kernel; armed by `Kernel.Notify`, read through `C()`. |
| `Executor`   | One publication's work batch, handed to its handlers: `Schedule`, `ScheduleAfter`, `Join`, `Publish`. |
| `Configurator` | Registration surface given to each plugin once: `InitResource`, `Resource`, `RegisterTask`, `PinToMainThread`, `Subscribe`, `OrderSubscriptions`. |
| `Access`     | Handed to a task's declare function: `Read[T]`, `Write[T]` and `Use(task)` declare its locks and return what the body uses. |
| `*Task[A, B]`| Registered code with pointer-free arg `A` and pointer-free result `B`; returned by `RegisterTask`. |
| `Job[T]`     | One scheduled run answering `T`; what `ScheduleAfter` feeds from. |
| `JobHandle`  | Untyped job handle, `Job[T].Handle()`; what dependencies are declared with. |
| `Plugin`     | Interface: `Name()`, `Init(configurator, config)`, `Start(kernel)`, `Stop(kernel)`, `Destroy()`. |

## Supervisor

- Kernels' resources are disjoint, so their locks never conflict. Nothing a
  job holds can be reached from another kernel.
- There is no `Run`. `main` spawns kernels, then lends the main thread to
  pinned jobs by calling `TickMainThread` until `Done` closes (see The main
  thread and pacing).

### Spawn and destroy

Kernels come and go on demand. Only the `Supervisor` spawns, destroys and
quits: a kernel does not manage kernels. A plugin that needs to is given
the `*Supervisor` in its config, and every Supervisor method is safe from its
jobs and handlers.

- **Spawn** runs every plugin's `Init`, then every `Start`, on the calling
  goroutine. The kernel is running when it returns. If any of them fails, the
  plugins already started are stopped in reverse, the plugins already
  initialized are destroyed in reverse, and nothing is left behind.
  Registration is paid on every spawn.
- **Destroy** never waits, so a kernel can destroy itself from its own job. The
  kernel stops being reachable, calls `Stop` on its plugins in reverse, finishes
  the jobs it already has (including those `Stop` scheduled), calls `Destroy`
  on its plugins in reverse, then is removed.
  Other kernels are not told.
- **Failure is the LogHandler's call.** The kernel never ends itself over an
  error. `LogErr` is told, and a LogHandler that wants the failing kernel gone
  calls `Destroy(source)`, or `Quit` if the game cannot live without it.
- **Quit** is the only end. It destroys every live kernel, refuses further
  spawns, and closes `Done` once all are gone. Running out of kernels does not
  end the application, since a game may destroy its last kernel just to spawn
  the next.

A kernel's config is given to `Spawn`, its constructor:
`Spawn(config map[PluginName]any, workerCount, plugins...) (KernelID, error)`.
Plugins cannot change it. `workerCount` is how many goroutines run the kernel's jobs, chosen
by the caller: `runtime.GOMAXPROCS(0)` keeps every CPU busy, and 1 runs the
kernel sequentially (see Sequential kernels). Below 1 is a composition error
and panics.

**Kernels are named by ID.** `Spawn` answers a `KernelID` from a counter the
supervisor increments atomically, so it is unique for the process's life and
never reused. `GetKernel`, `Destroy` and every log line take it. An
ID kept past its kernel's destroy names no kernel, rather than whichever was
spawned next. A plugin that needs another kernel's ID is given it in its
config, as it is given the supervisor.

### Interop

Kernels reach each other only by publishing into one another:
`Supervisor.GetKernel(id).Publish(event)` for one kernel,
`Supervisor.Broadcast(event)` for every live kernel in spawn order. The event
is copied in and published like any other: the target's handlers run on the
publisher's goroutine, and the jobs they schedule run in the target kernel.
There is no reply: a reply is another publish going back, and a handler
replying that way recurses, so an exchange between kernels needs a condition
that ends it.

Neither allocates. An event nothing in the target subscribes to is dropped
silently.

## Plugins

`Plugin` is an interface with `Name()`, `Init(configurator, config)`,
`Start(kernel)`, `Stop(kernel)` and `Destroy()`. It has no dependency list:

- **Name** is a `PluginName`, unique among one kernel's plugins; two plugins
  of one name are a composition error and panic in `Spawn`. It keys the
  plugin's part of the config.
- **Order** is the order of the plugins passed to `Spawn`. A plugin
  that needs another's resource during registration is listed after it and calls
  `Configurator.Resource[T]`. `Start` runs in that order, `Stop` in reverse.
- **Init and Destroy are a pair,** as Start and Stop are. Whatever `Init`
  acquires, such as an implementation it installs with `InitResource`,
  `Destroy` releases. `Destroy` runs after the kernel's last job, for every
  plugin whose `Init` succeeded, started or not.
- **Config** is a `map[PluginName]any` per kernel, given to `Spawn`. Each
  plugin's `Init` is handed only its own part, the value under its `Name`, or
  nil if there is none.
- **Ports and Adapters are gone.** A slot declares what it needs as a resource
  of interface type, and the extension that fills it calls
  `InitResource[gfx.Backend](impl)`. Any number of contributors is a resource
  holding a slice that each contributor appends to during registration.

## Tasks and jobs

A **Task** is registered code that touches resources. A **Job** is one run of
a task. Tasks are the only code that holds locks, and jobs are only ever
scheduled from inside a publication (see Executor).

```go
type DamageArg struct{ Target EntityID; Amount int32 }
type Dead struct{ Target EntityID; Dead bool }

// in Init:
c.damage = configurator.RegisterTask(func(access *kernel.Access) kernel.TaskBody[DamageArg, Dead] {
	health := access.Write[Health]()
	pos := access.Read[Position]()
	return func(k *kernel.Kernel, arg DamageArg) Dead { /* health.Get(), health.Set(...), pos.Get() */ }
})
c.announce = configurator.RegisterTask(func(access *kernel.Access) kernel.TaskBody[Dead, kernel.NoResult] {
	return func(k *kernel.Kernel, d Dead) kernel.NoResult { /* ... */ }
})
```

- **Requesting a handle is declaring the lock.** `RegisterTask` runs the
  declare function once. Each `access.Read[T]()` or `access.Write[T]()` both
  declares the lock and returns the handle the body uses, so the two cannot
  drift apart. A write supersedes a read of the same resource. The body is a
  `TaskBody[A, B]`, `func(k *Kernel, arg A) B`, closing over its handles. A
  task that locks nothing requests no handle, takes nothing `NoArg`, answers
  nothing `NoResult`.
- **Calling another task inline.** `access.Use(task)` folds `task`'s locks
  into this task's and returns the direct call, which the body runs inside its
  own job with those locks already held: no scheduling, no allocation, the
  result returned directly. Pinning does not apply to such a call.
- **Composition errors panic.** A resource not yet initialized, an arg or
  result that is not pointer-free, a nil declare function or body, or a task
  of another kernel passed to `Use` panics at registration, so the mistake
  surfaces in `Spawn` rather than at the first run. `A` and `B` have no size
  limit.
- **A task is a pointer, not a type.** `RegisterTask` returns a `*Task[A, B]`
  belonging to that kernel. A plugin keeps it, usually in a field of its own
  struct. One plugin value per kernel keeps each kernel's tasks apart.
- **Running allocates nothing.** The body is created once, at registration,
  so its captures are allocated once; scheduling and running a job allocate
  nothing.
- **Typed jobs, untyped dependencies.** Scheduling returns a `Job[B]`, typed
  by the task's result. Dependencies are declared with `JobHandle`, which
  `Job[B].Handle()` gives, so any job can wait on any other while only a
  matching job can feed one. `JobHandle` is a concrete struct, not an
  interface, because boxing a `Job[B]` allocates. A `JobHandle` kept past its
  job reads as finished.
- **Concurrency is the lock set's.** Jobs of one task run at once when it
  declares only reads, and one at a time when it declares a write.
  Many writers to different parts of one resource are better batched into one
  job than split across many.
- **No order between conflicting jobs.** Jobs whose locks conflict run one at
  a time, in no guaranteed order. The one promise is against starvation: a
  waiting writer reserves what it writes, so no job that became ready after it
  locks those resources first. Work that must happen in an order says so with
  a dependency.
- **Jobs always complete.** There is no failed job. A body that panics is
  recovered, its panic and stack go to `LogErr`, and the job finishes with the
  zero result, so its dependents, and the jobs fed by its result, still run.
- **Pinning.** `Configurator.PinToMainThread(task)` makes the task's jobs run
  on the main thread, for window and GPU calls a
  platform allows on one thread only. A pinned job waits for dependencies and
  locks like any other, and can feed and be depended on; only where it runs
  changes. The main thread runs them when `main` calls
  `Supervisor.TickMainThread` (see The main thread and pacing).

### Pointer-free data

Every task arg, task result and event must contain no pointers at any depth:
no pointers, slices, maps, strings, interfaces, channels or funcs.
`RegisterTask` checks a task's arg and result, and `Subscribe` checks its
event, and either fails registration otherwise. An event nothing
subscribes to is never delivered, so events published into another kernel
are covered too.

This buys:

- **The GC leaves the kernel alone.** Work in flight holds no pointers, so
  the GC never scans it, however much there is.
- **No leaks of locked state.** Nothing a task or handler passes on can point
  into a resource, so nothing it saw under a lock outlives the lock (a rule
  v1 could only state in a comment).
- **Values copy freely.** Every job fed by one result gets its own copy, and
  an event crosses into another kernel by plain copy.

Strings are not allowed. Text a task needs travels as a hash or an ID, with
the text itself kept in a resource; so does any other bulk data (paths,
buffers, lists).

## Executor

An `Executor` is one publication's work batch. `Kernel.Publish` hands one to
each handler in turn, and submits the whole batch once the last handler
returns. Nothing scheduled on it
starts before then, so every job of the batch is still pending while the
handlers build it.

```go
func (ex *Executor) Schedule[A, B any](task *Task[A, B], arg A, deps ...JobHandle) Job[B]
func (ex *Executor) ScheduleAfter[A, B any](task *Task[A, B], from Job[A], deps ...JobHandle) Job[B]
func (ex *Executor) Join(deps ...JobHandle) JobHandle
func (ex *Executor) Publish[E any](in JobHandle, event E) (E, JobHandle)
```

- **`Schedule`** adds a job with the arg given, to start once every job in
  `deps` has finished and the task's locks are free.
- **`ScheduleAfter`** is `Schedule` with `from`'s result as the arg. The job
  starts after `from` and `deps`, and every job scheduled after the same
  `from` gets its own copy of the result. The types line up by construction: a
  `Job[A]` can only feed a `Task[A, _]`. `from` must belong to the same batch,
  since a result does not outlive the batch it was produced in.
- **`Join`** adds an empty job that finishes once all of `deps` have. It is
  how several jobs travel on as one dependency.
- **`Publish`** on the executor is a nested publication into the same batch,
  so its handlers' jobs can depend on and feed jobs already scheduled. A
  handler publishing on its `*Kernel` instead starts a separate batch, which
  is correct but cannot feed this one.
- **Async only.** Nothing waits inline. That rules out v1's nested synchronous
  dispatch and the lock-closure folding it needed.
- **Why scheduling lives here.** The kernel has no `Run`. Every job is born in
  a publication's batch, which is what makes `ScheduleAfter` safe: a producer
  cannot finish before the consumer is attached. A task or a plugin's `Start`
  that wants work done publishes an event.

## Execution

- **Where jobs run is the kernel's business.** A job starts once its
  dependencies have finished and its locks are free; nothing is promised
  about which goroutine runs it, except for pinned jobs.
- **Workers.** Each kernel runs its jobs on its own `workerCount` goroutines,
  started before the first `Start` and ended once the kernel has drained. An
  idle worker parks and is woken when a job becomes ready.
- **Destroy completes.** A destroyed kernel is removed once its plugins'
  `Stop` has run and every job it has, including those `Stop` scheduled, has
  finished. From the moment the `Stop`s return, `Publish` publishes nothing,
  so the jobs left can only run out. That includes pinned jobs, so a kernel's
  destroy waits on the main thread to tick them.

### Sequential kernels

`Supervisor.Spawn(config, 1, plugins...)` spawns a kernel whose jobs all run
one at a time, on its one worker. It suits a kernel that wants sequential
execution, such as a server simulation, where parallelism comes from running
many kernels.

- A job still waits for its dependencies, and runs in the order it became
  ready.
- Locks never contend, since nothing runs alongside a job.
- A task pinned to the main thread still runs there.
- **No kernel is tied to an OS thread.** Only the main thread is, for pinned
  jobs. A worker locked to its own thread was measured against one that is
  not: on mostly idle servers the locked one cost twice the CPU and three to
  four times the tick latency, because waking it means waking that one OS
  thread, and under full load it gained nothing.

### The main thread and pacing

`main` spawns kernels, then ticks the main thread until the application
ends.

```go
func init() { runtime.LockOSThread() } // keep main on the main thread

func main() {
	s := kernel.NewSupervisor(nil)
	// ... spawn kernels
	for s.WaitMainThread() {
		s.TickMainThread()
	}
}
```

- **Kernels pace themselves.** Nothing in `main` drives frames. A plugin with
  a timer runs its own goroutine and publishes its kernel's update event on
  each tick, so kernels run at different rates and none waits on another.
- **`Supervisor.TickMainThread()`** runs every pinned job
  (`Configurator.PinToMainThread`) pending so far, and returns. It never
  waits: with nothing pending it returns at once.
- **`Supervisor.WaitMainThread()`** blocks until a pinned job may be pending
  or `Done` closes, and answers false once `Done` has closed, so main idles
  without spinning a core between ticks. It may wake with nothing pending.
- **Platform loops become pinned tasks.** gogpu's per-frame window and GPU
  work, event polling included, is pinned tasks its plugin schedules from its
  own timer or vsync events. A pinned job must return: one that never does
  holds the main thread for good.
- **A kernel's destroy cannot end the main thread,** because no kernel owns
  it. `Quit` does, through `Done`.
- **Main ticks until `Done`.** A kernel being destroyed waits for its pinned
  jobs, so a main loop that stops ticking early leaves that destroy, and so
  `Quit`, waiting for good.

### Waiting from outside

Code outside the kernel, a test or a tool, reaches a kernel with
`Supervisor.GetKernel(id)` and learns when its jobs are done through a
reusable `Signal`:

```go
game := s.GetKernel(gameID) // the KernelID Spawn answered
sig := kernel.NewSignal() // once; allocates once
_, done := game.Publish(Hit{Target: 1, Damage: 3})
game.Notify(sig, done) // signals once done finishes, or now if it has
<-sig.C()
```

- **`Kernel.Notify(sig, job)`** signals `sig` once `job` has finished, or at
  once if it has, so no finish is missed however late it is called. A `JobHandle` belongs to one kernel, so `Notify` lives on that
  kernel and names none.
- **`Signal`** holds one value and is sent to without blocking, so a job is
  never held up by a slow reader, and one `Signal` reused many times
  allocates nothing.
- **Only outside code waits, and never the main thread.** A task or handler
  blocking on a `Signal` can deadlock the kernel. The main thread blocking on
  one can deadlock a pinned job the waited-on work depends on.
- **A `*Kernel` kept past its destroy** stays safe to call: `Publish`
  publishes nothing and `Notify` signals at once.

## Events

- `Publish` is synchronous. Handlers run one by one, in their settled order,
  on the publishing goroutine, and `Publish` returns after the batch is
  submitted.
- **Order.** `Subscribe` returns a `Sub[E]`, and
  `Configurator.OrderSubscriptions(before, after)` declares that one handler
  runs before another. Once every plugin has registered, each event's
  handlers are sorted to satisfy every declaration, keeping subscription
  (plugin) order wherever none decides. A cycle fails registration. `Sub` is
  typed by its event, so only two subscriptions to the same event can be
  ordered. Any plugin holding both may order them. A plugin that lets others
  order around its handlers publishes its subscriptions in a resource.
- **The handler signature:**

  ```go
  type SubscriptionFunc[E any] func(k *Kernel, ex *Executor, in JobHandle, event E) (E, JobHandle)
  ```

  `in` is the previous handler's out dependency (the zero `JobHandle` for the
  first). A handler returns the event and its own out dependency: a job it
  scheduled, `ex.Join(...)` of several, or `in` unchanged. `Publish` answers
  the last handler's pair, so the publisher can order later work after
  everything the handlers started.
- **Value in, value out.** A handler changes the event by returning a changed
  copy, and the next handler receives that. A pointer would be simpler to
  write, but handing `&event` to a handler moves it to the heap at any size
  (measured: 1 alloc per publish, versus 0 by value). The cost is a copy of
  the event in and out of each handler, so events stay small.
- Handlers declare no locks and get no resource handles. A handler's work is
  to decide what to schedule, and to adjust the event.
- **Example:**

  ```go
  configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, e HitEvent) (HitEvent, kernel.JobHandle) {
  	hit := ex.Schedule(c.damage, DamageArg{Target: e.Target, Amount: e.Damage}, in)
  	said := ex.ScheduleAfter(c.announce, hit) // gets hit's Dead
  	return e, ex.Join(hit.Handle(), said.Handle())
  })
  ```

  An event is pointer-free, so it can be the arg: a `Task[HitEvent, B]`.
  The closure above captures `c`, but `Subscribe` runs at registration, so it is
  allocated once, not per publish.
- A task may publish. Its handlers run inline in the job while the job's
  locks are held, which is safe because handlers touch no resources.
- **A `JobHandle` means something only in its own kernel.** An event carrying
  one into another kernel arrives meaningless.

## Errors and logging

There is one `LogHandler` per supervisor, given to `NewSupervisor`.
Every method takes the source `KernelID` first: `LogErr(source, err)`,
`LogWarn(source, msg, args...)`, `LogDebug(source, msg, args...)`.
`Kernel.Logger()` returns the kernel's `Logger`, whose methods fill in its
ID. The kernel formats nothing and writes nothing itself.

The LogHandler is the only place errors go, and it is also what decides what they
mean. There is no error handler and no verdict: the kernel forwards the error
and carries on. A LogHandler that wants a failing kernel gone calls
`Supervisor.Destroy(source)`, and one that wants the application gone calls
`Quit` or panics. An application that wants the errors back implements a
LogHandler that keeps them. `LogWarn` and `LogDebug` are slog-style.

A job whose body panics is recovered, and the panic passed to `LogErr` with
its stack. That covers a handler panicking in a publication the body made,
though the batch that publication was building is lost. A panic anywhere
else, such as a handler run by a `Publish` from outside a job, is not the
kernel's to recover: a panic is a terminal state. A panic raised by `LogErr`
itself is not recovered again, so it ends the process. That is what
`StdLogger` does on purpose.

**Once forms.** `LogErrOnce`, `LogWarnOnce` and `LogDebugOnce` take a
string key and log only the first call under it, until `ForgetLogOnce(key)`
clears it. They are for a condition true every frame: worth saying once,
noise at the frame rate. The key names the condition, such as
`"sprite:missing:" + path`, and keys of different kernels never collide. A
constant key costs nothing to pass; one built at run time allocates, which
logging, being exempt from alloc-free, may do. A condition
that can be repaired must be forgotten, or it stays silent for the kernel's
life. Deduplication is the LogHandler's: the kernel only forwards.

A nil LogHandler means a new `StdLogger`: `LogErr` panics, `LogWarn` writes to
stderr, `LogDebug` to stdout, each prefixed `[kernel N]`, and the Once forms
keep their keys in a map. Every error is fatal under it, which suits
development.

Every method other than a getter (`ID`, `Done`, `Logger`,
`Configurator.Resource`, `Read.Get`) is named for what it does, starting with
a verb. Internally, paired verbs follow rules/GO-RULES.md.

## Implementation

How the kernel keeps its promises, and why it does so this way. The code
comments carry the detail; this is the shape.

- **Storage is pools of slots.** Jobs, successor edges, signal edges and each
  task's args live in `container.Pool`s: chunks that never move, addressed by
  `uint32` index, reused most recent first. A job slot carries a generation,
  advanced each time the slot is released, so a stale `JobHandle` reads as
  finished rather than naming the slot's next job.
- **The job graph owns waiting.** Only the job graph counts what a job
  still waits for (its dependencies and its batch's hold), so only it decides
  when a job is ready. It hands each ready job to the kernel, which routes it
  to the lock combiner, the workers or the main thread; finishing a job there
  sends its Signals, feeds its result and hands on its successors.
- **A job's successors are edge lists, not queues.** Each of a job's two
  lists (dependent jobs, armed `Signal`s) is one atomic `uint64`: the slot's
  generation in the high 32 bits, the head edge in the low 32. Linking is one
  compare-and-swap that succeeds only while the generation is the handle's
  and the list is not closed, so a dependent added after the job finished
  learns that at once, and a stale handle is refused. Finishing swaps in the
  closed marker and walks what it took. A mutex per job would guard the same
  invariant across several fields; packing it into one word makes it one
  compare-and-swap, and contention is per job and rare. Signal edges hold a
  pointer, so they have their own pool and the job and successor pools stay
  pointer-free.
- **Shared queues use a mutex.** Measured against lock-free versions, the
  mutex `container.Queue` and `Pool` lose little uncontended and avoid
  compare-and-swap storms under contention, without a tuned backoff.
- **A batch holds its jobs.** Every job starts waiting on one hold besides its
  dependencies; `submit` releases the batch's holds once the last handler
  returns, which is what keeps every job of a batch pending while handlers
  build it.
- **Locks are granted by a combiner.** A task's locks are two bit masks
  (reads, writes) over the kernel's resources, as long as its highest
  resource needs. A job whose dependencies have finished is queued for the
  lock combiner, which whichever goroutine wins a `TryLock` becomes: it
  unlocks the resources of finished jobs, then walks the waiting jobs oldest
  first, granting each whose locks are free and letting each that cannot run
  reserve what it writes. It looks at the queues again after unlocking, so no
  queued work is stranded. A job that locks nothing skips it.
- **Workers park on a token channel.** A worker that finds nothing ready
  counts itself idle, looks once more, and parks; a job made ready while any
  worker is idle puts a token in a channel with a slot per worker, so no wake
  is lost and a busy kernel pays one atomic load per job for it.
- **Draining counts what is outstanding.** One counter covers every job not
  yet finished and every `Publish` still running. A `Publish` counts itself
  before checking whether the kernel is closed, so a drain never sees zero
  while a publication that passed the check still runs.

## Decided against

- **`Exclusive`.** v1 let a handler refuse to run alongside itself, to guard
  mutable state in its factory closure. A v2 task's state lives in the
  resources it locks, which already guards it, so v2 has no `Exclusive`.
- **Policing payloads between kernels.** An event published into another
  kernel is copied as it is and never looked inside. A field that means something only in the sending kernel, such as a
  `JobHandle`, arrives meaningless, and keeping such fields out is the
  sender's business.
- **Resources at runtime.** Resources are created only during registration,
  and none is added or removed while a kernel runs. A resource that has no
  value yet is created empty instead: `InitResource[gfx.Backend](nil)`, filled
  later by a task holding its `Write`.

## Deferred

- **Introspection.** v1 could describe its handlers and lock sets, report
  contention, and trace the scheduler. Each task's lock set is known at
  registration, so this can be added without changing the API.
