# kernel v2: design

The v1 kernel lives in `_legacy/kernel`. v2 keeps its resource model and lock
scheduler and changes three things: several kernels run under one supervisor,
Ports and Adapters give way to plain resources, and jobs replace commands and
resource-holding subscribers.

## Alloc-free

Once a kernel is spawned, the kernel allocates nothing: not to publish, not to
schedule, not to run a job. Every API shape is chosen to keep that true, and a
shape that cannot is wrong.

- **Events travel by value.** A pointer passed through a function value
  escapes, whatever the event's size, so a handler takes the event and returns
  it (see Events).
- **Task arg and result are POD** and stored typed in each task's own job
  storage, beside handles and a caller cached per task at registration (see
  Tasks and jobs).
- **The Executor is pooled.** A stack `Executor` handed to handlers by pointer
  moves to the heap (measured), so `Publish` takes one from the kernel's pool
  and returns it after submitting. A pointer to a pooled object costs nothing
  to pass.
- **Dependencies are a concrete `JobHandle`**, not an interface. Converting an
  8-byte `Job[T]` to an interface allocates once it escapes (measured: 1 per
  conversion), so `Job[T].Handle()` is explicit.
- **`deps ...JobHandle`** is copied into the job's slot, never kept, so the
  variadic slice stays on the caller's stack.
- **`Send`** needs a typed inbox per event type, not a queue of `any`, since
  boxing allocates.
- **Exempt:** `Spawn`, `Destroy` and plugin `Register`/`Start`/`Stop` are cold
  paths and allocate freely. Logging is too: `LogWarn`/`LogDebug` take
  `...any`, and boxing those args allocates at the call site, so they do not
  belong on a per-frame path.

The kernel's own tests should assert `testing.AllocsPerRun == 0` for
`Publish`, the `Executor` methods and job execution.

## Shape

```
Supervisor ── worker pool, lifetime, Send routing
  ├─ Kernel 1  ── resources · jobs · handlers · job graph
  └─ Kernel 2  ── resources · jobs · handlers · job graph
```

| Type         | Role                                                              |
|--------------|-------------------------------------------------------------------|
| `Supervisor` | Spawns and destroys kernels, runs pinned jobs on the main thread, ends the application (`Spawn`, `SpawnSingleThreaded`, `GetKernel`, `Destroy`, `Send`, `TickMainThread`, `Quit`, `Done`); owns the one `Logger`. |
| `Kernel`     | Runtime handle given to handlers, tasks and the main loop: `Publish`, `Notify`, `LogErr`, `LogWarn`, `LogDebug` and their `Once` forms. It manages no kernels and schedules nothing directly. |
| `Signal`     | Reusable completion signal for code outside the pool; armed by `Kernel.Notify`, read through `C()`. |
| `Executor`   | One publication's work batch, pooled, handed to its handlers: `Schedule`, `ScheduleAfter`, `Join`, `Publish`. |
| `Configurator` | Registration surface given to each plugin once: `InitResource`, `Resource`, `RegisterTask`, `PinToMainThread`, `Subscribe`, `OrderSubscriptions`. |
| `*Task[A, B]`| Registered code with a resource struct, POD arg `A` and POD result `B`; returned by `RegisterTask`, nil on failure. |
| `Job[T]`     | One scheduled run answering `T`; what `ScheduleAfter` feeds from. |
| `JobHandle`  | Untyped job handle, `Job[T].Handle()`; what dependencies are declared with. |
| `Plugin`     | Interface: `Register(configurator, config)`, `Start(kernel)`, `Stop(kernel)`. |

## Supervisor

- Owns **one worker pool** for every kernel. Kernels' resources are disjoint,
  so their locks never conflict, and one pool sized to `GOMAXPROCS` avoids
  oversubscribing the machine with a pool per kernel. Its workers are the
  only goroutines the kernel package runs, apart from the one thread of each
  single-threaded kernel (see Execution).
- Each kernel owns its own lock tables and job graph. Nothing a job holds can
  be reached from another kernel.
- There is no `Run`. `main` spawns kernels, then lends the main thread to
  pinned jobs by calling `TickMainThread` until `Done` closes (see The main
  thread and pacing).

### Spawn and destroy

Kernels come and go on demand. Only the `Supervisor` spawns, destroys, sends
and quits: a kernel does not manage kernels. A plugin that needs to is given
the `*Supervisor` in its config, and every Supervisor method is safe from its
jobs and handlers.

