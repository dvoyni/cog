# What binding a property costs in Go

Research for [dvoyni/cog#775](https://github.com/dvoyni/cog/issues/775), child of the
mux map [#773](https://github.com/dvoyni/cog/issues/773). It measures, in time and in
heap allocations and over growing counts, each way of binding an override to one
property of a plain Go model, and each way of finding a property's active override.
It reports; the choice belongs to [#776](https://github.com/dvoyni/cog/issues/776)
(how an override addresses a property) and [#777](https://github.com/dvoyni/cog/issues/777)
(how a view reads shown values).

**Gist.** Nothing measured allocates per frame except reflection at read time and
boxing a new override behind an interface. Time separates the shapes: a direct or
inlined read is 0.6 ns, any indirect binding (func value, unsafe offset path) is
2.4 to 3.5 ns, a map step in the model is 4 to 11 ns, and finding an override by key
in a Go map costs 13 to 20 ns per property, while walking a sorted override list in
step with the model costs 2 to 5 ns.

## Method

- Code: [`bindingcost/`](bindingcost/), its own module (`module bindingcost`), not in
  `go.work`. Run with
  `GOWORK=off go test -run '^$' -bench . -benchmem -benchtime=300ms -count=6 .` from
  that directory. Raw output of the run these tables come from is in
  [`bindingcost/results.txt`](bindingcost/results.txt).
- Each table cell is the median of 6 runs, in **ns per element** (one property read,
  one override evaluated or one key looked up); ns/op is in `results.txt`. Run-to-run
  spread is under 5% unless marked.
- `allocs/op` comes from `-benchmem` and is cross-checked by
  `TestZeroAllocations` (`testing.AllocsPerRun`); both agree.
- Timing loops use the classic `for range b.N` form, not `b.Loop`, which wraps
  loop-carried values in `runtime.KeepAlive` and flattens register-use differences
  (found by the ecs map, `_legacy/docs/research/ecs-go-mechanics.md`).
- `TestResolveAgreesWithDirect` checks the unsafe path reads what direct code reads,
  including after the slice is reallocated by `append`, and from a map.

### The model

```go
type Vec2 struct{ X, Y float32 }
type Camera struct{ Position Vec2; Zoom float32 }
type Unit struct{ ID uint32; Position Vec2; Health int32 }
type Prop struct{ Position Vec2; Angle float32 }
type Model struct {
	Turn   int32
	Camera Camera           // nested struct
	Units  []Unit           // slice of units with Vec2 positions
	Props  map[uint32]Prop  // map keyed by id
}
type Key struct{ Path uint32; Slot0, Slot1 uint64 } // path id plus fixed key slots
```

Counts are 16, 256 and 4096 elements (units, props or overrides).

### Environment

```
go version go1.27.1 windows/amd64
AMD Ryzen 9 7950X3D 16-Core Processor, GOMAXPROCS=32
```

**Workspace Go version:** `go.work` and all seven modules (`_legacy`, `ecs`, `gfx`,
`gogpu`, `kernel`, `metronome`, `platform`) declare `go 1.27`. Go 1.27 has generic
methods: *"If the method declaration specifies type parameters (possibly in addition
to type parameters declared by the receiver specification), the method name denotes a
generic method [Go 1.27]"* (`$GOROOT/doc/go_spec.html`, Method declarations). So an
API such as `timeline.Collection[T]()` or `binding.Read[T](model)` can be a method.

## 1. Reading a property's model value

What each binding costs to reach the property it names, before any override lookup.

| Binding | 16 | 256 | 4096 | allocs |
|---|---|---|---|---|
| **slice** `Units[i].Position` | | | | |
| direct code | 0.60 | 0.61 | 0.60 | 0 |
| top-level accessor `func(*Model, Key) *Vec2`, called by name | 0.62 | 0.60 | 0.60 | 0 |
| same accessor through a registry `[]func(*Model, Key) *Vec2` | 2.41 | 2.47 | 2.40 | 0 |
| unsafe offset path, compiled by reflection, walked per read | 3.45 | 3.48 (28%) | 3.40 | 0 |
| reflection at read time (`reflect.Value.Index/Field`) | 41.7 | 39.6 | 41.8 (18%) | 0 |
| **nested** `Camera.Position` | | | | |
| direct code | 0.60 | 0.60 | 0.62 | 0 |
| accessor through registry | 2.41 | 2.41 | 2.45 | 0 |
| unsafe offset path | 2.68 | 2.64 | 2.72 | 0 |
| **map** `Props[id].Position` | | | | |
| direct code | 4.56 | 4.62 | 5.64 | 0 |
| accessor copying the value out, `func(*Model, Key) (Vec2, bool)` | 4.13 | 4.18 | 5.21 | 0 |
| unsafe path, map step through a typed lookup from a generic registration | 8.35 | 8.53 | 10.81 | 0 |
| unsafe path, map step through `reflect.Value.MapIndex` | 49.1 | 49.2 | 52.9 (36%) | **1 per read** |

### Observations

- **Every shape scales linearly**: per-element cost is flat from 16 to 4096, except
  map steps, which rise about 20% at 4096 as the table outgrows the near caches.
- **A top-level accessor is free only when called by name.** It inlines (`go build
  -gcflags=-m`: `can inline UnitPosition`) and matches direct code. Bound at runtime,
  which is what a binding is, it is a func value: an indirect call that cannot
  inline, 2.4 ns, four times direct.
- **The unsafe path costs about one indirect call more**: 2.7 ns for two field steps,
  3.4 ns with a slice step. `Path.Resolve` is an interpreter over a step list and is
  too large to inline; it is close to the registry accessor because both pay one
  non-inlined call and the path's steps are a few predictable branches.
- **Reflection at read time is 40 to 50 ns** per read, 70 times direct, and a map
  read through it allocates every time (below).
- **A map in the model sets a floor of 4 to 6 ns** per property for every shape,
  since every shape must hash the key; the unsafe path's map step doubles it,
  because it adds the func-value call and copies the value into scratch.

### What is impossible or unsafe

- **A map value has no address.** The spec allows `&x` only for *"a variable, pointer
  indirection, or slice indexing operation; or a field selector of an addressable
  struct operand; or an array indexing operation of an addressable array"*
  (`go_spec.html`, Address operators); a map index expression is not among them.
  Since Go 1.24 maps are Swiss tables (`$GOROOT/src/internal/runtime/maps/map.go`)
  that move entries when a table grows, so no stable address exists to take even with
  `unsafe`. So an accessor of the ticket's shape `func(*Model, Key) *T` **cannot
  exist for a property inside a map value**: it can only copy out, `(T, bool)`, and a
  write goes back through `m[k] = v`. `map[uint32]*Prop` would make the values
  addressable but stops the model being plain data: a copy of the model shares its
  props.
- **Reflection cannot instantiate a generic function**, so a path compiled from
  `reflect.Type` alone has no typed map lookup: its map step must either go through
  `reflect.Value.MapIndex`, which allocates per read (`$GOROOT/src/reflect/map.go:214`
  returns `copyVal`, which calls `unsafe_New` for any value type that is not
  pointer-shaped, `value.go:1892`), or get a typed lookup from a generic registration
  call that names the key and value types (`WithMapLookup[uint32, Prop]` here).
- **An `unsafe.Pointer` into a slice's backing array must not be kept between reads.**
  It is GC-safe (the GC traces `unsafe.Pointer`, so the old array stays alive), but
  after an `append` reallocates, it reads a stale array silently. `Path.Resolve`
  keeps only offsets and element sizes, plain `uintptr` integers, and rebuilds every
  pointer from the model's base on each call, re-reading the slice header.
- **A `uintptr` must not hold an address across statements**: *"A uintptr is an
  integer, not a reference ... nor will that uintptr keep the object from being
  reclaimed"*, and *"both conversions must appear in the same expression"*
  (`$GOROOT/src/unsafe/unsafe.go`, rule (3)). `unsafe.Add` is that pattern as one
  call; *"The rules for valid uses of Pointer still apply"*, so the result must stay
  inside the original allocation, which is why `Resolve` checks the index against the
  slice length and returns nil past it.
- **The unsafe path reads any copy of the model**, since it is relative to the base
  pointer: a saved or sent copy resolves the same paths.

## 2. Evaluating the active overrides of a frame

Every override active, each a tween with an easing enum; the result written to a
preallocated output. Per override evaluated.

| Shape | 16 | 256 | 4096 | allocs |
|---|---|---|---|---|
| **Vec2 only** | | | | |
| hand-written loop over `[]Tween[Vec2]` | 1.88 | 1.82 | 1.81 | 0 |
| generic `Collection[T Lerper[T]]`, lerp by constraint method | 2.57 | 2.46 | 2.45 | 0 |
| generic `FuncCollection[T]`, lerp by a registered func field | 3.39 (76%) | 2.51 | 2.48 | 0 |
| type-erased `Record` (kind enum, `[4]float32` from/to slots) | 2.42 (66%) | 2.22 | 2.22 | 0 |
| **Half Vec2, half Scalar** | | | | |
| two generic collections, one per type | 2.52 | 2.25 | 2.33 | 0 |
| records, grouped by kind | 2.02 | 1.95 | 1.95 | 0 |
| records, kinds interleaved at random | 2.00 | 1.95 | 2.13 | 0 |
| `[]Override` interface of `*BoxedTween[T]`, interleaved | 3.90 | 4.23 | 7.15 (16%) | 0 |

Scheduling one override into a collection with spare capacity:

| Shape | ns | allocs |
|---|---|---|
| append a `Tween[Vec2]` to a generic collection | 1.3 | 0 |
| append a `&BoxedTween[Vec2]{...}` to `[]Override` | 16.1 | **1** |

### Observations

- **Generic collections cost about 35% over hand-written code**, the same whether the
  lerp comes from a constraint method or a func field. Go compiles generics by GC
  shape with a dictionary, so the constraint's method is an indirect call, not
  inlined.
- **Type-erased records are as fast as generic collections or faster**, and random
  interleaving of kinds costs them little (2.13 against 1.95 at 4096): the kind switch
  is cheap next to the easing and the float maths. Each `Record` is 80 bytes here
  against 64 for `Tween[Vec2]`; fixed slots size every record for the largest kind.
- **Boxing behind an interface is the one shape that allocates**, once per scheduled
  override, and its evaluation degrades with count (3.9 to 7.2 ns) because each
  override is a separate heap object reached through a pointer.
- Evaluating 4096 overrides takes 8 to 10 µs in every unboxed shape.

## 3. Finding the active override of a property

`Key` is a comparable struct, the path id plus two key slots. "hits" looks up each of
count present keys in random order. "view" reads 4096 properties in model order with
count of them overridden, so most lookups miss; it is reported per property read.

| Lookup | 16 | 256 | 4096 | allocs |
|---|---|---|---|---|
| **hits** | | | | |
| Go `map[Key]int32` | 15.0 | 15.4 | 20.3 | 0 |
| sorted `[]Key`, binary search | 14.7 | 23.7 | 68.0 | 0 |
| **view of 4096 properties** | | | | |
| Go map | 12.7 | 13.5 | 20.2 | 0 |
| sorted slice, binary search per property | 13.2 | 24.1 | 48.4 (19%) | 0 |
| sorted slice, merge walk in step with the model | **2.24** | **2.99** | **5.30** | 0 |
| **rebuild the index from scratch**, per key | | | | |
| Go map, `clear` then insert | 19.6 | 20.2 | 25.6 | 0 |
| sorted slice, append all then `slices.SortFunc` | 11.0 | 23.0 | 63.3 (17%) | 0 |
| sorted slice, `slices.Insert` each | 20.7 | 40.6 (13%) | 317 | 0 |
| **churn**: one override ends, one starts, count active, ns per pair | | | | |
| Go map | 36 | 36 | 56 | 0 |
| sorted slice | 48 | 108 | 961 (34%) | 0 |

A key with no padding (`Path uint64`) measured the same as `Key` within 4% in every
row (`results.txt`, packed-key rerun): padding does not slow the hash.

### Observations

- **A map lookup is 13 to 20 ns, hit or miss**: hashing a 24-byte key dominates. A
  view that asks the map for every property pays that per property, even when nothing
  is overridden.
- **Binary search loses to the map from 256 keys** and is 3 times slower at 4096.
- **The merge walk is 4 to 6 times cheaper than the map**, because it compares keys
  instead of hashing them and visits both sequences in order. It needs the view to
  read properties in the same order the overrides are sorted in, so the key order has
  to follow the model's layout (path, then index).
- **Keeping a slice sorted is where it loses**: one end-and-start at 4096 is 1 µs
  against 56 ns for the map, since `slices.Insert` and `slices.Delete` shift the
  tail. A whole rebuild by sorting is 2.5 times the map's at 4096.
- No lookup or maintenance shape allocates once its capacity is reserved; growing a
  map or slice past its capacity does, so capacity is a registration-time concern.

## 4. A whole view: the shown position of every unit

count units, count/4 of them overridden by a tween, the view summing each unit's
shown position. Per unit.

| Shape | 16 | 256 | 4096 | allocs |
|---|---|---|---|---|
| map lookup, else accessor through registry | 15.3 (18%) | 15.5 | 19.3 | 0 |
| sorted merge walk, else unsafe path | 8.7 (12%) | 6.5 | 6.9 (15%) | 0 |
| sorted merge walk, else direct code (lower bound) | 3.7 (44%) | 3.9 | 6.2 (41%) | 0 |

For 4096 units that is 79 µs, 28 µs and 25 µs a frame. The lookup, not the binding,
decides most of it.

## What this means for the open questions

Options, for #776 and #777 to choose among; nothing here decides.

- **Addressing a property (#776).**
  - *Typed accessors* `func(*Model, Key) *T`, bound at registration: 2.4 ns a read,
    no reflection anywhere, but one function per property path written by hand or
    generated; impossible for properties inside map values, which need a copy-out
    `func(*Model, Key) (T, bool)` and a separate write.
  - *Unsafe offset paths* compiled by reflection at registration: 2.7 to 3.5 ns a
    read, registration from a path string or selector, works on any copy of the
    model; map steps need a typed lookup from a generic registration call (Go 1.27
    generic methods can carry the types) or they allocate per read.
  - *Restricting the model* to structs, arrays and slices (maps outside the model, or
    maps of ids to slice indices) removes the map case from both options.
- **Finding the active override (#777).**
  - *A Go map by key*: simple, cheap to change (36 to 56 ns per churn), 13 to 20 ns per
    property read whether overridden or not.
  - *A sorted list walked in step with the view*: 2 to 5 ns per property read, but
    the view must read in key order, and keeping the list sorted costs up to 1 µs per
    churn at 4096; a frame could re-sort once in bulk instead of inserting each.
  - *Per-property presence*: neither measured shape avoids paying per property when
    almost nothing is overridden; an index from a property to "has an override" kept
    beside the model, or a per-path count of active overrides that skips the lookup
    at zero, are untested alternatives.
- **Storing overrides.**
  - *Generic per-type collections* and *type-erased records* are within 15% of each
    other and both allocation-free; records are slightly faster mixed and keep one
    timeline in one slice, collections keep values typed. Interface boxing is out by
    requirement 1: it allocates per scheduled override.
- **The zero-allocation bar.** Every shape kept passes `allocs/op = 0` at every
  count, so allocs alone would not separate them; per-element time at 16, 256 and
  4096 does, as the ecs map found.
