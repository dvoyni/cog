# Cog

Cog is a Go engine for games, apps and backends, built for performance from
the ground up. One core runs a game client, its dedicated server, or a plain
service: you pick the plugins, and Cog runs them.

## Why Cog

- **Highly concurrent.** Work runs as jobs on a pool of workers that keeps
  every core busy, and jobs wait on each other only through the dependencies
  you declare. Nothing is serialized by default.
- **Automatic resource locking.** A task declares the state it reads and
  writes once, at registration, and the scheduler does the rest: readers run
  side by side, a writer runs alone, and no job ever touches state it didn't
  lock. No mutexes in your code and no data races.
- **Alloc-free on the hot path.** Once running, publishing events,
  scheduling jobs and running them allocate nothing, and the tests check it.
  Data in flight is pointer-free, so the garbage collector has nothing to
  scan and frames stay smooth.
- **Modular.** Everything is a plugin: the ECS, pacing and your own game code
  alike, and a renderer or a network layer plugs in the same way. A server
  spawns only the plugins it needs.
- **Extensible.** Plugins meet through resources and events, never through
  each other. Swap a backend by installing a different implementation of the
  same interface.
- **Deterministic when you need it.** Fixed-step simulation, replaceable time
  and a manual clock make tests and replays reproducible.
- **Performance oriented throughout.** A cache-friendly entity component
  system, pinned main-thread work for windows and GPUs, and benchmarks that
  guard the numbers.

Cog is written in Go 1.27, and its core depends only on the standard library.
Some plugins bring dependencies of their own, such as a graphics backend, and
only an application that uses them pulls them in.