- **Spawn** runs every plugin's `Register`, then every `Start`, on the calling
  goroutine. The kernel is running when it returns. If any of them fails, the
  plugins already started are stopped in reverse and nothing is left behind.
  Registration cost (one reflect pass per task) is paid on every spawn.
- **Destroy** never waits, so a kernel can destroy itself from its own job. The
  kernel stops accepting `Send`, calls `Stop` on its plugins in reverse, finishes
  the jobs it already has (including those `Stop` scheduled), then is removed.
  Other kernels are not told.
- **Failure is the Logger's call.** The kernel never ends itself over an
  error. `LogErr` is told, and a Logger that wants the failing kernel gone
  calls `Destroy(source)`, or `Quit` if the game cannot live without it.
- **Quit** is the only end. It destroys every live kernel, refuses further
  spawns, and closes `Done` once all are gone. Running out of kernels does not
  end the application, since a game may destroy its last kernel just to spawn
  the next.

A kernel's config is given to `Spawn`, its constructor:
`Spawn(config, plugins...) (KernelID, error)`. Plugins cannot change it.

**Kernels are named by ID.** `Spawn` answers a `KernelID` from a counter the
supervisor increments atomically, so it is unique for the process's life and
never reused. `GetKernel`, `Destroy`, `Send` and every log line take it. An
ID kept past its kernel's destroy names no kernel, rather than whichever was
spawned next. A plugin that needs another kernel's ID is given it in its
config, as it is given the supervisor.

### Interop

`Supervisor.Send(to, event)` is the only link between kernels. The supervisor
copies the event into the target kernel's inbox, and that kernel publishes it
like any other event. It is rare by design, so it has no synchronous form and
no reply: a reply is another `Send` going back.

Each kernel has one inbox per event type it subscribes to, made at
registration, so `Send` never allocates. An event nothing in the target
subscribes to is dropped silently.

## Plugins

`Plugin` is an interface (`plugin.go`) with `Register(configurator, config)`,
`Start(kernel)` and `Stop(kernel)`. It has no name and no dependency list:

- **Order** is the order of the plugins passed to `Spawn`. A plugin
  that needs another's resource during registration is listed after it and calls
  `Configurator.Resource[T]`. `Start` runs in that order, `Stop` in reverse.
- **Config** is one value per kernel, given to `Spawn` and handed to every
  plugin's `Register`. Each plugin takes from it what it needs.
- **Ports and Adapters are gone.** A slot declares what it needs as a resource
  of interface type, and the extension that fills it calls
  `InitResource[gfx.Backend](impl)`. Any number of contributors is a resource
  holding a slice that each contributor appends to during registration.

## Tasks and jobs

A **Task** is registered code that touches resources. A **Job** is one run of
a task. Tasks are the only code that holds locks, and jobs are only ever
scheduled from inside a publication (see Executor).

```go
type damageRes struct {
	Health kernel.Write[Health]
	Pos    kernel.Read[Position]
}
type DamageArg struct{ Target EntityID; Amount int32 }
type Dead struct{ Target EntityID; Dead bool }

func applyDamage(k *kernel.Kernel, res damageRes, arg DamageArg) Dead { /* ... */ }
func announce(k *kernel.Kernel, res kernel.NoResources, d Dead) kernel.NoResult { /* ... */ }

// in Register:
c.damage = configurator.RegisterTask(applyDamage) // *Task[DamageArg, Dead]
c.announce = configurator.RegisterTask(announce)  // *Task[Dead, NoResult]
```

- **Resources are the signature.** A task's body is a `TaskFunc[R, A, B]`:
  `func(k *Kernel, res R, arg A) B`, where `R` is a struct whose every field
  is a `Read[T]` or `Write[T]`. Declaring a field is declaring the lock, so the
  two cannot drift apart. A task that locks nothing uses `NoResources`, takes
  nothing `NoArg`, answers nothing `NoResult`.
- **Registration does the reflection.** `RegisterTask` walks `R` once, binds
  its handles, compiles its lock set, and checks that `A` and `B` are POD. They
  have no size limit. A missing resource or a bad type is a registration
  failure, so `Spawn` fails rather than the first run.
- **A task is a pointer, not a type.** `RegisterTask` returns a `*Task[A, B]`
  belonging to that kernel, or nil if registration failed. A plugin keeps it,
  usually in a field of its own struct. The `Task` carries its bound handles,
  lock set and caller, so scheduling reads them off the pointer with no table
  lookup. It is allocated once, at registration. One plugin value per kernel keeps
  each kernel's tasks apart.
