# ecs: design

`github.com/dvoyni/cog/ecs` is an entity component system on the v2 kernel. A
world's entities carry plain, pointer-free components; systems are plain
functions wrapped into kernel tasks, whose parameters declare their locks. It
knows nothing about networking, rendering or any other consumer: those build
on the generic pieces here (type-erased row access, change detection, command
buffers).

Status: agreed; steps 1 to 8 of the [Work order](#work-order) are built.

## Constraints

- **Alloc-free.** Once a kernel is spawned and the containers have grown to
  their working size, nothing allocates: not a query, a system call, a commit,
  an observation reset. Only pools, chunked arrays and buffers grow, and they
  keep their capacity.
- **Pointer-free storage.** Components contain no pointers at any depth, so
  the GC never scans a store, and a row can be copied and compared as bytes.
  Variable-size data (text, lists, blobs) lives in refs.
- **Chunked, not contiguous.** Storage is built on `kernel/container`'s
  chunked arrays: chunks never move, so growing copies nothing and a row's
  address is stable until it is swap-removed.
- **Deterministic mechanics.** Entity reuse, swap-remove and command order
  depend only on what was done, never on timing. Ordering conflicting systems
  is the user's job, through job dependencies.

## Shape

```
Kernel
  └─ worldResource[Game]   resource: entities, registry, structure
       ├─ storeResource[Game, Position]   resource per component type
       ├─ storeResource[Game, Velocity]
       ├─ refsResource[Game, string]      resource per ref payload type
       └─ commandBuffer[Game, AiCmds]   resource, registered by the user
```

| Type | Role |
|---|---|
| `WorldID` | Constraint over world IDs: `~[1]uint64 \| ~[2]uint64 \| ~[4]uint64 \| ~[8]uint64`. |
| `World[W]` | The barrier: write on the world. Spawn, despawn, `CreateAt`, type-erased row access. |
| `Store[W, T]` | Writes `T`'s store by entity: `Get` a pointer to read, `Write` a pointer marked changed, `Add`, `Remove`. |
| `Lookup[W, T]` | Reads `T`'s store by entity: `Has`, and `Get` a copy. |
| `Query1[W, A]` … `Query8[W, ...]` | Walks the entities holding every term, yielding each entity and its row. `*T` writes, `T` reads a copy. |
| `Changes[W, T]` | Reads what changed in `T`'s store this observation window, and turns observation on for it. |
| `Refs[W, T]` | Writes the refs holding `T`: `New`, `Get` a pointer, `Set`, `Release`. |
| `RefsLookup[W, T]` | Reads the refs holding `T`: `Get` a pointer, to read only. |
| `CommandBuffer[W, Tag]` | Records into the command buffer `Tag` of world `W`, holding a read on it. |
| `CommandSink[W, Tag]` | Applies that buffer to its world: the write on the buffer and the barrier. |
| `CommandRefs[W, Tag, T]` | Records making, setting and releasing refs of `T` into that buffer, holding a read on the refs. |
| `Entity` | 64-bit `{index:32, gen:32}`, layout private; `NoEntity` is 0. |
| `ObservationReset[W]` | The system that ends an observation window. |

## Worlds

- **A world is named by its world ID.** `type Game [2]uint64` declares a
  world that holds up to 128 component types: the ID's values are also the
  world's per-entity component masks. Its identity and its capacity
  are one type parameter, so every handle names its world once:
  `Query2[Game, *Position, Velocity]`. Two worlds of one size are two named
  types: `type Sim [2]uint64`, `type View [2]uint64`. Registering more
  component types than the ID holds is a composition error, and panics.
- **Mask operations are generic, not an interface.** The words are reached
  through `unsafe.Slice` with a length fixed per instantiation, so every
  operation on a mask compiles for its size and inlines; nothing dispatches
  dynamically.
- **Up to `[8]uint64`, 512 component types.** A Go constraint lists array
  lengths one by one, so the limit is where the list stops. It can be relaxed
  without changing any handle: make the constraint `comparable`, check at
  registration that `W` is an array of `uint64` (any length), and keep
  `unsafe.Sizeof(W)/8` as the word count, still a constant per instantiation.
  The cost is a wrong world ID caught at spawn rather than at compile time,
  and wider masks make every per-entity mask operation (add, remove, despawn,
  type-erased visits) touch more words, so a larger limit is a measurement
  question.
- **Several worlds per kernel.** Each world's own resource, stores and buffers are
  distinct resources, so worlds never contend, and one system can take
  queries over several worlds at once: its lock set covers each.
- **The ECS is one plugin, built empty.** `ecs.NewPlugin()` takes nothing, as
  every plugin constructor does; a plugin is configured only through its
  config part, and building worlds is not configuration. Its `Init` installs
  the ECS's registry of worlds.
- **Worlds are registered by the plugins that use them.**
  `ecs.RegisterWorld[Game](configurator, options)` in a plugin's `Init`
  registers the world, with its allocation range and how many entities to
  prepare. The registering plugin is given after the
  ECS plugin; registering before it, or one world twice, is a composition
  error, and panics. Components, buffers and systems of a world are
  registered after the world.
- **The ECS has no frame.** It subscribes to nothing and publishes nothing.
  The user's handlers schedule systems as jobs, in whatever graph the game
  needs.

## Entities

- **An entity is 64 bits: a 32-bit index and a 32-bit generation.** 0 is
  `NoEntity`. The layout is private. Generations start at 1 and skip 0 when
  they wrap.
- **Allocation range.** A world hands out indices only from its configured
  range. A client mirroring a server allocates its local entities from a range
  the server never uses, and places mirrored ones exactly where the server has
  them.
- **`CreateAt(e)`** makes `e` alive at its index and generation, for mirrors
  and for loading a save. The index must hold no live entity and lie outside
  the allocation range, so it never collides with an index the world hands
  out or has reserved.
- **LIFO reuse.** A despawned index goes to the back of the free list, and
  entities are handed out from the back, so reuse order is deterministic,
  live indices stay packed low, and taking entities never moves the rest.
  FIFO only spread generation wear, whose wrap is negligible at 32 bits.
  In an observing world an index goes on the list only at
  the next observation reset, so an index is not reused in the observation
  window it died in; in a world that does not observe, at once.
- **Component mask.** The world keeps each entity's components as one mask
  (`W` itself). A store's writer updates its bit atomically when it adds or
  removes, so no global lock is taken; type-erased access and despawn read it
  to visit only the stores an entity has.
- **Alive check** compares the generation, and costs one load.

## Components and stores

- **A component is a pointer-free value type,** registered once per world:
  `ecs.RegisterComponent[Game, Position](configurator)` in a plugin's `Init`.
  It is checked with `container.IsPointerFree`. Each gets a dense
  `ComponentID` in registration order, which plugin order makes the same on
  every run with the same plugins.
- **A store is a sparse set.** A sparse array maps an entity's index to a
  dense row; the dense part holds the owners and the values. Both are chunked.
  Removing swaps the last row into the gap. A probe is one load and one
  generation compare.
- **A tag** is a component of size zero: a store with owners and no values.
- **Each store is its own kernel resource,** so it is the lock unit: two
  systems touching different components run in parallel.
- **Stores are private; parameters are handles.** The store resource is
  unexported. What a system takes is a small struct holding a pointer to it,
  so every system parameter is passed by value alike: `Query2`, `Store`,
  `Lookup`, `Changes`, `kernel.Read`. `Store[W, T]` writes, `Lookup[W, T]`
  reads; the `*T` / `T` rule is for query terms only. The world and command
  buffers are unexported resources too, reached only through handles:
  `World`, `CommandBuffer`, `CommandSink`.
- **Erased metadata.** The registry keeps each component's `reflect.Type`,
  size and alignment, for consumers that work on rows as bytes. It keeps
  nothing about fields: a consumer that needs a field view (a serializer, the
  network layer) builds its own from the type, once, with whatever meaning
  it gives fields.

## Locking

- **Every handle of a world declares a read on it,** besides its own locks.
  No system takes the world for reading alone: what a read allows (alive
  checks, reservations) is on the handles that need it.
- **`World[W]` is the write.** A system taking it is the barrier: nothing
  else of that world runs alongside it.
- **Under the barrier,** a system may spawn and despawn at once, place
  entities with `CreateAt`, commit structural command buffers, run the
  observation reset, and reach any store by `ComponentID` without its type:
  read a row as bytes, write it, add a component from bytes, spawn with a
  mask and the bytes of each component. A consumer that serializes or mirrors
  a world (networking, saving) is a system holding the barrier.
- **`World` is typed and raw.** Typed: `Has[T](e)`, `Get[T](e)`,
  `Write[T](e)` (marks changed), `Add[T](e, v)`, `Remove[T](e)`, each finding
  `T`'s store by a map lookup, for barrier work rather than hot loops. Raw,
  by `ComponentID`: `ReadRaw(e, id)`, `WriteRaw(e, id)`, `AddRaw(e, id,
  bytes)`, `RemoveRaw(e, id)`, `SpawnRaw(mask, values)` and
  `CreateAtRaw(e, mask, values)`, whose values are each component's bytes in
  ascending `ComponentID` order. `ComponentCount`, `Component(id)`,
  `ComponentID[T]()` and `ComponentMask(e)` describe the world.
- **Outside the barrier,** a system holds what its parameters declare:
  - a query's `*T` term writes `T`'s store, a `T` term reads it;
  - `Store[W, T]` writes it, `Lookup[W, T]` reads it;
  - `Changes[W, T]` reads it;
  - `CommandBuffer[W, Tag]` reads its buffer; `CommandSink[W, Tag]` writes it
    and takes the barrier;
  - `kernel.Read[R]` and `kernel.Write[R]` lock resource `R`.
- **Adding or removing a component** needs only a write on its store: the
  entity's mask bit is updated atomically. Spawning and despawning change
  every store an entity has, so they take the barrier or go through a command
  buffer.

## Systems

A system is a function of one fixed form:

```go
func move(k *kernel.Kernel, t Tick, q ecs.Query2[Game, *Position, Velocity], settings kernel.Read[Settings]) kernel.NoResult {
	for _, row := range q.All() {
		pos, vel := row.Get()
		pos.X += vel.X * t.Dt * settings.Get().Speed
	}
	return kernel.NoResult{}
}

// in Init:
c.move = configurator.RegisterTask2(move)
```

- **The form is always the same:** the kernel, the job's arg, up to eight
  system parameters, and the job's result. A system that takes nothing takes
  `kernel.NoArg`; one that answers nothing returns `kernel.NoResult`.
- **A system is registered as any task is,** with the kernel's
  `RegisterTask0` … `RegisterTask8`, which take a plain function. The ECS has
  no wrapper of its own: a system is a task whose parameters include ECS
  handles.
- **A system parameter is a `kernel.Parameter`:** a type with
  `Declare(*kernel.Access) P`. Registration calls it once per parameter on
  its zero value: that declares the parameter's locks and binds it to its
  stores. The body only calls the system. Every ECS handle is one, and so are
  the kernel's `Read[R]` and `Write[R]`, so resources are taken as in any
  task.
- **A system that calls another task directly** (`Access.Use`) is registered
  with the kernel's `RegisterTask` instead, binding its handles by hand.
  `Use` needs a particular `*Task`, which a parameter declared from its zero
  value cannot know, so `RegisterTaskN` cannot offer it. Each handle's
  `Declare` is the same call `RegisterTaskN` makes, so locks, the direct call
  and the hot path are as they would be:

  ```go
  c.think = configurator.RegisterTask(func(access *kernel.Access) kernel.TaskBody[kernel.NoArg, kernel.NoResult] {
  	q := ecs.Query1[Game, *Brain]{}.Declare(access)
  	positions := ecs.Lookup[Game, Position]{}.Declare(access)
  	findPath := access.Use(c.findPath)
  	return func(k *kernel.Kernel, _ kernel.NoArg) kernel.NoResult {
  		for e, row := range q.All() {
  			brain := row.Get()
  			from, _ := positions.Get(e)
  			brain.Path = findPath(k, PathArg{From: from, To: brain.Goal})
  		}
  		return kernel.NoResult{}
  	}
  })
  ```
- **No reflection on the hot path.** Reflection runs at registration only
  (term modes). The body is a direct call with values bound
  once.
- **A system is a task.** It is scheduled, depended on and fed like any
  other. Ordering systems is ordering jobs.
- **Two jobs of one system may run at once** when its locks are all reads.
  Nothing per-run is kept in a parameter's fields: iteration state lives on
  the stack, and lanes tolerate it (see Commands).

## Queries

- **`Query1` … `Query8`, terms by type.** A pointer term `*T` writes `T` and
  yields the row's address; a value term `T` reads `T` and yields a copy, so
  a read cannot write through. The mode is read from the term type by
  reflection at registration.
- **Filters** `With[T]` and `Without[T]` are terms that yield nothing and
  read `T`'s store.
- **`All()`** is a range-over-func iterator yielding the entity and its row;
  `row.Get()` returns the terms. Go ranges over at most two values, so the
  terms come through the row, the same way for every query size. It
  allocates nothing.
- **The smallest store drives.** The others are probed by index.
- **Iteration goes backwards,** so removing the current entity from any
  store, the driver included, is safe. Adding to a queried store, or removing
  another entity from the driver, during iteration is undefined.
- **A yielded pointer is valid** for the system call, until the next
  structural change to its store within it.

## Change detection

- **Opt-in per store.** A system taking `Changes[W, T]` registers an
  observer of `T`'s store. A store nobody observes keeps no shadow and no
  states, and pays nothing.
- **Off until the first reset.** A world observes nothing until its first
  observation reset runs: no store keeps a tail or states, a despawned index
  is freed at once, and `Changes()` reports nothing. That first reset turns
  observation on, and every row present then counts as added in the first
  window, so an observer joining late, such as replication, sees the whole
  world. A world nobody resets therefore keeps nothing it would leak.
- **An observed store keeps a shadow and a state per row.** The shadow is
  each value as of the last observation reset. The state is one byte:
  unchanged, changed, added, removed, or discarded (added and removed within
  one window).

  | This window | State |
  |---|---|
  | Written through a `*T` query term, `Store.Write` or a commit | changed, unless added |
  | Added | added |
  | Removed while unchanged or changed | removed: moved to the tail, shadow kept |
  | Removed while added | discarded: moved to the tail, never reported |
  | Re-added while discarded | added |
  | Re-added while removed | changed, compared with its shadow |
  | Despawned | removed in every store it had |

- **Removed rows wait in the tail** of the dense part until the reset, so a
  change can still give the last value. Lookups and queries see only the
  live part.
- **`Changes()` yields** the entity, the kind (added, changed, removed), the
  value and the previous value (from the shadow; none when added). A changed
  row whose bytes equal its shadow is skipped, so A→B→A is not a change.
  Changes come in row order, so their order is deterministic.
- **No field masks.** Which fields changed is the consumer's business: it
  compares value with previous using whatever it knows about the fields.
  Raw-byte masks would report float noise a consumer filters out anyway.
- **A query marks what it yields.** A `*T` term marks every row it yields as
  changed. A pass that writes few rows reads with a `T` term and writes
  through `Store.Write(e)`.
- **Bytewise.** `-0.0` and `0.0`, and NaN payloads, differ.

### Observation window and reset

- **An observer sees changes made since the last observation reset by
  systems that ran before it.** Ordering observers after writers is the
  user's job, by job dependencies.
- **`ObservationReset[W]`** is a system the ECS provides:

  ```go
  c.reset = configurator.RegisterTask1(ecs.ObservationReset[Game])
  ```

  It takes `World[W]`, the barrier. It copies changed and added rows into each observed
  store's shadow, clears their states, drops the tail, and frees the
  window's despawned indices in the allocation range to the free list.
  A reservation never spawned goes back too, under a new generation, so the
  stale entity never comes alive. Its first run turns observation on
  instead. The user schedules it where their frame needs: first, last,
  after the network send, after the renderer has read its changes.
- **Writes after a store's last observer are reset unseen.** That is the
  window's definition, not an error.
- **A world reset once is reset for good.** From its first reset on, removed
  rows wait in the tail and despawned indices stay off the free list until
  the next one, so a world whose resets stop grows without bound. A world
  never reset observes nothing and keeps nothing.

## Commands

A command buffer records changes for later, so systems that cannot hold the
barrier can still spawn, despawn, add and remove.

```go
type AiCmds struct{}

// in Init:
ecs.RegisterCommandBuffer[Game, AiCmds](configurator)

func think(k *kernel.Kernel, _ kernel.NoArg, cmds ecs.CommandBuffer[Game, AiCmds], q ecs.Query1[Game, Brain]) kernel.NoResult {
	e := cmds.Reserve()
	cmds.Spawn(e)
	cmds.Add[Position](e, Position{})
	return kernel.NoResult{}
}

func commitAi(k *kernel.Kernel, _ kernel.NoArg, commit ecs.CommandSink[Game, AiCmds]) kernel.NoResult {
	commit.Commit()
	return kernel.NoResult{}
}
```

- **The buffer is a resource the user registers,** one per world and `Tag`.
  A world may have several, so producers of unrelated work do not share
  lanes, and each commits where it is needed.
- **Two handles, by world and tag.** `CommandBuffer[W, Tag]` records,
  `CommandSink[W, Tag]` applies. The buffer resource behind them is
  unexported.
- **Producers read the buffer.** `CommandBuffer[W, Tag]` declares a read on it, so
  producers run in parallel. Each system taking it gets its own **lane** at
  registration and appends there. Two jobs of one read-only system may share
  a lane at once, so an append takes the lane's mutex, uncontended but for
  that case; their records then interleave.
- **The commit writes the buffer and holds the barrier.** The commit is a
  system the user writes, and is one call: `commit.Commit()`.
  `CommandSink[W, Tag]` declares the write on the buffer and on the world,
  so any record, structural or not, can be applied.
- **Records:** `Reserve`, `Spawn`, `Despawn`, `Add[T]`, `Set[T]`, `Remove[T]`.
  A record is a header (op, entity, component, where its value starts) in
  one chunked array, and the value's bytes in another, each value starting
  at a word. Ref records come from `CommandRefs` (see Refs).
- **Order.** Despawns of every lane first, then the other records in lane
  order (registration order) and sequence order, the lanes of ref records
  last: nothing in a commit reads a ref, so their place is free. A record on
  an entity not alive is dropped, and so is a `Spawn` of an entity that is
  not a reservation still waiting (its index holds a live entity, or it is
  not the generation the index hands out next), and a release of a ref not
  alive; `Commit` answers how many were.
  `Add` adds or replaces, `Set` replaces only, `Remove` of a component the
  entity lacks does nothing.
- **Commits go through the same row-state path** as direct writes, so
  observers see committed changes.
- **Reservations** come from one atomic cursor over the free list, under a
  read on the world. A reserved entity is not alive until its spawn is
  committed. The cursor is folded back under the barrier. In an observing
  world the observation reset does it, and a reservation never spawned goes
  back to the free list under a new generation. In a world that does not
  observe, every barrier operation does it, and such an index is lost:
  reserving without spawning is a bug, and costs one index.
- **Lanes keep their capacity.** After warm-up, recording and committing
  allocate nothing.

## Refs

Text, lists, maps and anything else that cannot live in a component lives
in a ref:

- **A handle in the component, the value outside it.** A component holds a
  `Ref[T]`: an index and a generation, pointer-free. The value is a `T` in
  its world's refs of that type. Any `T` is allowed, pointers included; the
  GC scans refs, never stores.
- **One container and one lock per payload type.** `RegisterRefs[W, T](c)`
  registers world `W`'s refs holding `T`, a resource of its own, so systems
  on refs of different types run in parallel. Declaring refs that were never
  registered is a composition error. Each declaration also holds a read on
  the world, so the barrier excludes it.
- **Values sit in a chunked array of `T`,** with a generation per slot and
  freed slots reused last in, first out: the free list holds each freed slot
  at its next generation, and a free slot's own generation is 0, so nothing
  reads it. Chunks never move, so the `*T` a `Get` returns stays valid until
  the ref is released.
- **`Refs[W, T]` writes:** `New(v)` returns a handle, `Get(h)` a `*T`,
  `Set(&field, v)` stores a value, `Release(h)` frees the slot.
  **`RefsLookup[W, T]` reads:** `Get(h)` returns a `*T` that must only be
  read. Several lookups run in parallel.
- **`Set` is the change.** It writes the value into the same slot, advances
  the slot's generation and writes the new handle back into the field, on
  every call. The component's bytes change, so `Changes` reports the
  component as changed, provided the component was reached through a pointer
  that marks: a `*T` query term or `Store.Write`. Writing through a `*T` from
  `Get` is not a change.
- **A stale handle reads as nothing.** `Get` returns nil for the zero handle,
  a released one, and a copy kept from before a `Set`. A handle holds its
  slot's index plus one, so the zero handle lies past every slot, and
  generations start at 1, so no handle matches a free slot. `Set` and
  `Release` report whether the handle was alive, as `Store.Add` does.
- **Released by the user.** The ECS knows nothing about a component's
  fields, so it cannot find the refs a component holds: whoever removes the
  component or despawns its entity calls `Release` first, or records a
  release beside a buffered `Remove` or `Despawn` in the same buffer. A ref
  never released lives as long as its world. `Release` frees the slot at
  once and advances its generation, so every handle to it, a shadow's
  included, goes stale.
- **Refs through a command buffer.** `CommandRefs[W, Tag, T]` records into
  buffer `Tag`: `New(v)` and `Set(old, v)` return a handle at once, to record
  in a component, and `Release(h)` frees one, all at the commit. It holds a
  read on the refs, so producers run in parallel with each other and with
  lookups, and `Refs` writers wait. Each system taking it gets a lane of its
  own, holding the values of its new refs typed, so the GC sees them.
  - A handle is reserved as an entity is: one atomic cursor over the free
    list, then fresh slots. Every `Refs` write and every commit folds the
    cursor back first. A reserved ref reads nothing until its commit.
  - A buffered `Set` takes a new slot for the value and releases `old`. A
    direct `Set` keeps the slot; either way the handle changes, the old one
    goes stale and the component holding it changes.
  - A release of a ref not alive is dropped and counted in `Commit`'s
    answer. A release does not depend on its entity, so it applies even when
    the despawn beside it is dropped.
- **A ref belongs to one field and is never shared.** Copying a component
  that holds one gives two fields one ref; after either is set or released,
  the other reads nothing. This is documented, not checked.

## Fit for networking

A replication layer is meant to be built on this without the ECS knowing it
exists:

- **It is systems holding the barrier,** which reach every replicated store
  by `ComponentID` as bytes.
- **Server ids are wire ids.** The client mirrors server entities with
  `CreateAt` and allocates its own from another allocation range, so an
  `Entity` inside a component means the same on both ends and is never
  remapped.
- **`Changes()` on the replicated stores** narrows what is captured again;
  field masks, quantization, ownership and interest are the consumer's own
  components and code.
- **Several worlds per kernel** give a client its simulated and its displayed
  world side by side.
- **Spawning with a mask and bytes** under the barrier gives a mirrored entity
  all its components at once, seen by observers as one spawn.

## Decided against

- **Archetypes.** Moving rows between tables on every add and remove, and no
  per-component lock unit. Sparse sets keep one store per component, which is
  what lets systems on different components run in parallel.
- **One world per kernel.** A client needs its simulated and its displayed
  world together, and an `Entity` in one cannot point into another kernel.
- **Components holding strings, slices or lists directly.** A row stops being
  a fixed width, copying and comparing it needs a walk per type, and the GC
  scans every store. Refs keep that data out of rows.
- **Finding refs in components by reflection,** to release them with their
  component. It gives the ECS knowledge of fields; releasing is the user's.
- **A version per ref.** Advancing the generation on `Set` changes the
  handle, which makes a set an ordinary component change.
- **Ref records as generic methods on `CommandBuffer`.** Nothing would be
  declared on the refs, so a reservation could not touch their free list:
  refs made through buffers would never reuse a freed slot.
- **One refs container per world.** One lock would serialize every system
  touching any ref.
- **Reflected system calls.** The body is a direct call of the system with
  parameters bound at registration.
- **A change log per store.** Its memory grows with the changes made (a level
  load), and readers must keep up with it. A shadow is one copy, whatever
  happens in the window.
- **Copying a whole store per writer** to find changes. It costs the store's
  size on every run of every writer of an observed store.
- **Field masks in the ECS.** It would need field metadata the ECS does not
  have, and raw-byte masks report noise.
- **Exclusive producers.** A buffer written by every producer serializes
  them.
- **An ECS frame or drain of its own.** Where systems, commits and the reset
  run is the game's decision, made with job dependencies.
- **A warning for a forgotten reset.** A world never reset does no harm, and
  catching resets that stopped needs a size check on the remove path.

## Deferred

- **Parallel writes to one store.** Two jobs writing disjoint rows of one
  component serialize on its lock. Partitioned write locks can be added
  without changing the system form.
- **Relations** between entities.
- **Shrink.** Giving capacity back, as a system under the barrier.
- **Introspection** for tools (by-name reads and writes).

## Work order

Each step ends with its tests passing under `go test -race`.

1. **`kernel/container`.** Make `Pool` and `Queue` public there; add an
   unsynchronized chunked array, chunk bits, and a public `IsPointerFree`.
   The kernel uses them from there.
2. **Kernel API.** Rename the declare function type to
   `TaskConstructorFunc[A, B]`; give `Read[T]` and `Write[T]` an exported
   `Declare(*Access)`.
3. **Core.** The `ecs` module; `WorldID`; `Entity` and the entity table
   (allocation range, `CreateAt`, LIFO reuse, reservations); `World[W]`, the
   plugin, `RegisterWorld` and the component registry; `Store[W, T]`, tags, `Lookup`; `Query1`…`Query8`
   with `With` and `Without`; `RegisterTask0`…`RegisterTask8` in the kernel. Alloc tests for
   queries and system calls.
4. **Change detection.** Shadows and row states, `Changes[W, T]`,
   `ObservationReset[W]`. A property test against a reference model.
5. **Commands.** `RegisterCommandBuffer`, `CommandBuffer[W, Tag]`, lanes,
   `CommandSink[W, Tag]`, type-erased row access under the barrier, spawning from a mask
   and bytes. A property test of buffered changes from several systems, and
   same results with one worker and many.
6. **Benchmarks.** Iteration, change detection, commits; measured against
   hand-written loops. `docs/benchmarks-baseline.txt` holds ten runs from
   after step 8; a change is checked with
   `go test -run '^$' -bench . -count 10 > new.txt` and
   `benchstat docs/benchmarks-baseline.txt new.txt`.
7. **Deepening.** Each is designed with the user first, then built, and
   measured against step 6:
   1. **An entity allocator.** Built as `entityTable`. One module owns allocation, reservations,
      windowed reuse and the generation burn, which are now split across
      `Spawn`, despawn, the observation reset and the commit, each branching
      on whether the world observes. Callers say what happened (reserved,
      spawned, released, window ended); the allocator decides the
      settling. The alive check stays one load. The baseline shows why:
      in a world that does not observe, every `Spawn` and despawn settles,
      and settling shifts the whole free list, so `BenchmarkSpawnDespawn`
      costs about 3.7 µs per entity, quadratic in the free entities. Taking
      from the back (LIFO reuse) makes settling a truncation.
   2. **Observation behind the store.** Built, with a row cursor. The store yields its own changes
      (owner, kind, value and shadow addresses, the bytewise skip
      included) and holds one observation state instead of two flags;
      `Changes[W, T]` only converts to `T`.
   3. **One row-access path.** Built: the store answers `get` and `write`
      (marked) by entity, the world `put` and `remove`; `Store`, `Lookup`,
      typed and raw `World`, and the commit only declare locks and convert.
   4. **Walking a mask.** Built as `maskIDs`, an iterator over a mask's
      component IDs in ascending order, for despawn, checking and spawning
      from bytes. It inlines whole, loop body included.
   5. **The query walk.** Built: a walk goes by the driver's chunks, stepping
      its address, probes only the other terms, once each, and marks the
      matched rows of observed write terms from those probes. Most of the
      15 ns a dense two-term query cost per entity was not probing but the
      row: an array of addresses lives in memory, and copying it to the
      loop body loaded 16 bytes over two 8-byte stores, which the CPU cannot
      forward. A row now holds each term's address as a field of its own,
      in registers for up to three terms. Dense costs 6.4 ns per entity,
      7.4 ns observed, against 0.5 ns for a slice loop.
8. **Refs.** Built as above. Reading a ref by handle costs 2.4 ns; setting
   one through its component, observed, 15 ns. Making one through a command
   buffer and releasing it through another commit, 22 ns.

## Todo next

Gaps in the design:

1. **Entities held in components.** A stored `Entity` only goes stale when
   its entity dies; nothing clears or reports it. To be decided, or
   documented as the rule.
2. **Change filters in queries.** `Changes[W, T]` walks one store; a query
   cannot ask for changed or added `T` among its terms, so a system walks the
   changes and probes the other stores by hand.
3. **Optional query terms.** A term that yields the row's `T` when the
   entity has one and nil when not, instead of a `Lookup` probed per row.

Major features other ECSs have:

- **Parallel iteration within a query.** Splitting one walk over several
  workers, with partitioned write locks on a store (see Deferred).
- **Relations and hierarchy.** Pairs between entities, parent and child,
  despawning a subtree, propagating along it (see Deferred).
- **Hooks on add, remove and set.** A callback when a component arrives,
  leaves or is set, run where the change is applied. A remove hook could
  release a component's refs without the user recording it.
- **Prefabs and bundles.** Typed templates of several components spawned
  together, and instances that share or copy from a prefab. `SpawnRaw` does
  it untyped.
- **Serialization and reflection.** A field view of components for saving,
  loading and the network layer, built on the erased metadata (see
  Introspection under Deferred).
- **Cached queries and groups.** Keeping a query's matches, or packing
  stores owned together in one order, so walks skip the probes. Weighed
  against the sparse-set choice; low.
- **Shrink.** Giving capacity back (see Deferred).

Networking comes after, as its own design on top of these pieces.
