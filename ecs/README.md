# ecs

`github.com/dvoyni/cog/ecs` is Cog's entity component system, built on the
[kernel](../kernel). It is its own Go module (Go 1.27) and depends on nothing
but the standard library and the kernel.

## Features

- **Sparse-set stores, one lock each.** Every component type has its own
  store, and every store is a kernel resource, so it is the lock unit:
  systems touching different components run in parallel, with no locking in
  your code.
- **Systems are plain functions.** A system takes the kernel, the job's arg
  and up to eight parameters (queries, stores, lookups, command buffers,
  kernel resources), and registers as an ordinary kernel task. Its
  parameters are its locks, declared once at registration.
- **Typed queries.** `Query2[Game, *Position, Velocity]` writes positions and
  reads velocities; `With`, `Without` and `Optional` terms filter and widen
  a match. The smallest store drives, and the walk allocates nothing.
- **Command buffers.** Systems that cannot hold the barrier record spawns,
  despawns, adds and removes in parallel, and one commit applies them in a
  deterministic order.
- **Change detection, opt-in.** Delta queries yield what was added, changed
  or removed in an observation window, with the values before and after. A
  store nobody observes pays nothing.
- **Refs.** Text, lists, meshes and any other data a component can't hold
  live in refs: stable, shareable handles, plain or found by key, with
  deltas of their own.
- **Saves.** A world saves to JSON and loads back, remapping every entity
  and ref, and forgiving fields added or removed since.
- **Built for networking.** Type-erased row access, a schema of fields and
  markers, allocation ranges and placement at exact ids let a replication
  layer mirror a world without the ECS knowing about it.
- **Pointer-free and alloc-free.** Components hold no pointers, so the GC
  never scans a store. Once the containers have grown, nothing on the hot
  path allocates, which the tests assert.

## Shape

- **`Plugin`**, from `NewPlugin()`, is the ECS. It is built empty: the
  plugins that use a world register it, its components and buffers in their
  `Init`.
- A **world** is named by its world ID, `type Game [2]uint64`, whose bits are
  also each entity's component mask. A kernel holds any number of worlds.
- An **`Entity`** is a 32-bit index and a 32-bit generation; components are
  plain, pointer-free values in stores.
- **`World[W]`** is the barrier, the write on the whole world; every other
  handle locks only what it touches.

See [docs/README.md](docs/README.md) for the guide with examples, and
[docs/design.md](docs/design.md) for the design and why it is this way.