- **Running allocates nothing.** Scheduling copies the arg into a slot of the
  task's own typed job storage, next to its cached handles and caller.
  Prototyped at 0 allocs and about 15 ns per run. The body should be a
  top-level func or a closure that captures nothing: a capturing closure
  allocates once per call.
- **Typed jobs, untyped dependencies.** Scheduling returns a `Job[B]`, typed
  by the task's result. Dependencies are declared with `JobHandle`, which
  `Job[B].Handle()` gives, so any job can wait on any other while only a
  matching job can feed one. `JobHandle` is a concrete struct of slot and
  generation, not an interface, because boxing a `Job[B]` allocates.
- **Concurrency is the lock set's.** Jobs of one task run at once when its
  `R` holds only `Read` fields, and one at a time when it holds a `Write`.
  Many writers to different parts of one resource are better batched into one
  job than split across many.
- **No order between conflicting jobs.** Jobs whose locks conflict run one at
  a time, in no guaranteed order. Work that must happen in an order says so
  with a dependency.
- **Jobs always complete.** There is no failed job. A body that panics is
  recovered, its panic and stack go to `LogErr`, and the job finishes with the
  zero result, so its dependents, and the jobs fed by its result, still run.
- **Pinning.** `Configurator.PinToMainThread(task)` makes the task's jobs run
  on the main thread instead of a pool worker, for window and GPU calls a
  platform allows on one thread only. A pinned job waits for dependencies and
  locks like any other, and can feed and be depended on; only where it runs
  changes. The main thread runs them when `main` calls
  `Supervisor.TickMainThread` (see The main thread and pacing).

### Plain old data

Every task arg, task result and event must contain no pointers at any depth:
no pointers, slices, maps, strings, interfaces, channels or funcs.
`RegisterTask` checks a task's arg and result, and `Subscribe` checks its
event, with reflect, and either fails registration otherwise. An event nothing
subscribes to is never delivered, so `Send` is covered too.

This buys:

- **The GC leaves the kernel alone.** Job storage and inboxes hold no
  pointers, so the GC never scans them, however much work is in flight.
- **No leaks of locked state.** Nothing a task or handler passes on can point
  into a resource, so nothing it saw under a lock outlives the lock (a rule
  v1 could only state in a comment).
- **Values copy freely.** Every job fed by one result gets its own copy, and
  an event crosses into another kernel by plain copy.

Strings are not allowed. Text a task needs travels as a hash or an ID, with
the text itself kept in a resource; so does any other bulk data (paths,
buffers, lists).

### Job storage

Arg and result have no size limit. Each task stores its own jobs' arg and
result typed, in storage of its own:

- **Chunked, never moved.** Storage grows by adding a chunk, and a slot never
  moves once allocated, so a worker reading one never takes a lock.
- **Lock-free free list.** Free slots are kept on a lock-free stack. Only
  growth, when the stack is empty, takes a mutex and allocates, so steady
  state allocates nothing and never touches the mutex.
- **`JobHandle` stays 8 bytes.** The kernel-wide job graph names a job by
  slot and generation and knows which task's storage holds it.
- **Results are read in place.** A job fed by `ScheduleAfter` reads its
  producer's result straight out of the producer's slot, which stays alive
  until every consumer has read it.

## Executor

An `Executor` is one publication's work batch. `Kernel.Publish` takes one from
the kernel's pool, hands it to each handler in turn, and submits the whole
batch to the scheduler once the last handler returns. Nothing scheduled on it
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

### No scheduler thread

A kernel owns no goroutine. It is passive data: resources, tasks, its job
graph and a lock table behind a mutex. Scheduling happens inline, on whichever
goroutine is already working:

- **Submit.** At the end of `Publish`, the publishing goroutine adds the batch
  to the kernel's job graph and lock table. Jobs whose dependencies have all
  finished and whose locks are free are ready, and go to the pool.
- **Complete.** When a job finishes, the worker that ran it releases its
  locks, marks dependents whose last dependency just finished, and hands any
  that became ready to the pool. A job fed by its result reads it in place
  later (see Job storage).

Readiness is decided inside the kernel, because dependencies and locks are the
kernel's. The pool only ever receives jobs that can run now, as
`{kernel, slot}` pairs.

