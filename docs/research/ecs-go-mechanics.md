# What Go 1.27 permits for zero-allocation iteration and reflective calls

Research for [dvoyni/cog#234](https://github.com/dvoyni/cog/issues/234), child of the
ecs map [#180](https://github.com/dvoyni/cog/issues/180). Answers what
range-over-func `iter.Seq2` actually costs, what `reflect.Value.Call` actually costs,
and what an `any`-boxed resource cell does to a component store.

Every number here comes from a benchmark in `docs/research/ecs-go-mechanics-bench/`
on this branch, run on this machine. Every number is *explained* from
`go build -gcflags=-m` escape analysis or from the Go source in `$GOROOT`, not from
folklore. **Two claims the map and the ticket carry turn out to be false; they are
called out in [What this overturns](#what-this-overturns).**

## Environment

```
go version go1.27.1 windows/amd64
AMD Ryzen 9 7950X3D 16-Core Processor, GOMAXPROCS=32
```

Workload is a 1024-entity store of a 16-byte `Body{X, Y, VX, VY float32}`. "Per op"
in the iteration tables means one full pass over all 1024 entities, i.e. what one
system does to one archetype in one frame.

## Methodology note: `testing.B.Loop` distorts this measurement

`go doc testing.B.Loop`: *"arguments to and results from function calls and assigned
variables within the loop are kept alive... implemented as a compiler transformation
that wraps such variables with a `runtime.KeepAlive` intrinsic call."*

That pins a loop-carried accumulator to memory and blocks register allocation, which
flattens exactly the difference this ticket is about. With `b.Loop`, the inlined and
the boxed iterator both measured ~2450 ns/op and looked identical. With the classic
`for i := 0; i < b.N; i++` form they are 622 ns and 2470 ns. **Allocation counts are
unaffected either way** — escape analysis does not care about `KeepAlive`, and the
`b.Loop` and classic runs agree on `allocs/op` everywhere. The timing tables below
use the classic form (`speed_test.go`); the allocation tables use
`testing.AllocsPerRun` plus `runtime.MemStats` deltas (`allocs_test.go`).

---

## 1. Iteration

### 1.1 The single rule that decides everything

A range-over-func yield closure stays on the stack **iff the range statement's
compilation unit can see one unambiguous func literal at the range site.** That is
the whole rule. Package boundaries, `break`, capture, nesting and value receivers do
not enter into it.

The compiler rewrites `for k, v := range seq { body }` into a yield closure
`#yield1` plus a range-state variable `#state1`, then calls `seq(#yield1)`. If `seq`
resolves to a literal, the call is devirtualized, `store.(*Store).All.func1` is
inlined, its `yield` parameter is proven non-escaping, and everything stays on the
stack. If `seq` is opaque, escape analysis must assume the callee retains the
closure, so the closure, `#state1`, and every loop-body local the closure captures are
moved to the heap.

The compiler says so directly (`go build -gcflags=-m -m`, on the boxed shape):

```
escape.go:54:2: func literal escapes to heap in EscViaHandle:
escape.go:54:2:   flow: {heap} ← #yield1:
escape.go:54:2:     from .autotmp_5(#yield1) (call parameter) at escape.go:54:2
escape.go:53:6: moved to heap: sum
escape.go:54:2: moved to heap: #state1
```

### 1.2 Escape analysis by shape

From `go build -gcflags='...=-m' ./docs/research/ecs-go-mechanics-bench/`, over
`escape.go`:

| Shape | Verdict |
| --- | --- |
| `range s.All()`, same package, `All` inlinable | `func literal does not escape` |
| `range s.All()`, **different package**, `All` inlinable | `func literal does not escape` |
| `range s.All()` on a **value receiver** `ValStore` | `func literal does not escape` |
| `seq := s.All(); range seq` — one assignment | `func literal does not escape` |
| `seq := s.All(); if x { seq = s.AllFat() }; range seq` — **two** assignments | `func literal escapes to heap` |
| `range s.AllFat()` where `AllFat` is `//go:noinline` | `func literal escapes to heap` |
| `range seq` where `seq` is a **func parameter** | `func literal escapes to heap` |
| `range v.(iter.Seq2[...])` out of an **`any`** | `func literal escapes to heap` |
| **`range Read[iter.Seq2[...]].Get()`** — the kernel handle shape | `func literal escapes to heap` |
| **`range Read[*Store].Get().All()`** — handle holds the store | `func literal does not escape` |
| `break` out of the loop — inlinable / boxed | no change to either verdict |
| `return` out of the loop — inlinable | `func literal does not escape` |
| `return` out of the loop — boxed | escapes, **and the return value `#rv1` is moved to the heap too** |
| loop body writes an outer variable | no change to either verdict |
| nested range-over-func — inlinable | both literals stay on the stack |
| nested range-over-func — boxed | both literals escape, plus `#state1` **and** `#state2` |

The package boundary is irrelevant because Go's export data carries inlinable
function bodies. `All` is 36 cost units against a budget of 80, so it crosses
packages and still inlines. The two-assignment case is the precise boundary: the
compiler devirtualizes a func value in a local only while that local has a single
visible producer.

### 1.3 Allocations per pass (1024 entities)

`testing.AllocsPerRun` + `MemStats`, `go test -run TestExactAllocations -v`:

| Shape | allocs/op | B/op |
| --- | ---: | ---: |
| plain `for i := range` slice loop | 0 | 0 |
| `range s.All()` (inlinable) | **0** | **0** |
| `range seqVar` (single-assignment local) | **0** | **0** |
| **`range Read[*Store].Get().All()`** | **0** | **0** |
| `range s.AllFat()` (`//go:noinline`) | 3 | 56 |
| `range boxed.(iter.Seq2)` (bare `any`) | 3 | 40 |
| **`range Read[iter.Seq2].Get()`** | **2** | **40** |
| `range Read[iter.Seq2].Get()` **with `break`** | 2 | 40 |
| nested `range s.All()` × 2 (inlinable) | **0** | **0** |
| nested `range Read[iter.Seq2].Get()` × 2 | **2050** | **57411** |
| nested: boxed outer, inlinable inner | 2 | 64 |

Composition of the boxed shape's 2 allocations, isolated by varying what the loop
body captures:

| Boxed loop body captures | allocs/op | B/op |
| --- | ---: | ---: |
| nothing (writes a package var) | 2 | 24 |
| 1 local | 2 | 40 |
| 3 locals | 3 | 72 |

So the floor is **2 allocations / 24 B per range statement** — the yield closure and
`#state1` — and it grows with the number of loop-body locals the closure captures.
These are per *range statement execution*, i.e. per system per archetype per frame,
not per entity.

**Except when nested.** `2050 = 2 + 2 × 1024`: the inner range statement rebuilds its
yield closure on every outer iteration. A two-component query written as a nested
range-over-func through a boxed seq allocates **per entity**, 57 KB per frame at 1024
entities. Making only the *inner* loop devirtualizable drops it straight back to 2.
This is the single sharpest hazard found.

### 1.4 The allocation is the smaller half of the cost

`go test -bench BenchmarkSpeed -benchmem -count 3 -benchtime 200000x`, one 1024-entity
pass per op:

| Shape | ns/op | allocs/op | B/op |
| --- | ---: | ---: | ---: |
| plain slice loop | 606 | 0 | 0 |
| `range s.All()` (inlinable) | 622 | 0 | 0 |
| `range seqVar` (single-assignment local) | 630 | 0 | 0 |
| **`range Read[*Store].Get().All()`** | **608** | **0** | **0** |
| `range s.AllFat()` (`//go:noinline`) | 2540 | 3 | 48 |
| **`range Read[iter.Seq2].Get()`** | **2470** | **2** | **32** |
| opaque seq, body captures nothing | 2465 | 2 | 24 |

Two things to read off this.

**Range-over-func is free when devirtualized.** 622 ns versus 606 ns for a hand-written
slice loop — 2.6% — and `Read[*Store].Get().All()` at 608 ns is inside the noise of the
raw loop. The generic `Read[T].Get()` type assertion contributes nothing measurable.

**Boxing costs ~4× — and only about 3% of that is the allocation.** The boxed shape
costs 1848 ns more per pass. Two heap allocations are ~50 ns of it. The other ~1800 ns
is that the yield closure cannot be inlined into the iterator body, so 1024 straight-line
loop iterations become 1024 indirect calls — about 1.8 ns per entity. **The penalty
scales with entity count, not with frame count.** A zero-allocation criterion alone
would not have caught this: the "body captures nothing" row allocates the same 2 objects
as the others and is still 4× slower than the inlined shape.

### 1.5 GOMAXPROCS and heap size

`-cpu 1,2,4,8,16,32`, classic form, 1024-entity pass per op:

| GOMAXPROCS | inlined ns/op | boxed ns/op | penalty |
| ---: | ---: | ---: | ---: |
| 1 | 631 | 2604 | 4.1× |
| 2 | 323 | 1303 | 4.0× |
| 4 | 164 | 666 | 4.1× |
| 8 | 84.4 | 355 | 4.2× |
| 16 | 46.2 | 234 | 5.1× |
| 32 | 30.6 | 163 | 5.3× |

**Yes, there is a cost an idle single-threaded microbenchmark hides, but it is modest
and not a cliff.** The penalty is flat at ~4.1× to 8 Ps and widens to 5.3× at 32.
Across 1→32 Ps the inlined shape speeds up 20.6× and the boxed shape 16.0×. Both still
scale; the allocating one just scales slightly worse.

**A large live heap changes nothing measurable.** With a 1 GiB ballast
(`BenchmarkSpeedViaHandleParBallast`), the boxed shape is 147–156 ns/op at GOMAXPROCS=32
versus 151–157 ns/op without; the inlined shape is 25–26 ns versus 24–29 ns. The GC was
genuinely running: `GODEBUG=gctrace=1` over the same run counts **47 GC cycles with
ballast against 33 without**. At this allocation volume the mark cost is not where the
time goes.

---

## 2. Reflective call construction

### 2.1 `reflect.Value.Call` does not allocate

This is the finding that overturns the map's premise. `-benchtime 1000000x -count 3`,
`[]reflect.Value` built once at setup and reused, callee returns nothing:

| Call | ns/op | allocs/op | B/op |
| --- | ---: | ---: | ---: |
| direct typed closure (baseline) | 2.37 | 0 | 0 |
| interface method call | 2.37 | 0 | 0 |
| `reflect.Value.Call`, arity 0 | 58.5 | **0** | **0** |
| `reflect.Value.Call`, arity 1 | 66.7 | **0** | **0** |
| `reflect.Value.Call`, arity 2 | 78.0 | **0** | **0** |
| `reflect.Value.Call`, arity 3 | 87.2 | **0** | **0** |
| `reflect.Value.Call`, arity 4 | 97.2 | **0** | **0** |
| `reflect.Value.Call`, arity 8 | 159 | **0** | **0** |
| `reflect.Value.Call`, arity 12 | 197 | **0** | **0** |
| arity 3, one 512 B struct arg | 93.0 | **0** | **0** |
| arity 3, **args re-boxed every call** (pointer-shaped) | 85.7 | **0** | **0** |

About **10 ns per additional argument**, no allocation at any arity tested.

Why, from the Go source (`$GOROOT/src/reflect/value.go:473-484`):

```go
frametype, framePool, abid := funcLayout(t, rcvrtype)
var stackArgs unsafe.Pointer
if frametype.Size() != 0 {
    if nout == 0 {
        stackArgs = framePool.Get().(unsafe.Pointer)
    } else {
        // Can't use pool if the function has return values.
        // We will leak pointer to args in ret, so its lifetime is not scoped.
        stackArgs = unsafe_New(frametype)
    }
}
```

and on the way out (`value.go:594-598`) the frame is `typedmemclr`ed and returned to
the pool. Two separate mechanisms produce the zero:

- **`frametype.Size() == 0`.** Under the register ABI, a callee whose arguments all fit
  in argument registers has no stack frame at all, so the pool is never touched.
- **`nout == 0` plus a `sync.Pool` hit.** A callee that does spill to a stack frame
  takes it from a pool and gives it back.

### 2.2 Where `Call` *does* allocate

| Call | ns/op | allocs/op | B/op |
| --- | ---: | ---: | ---: |
| callee **returns a value** | 92.5 | **2** | **28** |
| boxing a 4-byte `float32` arg per call | 110 | **1** | **4** |
| boxing a `float32` + a 512 B struct per call | 181 | **2** | **516** |

Three rules, and each matters to the spec:

1. **A system func must return nothing.** A single return value costs 2 allocations —
   `unsafe_New(frametype)` because the pool is unusable, plus `ret := make([]Value, nout)`.
2. **Arguments must be pointer-shaped, or boxed once at registration.** `reflect.ValueOf`
   of a pointer, a func value, or any single-pointer struct is free because the interface
   holds the pointer directly. `reflect.ValueOf` of a value type heap-allocates a copy.
   A query handle shaped like `Query[T]{s *Store}` is pointer-shaped and therefore free
   even if re-boxed every frame.
3. **The pool is a cache, not a guarantee.** `sync.Pool` is emptied at every GC, with a
   victim cache that carries an entry through exactly one cycle. Measured, with the
   callee's arguments forced onto a stack frame and two `runtime.GC()` calls before every
   invocation (which drops both primary and victim):

   | Callee | normal | after 2 forced GCs |
   | --- | ---: | ---: |
   | `sys3` — 3 pointer args, all in registers | 0 allocs | **0 allocs** |
   | `sys12` — 12 pointer args, spills past 9 registers | 0 allocs | 3 allocs / 4900 B |
   | `sysBig4` — 4 × 512 B by value, 2 KiB frame | 0 allocs | 3 allocs / 6925 B |
   | *control: 2 forced GCs, no call at all* | — | 0 allocs / 3 B |

   Under a realistic GC rate rather than a forced one, the pool holds: the same call
   against a background allocation churn goroutine measured **0 allocs/op**. The safe
   design rule is nonetheless to keep a system's parameters inside the register ABI
   (on amd64, ≤9 integer words), which makes the zero unconditional rather than cached.

### 2.3 `reflect.MakeFunc` does not help here

| Call | ns/op | allocs/op | B/op |
| --- | ---: | ---: | ---: |
| `MakeFunc` → typed func → call, body reads args | 79.4 | **1** | **80** |
| `MakeFunc` → typed func → call, body ignores args | 77.2 | **1** | **80** |

`MakeFunc` builds a func value of a type known only at runtime, which is genuinely
useful — but calling it enters reflect's `callReflect` trampoline, which allocates the
`in []Value` slice every invocation. It is strictly worse than `Value.Call` on both
axes (80 B/call versus 0) and buys nothing the ECS needs: the resulting func still
cannot be called as typed Go code without a type assertion naming its type literally.

### 2.4 Generic instantiation from a `reflect.Type`: exactly where the wall is

**You cannot.** Type arguments are syntax, not values. The spec's *Instantiations*
section (`$GOROOT/doc/go_spec.html`) defines instantiation as substituting *type
arguments* for type parameters "across the entire function or type declaration", and
type arguments are either written or inferred "from the context in which the function
is used" — all of it static. A `reflect.Type` is a runtime value and has no path into
that grammar. The compiler says it plainly:

```go
var t reflect.Type = reflect.TypeFor[int]()
_ = NewStore[t]()          // t (local variable) is not a type
var _ []t                  // t (local variable) is not a type
_ = reflect.TypeFor[t]()   // t (local variable) is not a type
```

`reflect` offers no instantiation API: `New`, `MakeFunc`, `MakeSlice`, `MakeMap`,
`MakeChan` all build *values* of a runtime type, never code specialized to one.

**The way round the wall is not to cross it.** A `reflect.Type` can be mapped back to
generic code that some *compile-time* call site already instantiated. That is what
registration is for:

```go
type binder interface {                    // non-generic
    Bind(s *store.Store) reflect.Value
    Type() reflect.Type
}
type typedBinder[T any] struct{}           // generic implementation
func RegisterComponent[T any]() {          // the call site that instantiates
    b := typedBinder[T]{}
    registry[b.Type()] = b                 // map[reflect.Type]binder
}
```

After that, a system of *arbitrary* arity can be baked by walking its `reflect.Type`,
pulling each parameter's binder out of the registry, and closing over the resulting
`[]reflect.Value`. Measured (`bakeByReflection`): **arity 3 → 86.8 ns/op, 0 allocs;
arity 8 → 142 ns/op, 0 allocs.**

Two constraints on that shape, both confirmed by the compiler:

- **An interface method may not have type parameters.** `Bind[T any]() T` in an
  interface is rejected: `interface method must have no type parameters`. The generic
  work must happen at a concrete call site and be stored behind a plain interface, as
  above.
- **Generic methods on concrete types are available in Go 1.27.** The spec's
  `MethodDecl` grammar carries `[ TypeParameters ]`, and *Instantiations* is tagged
  `[Go 1.18][Go 1.27]` — the 1.27 half being generic methods. `func (a Access) Get[T any]() T`
  compiles, which is what lets `kernel.ResourceAccess.GetRead[T]` exist.

### 2.5 The fixed-arity generic fallback

| Bake mechanism | ns/op | allocs/op | arity |
| --- | ---: | ---: | --- |
| direct typed closure | 2.37 | 0 | — |
| type switch on the concrete func type | 2.40 | 0 | each arity named literally |
| **generic `System3`** | **2.40** | **0** | fixed at 3 |
| **generic `System8`** | **2.44** | **0** | fixed at 8 |
| `reflect.Value.Call`, baked args, arity 3 | 86.8 | 0 | **any** |
| `reflect.Value.Call`, baked args, arity 8 | 142 | 0 | **any** |

The fixed-arity constructor is 36× faster and is indistinguishable from a direct call,
because it *is* one — the closure `func() { fn(qa, qb, qc) }` holds pre-bound values:

```go
func System3[A, B, C any](fn func(Query[A], Query[B], Query[C]), s *Store) (func(), []reflect.Type) {
    qa, ta := bindQuery[A](s)
    qb, tb := bindQuery[B](s)
    qc, tc := bindQuery[C](s)
    return func() { fn(qa, qb, qc) }, []reflect.Type{ta, tb, tc}
}
```

**Arity reach without codegen.** Type inference scales cleanly: `System8(genericSys8, s)`
is written with **no explicit type arguments** and the compiler infers all eight from the
function's parameter types, then `reflect.TypeFor[A]()` … `reflect.TypeFor[H]()` yields
the lock set — verified by `TestGenericArityInfersLockSet`, which gets back
`[C1 C2 C3 C4 C5 C6 C7 C8]`. So the arity ceiling is not a language limit at all. It is
the number of `SystemN` constructors someone is willing to hand-write, each about 12 lines
and each mechanically identical to the last. There is no inference cliff, and no
combinatorial blow-up: `SystemN` is linear in N, not exponential, because the type
parameters are independent.

**Cost framing for the arbitrary-arity path.** 86.8 ns per system per frame. At 60 Hz
with 200 registered systems that is 17 µs of a 16.6 ms frame, or 0.1%. `reflect.Value.Call`
is not the disqualifying cost the map assumed.

---

## 3. The `any` boxing question

`kernel.resource.value` is an `any` and `resource.get[T]()` does `r.value.(T)`
(`kernel/resource.go:17-23`). Replicated verbatim in `kernelshape.go`.

### 3.1 `Read[T].Get()` — never allocates, but always copies

| `T` | ns/op | allocs/op |
| --- | ---: | ---: |
| `*Store` (pointer) | **0.61** | 0 |
| `iter.Seq2[...]` (func value) | **0.61** | 0 |
| 16 B struct | 1.06 | 0 |
| 512 B struct | 26.0 | 0 |
| 4 KiB struct | 126 | 0 |
| 4 KiB struct, reading **one field** | 62.9 | 0 |
| `*Huge` — 4 KiB behind a pointer, one field | **0.60** | 0 |

**Confirmed: for a store held as a pointer the type assertion is free.** 0.61 ns is a
single load; a pointer is stored directly in the interface word, so `r.value.(*Store)`
is a type-word compare and a move. A func value is pointer-shaped too, so an
`iter.Seq2` in the cell is equally free to extract — the seq's problem in §1 is not
the assertion, it is the loss of devirtualization downstream.

**Confirmed: a non-pointer store copies the whole struct on every access.** The
assertion materialises a `T`, so it copies out of the heap-boxed data even when the
caller wants one field: 62.9 ns to read `F[0]` from a boxed 4 KiB struct, against
0.60 ns through a pointer — **105× worse**. It never shows up as `allocs/op`, because
the copy goes to the caller's stack. A zero-allocation criterion alone would not catch
this either.

### 3.2 `Write[T].Set()` — a non-pointer store allocates on every write

| `T` | ns/op | allocs/op | B/op |
| --- | ---: | ---: | ---: |
| `*Store` (pointer) | **0.80** | **0** | **0** |
| 16 B struct | 11.3 | **1** | **16** |
| 4 KiB struct | ~700 | **1** | **4096** |

Assigning a non-pointer-shaped value to an `any` field heap-allocates a copy to box it.
A pointer needs no box.

### 3.3 A non-pointer store is also silently wrong

Not a performance finding. `Write[T].Get()` returns `T` **by value**, so mutating what
comes back mutates a temporary and the write is discarded with no error.
`TestNonPointerStoreLosesMutation` proves both halves: through `*counterStore` two
`Inc()` calls leave `n == 2`; through `counterStore` by value they leave `n == 0`.

**So the spec must require a component store to be a pointer, and the reason is
correctness first and cost second.** A value store loses writes, allocates 1× per
`Set`, and copies the entire store per `Get`.

---

## What this overturns

**1. "`reflect.Value.Call` allocates an argument slice and boxes every argument, per
call, per frame" — false as stated.** This appears in #180's Notes and again in #234 §2.
Measured on Go 1.27: `Call` allocates **nothing** for a result-less callee, at every
arity from 0 to 12, whether the `[]reflect.Value` is reused or rebuilt each call.
`funcLayout` gives a register-ABI callee no stack frame at all, and pools the frame for
the rest. The claim is true only under conditions the ECS controls and can simply avoid:
a callee that returns a value (2 allocs), or arguments that are not pointer-shaped and
are re-boxed each frame (1 alloc each). The map's fallback to fixed-arity generic
constructors is still *faster* — 2.4 ns against 86.8 ns — but it is a performance
preference, not the forced move the map recorded it as.

**2. The iteration risk is real but mis-located, and the allocation is the smaller
half.** #234 §1 asks about package boundaries, `break`, and capture. None of the three
matters — all measured 0 allocs/op. The one thing that matters is whether the range
statement can see an unambiguous func literal, which an `any`-boxed `iter.Seq2` destroys
absolutely. And the damage is not mainly the 2 allocations (~50 ns): it is 1024 indirect
yield calls instead of an inlined loop, ~1800 ns per 1024-entity pass, **a cost that
scales with entity count and that a pure `allocs/op` acceptance criterion would score as
a pass.** The map's stated requirement — "zero heap allocation on the hot path, proven by
measurement" — is necessary but not sufficient for this design; `ns/op` against the
hand-written slice loop has to be measured beside it.

**3. Nested boxed iteration is the sharp edge.** Two nested range-over-func loops over a
boxed seq cost **2050 allocs and 57 KB per frame** at 1024 entities, because the inner
yield closure is rebuilt per outer entity. Nested inlinable loops cost 0. Whatever the
query API looks like, a multi-component query must not compile into a nested range over
an opaque sequence.

## Consequences for the shapes under design

Stated as facts, not recommendations.

- **`kernel.Read[iter.Seq2[Entity, T]]` is the one shape that does not work.** Boxing
  through `resource.value` makes the yield closure escape unconditionally: 2 allocs and
  ~4× slower per pass, 2050 allocs when nested.
- **`kernel.Read[*Store]` with the iterator produced inside the handler costs nothing.**
  0 allocs, 608 ns against 606 ns for a raw slice loop. The handle, the generic method,
  the type assertion and the range-over-func together are unmeasurable.
- **A component store must be a pointer** — `Write[T].Get()` on a value store silently
  discards mutations, `Set` allocates, and `Get` copies the store.
- **A System func must return nothing**, or `reflect.Value.Call` costs 2 allocs/call.
- **Query parameters should be pointer-shaped** (e.g. `Query[T]{s *Store}`), which makes
  `reflect.ValueOf` free and keeps the callee inside the register ABI, so `Call`'s zero
  is unconditional rather than dependent on a `sync.Pool` hit.
- **Arbitrary arity is reachable** by registering a non-generic binder per component type
  at a generic call site, then walking the system's `reflect.Type`: 86.8 ns at arity 3,
  142 ns at arity 8, 0 allocs. **Generic methods cannot live in an interface**, so the
  generic-to-non-generic hand-off has to happen at a concrete registration call site.
- **`.github/instructions/kernel.instructions.md`'s "Keep `Lock` Straight-Line" rule and
  a reflective lock derivation are not actually in tension** in the fixed-arity shape:
  `System3`'s `bindQuery[A]`/`bindQuery[B]`/`bindQuery[C]` are three unconditional
  straight-line calls, one per type parameter, with no loop.

## Reproducing

Benchmark code is `docs/research/ecs-go-mechanics-bench/` on branch
`research/ecs-go-mechanics`. It is throwaway; it is not production code and is not meant
to be merged.

```sh
go test ./docs/research/ecs-go-mechanics-bench/ -run TestExactAllocations -v
go test ./docs/research/ecs-go-mechanics-bench/ -run '^$' -bench BenchmarkSpeed  -benchmem -count 3 -benchtime 200000x
go test ./docs/research/ecs-go-mechanics-bench/ -run '^$' -bench BenchmarkReflect -benchmem -count 3 -benchtime 1000000x
go test ./docs/research/ecs-go-mechanics-bench/ -run '^$' -bench 'BenchmarkGet|BenchmarkSet' -benchmem -count 3 -benchtime 2000000x
go test ./docs/research/ecs-go-mechanics-bench/ -run '^$' -bench 'SpeedInlinedPar$|SpeedViaHandlePar$' -benchmem -cpu 1,2,4,8,16,32 -benchtime 200000x

go build -gcflags='github.com/dvoyni/cog/docs/research/ecs-go-mechanics-bench=-m' ./docs/research/ecs-go-mechanics-bench/
```

| File | What it holds |
| --- | --- |
| `escape.go` | Minimal non-test functions, one per shape, so `-gcflags=-m` output is readable |
| `kernelshape.go` | Verbatim copy of `kernel/resource.go`'s cell, `Read`, `Write` |
| `store/store.go` | A component store in a *different package*, with inlinable and `//go:noinline` iterators |
| `allocs_test.go` | Exact allocs/op and B/op per iteration shape |
| `speed_test.go` | Classic `b.N` timing, serial and parallel, with and without a 1 GiB ballast |
| `reflectcall_test.go` | `Call` arity sweep, `MakeFunc`, both bake mechanisms, pool-eviction probes |
| `boxing_test.go` | `Read[T].Get()` / `Write[T].Set()` by shape, plus the lost-mutation test |

## Sources

- `$GOROOT/src/reflect/value.go:468-499, 560-605` — `funcLayout`, the `framePool`
  fast path, and the `nout != 0` fallback to `unsafe_New`.
- `$GOROOT/doc/go_spec.html` — *Instantiations* (type arguments are substituted
  statically; tagged `[Go 1.18][Go 1.27]`), *Method declarations* (`MethodDecl`
  grammar carrying `[ TypeParameters ]`).
- `go doc testing.B.Loop` — the `runtime.KeepAlive` transformation.
- `go build -gcflags=-m -m` on `docs/research/ecs-go-mechanics-bench` — every escape
  verdict in §1.2.
- `cog` `kernel/resource.go` — the cell shape replicated in `kernelshape.go`.
