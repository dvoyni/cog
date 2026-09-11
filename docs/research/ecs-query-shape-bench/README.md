# ecs query shape benchmarks

Throwaway benchmarks behind the resolution of
[How a System declares what it touches](https://github.com/dvoyni/cog/issues/238).
Branch-only: they are evidence for a design decision, not a package cog ships.

A nested module, so `go build ./...` at the repo root skips it. Run with:

```
cd docs/research/ecs-query-shape-bench
go test -run XXX -bench . -benchtime 5000x -count 3
```

One pass over 1024 entities. `Body` (32 B) is written; `Collider` (16 B),
`Health` (8 B) and `Faction` (8 B) are read. go1.27.1 windows/amd64,
Ryzen 9 7950X3D, GOMAXPROCS=32.

## What each shape is

| | shape |
|---|---|
| **A** | hand-written slice loop — the baseline |
| **B** | positional `At(i)`, typed multi-return, index loop |
| **C** | positional `Each(fn)` closure |
| **D** | `Query[T]` struct, monomorphised typed fill, yields `*T` |
| **E** | `Query[T]` struct, offset + `copy` over `unsafe.Slice` |
| **F** | `Query[T]` struct, one binder closure per field |
| **G** | `Query[T]` struct, flat field table, per-field loop |
| **H** | `Query[T]` struct, flat field table, loop unrolled by field count |
| **I** | `Query[T]` struct, monomorphised typed fill, yields `T` by value |
| **J** | `Query[T]` struct, unsafe fill, yields `T` by value |
| **L** | all-pointer struct, unsafe fill — isolates the component copy |

## Results, ns per entity (ns/op ÷ 1024)

| shape | 2 comp | ×A | 4 comp | ×A | allocs |
|---|---|---|---|---|---|
| A hand loop | 1.45 | 1.00 | 2.56 | 1.00 | 0 |
| B positional `At(i)` | 1.46 | 1.01 | 3.87 | 1.51 | 0 |
| C positional `Each(fn)` | 1.46 | 1.01 | — | — | 0 |
| **H struct, unrolled reflect fill** | **1.98** | **1.37** | **3.83** | **1.50** | **0** |
| D struct, typed fill, `*T` | 2.00 | 1.38 | 3.50 | 1.37 | 0 |
| **I struct, typed fill, by value** | **1.46** | **1.00** | 3.50 | 1.37 | 0 |
| J struct, unsafe fill, by value | 2.00 | 1.38 | 9.65 | 3.77 | 0 |
| L all-pointer struct, unsafe fill | 1.95 | 1.35 | — | — | 0 |
| G struct, generic per-field loop | 3.90 | 2.69 | 6.00 | 2.34 | 0 |
| F struct, per-field binder closures | 7.14 | 4.92 | 12.78 | 4.99 | 1 |

## What the numbers establish

1. **Go caps range-over-func at two values.** `for e, b, c := range q.All()`
   does not compile — `expected at most 2 expressions`. A positional query can
   never use the idiomatic loop beyond one component; a struct query yields
   `(Entity, *T)` and can.
2. **The offset copy is free.** L copies no component bytes at all and matches
   H, which copies 16 per entity.
3. **The overhead is the memory round-trip, not `unsafe`.** Taking
   `unsafe.Pointer(&buf)` forces the buffer into memory and defeats register
   promotion; D, which uses typed writes and no `unsafe` at all, costs the same
   1.38×.
4. **Remove the address-taking and the overhead vanishes.** I is 1490 ns
   against the baseline's 1485 — parity to 0.3%. The struct shape itself is
   free; the 1.37× is the price of not knowing the struct's type at compile
   time, and only generated code can pay it off.
5. **By-value yielding only works monomorphised.** J is no better than H at two
   components and 2.5× worse at four, because a 40 B struct sourced from memory
   is copied wholesale per entity.
6. **Positional degrades faster with arity than the struct does**, crossing over
   at four components, where six return values exceed the register ABI.

Caveat on the 4-component baseline: A reads single fields straight out of the
dense arrays and never materialises the components, so it is not like-for-like.
The 2-component baseline does materialise, which is where parity is exact.