The only goroutines are the supervisor's workers, plus whoever calls
`Publish`, `Spawn` or `Send`, and the one thread of each single-threaded
kernel.

- **Delivered events.** A `Send` queues a "publish this event in kernel X"
  item on the pool. A worker runs that publication, which builds and submits a
  batch like any other.
- **Destroy.** Each kernel counts its unfinished jobs. Once its plugins'
  `Stop` has run and that count reaches zero, the worker that brought it to
  zero removes the kernel.

### Single-threaded kernels

`Supervisor.SpawnSingleThreaded(config, plugins...)` spawns a kernel that is
the one exception to "a kernel owns no goroutine". All its jobs run one at a
time, on one goroutine of its own locked to an OS thread, instead of on the
pool. It suits a kernel that wants sequential execution, such as a server
simulation.

- A job still waits for its dependencies, and runs in the order it became
  ready.
- Locks never contend, since nothing runs alongside a job, so the kernel's
  lock table is never a cost.
- A task pinned to the main thread still runs there.
- The thread ends with the kernel.

### Pool: the plan

The pool sits behind a small internal interface, `push(batch)` and `pop()`,
so it can be replaced without touching the kernel.

1. **A global queue that takes batches.** This design produces ready work in
   batches: a submitted publication, or one job's completion readying several
   dependents. A ring buffer behind a mutex takes a whole batch under one lock
   and wakes only as many idle workers as there are new jobs. A channel would
   cost a lock and a wake-up per job.
2. **A run-next slot per worker.** When a job's completion readies exactly
   one dependent, the worker runs it next itself, with no queue involved (Go's
   runtime does the same with `runnext`). A `ScheduleAfter` chain then costs
   zero queue operations, and the consumer runs on the core where the
   producer's result was just written. When several dependents become ready,
   the worker keeps one and pushes the rest as a batch.

Two rules hold for any pool:

- **A worker never blocks on push.** A worker blocked handing off dependents
  can deadlock the pool, with every worker waiting and none popping. Job slots
  are preallocated, so ready work is bounded by the slot count and the queue
  is sized to hold it all.
- **Spin before parking.** Frame work arrives in bursts. A worker spins about
  a microsecond before it sleeps, so a burst does not pay a wake-up per
  worker.

### Improvement proposals

These wait for a profile that asks for them.

- **Work stealing.** Each worker gets its own lock-free deque. A worker
  pushes newly ready jobs to its own deque and pops them in LIFO order, and
  an idle worker steals from another's. The global queue stays as the
  injector for batches submitted from outside the pool, such as a `Publish`
  caller that is not a worker. This is how Rayon, Tokio and Go's scheduler
  scale. It costs a lock-free deque written with atomics, and careful
  parking and wake-up logic. It would replace option 1 behind the same
  interface, keeping option 2.
- **The kernel's lock table.** Every completion takes its kernel's mutex to
  release locks and ready dependents. That is a likelier first bottleneck
  than the pool's queue. If it shows, the table can be sharded by resource,
  or releases batched per worker.

### The main thread and pacing

`main` spawns kernels, then ticks the main thread until the application
ends.

```go
func init() { runtime.LockOSThread() } // keep main on the main thread

func main() {
	s := kernel.NewSupervisor(nil)
	// ... spawn kernels
	for {
		s.TickMainThread()
		select {
		case <-s.Done():
			return
		default:
		}
	}
}
```

- **Kernels pace themselves.** Nothing in `main` drives frames. A plugin with
  a timer runs its own goroutine and publishes its kernel's update event on
  each tick, so kernels run at different rates and none waits on another.
- **`Supervisor.TickMainThread()`** runs every pinned job
  (`Configurator.PinToMainThread`) pending so far, and returns. It never
  waits: with nothing pending it returns at once.
- **Platform loops become pinned tasks.** gogpu's per-frame window and GPU
  work, event polling included, is pinned tasks its plugin schedules from its
  own timer or vsync events. A pinned job must return: one that never does
  holds the main thread for good.
- **A kernel's destroy cannot end the main thread,** because no kernel owns
  it. `Quit` does, through `Done`.

### Waiting from outside

Code outside the pool, a test or a tool, reaches a kernel with
`Supervisor.GetKernel(id)` and learns when its jobs are done through a
reusable `Signal`:

