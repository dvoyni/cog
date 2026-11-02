# ECS

The ECS is Cog's entity component system: worlds of entities whose data lives in plain, pointer-free components, worked on by systems that run as kernel tasks. It knows nothing about networking, rendering or any other consumer. This glossary records its domain language; the kernel's own terms (resource, task, job, lock set) are in [kernel/CONTEXT.md](../kernel/CONTEXT.md).

## Worlds and entities

**World**:
One set of entities and their components inside a kernel, named by its world ID. A kernel may hold several worlds.
_Avoid_: Scene, registry, kernel

**World ID**:
A type that names a world, such as `type Game [2]uint64`. Its values are the world's component masks, one per entity, so its size bounds how many component types the world holds.
_Avoid_: World tag, mask type

**Entity**:
An identity in one world: an index and a generation. It holds no data itself.
_Avoid_: Object, actor, handle

**Allocation range**:
The span of indices a world hands out to new entities. Indices outside it are only ever placed with `CreateAt`.

**Reservation**:
An entity taken from the world for a later spawn. It is not alive until a commit spawns it.

## Data

**Component**:
A pointer-free value type attached to entities. Each component type has one store per world.
_Avoid_: Property, field, attribute

**Store**:
All values of one component type in one world, and the unit a system locks.
_Avoid_: Pool, table, column, archetype

**Ref**:
A pointer-free handle, held in a component, to a value of any type kept outside the stores: text, a list, anything a component cannot hold. Setting its value gives it a new handle, so the component holding it changes.
_Avoid_: Pointer, reference, asset

**Refs**:
All refs to values of one type in one world, and the unit a system locks for them.
_Avoid_: Arena, heap, blob store

## Systems

**System**:
A function of a fixed form (kernel, arg, then parameters, returning a result) that is registered as a kernel task.
_Avoid_: Handler, job, behaviour

**System parameter**:
A value a system takes whose type declares, at registration, the locks it needs: a query, a store, changes, commands, the world, or a kernel resource handle.

**Query**:
A system parameter that walks every entity holding a set of components. A pointer term writes its component, a value term reads a copy.
_Avoid_: View, filter, iterator

**Barrier**:
A write on a world, which excludes every other system of that world while it runs. Structural and type-erased work happens under it.

## Change detection

**Observed store**:
A store some system reads changes from. Only observed stores pay for change detection.

**Change**:
One entity's component added, changed or removed since the last observation reset, with its current and previous value.
_Avoid_: Event, diff, dirty mark

**Shadow**:
An observed store's copy of each value as of the last observation reset, which a change's previous value comes from.

**Observation window**:
What an observer can see: changes made by systems that ran before it since the last observation reset.

**Observation reset**:
The system that ends an observation window: it brings every shadow up to date and frees despawned indices. Its first run turns observation on; a world it never runs in observes nothing. The user decides where it runs.
_Avoid_: Flush, clear, frame end

## Commands

**Command buffer**:
A resource holding recorded changes to a world, to be applied together later. A world may have several, each a distinct type.
_Avoid_: Queue, deferred list

**Lane**:
One producing system's own part of a command buffer, which it appends to without contention.

**Commit**:
Applying a command buffer's records to its world, then emptying its lanes. It runs in a system the user writes.
_Avoid_: Drain, flush, apply
