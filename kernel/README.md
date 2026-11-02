# kernel

`github.com/dvoyni/cog/kernel` is Cog's plugin microkernel. It is its own Go
module (Go 1.27) and depends on nothing but the standard library.

## Features

- **Job scheduler.** Each kernel runs its jobs on its own pool of workers,
  sized at spawn: `runtime.GOMAXPROCS(0)` to use every CPU, or 1 for a
  sequential kernel. Idle workers park until a job is ready. Tasks pinned to
  the main thread run there, for window and GPU calls. A job always
  completes: a panicking body is recovered, logged, and finishes with the
  zero result.
- **Job ordering.** A job waits on any other job through `JobHandle`
  dependencies, takes another job's result as its arg (`ScheduleAfter`, typed
  so only a matching job can feed it), and several jobs combine into one
  dependency with `Join`. Each event handler receives the previous handler's
  out dependency and returns its own, and `OrderSubscriptions` fixes the
  order handlers run in.
- **Resource lock management.** A task declares its locks by requesting
  resource handles (`Read[T]`, `Write[T]`) once, at registration, so the
  locks and the code that uses them cannot drift apart. A job starts only
  once all its locks are free and holds them for its whole run. Readers of a
  resource run together; a writer runs alone against anything else that
  locks it. A waiting writer reserves what it writes, so
  readers cannot starve it. `Use` calls another task inline with its locks
  already held.
- **Zero allocations on the hot path.** Once a kernel is spawned, publishing
  an event, scheduling jobs and running them allocate nothing, which the
  tests assert. Task args, results and events are pointer-free, so the GC
  never scans work in flight.

## Shape

- A **Supervisor** runs one or more **Kernels** in one application. Kernels
  share nothing, and reach each other only by publishing events.
- **Plugins** register **resources**, **tasks** and **event handlers** against
  a kernel, and are started and stopped with it. Each has a `Name`, and the
  kernel's config is a `map[PluginName]any` of which each plugin gets only
  its own part.
- A **task** is the only code that touches resources. Its declare function
  requests resource handles, and those requests are its locks.
- Handlers schedule **jobs** (runs of tasks) when an event is published.

See [docs/README.md](docs/README.md) for the guide with examples, and
[docs/design.md](docs/design.md) for the design and why it is this way.