```go
game := s.GetKernel(gameID) // the KernelID Spawn answered
sig := kernel.NewSignal() // once; allocates once
_, done := game.Publish(Hit{Target: 1, Damage: 3})
game.Notify(sig, done) // signals once done finishes, or now if it has
<-sig.C()
```

- **`Kernel.Notify(sig, job)`** checks whether `job` has finished and arms
  `sig` under the kernel's lock, so no finish is missed however late it is
  called. A `JobHandle` belongs to one kernel, so `Notify` lives on that
  kernel and names none.
- **`Signal`** holds one value and is sent to without blocking, so a worker is
  never held up by a slow reader, and one `Signal` reused many times
  allocates nothing.
- **Only outside code waits, and never the main thread.** A task or handler
  blocking on a `Signal` can deadlock the pool. The main thread blocking on
  one can deadlock a pinned job the waited-on work depends on.
- **`GetKernel` is not for plugins.** Kernels do not manage kernels: a plugin
  reaching another kernel uses `Send`. A `*Kernel` kept past its destroy stays
  safe to call: `Publish` publishes nothing and `Notify` signals at once.

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

  When the event itself is POD, it can be the arg: a `TaskFunc[R, HitEvent, B]`.
  The closure above captures `c`, but `Subscribe` runs at registration, so it is
  allocated once, not per publish.
- A task may publish. Its handlers run on the job's worker while the job's
  locks are held, which is safe because handlers touch no resources.
- **A `JobHandle` means something only in its own kernel.** An event carrying
  one through `Supervisor.Send` arrives meaningless, so `Send` should refuse
  or zero such fields.

## Errors and logging

There is one `Logger` (`logger.go`) per supervisor, given to `NewSupervisor`.
Every method takes the source `KernelID` first: `LogErr(source, err)`,
`LogWarn(source, msg, args...)`, `LogDebug(source, msg, args...)`.
`Kernel.LogErr`, `Kernel.LogWarn` and `Kernel.LogDebug` are proxies that fill
in their own ID. The kernel formats nothing and writes nothing itself.

The Logger is the only place errors go, and it is also what decides what they
mean. There is no error handler and no verdict: the kernel forwards the error
and carries on. A Logger that wants a failing kernel gone calls
`Supervisor.Destroy(source)`, and one that wants the application gone calls
`Quit` or panics. An application that wants the errors back implements a
Logger that keeps them. `LogWarn` and `LogDebug` are slog-style.

A job or handler that panics is recovered on its worker and the panic passed
to `LogErr`. A panic raised by `LogErr` itself is not recovered again, so it
reaches the top of the worker and ends the process. That is what `StdLogger`
does on purpose.

**Once forms.** `LogErrOnce`, `LogWarnOnce` and `LogDebugOnce` take a
string key and log only the first call under it, until `ForgetLogOnce(key)`
clears it. They are for a condition true every frame: worth saying once,
noise at the frame rate. The key names the condition, such as
`"sprite:missing:" + path`, and keys of different kernels never collide. A
constant key costs nothing to pass; one built at run time allocates, which
logging, being exempt from alloc-free, may do. A condition
that can be repaired must be forgotten, or it stays silent for the kernel's
life. Deduplication is the Logger's: the kernel only forwards.

A nil Logger means a new `StdLogger`: `LogErr` panics, `LogWarn` writes to
stderr, `LogDebug` to stdout, each prefixed `[kernel N]`, and the Once forms
keep their keys in a map. Every error is fatal under it, which suits
development.

Every method other than a getter (`ID`, `Done`, `Configurator.Resource`,
`Read.Get`) is named for what it does, starting with a verb.

## Decided against

- **`Exclusive`.** v1 let a handler refuse to run alongside itself, to guard
  mutable state in its factory closure. Task bodies capture nothing, so there
  is no such state to guard, and v2 has no `Exclusive`.
- **Policing `Send` payloads.** `Send` copies an event as it is and never looks
  inside. A field that means something only in the sending kernel, such as a
  `JobHandle`, arrives meaningless, and keeping such fields out is the
  sender's business.
- **Resources at runtime.** Resources are created only during registration,
  and none is added or removed while a kernel runs. A resource that has no
  value yet is created empty instead: `InitResource[gfx.Backend](nil)`, filled
  later by a task holding its `Write`.

## Deferred

- **Introspection.** v1 could describe its handlers and lock sets, report
  contention, and trace the scheduler. Each task's lock set is compiled at
  registration and kept on its `Task`, so this can be added without changing
  the API.
