# ecs: design

`github.com/dvoyni/cog/ecs` is an entity component system on the v2 kernel. A
world's entities carry plain, pointer-free components; systems are plain
functions wrapped into kernel tasks, whose parameters declare their locks. It
knows nothing about networking, rendering or any other consumer: those build
on the generic pieces here (type-erased row access, change detection, command
buffers).

Status: built; what comes next is under [Todo next](#todo-next).

## Constraints

- **Alloc-free.** Once a kernel is spawned and the containers have grown to
  their working size, nothing allocates: not a query, a system call, a commit,
  an observation reset. Only pools, chunked arrays and buffers grow, and they
  keep their capacity until a shrink gives it back.
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
       ├─ refsResource[Game, Text]        resource per ref payload type
       └─ commandRecorder[Game, AiCmds] resource, registered by the user
```

| Type | Role |
|---|---|
| `WorldID` | Constraint over world IDs: `~[1]uint64 \| ~[2]uint64 \| ~[4]uint64 \| ~[8]uint64`. |
| `World[W]` | The barrier: write on the world. Spawn, despawn, `CreateAt`, type-erased row access, `Shrink`, `Save`, `Load`. |
| `WorldLookup[W]` | Reads the world: `Alive`, and the `Schema`. No component. |
| `Store[W, T]` | Writes `T`'s store by entity: `Get` a pointer to read, `Write` a pointer marked changed, `Add`, `Remove`. |
| `Lookup[W, T]` | Reads `T`'s store by entity: `Has`, and `Get` a copy. |
| `Query0[W]` | Walks every live entity, in index order. Reads the world alone. |
| `Query1[W, A]` … `Query8[W, ...]` | Walks the entities holding every term, yielding each entity and its row. `*T` writes, `T` reads a copy, `Optional[T]` and `Optional[*T]` match with or without one. |
| `DeltaQuery1[W, A]` … `DeltaQuery8[W, ...]` | Walks the entities with a delta in a term this observation window, among those that have or had every term, yielding each entity and its row: values now, as of the reset, and deltas. Observes each term's store. |
| `Refs[W, T]` | Writes the refs holding `T`: `New`, `NewAt`, `Get` a pointer, `Set`, `Alive`, `Release`, `All`. |
| `RefsLookup[W, T]` | Reads the refs holding `T`: `Get` a pointer, to read only, `Alive` and `All`. |
| `KeyedRefs[W, K, T]` | Writes refs found by their value's key, one per key: `Put`, `PutAt`, `Update`, `Find`, `Get`, `Alive`, `Release`, `ReleaseKey`, `All`. |
| `KeyedRefsLookup[W, K, T]` | Reads keyed refs: `Find`, `Get`, `Alive` and `All`. |
| `DeltaRefs[W, T]` | Walks the refs of `T`, plain or keyed, with a delta this observation window: added, changed or released, with the value. Observes them. |
| `CommandBuffer[W, Tag]` | Records into the command buffer `Tag` of world `W`, holding a read on it. |
| `CommandSink[W, Tag]` | Applies that buffer to its world: the write on the buffer and the barrier. |
| `CommandRefs[W, Tag, T]` | Records making, setting and releasing refs of `T` into that buffer, holding a read on the refs. |
| `CommandKeyedRefs[W, Tag, K, T]` | Records updates and releases of keyed refs of `T` into that buffer. |
| `Entity` | 64-bit `{index:32, gen:32}`, layout private; `NoEntity` is 0. |
| `ObservationResetSystem[W]` | The system that ends an observation window. |
| `ShrinkSystem[W]` | The system that gives back the memory a world holds past what is in use. |

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
- **A shrink gives memory back,** where the game can afford regrowing, such
  as after a level unloads. `World.Shrink()`, or the system `ShrinkSystem[W]`,
  runs under the barrier, which excludes the world's refs and command
  buffers too, since each of their handles holds a read on the world; they
  register with the world for it. In a world that observes it ends the
  window first, as the observation reset does, letting go of removed rows
  and despawned indices and taking back reservations not spawned yet; a
  world that never observed starts no observation. It then releases every
  chunk past what is in use: a store's owners, values, shadows and row
  states, and its index pages that link no row; the free entities; each
  refs' free list; each command lane. Entity and ref slots never shrink:
  each keeps its last generation, so a stale handle never comes alive.
  Nothing alive or recorded is lost, and growing again allocates anew, once.

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
- **Alive check** compares the generation, and costs one load. `World` and
  `WorldLookup` answer it; every store probe makes it too, since a row's
  owner is compared whole.
- **An entity held in a component may be dead.** A stored `Entity` is an
  id, not a link: nothing clears it or reports when its entity dies. Its
  generation no longer matches, so it reads as dead even once its index is
  reused, and every probe through it finds nothing. Whoever reads it checks
  it, with `WorldLookup.Alive` or by the probe it makes anyway. Clearing it,
  or despawning along with it, is for whoever stored it, such as a scene
  module keeping a hierarchy.

## Components and stores

- **A component is a pointer-free value type,** registered once per world
  under a name: `ecs.RegisterComponent[Game, Position](configurator,
  "position")` in a plugin's `Init`. It is checked with
  `container.IsPointerFree`. Each gets a dense `ComponentID` in registration
  order, which plugin order makes the same on every run with the same
  plugins. The name is what saves know it by, so the Go type can be renamed
  or moved; a name registered twice in a world panics.
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
  `Lookup`, `DeltaQuery1`, `kernel.Read`. `Store[W, T]` writes, `Lookup[W, T]`
  reads; the `*T` / `T` rule is for query terms only. The world and command
  buffers are unexported resources too, reached only through handles:
  `World`, `WorldLookup`, `CommandBuffer`, `CommandSink`.
- **The schema.** `World.Schema()` and `WorldLookup.Schema()` describe the
  world without its types: each component's `ComponentInfo` (ID, name,
  `reflect.Type`, size, alignment, markers and fields) and each refs' `RefInfo`
  (name, value type, keyed, transient). It is built once, at registration,
  for consumers that work on rows as bytes: saves, the network layer, tools.
- **Fields are the exported ones,** flattened into dotted paths with their
  type, offset, size and struct tag: `Position.X`. An embedded struct's
  fields are promoted, as Go promotes them; an array, an `Entity` and a `Ref`
  are one field each; a component that is not a struct is one field with an
  empty path. Two fields at one path panic at registration. The ECS gives
  tags no meaning: the network layer reads its own (`net:"bits=12"`).
- **Markers say what a component is,** beyond what it holds: every embedded
  zero-size struct, listed in `ComponentInfo.Markers` and asked with
  `HasMarker[M](info)`. The ECS has one, `Transient`; the network layer
  defines its own (replicated, owned by a player). Markers come before every
  field, or registration panics: they read as the component's header, and a
  zero-size field last would pad the struct.

## Locking

- **Every handle of a world declares a read on it,** besides its own locks.
  `WorldLookup[W]` takes that read alone, for alive checks and the
  schema; reservations are on `CommandBuffer`. A world read
  shuts out only the barrier: store writers run beside it and set mask bits,
  so it reaches neither a store nor a mask.
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
  ascending `ComponentID` order. `Schema()`, `ComponentID[T]()` and
  `ComponentMask(e)` describe the world.
- **Outside the barrier,** a system holds what its parameters declare:
  - a query's `*T` term writes `T`'s store, a `T` term reads it;
  - `Store[W, T]` writes it, `Lookup[W, T]` reads it;
  - a delta query's `T` term reads it;
  - `WorldLookup[W]` reads the world alone;
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

- **`Query0[W]` walks every live entity,** in index order, yielding only
  the entity. It reads the world alone, so it reaches no component; a
  `Lookup` or a `Store` beside it does. A system taking `World` too holds
  the world's write, which supersedes the read, and may despawn the entity
  it is at: slots never move, so clearing a level is one walk.
- **`Query1` … `Query8`, terms by type.** A pointer term `*T` writes `T` and
  yields the row's address; a value term `T` reads `T` and yields a copy, so
  a read cannot write through. The mode is read from the term type by
  reflection at registration.
- **Filters** `With[T]` and `Without[T]` are terms that yield nothing and
  read `T`'s store.
- **Optional terms** `Optional[T]` and `Optional[*T]` match an entity with
  or without a `T`, reading or writing `T`'s store as `T` and `*T` do, and
  yield themselves: `Has()` tells whether the entity has one, copying
  nothing, and `Get()` returns it and true, or the zero `T` and false. An
  `Optional[*T]` row is marked changed only when the entity has one. An
  optional term never drives, so a query of `Without` and `Optional` terms
  only panics at registration. `Optional` reads the walked entity's
  components; `Lookup` reads any other's, such as an entity held in a
  component.
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

- **Opt-in per store.** A delta query registers an observer of each of its
  value terms' stores. A store nobody observes keeps no shadow and no
  states, and pays nothing.
- **Off until the first reset.** A world observes nothing until its first
  observation reset runs: no store keeps a tail or states, a despawned index
  is freed at once, and a delta query yields nothing. That first reset turns
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
  delta query can still give the last value. Lookups and queries see only
  the live part.
- **A delta query** `DeltaQuery2[Game, Position, Sprite]` walks the
  entities that have or had every value term this window, a removed term
  included and a discarded one not, with a delta in at least one. An entity
  holding only a `Position` never matches. Its row gives:
  - `Get()`, the values now, or the last one of a term removed;
  - `Previous()`, the values as of the reset, zero for a term added;
  - `Delta()`, one `Delta` per term: `DeltaNone`, `DeltaAdded`,
    `DeltaChanged` or `DeltaRemoved`.

  A changed row whose bytes equal its shadow has `DeltaNone`, so A→B→A is
  no delta, and an entity with no other delta is not yielded. A despawn is
  `DeltaRemoved` on every term; `WorldLookup.Alive` tells it from a removal.
- **Terms are values.** `T` reads and observes `T`'s store; `With[T]` and
  `Without[T]` filter on what the entity has now, and their changes are no
  delta. A `*T` term, or a query of filters only, is a composition error:
  an observer that wrote what it observes would see its own marks.
- **Walk.** Each value term drives in turn, through the rows of its store
  with a state, live and in the tail; the other terms are probed by entity.
  An entity is yielded at the first term with a delta and skipped at the
  later ones, so it comes once, in term order and then in the order the
  store visits its rows: deterministic.
- **An observed store lists its changed entities,** each once, when its row
  first leaves unchanged in the window, so the walk and the reset visit only
  them: a window with few changes costs per change, not per row. Past an
  eighth of the live rows, the listing stops and both visit every row,
  skipping the unchanged ones inline, at a state byte per row: a level load
  or a pass that writes every row costs what a walk of every row does, and
  the list never holds more than an eighth of the rows, one byte per row.
  An index reused within the window, by `CreateAt` of a new generation
  while the old one waits in the tail, stops it too: the tail row loses its
  link to the index, and only a walk of every row finds it.
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
- **`ObservationResetSystem[W]`** is a system the ECS provides:

  ```go
  c.reset = configurator.RegisterTask1(ecs.ObservationResetSystem[Game])
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
  at a word. Ref records come from `CommandRefs` and `CommandKeyedRefs`
  (see Refs).
- **Order.** Despawns of every lane first, then the other records in lane
  order (registration order) and sequence order, the lanes of ref records
  last: nothing in a commit reads a ref, so their place is free. A record on
  an entity not alive is dropped, and so is a `Spawn` of an entity that is
  not a reservation still waiting (its index holds a live entity, or it is
  not the generation the index hands out next), and a set or release of a ref not
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
- **Lanes keep their capacity** until a shrink. After warm-up, recording and
  committing allocate nothing.

## Refs

Text, lists, meshes and anything else that cannot live in a component lives
in a ref:

- **A handle in the component, the value outside it.** A component holds a
  `Ref[T]`: an index and a generation, pointer-free. The value is a `T` in
  its world's refs of that type. Any `T` is allowed, pointers included; the
  GC scans refs, never stores.
- **A handle is stable and shared.** Setting a ref's value keeps its handle,
  so any number of components hold one ref, as models hold one mesh. A
  holder does not change when the value does: `DeltaRefs` sees the change.
  A component changes only when a handle is written into it.
- **One container and one lock per payload type.**
  `RegisterRefs[W, T](c, name, options)` registers world `W`'s refs holding
  `T` under a name, a resource of its own, so systems on refs of different
  types run in parallel. Declaring refs that were never registered is a composition
  error. Each declaration also holds a read on the world, so the barrier
  excludes it.
- **Values sit in slots paged by index,** as entity slots are, with a
  generation and a state per slot. Pages never move, so the `*T` a `Get`
  returns stays valid until the ref is released. Freed slots are reused
  last in, first out: the free list holds each at its next generation.
- **`Refs[W, T]` writes:** `New(v)` returns a handle, `Get(h)` a `*T`,
  `Set(h, v)` stores a value, `Release(h)` frees the slot, `Alive(h)` tells
  a live handle from a dead one. **`RefsLookup[W, T]` reads:** `Get(h)`
  returns a `*T` that must only be read, and `Alive(h)`. Several lookups
  run in parallel.
- **`All()` walks every live ref,** on each of the four handles, in index
  order, yielding its handle and value: for tools, and for releasing every
  ref of a level. Releasing during the walk moves no other ref.
- **`Set` is the change.** Writing through a `*T` from `Get` is not one.
- **A released handle reads as nothing.** `Get` returns nil for the zero
  handle and for a released one, and `Alive` false. Generations start at 1,
  and a slot holds whether its ref is live, so no copy of a released handle
  reads the value placed there later. `Set` and `Release` report whether
  the handle was alive.
- **Released by the user.** The ECS does not count the components holding a
  ref. Whoever releases it knows none needs it, as a mesh library does; a
  holder left behind reads nothing, as a stale `Entity` does. A ref never
  released lives as long as its world.
- **Allocation range.** `RefsOptions` bounds the indices `New` hands out,
  all by default, as `WorldOptions` does for entities. `NewAt(h, v)` places
  a ref at exactly `h`, outside the range, on a slot holding no live ref nor
  one released this window: a client mirrors the server's refs where the
  server has them, and makes its own from another range.

### Keyed refs

- **A ref per key.** `RegisterKeyedRefs[W, K, T](c, name, options)` registers
  refs of a `T` that answers `Key() K`, as `Keyed[K]`: a mesh its asset path.
  `Text`, a string keyed by itself, serves names and labels.
- **`KeyedRefs[W, K, T]` writes:** `Put(v)` returns the ref of `v.Key()`,
  or stores `v` in a new one; a ref found keeps its value. `Update(v)`
  stores `v` in the ref of its key, reported as a change. `Find(k)`,
  `Get(h)`, `Alive(h)`, `Release(h)` and `ReleaseKey(k)`, which free the key
  with the ref, so a later `Put` makes a new one. `PutAt(h, v)` places a
  mirrored ref and its key. There is no `Set`: a value with another key
  would move the ref to it. For the same reason a value's key must not
  change through the `*T` from `Get`: the ref stays found by its old key,
  and a release frees the new one. **`KeyedRefsLookup[W, K, T]`** reads:
  `Find`, `Get`, `Alive`, `All`.
- **One kind per type.** Refs of one `T` in one world are plain or keyed,
  never both: registering both panics, and so does declaring `Refs` or
  `CommandRefs` of keyed refs, or a keyed handle of plain ones.

### Ref deltas

- **`DeltaRefs[W, T]`** walks the refs of `T`, plain or keyed, with a delta
  this observation window, each once, with its value:

  | This window | Delta |
  |---|---|
  | Made | added |
  | Set or updated | changed, even to the same value |
  | Made, then set | added |
  | Released | released, with the last value |
  | Made, then released | none |

  There is no shadow and no previous value: a `T` may hold pointers, and a
  copy would share what it points to.
- **The world's window.** Refs observe from the world's first observation
  reset once a `DeltaRefs` of them is declared, every live ref counting as
  added then. `ObservationResetSystem` ends their window with the stores':
  it clears the states and frees the window's released slots, values
  cleared. Until then a released slot keeps its value and stays off the
  free list, so no index is reused in the window its ref was released in.
  Refs nobody observes keep no state, and free a slot at once.
- **The listing.** Observed refs list the refs whose slots changed this
  window, once each, so `DeltaRefs` and the reset cost per change. Past an
  eighth of the live refs the listing stops, and they visit every slot on a
  page that exists, as stores visit every row, so refs far from index 0 cost
  only their pages.

### Refs through a command buffer

- **`CommandRefs[W, Tag, T]`** records into buffer `Tag`: `New(v)` returns
  a handle at once, to record in a component, `Set(h, v)` and `Release(h)`,
  all applied at the commit. It holds a read on the refs, so producers run
  in parallel with each other and with lookups, and `Refs` writers wait.
  Each system taking it gets a lane of its own, holding its values typed,
  so the GC sees them.
  - A handle is reserved as an entity is: one atomic cursor over the free
    list, then fresh slots. Every `Refs` write and every commit folds the
    cursor back first. A reserved ref reads nothing until its commit.
  - A set or release of a ref not alive is dropped and counted in
    `Commit`'s answer. A release does not depend on its entity, so it
    applies even when the despawn beside it is dropped.
- **`CommandKeyedRefs[W, Tag, K, T]`** records `Update`, `Release` and
  `ReleaseKey`. A `Put` is not recorded: two producers putting one key in
  parallel would take two refs for it. An update or a release by key of a
  key with no ref is dropped.

## Saves

A world saves to JSON and loads from it, for save games and levels edited
by hand:

- **`World.Save(writer)` writes the whole world** under the barrier: every
  live ref of every refs not transient, then every live entity with its
  components not transient, each by its name.

  ```json
  {"refs": {"text": [{"ref": 4294967297, "value": "hello"}]},
   "entities": [{"entity": 4294967297, "position": {"X": 1, "Y": 2}, "frozen": {}}]}
  ```

  It is entity-major, so an entity reads and diffs as one piece; a tag is
  `{}`. It is indented, and map values in refs are sorted.
- **Fields by the schema's rules.** A component is written field by field,
  by Go name, its markers and unexported fields left out; an array of
  structs is an array of its elements, written by the same rules. Any other
  field is marshaled by `encoding/json/v2` whole, so a `json` tag means
  nothing to a component's own fields or its elements'.
- **Transient is left out.** A component embedding `Transient`, or refs
  registered with `RefsOptions.Transient`, are not saved: caches, state that
  lives a frame.
- **Entities and refs are numbers:** `SerializeEntity(e)` and
  `SerializeRef(r)`, wherever they appear, in components and in ref values,
  through per-call marshalers of `encoding/json/v2`. Ref values are
  marshaled by it whole, so any `T` it handles saves.
- **`World.Load(logger, reader)` adds a save to the world.** Saved entities
  take the next fresh indices, one after another in the order the save
  lists them, keeping their generations, so a save's gaps, and indices far
  apart, cost nothing. `Save` lists them in index order, so their order is
  kept too; a file edited out of order loads in its own. Refs do the same. A
  saved keyed ref whose key has a ref in the world, or in an earlier saved
  ref, takes that ref and no index, and the world's value is kept. Every
  `Entity` and `Ref` field is remapped to match; one naming nothing in the
  save, a dead entity or a released ref, loads as `NoEntity` or the zero
  `Ref`.
- **Load forgives drift.** Fields the save has and the component lacks are
  ignored, fields it lacks are zero, and so are unexported fields, which are
  never saved; so are elements past an array's length, and those it lacks.
  A component or refs name the world does not know is warned of once through
  the logger, if one is given, and skipped, and top-level members other than
  `refs` and `entities` are ignored. A change beyond that is the user's to
  patch in the JSON.
- **Load fails whole.** It reads and decodes everything before changing the
  world, so malformed JSON, a value of the wrong type, a missing handle, two
  handles at one index, or more saved than the allocation range has fresh
  indices for return an error and leave the world as it was. An error in a
  value names its saved entity and component, or its refs and saved ref,
  with the offset in that value's JSON. Only reservations are settled: one
  never committed loses its index, as a despawn outside a window does.
- **Observers see a load as adds,** of every loaded component and ref.

## Fit for networking

A replication layer is meant to be built on this without the ECS knowing it
exists:

- **It is systems holding the barrier,** which reach every replicated store
  by `ComponentID` as bytes.
- **Server ids are wire ids.** The client mirrors server entities with
  `CreateAt` and allocates its own from another allocation range, so an
  `Entity` inside a component means the same on both ends and is never
  remapped.
- **Ref ids are wire ids too.** `DeltaRefs` gives each changed ref once,
  whatever number of components hold it; the client places it with `NewAt`
  or `PutAt`, so a handle inside a mirrored component reads the mirrored
  value unchanged.
- **The schema is its field view.** It bit-packs fields by the paths and
  offsets the schema gives, reads its markers (replicated, owned by a
  player, affects visibility) and its field tags (bits, interpolation), and
  hashes the schema its own way to match peers. Saves are not its format.
- **A delta query on the replicated stores** narrows what is captured again;
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
- **A new handle per set.** It made a set an ordinary component change, seen
  by delta queries, but a ref could not be shared: every other copy of the
  handle went stale. Stable handles let models share a mesh, and
  `DeltaRefs` reports the change once.
- **Counting a ref's holders.** The ECS would have to find the refs in
  components, which needs fields it does not know, or the user would keep
  the count by hand. A released ref leaves dead links, as a despawned
  entity does.
- **A shadow per ref.** A `T` may hold pointers, so a copy shares what it
  points to, and a previous value or an equality check would lie after a
  change in place.
- **Recording `Put` in a command buffer.** Two producers putting one key in
  parallel would reserve two refs for it, and their components would hold
  different ones.
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
- **`Changes[W, T]` beside delta queries.** A one-term delta query with
  `Previous()` is the same question; two ways to ask it would drift apart.
  The delta query's row of values, previous values and deltas cost 25 µs
  over 10,000 rows with a tenth written, against 20 µs for `Changes`, both
  walking every row.
- **Created and Destroyed deltas.** A despawn already reads as every term
  removed, and `WorldLookup.Alive` tells it from a removal; that an entity
  is new means little to an observer beyond its terms being added, and
  knowing it would cost a window number per entity.
- **Relations and hierarchy.** Parents, children, despawning a subtree and
  propagating along it are a scene module's, built on entities held in
  components. The ECS cannot clear those without reading fields, and pairs
  held in an entity's type instead would make every pair a component type,
  where a world's masks have a fixed number of bits.
- **Optional delta query terms.** They would only yield an entity's deltas
  in several components in one visit. A delta query per component, with
  `With` and `Without` filters for the rest, asks the same; a consumer that
  builds one message per entity groups them itself. An `Optional` term in a
  delta query panics at registration.
- **A warning for a forgotten reset.** A world never reset does no harm, and
  catching resets that stopped needs a size check on the remove path.
- **Migrations and a saved schema.** A save holds no schema nor version,
  and load takes no migrator: fields are matched by name, and a component
  renamed, split or retyped is patched in the JSON by whoever changed it.
- **A schema hash in the ECS.** The network layer is its one user, and
  hashes what it means to match.
- **Components marshaled by their own JSON.** Saves marshal each field
  through `encoding/json/v2` at the schema's offsets, so markers and
  unexported fields stay out as the schema says, whatever the type's JSON
  would write.

## Performance

Measured per entity over 10,000 entities in `benchmark_test.go`, against
hand-written loops:

- **A dense two-term query** costs 6.4 ns, 7.4 ns with its written store
  observed, against 0.5 ns for a slice loop. A row holds each term's address
  as a field of its own, in registers for up to three terms: an array of
  addresses lives in memory, and copying it to the loop body loaded 16 bytes
  over two 8-byte stores, which the CPU cannot forward. As an array, a dense
  query cost 15 ns.
- **A probed term** costs more than a `Lookup`: a walk of positions with an
  optional velocity on every tenth entity costs 6.3 ns, against 4.1 ns for
  the same walk reading the velocity through a `Lookup`, and a dense
  two-term query 7.4 ns against 5.0 ns. The cost is the walk's probe of a
  term, optional or not.
- **A delta query** over 10,000 rows with a tenth written through
  `Store.Write`, read and reset costs 18 µs, 1.8 ns per row of the store.
  The listing of changed entities makes it cost per change below an eighth
  of the rows written, and a walk of every row above it:

  | Written per window | Listed | Walked every row |
  |---|---|---|
  | none | 12 ns | 9.6 µs |
  | 1% | 2.0 µs | 12.8 µs |
  | 5% | 9.3 µs | 17.5 µs |
  | 10% | 18 µs | 25 µs |
  | 25% | 49 µs, overflowed | 45 µs |
  | 50% | 84 µs, overflowed | 78 µs |
  | 100% | 155 µs, overflowed | 157 µs |

  A listed change costs about 18 ns, and a walk of every row 9.6 µs plus
  15 ns a change, so they meet near 29%; a cap of a quarter measured no
  better at 25% and worse at 50%. Past the cap, a window pays the listing up
  to it, 7 to 10% at a quarter or half written. Writes cost 3 to 5% more:
  `Store.Write` 5.3 ns against 5.1 ns, a dense two-term query writing an
  observed store 8.3 ns against 8.0 ns, a committed `Set` to one 33 ns
  against 32 ns. The list holds at most 1 byte a row, a store of
  `position`s 93 bytes per entity against 92: loading as many entities
  again in one window, reading and resetting, then despawning them, costs
  the same, with the list kept at an eighth of the rows.
- **A ref** costs 2.6 ns to read by handle and 6.2 ns to set by handle,
  observed, its share of the reset included. Walking `DeltaRefs` with a
  tenth of the refs set, and resetting, costs 0.8 ns per ref. A `Put` of a
  key with a ref costs 19 ns, a map lookup. Making one through a command
  buffer and releasing it through another commit costs 26 ns, up from 22 ns
  over a dense array: slots are paged by index, so an allocation range far
  from 0 costs only its pages.
- **Walking a mask's component IDs,** for despawn, checking and spawning
  from bytes, inlines whole, loop body included.

`docs/benchmarks-baseline.txt` holds ten runs. A change is checked with
`go test -run '^$' -bench . -count 10 > new.txt` and
`benchstat docs/benchmarks-baseline.txt new.txt`.

## Deferred

- **Parallel writes to one store.** Two jobs writing disjoint rows of one
  component serialize on its lock. Partitioned write locks can be added
  without changing the system form.
- **Hooks on add, remove and set.** A callback when a component arrives,
  leaves or is set, run where the change is applied. A remove hook could
  release a component's refs without the user recording it. Networking does
  not need them: a set hook costs an indirect call per write in the writing
  jobs, where a delta query finds the changes once under the barrier and
  drops writes that leave a value as it was.
- **Prefabs and bundles.** Typed templates of several components spawned
  together, and instances that share or copy from a prefab. `SpawnRaw` does
  it untyped, and keyed refs share the heavy data a prefab would.