# ecs zero-allocation prototype

The throwaway prototype behind
[The zero-allocation proof](https://github.com/dvoyni/cog/issues/243).
Branch-only: it is evidence for a design decision, not a package cog ships, and
it is deleted once its numbers are recorded.

A nested module, so `go build ./...` at the repo root skips it. Run with:

```
cd docs/research/ecs-zero-alloc-proto
go test ./...                                     # correctness
go test -run XXX -bench . -benchmem -benchtime 10000x -count 3
go test -run TestSteadyState -v                   # 10 000 frames, absolute bytes
go build -gcflags=-m . 2>&1 | grep "moved to heap"
```

go1.27 windows/amd64, Ryzen 9 7950X3D, NumCPU=32.

## What it is

Not a microbenchmark. The decided design, running on a **real `kernel.Engine`**,
driven by a **real `app.UpdateEvent`** at nox's 30Hz, because the `any`-boxed
resource cell, the real lock acquisition and the reflective call are exactly
where an allocation hides and none of them exist standalone.

It embodies what the map already locked: `Store` as a flat sparse index of
8-byte generation-folded slots over a packed dense array
([#239](https://github.com/dvoyni/cog/issues/239)); the Driver picked per run by
scanning Store lengths; `All()` walking it **backwards**
([#240](https://github.com/dvoyni/cog/issues/240)); a Query as a **struct whose
field pointer-ness is its access mode**, and a System as a plain func turned
into a `(Lock, Observe)` pair at registration
([#238](https://github.com/dvoyni/cog/issues/238)); `Entities` holding every
Store so one lock covers a Despawn; eager despawn; `Spawn[B]` as a handle.

## The answer: zero

`allocs/op` per **whole frame** — publish the tick, acquire every declared lock,
run every System, wait for the publication.

| shape | allocs/op | B/op | ECS share |
| --- | --- | --- | --- |
| nothing subscribed | 2 | 160 | — |
| **1 hand-written subscription, no ECS** | **6** | **320** | baseline |
| 1 System, reflected builder, 1k | **6** | 320 | **0** |
| 1 System, reflected builder, 10k | **6** | 320 | **0** |
| 1 System, baked builder, 1k / 10k | **6** | 320 | **0** |
| 3 Components, 1k / 10k | **6** | 320 | **0** |
| `Without` filter, 1k / 10k | **6** | 320 | **0** |
| 2 Systems, disjoint writes | **10** | 608 | **0** |
| move + spawn + despawn every tick | **10** | 611 | **0** |

The engine charges **2 per publication plus 4 per subscriber**, and every ECS
shape lands exactly on that line. The ECS contributes **nothing**, including
`Spawn` and `Despawn`, which run every tick in the structural row.

Unchanged at ten times the entities, which is the point: an allocation in the
iteration would scale with entity count and none does.

### Steady state, 10 000 frames

An average over `b.N` can hide amortised growth, so this is the absolute count.

| | objects/frame | bytes/frame |
| --- | --- | --- |
| 1 000 entities | 6.005 | 321.9 |
| 10 000 entities | 6.003 | 320.8 |
| with structural change | 10.048 | 617.0 |
| two disjoint Systems | 10.037 | 616.5 |

Flat to three decimal places across a 10× entity range, and 10 000 spawns and
10 000 despawns add nothing: the free list recycles ids and the Store reuses the
dense row, so growth stops at the high-water mark.

### Escape analysis

`go build -gcflags=-m` reports **no `moved to heap` anywhere on the iteration
path**. The only ones left are the hand-written baseline's captured handles,
bound once at registration. A zero result explained by the compiler, not just
observed by the allocator.

## The one allocation, and what it cost to remove

The first run measured **7 allocs / 344 B**, one more than the baseline, and the
compiler named it: `query.go: moved to heap: buf`. `All()` held the fill buffer
as a local and handed `&buf` to an opaque `yield`, so it escaped — **one
allocation per Query per tick**, and exactly 24 B, the width of a two-Component
query struct.

The fix is that the fill buffer is a **field of the `Query`**, which is allocated
once at registration. Nothing changes semantically: the same buffer was already
reused for every Entity, so a System retaining a yielded `*Q` across iterations
was reading the wrong Entity either way.

**This is a constraint the spec has to carry**, not an implementation detail. It
is invisible in a microbenchmark, where the iterator inlines at the range site
and the buffer stays on the stack; it appears only across a package boundary
with a real callee, which is how every real System will be written.

The same hazard, same fix, in `Spawn.New`: taking the address of the bundle
parameter and handing it to a cached closure heap-allocates it per spawn, so the
bundle is staged through a field too.

And a third: a **System must return nothing**. `reflect.Value.Call` allocates for
a callee that returns a value, so the builder rejects one at registration.

## Time, which is also part of the bar

Per entity, derived from the 1k→10k slope so the per-frame floor drops out.

| | ns/entity | × hand-written |
| --- | --- | --- |
| hand-written loop, 2 Components | 2.07 | 1.00 |
| **Query, 2 Components** | **3.21** | **1.55** |
| Query, 3 Components | 10.8 | 5.2 |
| Query, 2 Components + `Without` | ~9 | ~4.3 |

Consistent with [#238](https://github.com/dvoyni/cog/issues/238)'s 1.37× for the
struct fill, against a stricter baseline here (this one probes both Stores).

**A third probe costs far more than the second** — about 7 ns an entity, whether
it is a third Component or a filter. It is *not* the loop shape: unrolling a
three-field path beside the two-field one cost 6% rather than saving any, and
hoisting the field data into scalar registers **doubled** the two-Component
frame (37µs → 73µs at 10k) by stopping `fill` inlining. Both were reverted. Why
the third probe is so expensive is unresolved, and it is the one number here
worth another look before the spec fixes a recommended Query width.

### The reflected call is much more expensive in situ than in a microbenchmark

| | per tick |
| --- | --- |
| baked builder, System that does nothing | 3.04 µs |
| reflected builder, System that does nothing | 5.15 µs |
| **difference** | **2.1 µs** |

Attributed, not guessed: a hybrid builder keeping *every* part of the reflective
path — the same signature reflection, the same `reflect.New` parameters, the
same stable event and Kernel cells — and changing only `fnv.Call(args)` to a
direct call lands on the baked number. So the 2.1 µs is `reflect.Value.Call`
itself.

That contradicts [#234](https://github.com/dvoyni/cog/issues/234)'s ~87 ns, and
the map's Notes repeat that figure. Reproduced here at **79 ns** in a tight loop,
so #234 is not wrong — it is measuring something the engine does not do. Four
candidate explanations were tested and all rejected:

| | gap |
| --- | --- |
| tight loop (reproduces #234) | 78 ns |
| caches evicted between calls | 90 ns |
| fresh goroutine per call | 164 ns |
| fresh goroutine, deep stack | 155 ns |
| **inside a real publication** | **2100 ns** |

The cause is unexplained. The consequence is not: **2.1 µs is comparable to the
whole ~2.2 µs scheduling floor** [#241](https://github.com/dvoyni/cog/issues/241)
measured, so a reflected call roughly doubles the cost of dispatching a cheap
System. Bake the call — which is what the map already preferred, now for a reason
25× larger than the one recorded.

### Concurrency costs no allocation

Two Systems with disjoint write sets (`Body` against `Health`):

| | GOMAXPROCS=1 | GOMAXPROCS=32 | speedup | allocs |
| --- | --- | --- | --- | --- |
| 1 000 entities | 12.3 µs | 13.3 µs | **0.92×** | 10 both |
| 10 000 entities | 64.6 µs | 44.0 µs | **1.47×** | 10 both |

The scheduler does run them concurrently, and parallelism is free of
allocations. At 1 000 entities running them in parallel is *slower* than
serialising — independently reproducing
[#241](https://github.com/dvoyni/cog/issues/241)'s finding that a System doing
little work is cheaper to run than to schedule.

## Files

- `ecs/store.go` — `Store[T]`, the flat sparse index, swap-remove, and the
  type-erased `storeCore` that lets iteration and Despawn avoid generics.
- `ecs/entities.go` — the id authority, the free list, eager Despawn across
  every Store, and `RegisterComponent[C]`, which bakes the per-type closures
  reflection cannot produce.
- `ecs/query.go` — the struct Query, registration-time `plan`, per-tick Driver
  selection, and the backwards `All()`.
- `ecs/handler.go` — `ToHandler` (reflective), `ToHandler1`/`ToHandler2`
  (baked), `ToHandlerHybrid` (the attribution probe), `Spawn[B]`,
  `WriteableEntities`.
- `world.go` — Components, Queries, Systems and the plugin, on a real engine.
- `bench_test.go` — whole-frame benchmarks, steady state, correctness.
- `call_test.go` — the four rejected explanations for the reflected-call gap.
