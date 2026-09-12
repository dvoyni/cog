# ecs

`github.com/dvoyni/cog/ecs` lets an app describe things in the world as
**Entities** carrying **Components**, and describe behaviour as **Systems** —
plain Go funcs whose parameter types say what they touch.

**What exists today is the storage the rest of it sits on, and nothing else.**
This package currently holds the Entity handle, the Store that keeps one
Component type, the Entities authority that hands out ids and empties every
Store when one is despawned, and the rule that decides whether a Go type may be
a Component at all. Component registration, Queries, Systems, spawning and the
handler builder are not here yet;
[`docs/specs/ecs.md`](docs/specs/ecs.md) is the specification the whole plugin
is judged against, and this README covers only what is built.

## Files

`contract.go` holds the package documentation and `Entity`; `component.go` the
`PointerFree` rule; `entities.go` the `Entities` authority; `store.go` the
`Store` and the type-erased `storeCore` a despawn reaches every Store through.

## Dependencies

- Go packages: the standard library only
- Plugin dependencies: none — this is not a plugin yet
- Configuration: none

## Entity

```go
type Entity uint64
const NoEntity Entity = 0
```

An `Entity` is an opaque handle to one thing. It is comparable, copyable and
usable as a map key, and its zero value means no Entity. It carries a
generation, so a handle to a despawned entity is detectably stale rather than
silently addressing whatever took its place — which is what makes it safe for a
game to hold entities across frames: a creature's target, a spell's owner, a
projectile's caster.

The index and generation live in one `uint64`, and **that split is not public
contract**: there is no exported `Index` or `Generation`. Both this and a struct
of two `uint32`s are 8 bytes and both are comparable, but the struct leaks its
layout into every call site and can never be re-cut. Generations start at 1, so
`Entity(0)` unambiguously means "no Entity" while index 0 stays an ordinary
usable slot.

`Entity` implements `fmt.Stringer`. `Entity(7v2)` is index 7 at generation 2;
`NoEntity` prints as itself.

## Entities

```go
entities := ecs.NewEntities(maxIDs)   // at the composition root
entities.Alive(e)                     // does this handle name an entity that exists?
```

`Entities` is the id authority: it allocates indices, tracks their generations,
answers `Alive`, and holds a reference to every `Store`. There is exactly one
per Engine, and that is what makes an Engine the boundary of one simulation — a
second simulation is a second Engine. **No identifier in this package contains
the word `World`**, and a test enforces it.

`NewEntities(ids)` reserves room for `ids` indices. The number is the peak
concurrent entity count the app expects, **not a cap**: exceeding it costs a
growth, not an error. It is built at the composition root because both component
registration and the handler builder need the value at registration, where no
resource may be read.

Indices are **recycled through a free list**, which is what bounds every Store's
flat sparse index by *peak concurrent* entities rather than by entities ever
created. Reclamation is **eager and is therefore nothing at all**: a despawn
empties every Store immediately and the index returns to the free list at once.
There is no compaction, no shrink and no sweep anywhere in the package.

**Allocating and despawning are not methods a reader can call.** `Entities` is
held for read by every handler that touches any Store, so the authority to
change which entities exist arrives only through its write-locked promotion
(`WriteableEntities`, in a later ticket) and is visible in a System's signature
and nowhere else. `Alive` is the only question a read may ask, and it is rarely
the one wanted: "is my target still alive" almost always means "does my target
still have `Health`", which a Store probe answers under a lock the System
already holds.

A despawn is **total**. Nothing records which Stores hold an entity, so every
Store is asked, through an interface carrying exactly one method. That is also
why no per-entity index of "which Stores hold me" may ever be added: the index
would live in `Entities`, so maintaining it would move every Component addition
from that Component's lock to the one lock every System holds. **Any global
index is a global lock.**

## Store

```go
store := ecs.NewStore[Position](entities, peak)

store.Set(e, Position{X: 1})   // give e this Component, or replace its value
store.Has(e)                   // one load, one compare
store.Get(e)                   // (Position, bool) — a copy
store.Ref(e)                   // (*Position, bool) — for a writer
store.Remove(e)                // swap-remove; reports whether e had one
store.Len()                    // the population
```

A `Store` is the holding of every value of one Component type, one per
registered type, and **the unit a lock is taken on**. It is three arrays: a flat
`sparse` index of 8-byte slots, an `owners` list from dense row to the full
entity id, and the packed `dense` data. `Len` is exactly the population, because
removal is swap-remove and the packed arrays therefore never contain holes.

**The probe is one load and one compare, and the compare that finds the row is
the compare that rejects a stale handle.** The sparse slot folds the generation
in, so liveness is not an extra cost and there is no separate liveness structure
to consult: a stale handle fails membership for the same reason an absent one
does. A probe reads two arrays, not three — `owners` is for the driver of a
Query, not for the probe.

A Store must be reached as `*Store[T]`. A value store makes a kernel write
handle's `Get` return a copy, so mutations through it are silently discarded.

`NewStore` **enrols the Store with the authority**, which is what lets a despawn
empty it; component registration is its sanctioned caller, and that is where the
pointer-free check and the kernel resource come from. Its `ids` argument is a
reserve hint: reserving buys an allocation-free fill, where growing from empty
doubles repeatedly. **Nothing shrinks** — the arrays are re-sliced, never handed
back, so a respawn reuses the row.

Two consequences of swap-remove are contract:

- **No dense row index may be held across a mutation**, and a `Ref` is valid
  only until the Store next changes structurally.
- **Iteration order is unspecified.** Direction is not: a forward walk that
  removes as it goes silently skips — measured here at 667 of 1000 — because the
  tail row drops into the hole and the next step goes straight past it. A
  reverse walk visits everyone, which is what the Query's `All()` will rest on.

A **Tag** — a Component with no fields — is an ordinary Store whose rows carry
nothing, so its dense array costs no memory however many entities it holds.

## Components: the pointer-free rule

```go
func PointerFree(t reflect.Type) error
```

**A Component type contains no pointers, transitively.** That replaces "a plain
copyable struct" and is better because it is mechanically checkable: this walk,
run once when the type is registered. It forbids pointers, slices, maps,
channels, funcs, interfaces and **strings**, and permits numerics, bools,
fixed-size arrays, `Entity`, and structs of those.

The reason is copying before it is the collector: a Component is copied into and
out of a Store by value and must stay meaningful after the thing it was copied
from is gone. Being pointer-free also makes it trivially serialisable, and puts
the dense array in a span the mark phase never walks.

The error names the offending field **by path**, because the field that fails is
usually several structs down and naming only the Component is useless:

```
ecs.PathedDrawable.Deep.Inner.Path is a string, which is not pointer-free
```

An engine-side thing is named by a hash of its name rather than by the name
itself. Variable-length data has three answers: a child entity with an owning
reference, a fixed-capacity array where the bound is small and real, or a hash.
`sync` types are not special-cased — a mutex is pointer-free by this walk and is
still wrong in a Component, for the same reason `go vet` already says so.
