# ecs guide

How to use `github.com/dvoyni/cog/ecs`, with examples. For the reasons behind
each choice, see [design.md](design.md). It assumes the
[kernel guide](../../kernel/docs/README.md).

- [Concepts](#concepts)
- [Quick start](#quick-start)
- [Worlds](#worlds)
- [Components](#components)
- [Systems](#systems)
- [Queries](#queries)
- [Stores and lookups](#stores-and-lookups)
- [The barrier](#the-barrier)
- [Command buffers](#command-buffers)
- [Refs](#refs)
- [Change detection](#change-detection)
- [Saves](#saves)
- [Memory](#memory)
- [Rules](#rules)

## Concepts

```
Kernel
  ├─ ecs plugin ── the registry RegisterWorld finds
  └─ world Game ── entities, component masks
       ├─ store of Position   ── a resource, its own lock
       ├─ store of Velocity   ── a resource, its own lock
       ├─ refs of Text        ── a resource, its own lock
       └─ command buffer Tag  ── a resource, its own lock
```

| Type | What it is |
|---|---|
| `Plugin` | The ECS, from `NewPlugin()`. Built empty; it subscribes to nothing and publishes nothing. |
| `WorldID` | The constraint on world IDs: `~[1]uint64`, `~[2]uint64`, `~[4]uint64` or `~[8]uint64`. |
| `Entity` | A 32-bit index and a 32-bit generation. `NoEntity` is 0. |
| `World[W]` | The barrier: the write on the whole world. Spawns, despawns, typed and raw row access, saves. |
| `WorldLookup[W]` | Reads the world: `Alive` and the `Schema`. |
| `Query0` … `Query8` | Walk the entities holding every term, yielding each entity and its row. |
| `Store[W, T]` / `Lookup[W, T]` | Write or read `T`'s store by entity. |
| `CommandBuffer[W, Tag]` / `CommandSink[W, Tag]` | Record changes for later, and apply them. |
| `Ref[T]` | A handle to a value held outside components. |
| `Refs`, `KeyedRefs`, and their lookups | Write or read refs, plain or found by key. |
| `DeltaQuery1` … `DeltaQuery8`, `DeltaRefs` | Walk what changed in the observation window. |
| `ObservationResetSystem[W]`, `ShrinkSystem[W]` | The systems the ECS ships. |

A **system** is a kernel task whose parameters are ECS handles. Each handle
declares its locks at registration, so the kernel runs systems on different
components in parallel and serializes the ones that conflict. The ECS has no
frame of its own: your handlers schedule systems as jobs, in whatever order
your game needs.

## Quick start

One plugin registers a world with two components, spawns three ships, moves
them one step, and prints where they are. `main` publishes the events and
waits for their jobs.

```go
package main

import (
	"fmt"

	"github.com/dvoyni/cog/ecs"
	"github.com/dvoyni/cog/kernel"
)

const shipsName kernel.PluginName = "ships"

// Game is a world: its ID's 128 bits are also each entity's component mask,
// so it holds up to 128 component types.
type Game [2]uint64

// Components are plain, pointer-free values.
type Position struct{ X, Y float32 }
type Velocity struct{ X, Y float32 }

// Setup and Step are events.
type Setup struct{}
type Step struct{ Dt float32 }

// spawnShips holds the barrier, World[Game], so it may spawn.
func spawnShips(k *kernel.Kernel, _ kernel.NoArg, world ecs.World[Game]) kernel.NoResult {
	for i := range 3 {
		e := world.Spawn()
		world.Add(e, Position{})
		world.Add(e, Velocity{X: float32(i + 1)})
	}
	return kernel.NoResult{}
}

// move writes positions (*Position) and reads velocities (Velocity).
func move(k *kernel.Kernel, step Step, q ecs.Query2[Game, *Position, Velocity]) kernel.NoResult {
	for _, row := range q.All() {
		position, velocity := row.Get()
		position.X += velocity.X * step.Dt
		position.Y += velocity.Y * step.Dt
	}
	return kernel.NoResult{}
}

// report only reads positions, so it could run beside any other reader.
func report(k *kernel.Kernel, _ kernel.NoArg, q ecs.Query1[Game, Position]) kernel.NoResult {
	for _, row := range q.All() {
		position := row.Get()
		fmt.Printf("x=%v\n", position.X)
	}
	return kernel.NoResult{}
}

type shipsPlugin struct {
	spawn  *kernel.Task[kernel.NoArg, kernel.NoResult]
	move   *kernel.Task[Step, kernel.NoResult]
	report *kernel.Task[kernel.NoArg, kernel.NoResult]
}

func (p *shipsPlugin) Name() kernel.PluginName { return shipsName }

// Dependencies names the ECS plugin, whose registry RegisterWorld finds.
func (p *shipsPlugin) Dependencies() []kernel.PluginName { return []kernel.PluginName{ecs.PluginName} }

func (p *shipsPlugin) Init(c *kernel.Configurator, config any) error {
	ecs.RegisterWorld[Game](c, ecs.WorldOptions{Entities: 1024})
	ecs.RegisterComponent[Game, Position](c, "position")
	ecs.RegisterComponent[Game, Velocity](c, "velocity")

	// A system's parameters declare its locks at registration.
	p.spawn = c.RegisterTask1(spawnShips)
	p.move = c.RegisterTask1(move)
	p.report = c.RegisterTask1(report)

	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event Setup) (Setup, kernel.JobHandle) {
		return event, ex.Schedule(p.spawn, kernel.NoArg{}, in).Handle()
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, step Step) (Step, kernel.JobHandle) {
		moved := ex.Schedule(p.move, step, in)
		return step, ex.Schedule(p.report, kernel.NoArg{}, moved.Handle()).Handle()
	})
	return nil
}

func (p *shipsPlugin) Start(k *kernel.Kernel) error { return nil }
func (p *shipsPlugin) Stop(k *kernel.Kernel) error  { return nil }
func (p *shipsPlugin) Destroy() error               { return nil }

// publishAndWait publishes event and waits for the jobs it started.
func publishAndWait[E any](k *kernel.Kernel, event E) {
	_, job := k.Publish(event)
	signal := kernel.NewSignal()
	k.Notify(signal, job)
	<-signal.C()
}

func main() {
	s := kernel.NewSupervisor(nil)
	defer s.Quit()
	// The ECS plugin is listed before the plugins that register worlds.
	id, err := s.Spawn(nil, 4, ecs.NewPlugin(), &shipsPlugin{})
	if err != nil {
		panic(err)
	}
	k := s.GetKernel(id)
	publishAndWait(k, Setup{})
	publishAndWait(k, Step{Dt: 0.5})
}
```

Output, newest entity first, since a query walks backwards:

```
x=1.5
x=1
x=0.5
```

## Worlds

A world is a named array of `uint64`s. Its type is its identity, and its size
is how many component types it holds, 64 per word:

```go
type Game [2]uint64 // up to 128 component types
type View [2]uint64 // another world of the same size
```

Register it in a plugin's `Init`, with that plugin listed after the ECS
plugin:

```go
ecs.RegisterWorld[Game](c, ecs.WorldOptions{
	FirstIndex: 0,         // the allocation range, inclusive:
	LastIndex:  1<<20 - 1, // 0 means the top of the 32-bit range
	Entities:   4096,      // room to prepare up front
})
```

- **Several worlds per kernel.** Each has its own resources, so they never
  contend, and one system can take handles of several worlds at once. A
  client keeps its simulated and its displayed world side by side.
- **The allocation range** bounds the indices `Spawn` and `Reserve` hand
  out. Indices outside it are only placed by hand, with `CreateAt`: a client
  mirrors server entities at their server ids and spawns its own from a
  range the server never uses.
- **Indices are reused last in, first out,** so live entities stay packed
  low and reuse is deterministic. A stale `Entity` never comes back to life:
  its generation no longer matches.

## Components

A component is a plain value with no pointers at any depth: no strings,
slices, maps or interfaces. Register each one once per world, under the name
saves know it by:

```go
type Health struct{ Points int32 }
type Frozen struct{} // a tag: a component of size zero

ecs.RegisterComponent[Game, Health](c, "health")
ecs.RegisterComponent[Game, Frozen](c, "frozen")
```

- **Variable-size data goes in a ref:** a component holds a `Ref[T]`, and
  the value lives in the world's refs (see [Refs](#refs)).
- **An `Entity` field is an id, not a link.** Nothing clears it when its
  entity dies; whoever reads it checks it with `WorldLookup.Alive`, or by the
  probe it makes anyway.
- **Markers** are embedded zero-size structs that say what a component is.
  The ECS has one, `Transient`, which keeps a component out of saves; other
  layers define their own and ask `HasMarker[M](info)`.

## Systems

A system is a function of one fixed form: the kernel, the job's arg, up to
eight parameters, and the job's result. Register it with the kernel's
`RegisterTask0` … `RegisterTask8`, by its parameter count:

```go
func heal(k *kernel.Kernel, _ kernel.NoArg, q ecs.Query2[Game, *Health, ecs.Without[Frozen]]) kernel.NoResult {
	for _, row := range q.All() {
		health, _ := row.Get()
		health.Points++
	}
	return kernel.NoResult{}
}

p.heal = c.RegisterTask1(heal)
```

What a system may take, and what each locks:

| Parameter | Locks |
|---|---|
| `Query1[W, *T, U]` … | writes `T`'s store, reads `U`'s |
| `Store[W, T]` / `Lookup[W, T]` | writes / reads `T`'s store |
| `DeltaQuery1[W, T]` … | reads `T`'s store, and observes it |
| `World[W]` | writes the whole world: the barrier |
| `WorldLookup[W]` | reads the world alone |
| `CommandBuffer[W, Tag]` / `CommandSink[W, Tag]` | reads the buffer / writes it and takes the barrier |
| `Refs[W, T]` / `RefsLookup[W, T]` | writes / reads the refs of `T` |
| `kernel.Read[R]` / `kernel.Write[R]` | reads / writes resource `R` |

Every ECS handle also holds a read on its world, so the barrier excludes all
of them. A system is a task like any other: scheduled, depended on and fed
the same way, so ordering systems is ordering jobs.

A system that calls another task inline, with `Access.Use`, is registered
with `RegisterTask` instead, declaring each handle by hand:

```go
p.think = c.RegisterTask(func(access *kernel.Access) kernel.TaskBody[kernel.NoArg, kernel.NoResult] {
	q := ecs.Query1[Game, *Brain]{}.Declare(access)
	positions := ecs.Lookup[Game, Position]{}.Declare(access)
	findPath := access.Use(p.findPath)
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

## Queries

`Query1` … `Query8` walk the entities that hold every term:

| Term | Matches | Yields | Locks |
|---|---|---|---|
| `*T` | has a `T` | its address, marked changed | writes `T` |
| `T` | has a `T` | a copy | reads `T` |
| `With[T]` / `Without[T]` | has / lacks a `T` | nothing to use | reads `T` |
| `Optional[*T]` / `Optional[T]` | with or without a `T` | `Has()`, `Get()` | writes / reads `T` |

```go
func drift(k *kernel.Kernel, step Step, q ecs.Query2[Game, *Position, ecs.Optional[Velocity]]) kernel.NoResult {
	for _, row := range q.All() {
		position, velocity := row.Get()
		if v, ok := velocity.Get(); ok {
			position.X += v.X * step.Dt
		}
	}
	return kernel.NoResult{}
}
```

- **`All()`** yields the entity and its row; `row.Get()` returns the terms.
  It allocates nothing.
- **The smallest store drives** the walk, and the others are probed by index.
  An optional term never drives.
- **The walk goes backwards,** so removing the current entity from any store
  is safe. Adding to a queried store during the walk, or removing another
  entity from the driving one, is not.
- **A `*T` term marks every row it yields as changed.** A pass that writes
  few rows reads with a `T` term and writes through `Store.Write(e)`.
- **`Query0[W]`** walks every live entity, yielding only the entity. Beside
  `World[W]`, it may despawn as it goes, which clears a level in one walk.

## Stores and lookups

A store handle reaches one component by entity, for an entity a query didn't
yield, such as one held in a component:

```go
func chase(k *kernel.Kernel, _ kernel.NoArg, q ecs.Query2[Game, *Position, Target], positions ecs.Lookup[Game, Position]) kernel.NoResult {
	for _, row := range q.All() {
		position, target := row.Get()
		if goal, ok := positions.Get(target.Entity); ok {
			position.X += (goal.X - position.X) / 2
		}
	}
	return kernel.NoResult{}
}
```

- **`Lookup[W, T]`** reads: `Has(e)`, and `Get(e)`, a copy.
- **`Store[W, T]`** writes: `Get(e)` a pointer to read, `Write(e)` a pointer
  marked changed, `Add(e, v)` and `Remove(e)`. Adding and removing need only
  this lock: the entity's mask bit is updated atomically.

## The barrier

`World[W]` is the write on the whole world, so nothing else of `W` runs
beside it. Spawning and despawning change every store an entity has, so they
happen here or through a command buffer.

- **Typed:** `Spawn()`, `Despawn(e)`, `Alive(e)`, `CreateAt(e)`, and
  `Has[T]`, `Get[T]`, `Write[T]`, `Add[T]`, `Remove[T]`, each finding `T`'s
  store by a map lookup: for setup and barrier work, not hot loops.
- **Raw, by `ComponentID`:** `ReadRaw`, `WriteRaw`, `AddRaw`, `RemoveRaw`,
  `SpawnRaw(mask, values)` and `CreateAtRaw`, which work on rows as bytes.
  With `Schema()`, they let a network layer or a tool handle any component
  without knowing its type.

## Command buffers

A command buffer records changes to apply later, so systems that can't hold
the barrier can still spawn, despawn, add and remove, in parallel with each
other. Each buffer is named by a tag type and registered by you:

```go
type AiCmds struct{}

ecs.RegisterCommandBuffer[Game, AiCmds](c)

func spawnWave(k *kernel.Kernel, _ kernel.NoArg, cmds ecs.CommandBuffer[Game, AiCmds]) kernel.NoResult {
	for range 10 {
		e := cmds.Reserve() // an entity now, alive once committed
		cmds.Spawn(e)
		cmds.Add(e, Position{})
		cmds.Add(e, Health{Points: 100})
	}
	return kernel.NoResult{}
}

func commitAi(k *kernel.Kernel, _ kernel.NoArg, sink ecs.CommandSink[Game, AiCmds]) kernel.NoResult {
	sink.Commit()
	return kernel.NoResult{}
}
```

- **Producers run in parallel.** `CommandBuffer` holds a read on its buffer,
  and each system taking it records into a lane of its own.
- **The commit** holds the barrier and applies every record: spawns first,
  then despawns, then the rest in lane and record order. It is
  deterministic.
- **A record on an entity not alive is dropped,** and `Commit` answers how
  many were.
- **Records:** `Reserve`, `Spawn`, `Despawn`, `Add`, `Set` (replaces only)
  and `Remove`. `CommandRefs` and `CommandKeyedRefs` record ref changes into
  the same buffer.
- **Several buffers per world** keep unrelated producers apart, each
  committed where it's needed.

## Refs

Data that can't live in a component (text, lists, meshes) lives in a ref. A
component holds the `Ref[T]` handle, which is pointer-free; the value may
hold anything.

```go
type Label struct{ Text ecs.Ref[ecs.Text] }

ecs.RegisterRefs[Game, ecs.Text](c, "text", ecs.RefsOptions{})

func name(k *kernel.Kernel, _ kernel.NoArg, q ecs.Query1[Game, *Label], texts ecs.Refs[Game, ecs.Text]) kernel.NoResult {
	for _, row := range q.All() {
		label := row.Get()
		if !texts.Alive(label.Text) {
			label.Text = texts.New("unnamed")
		}
	}
	return kernel.NoResult{}
}
```

- **A handle is stable and shared.** `Set` keeps it, so any number of
  components hold one ref, as models share one mesh.
- **You release refs.** The ECS doesn't count holders. A released handle
  reads as nothing: `Get` returns nil and `Alive` false, even once the slot
  is reused.
- **`Refs[W, T]`** writes: `New`, `NewAt`, `Get`, `Set`, `Alive`, `Release`
  and `All`. **`RefsLookup[W, T]`** reads, and several run in parallel.
- **Each payload type is its own resource,** so systems on refs of different
  types run in parallel.

### Keyed refs

Keyed refs hold one ref per key, for values found by what they are, such as
a mesh by its asset path:

```go
type Mesh struct {
	Path     string
	Vertices []float32
}

func (m Mesh) Key() string { return m.Path }

ecs.RegisterKeyedRefs[Game, string, Mesh](c, "mesh", ecs.RefsOptions{})

ref := meshes.Put(Mesh{Path: "ship.mesh"}) // the existing ref of that path, or a new one
```

`KeyedRefs[W, K, T]` writes with `Put`, `PutAt`, `Update`, `Find`, `Get`,
`Alive`, `Release`, `ReleaseKey` and `All`; `KeyedRefsLookup` reads. `Text`
is a string keyed by itself, for names and labels. Don't change a value's key
through the pointer `Get` returns: the ref stays filed under its old key.

## Change detection

A delta query walks the entities whose terms were added, changed or removed
in the current **observation window**, with their values now and as of the
window's start:

```go
func watchHealth(k *kernel.Kernel, _ kernel.NoArg, q ecs.DeltaQuery1[Game, Health]) kernel.NoResult {
	for e, row := range q.All() {
		switch row.Delta() {
		case ecs.DeltaAdded:
			k.Logger().LogDebug("appeared", "entity", e, "health", row.Get().Points)
		case ecs.DeltaChanged:
			k.Logger().LogDebug("hit", "entity", e, "by", row.Previous().Points-row.Get().Points)
		case ecs.DeltaRemoved:
			k.Logger().LogDebug("gone", "entity", e)
		}
	}
	return kernel.NoResult{}
}
```

- **`ObservationResetSystem[W]` ends the window.** Schedule it where your
  frame needs: last, after the network send, after the renderer has read its
  changes.

  ```go
  p.reset = c.RegisterTask1(ecs.ObservationResetSystem[Game])
  ```

- **Off until the first reset.** A world observes nothing until its first
  reset, and every row present then counts as added. A world you never reset
  keeps nothing for observers; one you've reset must keep being reset, or
  its removed rows pile up.
- **Opt-in per store.** Only stores a delta query names keep the copy and
  the state per row this costs; the rest pay nothing.
- **Changes are compared as bytes.** A value written back to what it was
  reads as no delta. Which fields changed is yours to compare.
- **An observer sees writes by systems that ran before it** in the window.
  Order it after the writers with job dependencies.
- **Terms are values,** with `With` and `Without` filters; a `*T` or an
  `Optional` term is a composition error.
- **`DeltaRefs[W, T]`** walks refs added, set or released in the window,
  with their values.

## Saves

`World.Save` and `World.Load` hold the barrier, so a save is a system too:

```go
func save(k *kernel.Kernel, _ kernel.NoArg, world ecs.World[Game]) kernel.NoResult {
	file, err := os.Create("save.json")
	if err != nil {
		k.Logger().LogErr(err)
		return kernel.NoResult{}
	}
	defer file.Close()
	if err := world.Save(file); err != nil {
		k.Logger().LogErr(err)
	}
	return kernel.NoResult{}
}
```

```json
{
	"refs": {
		"mesh": [{"ref": 4294967296, "value": {"Path": "ship.mesh", "Vertices": []}}]
	},
	"entities": [
		{"entity": 4294967296, "position": {"X": 0, "Y": 0}, "health": {"Points": 100}}
	]
}
```

- **Readable JSON,** entity by entity, components and refs by their
  registered names, fields by their Go names. Components marked `Transient`
  and refs registered with `RefsOptions{Transient: true}` are left out.
- **`Load(logger, reader)` adds a save to the world.** Saved entities and
  refs take fresh indices, and every `Entity` and `Ref` field is remapped to
  match.
- **Load forgives drift:** unknown fields are ignored, missing ones are zero,
  and an unknown component is warned of once and skipped. Anything bigger is
  patched in the JSON.
- **Load fails whole.** On an error it leaves the world as it was.

## Memory

Containers grow and keep their capacity, so a running game allocates nothing.
Where you can afford regrowing, such as after a level unloads, give the
memory back with `World.Shrink()`, or by scheduling the system:

```go
p.shrink = c.RegisterTask1(ecs.ShrinkSystem[Game])
```

Nothing alive or recorded is lost, and a stale handle still never comes back
to life.

## Rules

- **Components are pointer-free.** Variable-size data goes in a ref.
  Registration checks it.
- **Composition errors panic** at registration: a world registered before
  the ECS plugin or twice, a component registered twice, more component types
  than the world ID holds, or a handle that can't be declared, such as a
  delta query with a `*T` term.
- **Order systems with job dependencies.** The ECS runs nothing on its own,
  and locks only keep conflicting systems apart; which runs first is yours.
- **Reset what you observe.** Once a world's observation is on, schedule
  `ObservationResetSystem` every frame.
- **Alloc-free on the hot path.** Once the containers have grown, queries,
  system calls, commits and resets allocate nothing; the tests assert it.
