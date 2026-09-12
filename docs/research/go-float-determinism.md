# What bit-identical replay would cost in Go, on float32

Research for [dvoyni/cog#286](https://github.com/dvoyni/cog/issues/286), child of the
physics2d map [#182](https://github.com/dvoyni/cog/issues/182). This is a **costing
exercise**. Determinism is neither required nor pursued; the point is that a later
"we need rollback netcode" is a priced decision rather than an archaeology project.
Nothing here becomes a requirement.

## The answer, in three lines

| Price | Cost |
| --- | --- |
| **(a) Bit-identical across runs on one machine** | **Free.** One seeded RNG (already planned) and one `Before`/`After` edge per pair of Systems that write the same Component (also already planned). No arithmetic changes. |
| **(b) Across machines of one architecture** | **Free in code, not free in the build.** Same `GOARCH` is not enough: `GOAMD64=v3` fuses and `v1` does not, and Go 1.25 changed which. The price is pinning `GOARCH`, `GOAMD64`/`GOARM64`, and the toolchain version — a build-configuration lock, zero lines of physics. |
| **(c) Across architectures** | **One specific thing.** Every `x*y + z` in `physics2d` and in the response System must be written `float32(x*y) + z`. That is the whole bill, because every other operation this design needs is exactly specified by IEEE 754 and Go's own spec. The residual costs are one lost instruction on arm64's hot path and one test to stop the barrier rotting. |

## Environment

```
go version go1.27.1 windows/amd64
GOAMD64=v1 (the default), AMD Ryzen 9 7950X3D
```

Every measurement below was taken **on amd64**, cross-compiling to arm64 with
`GOARCH=arm64 go build -gcflags=-S`. Instruction selection is a compile-time
property, so cross-compiled disassembly is authoritative for *which* instruction is
emitted; the numeric divergence figures in §7 were produced on amd64 by emulating a
single-rounding FMA with `math/big` at 120–200 bits, not by running on arm64
hardware. **No arm64 machine was available; nothing here is generalised from a
single architecture's runtime behaviour.** The probe programs are reproduced inline
and were run from a scratch directory outside the repo.

---

## 1. What the spec permits, and when

`$GOROOT/doc/go_spec.html`, **§Floating-point operators**:

> An implementation may combine multiple floating-point operations into a single
> fused operation, possibly across statements, and produce a result that differs
> from the value obtained by executing and rounding the instructions individually.
> **An explicit floating-point type conversion rounds to the precision of the target
> type, preventing fusion that would discard that rounding.**

and its own worked examples:

```go
// FMA allowed for computing r, because x*y is not explicitly rounded:
r  = x*y + z
r  = z;   r += x*y
t  = x*y; r = t + z
*p = x*y; r = *p + z
r  = x*y + float64(z)

// FMA disallowed for computing r, because it would omit rounding of x*y:
r  = float64(x*y) + z
r  = z; r += float64(x*y)
t  = float64(x*y); r = t + z
```

Four consequences worth stating plainly, because each is a thing someone will try:

- **Parentheses do not stop fusion.** `(x*y) + z` is `x*y + z`.
- **A local variable does not stop fusion.** `t = x*y; r = t + z` is listed as
  *allowed*.
- **A round-trip through memory does not stop fusion.** `*p = x*y; r = *p + z` is
  listed as *allowed*. "Possibly across statements" is the operative phrase.
- **An explicit conversion is the only barrier**, and it is a conversion of the
  *product*, not of the sum.

The spec's examples are all written in `float64` because that is the type the
proposal's examples used ([golang/go#17895](https://github.com/golang/go/issues/17895),
*spec: allow the use of fused multiply-add floating point instructions*). The rule is
stated for "an explicit floating-point type conversion", not for `float64`
specifically, and §3 confirms the `float32` analogue is implemented.

## 2. Which architectures actually fuse

The Go project's own codegen test is the authoritative list —
`$GOROOT/test/codegen/floats.go:77`:

```go
func FusedAdd32(x, y, z float32) float32 {
	// s390x:"FMADDS "
	// ppc64x:"FMADDS "
	// arm64:"FMADDS"
	// loong64:"FMADDF "
	// riscv64:"FMADDS "
	// amd64/v3:"VFMADD231SS "
	return x*y + z
}
```

Cross-checked against the lowering rules, which is where the decision actually lives:

| GOARCH | Fuses `x*y+z`? | Rule |
| --- | --- | --- |
| `arm64` | **yes**, always | `_gen/ARM64.rules:1798` |
| `ppc64`, `ppc64le` | **yes**, always | `_gen/PPC64.rules:1013` |
| `riscv64` | **yes**, always | `_gen/RISCV64.rules:808` |
| `loong64` | **yes**, always | `_gen/LOONG64.rules:770` |
| `s390x` | **yes**, always | `_gen/S390X.rules:1266` |
| `amd64` | **only at `GOAMD64=v3`+** | `_gen/AMD64.rules:1501` — `&& buildcfg.GOAMD64 >= 3` |
| `386`, `arm`, `mips`, `mips64`, `wasm` | **no** — no `useFMA` rule at all | — |

(Paths relative to `$GOROOT/src/cmd/compile/internal/ssa/`.)

Every one of those rules is guarded by `Func.useFMA`, whose only job is a debugging
hook — `ssa/func.go:848`:

```go
// useFMA allows targeted debugging w/ GOFMAHASH
// If you have an architecture-dependent FP glitch, this will help you find it.
func (f *Func) useFMA(v *Value) bool {
	if base.FmaHash == nil { return true }
	return base.FmaHash.MatchPos(v.Pos, nil)
}
```

**Two things make this worse than "platform-dependent".**

*The default is a build knob, not the hardware.* `GOAMD64` defaults to `v1`
(`internal/buildcfg/zbootstrap.go:8`, `DefaultGOAMD64 = "v1"`; `go help environment`:
*"Valid values are v1 (default), v2, v3, v4"*) — but `zbootstrap.go` is generated when
the *toolchain* is built, so a distribution can ship a different default. Two
developers on identical CPUs, one building with `GOAMD64=v3` for speed, get different
physics.

*And it changed under the same architecture.* amd64 fusion is new in **Go 1.25**
([golang/go#71204](https://github.com/golang/go/issues/71204), *cmd/compile: no
automatic use of fused multiply-add on amd64 even with GOAMD64=v3*). The
[Go 1.25 release notes](https://go.dev/doc/go1.25) say so outright:

> In `GOAMD64=v3` mode or higher, the compiler will now use fused multiply-add
> instructions to make floating-point arithmetic faster and more accurate. **This may
> change the exact floating-point values that a program generates.**
>
> To avoid fusing use an explicit `float64` cast, like `float64(a*b)+c`.

So a *toolchain upgrade* is a divergence event even at price (a) if the build uses
`v3`. Pinning the Go version is part of the bill for (b), not optional hygiene.

## 3. The `float32` barrier works — measured

The frontend's conversion table gives `float32 → float32` its own SSA op —
`cmd/compile/internal/ssagen/ssa.go:2710`:

```go
{types.TFLOAT64, types.TFLOAT64}: {ssa.OpRound64F, ssa.OpCopy, types.TFLOAT64},
{types.TFLOAT32, types.TFLOAT32}: {ssa.OpRound32F, ssa.OpCopy, types.TFLOAT32},
```

`Round32F` is a real value on every fusing architecture (`ARM64.rules:235`,
`AMD64Ops.go:802`, and so on), so it sits between the multiply and the add and the
fusion pattern no longer matches. Confirmed by disassembly, `GOARCH=arm64`:

| Source | arm64 codegen |
| --- | --- |
| `x*y + z` | `FMADDS` — **fused** |
| `float32(x*y) + z` | `FMULS` + `FADDS` — **not fused** |
| `t := float32(x*y); return t + z` | `FMULS` + `FADDS` — **not fused** |
| `ax*bx + ay*by` (a dot product) | `FMULS` + `FMADDS` — **half fused** |
| `float32(ax*bx) + float32(ay*by)` | `FMULS` + `FMULS` + `FADDS` — **not fused** |
| `float32(math.Sqrt(float64(x*x + y*y)))` (`Vec2.Length`) | `FMULS` + `FMADDS` + `FSQRTS` — **fused** |

The dot-product row is the one to internalise: `ax*bx + ay*by` fuses *one* of its two
multiplies and rounds the other, which is neither of the two things a reader expects.
Squared distance, the dot product, the perp-dot, and every quadratic discriminant in a
swept test have exactly this shape. `m.Vec2.Length` — `m/vector.go:378`,
`func sqrt(value float32) float32 { return float32(math.Sqrt(float64(value))) }` — is
already a fusing shape today.

**An inlinable helper does not protect you; a conversion inside the helper does.**

```go
func mul(x, y float32) float32        { return x * y }
func mulRounded(x, y float32) float32 { return float32(x * y) }

func AcrossInline(x, y, z float32) float32        { return mul(x, y) + z }        // arm64: FMADDS
func AcrossInlineRounded(x, y, z float32) float32 { return mulRounded(x, y) + z } // arm64: FMULS; FADDS
```

That is the design consequence: the barrier cannot live at the call site of a
`physics2d` helper. It has to live *inside* every helper that returns a product, or
the helper has to not return a bare product at all.

**The undocumented kill switch works too.** `-d=fmahash` is a compiler debug flag
(`base/debug.go:42`: *"hash value for use in debugging platform-dependent
multiply-add use"*), and the bisect pattern `n` matches nothing:

```
$ GOARCH=arm64 go build -a -gcflags="-S" .                    # 4 FMADD instructions
$ GOARCH=arm64 go build -a -gcflags="-S -d=fmahash=n" .       # 0 FMADD instructions
$ GOARCH=arm64 go build -a -gcflags="-S -d=fmahash=y" .       # 4 FMADD instructions
```

`-gcflags=all=-d=fmahash=n` disables fusion across the standard library too, and
builds clean (it prints `fmahash triggered [DISABLED] …` bisect chatter to stderr).
It is a debugging facility with no compatibility promise and no documentation outside
`go tool compile -d help`. It is a legitimate belt-and-braces measure for a
determinism CI job; it is not the mechanism to build the design on.

## 4. Go does not reassociate — confirmed, not assumed

`_gen/generic.rules:1323` is the complete list of machine-independent floating-point
rewrites in the compiler:

```
// floating point optimizations
(Mul(32|64)F x (Const(32|64)F [1])) => x
(Mul32F x (Const32F [-1])) => (Neg32F x)
(Mul64F x (Const64F [-1])) => (Neg64F x)
(Mul32F x (Const32F [2])) => (Add32F x x)
(Mul64F x (Const64F [2])) => (Add64F x x)

(Div32F x (Const32F <t> [c])) && reciprocalExact32(c) => (Mul32F x (Const32F <t> [1/c]))
(Div64F x (Const64F <t> [c])) && reciprocalExact64(c) => (Mul64F x (Const64F <t> [1/c]))

// rewrite single-precision sqrt expression "float32(math.Sqrt(float64(x)))"
(Cvt64Fto32F sqrt0:(Sqrt (Cvt32Fto64F x))) && sqrt0.Uses==1 => (Sqrt32 x)
```

Every one of those is **bit-exact**: `x*1`, `x*-1` and `x*2` are exact in IEEE 754;
division by a constant becomes a multiply *only* when `reciprocalExact32` says the
reciprocal is exact. There is no reassociation rule, no distribution rule, no
`x+0 => x`, no `x-x => 0`. Verified by disassembly on arm64:

| Source | arm64 codegen | Why |
| --- | --- | --- |
| `a * 1.0` | *nothing* — the argument is the result | exact identity |
| `(a + b) + c` | `FADDS`; `FADDS`, in that order | **no reassociation** |
| `a + 0.0` | `FADDS` **retained** | `-0.0 + 0.0 == +0.0`, so it is not an identity |
| `a - a` | `FSUBS` **retained** | `NaN - NaN` is `NaN`, not `0` |

Go has **no `-ffast-math`**. There is no flag that turns these on, and there is no
`float` contraction mode to switch off; FMA formation is the entire set of
value-changing floating-point liberties the Go compiler takes. That is a much smaller
surface than C or C++ and it is why price (c) is *one* thing rather than a research
programme.

## 5. Which operations this design needs, and whether each is exact

The map's §5 needs no transcendentals at all: the shape vocabulary is circle, AABB,
point and segment with **no rotation anywhere**, so every pair test is closed-form,
and the response model is a rebuilt accumulator of penetration springs with no
restitution. Enumerating what that actually requires:

| Operation | Where it is used | Bit-identical across architectures? |
| --- | --- | --- |
| `+ - * /` | everything | **Yes.** IEEE 754-2019 §5.4.1 requires these correctly rounded. Go inherits it — the spec's only carve-out is fusion (§1) and division by zero. |
| `sqrt` | distance, normalise, swept-test discriminant | **Yes.** §5.4.1 requires `squareRoot` correctly rounded. See below. |
| comparison `< <= == >` | every branch, every threshold | **Yes.** IEEE 754 comparisons are exact predicates. |
| `abs` | separating-axis tests, AABB overlap | **Yes.** §5.5.1 quiet-computational: clears the sign bit, no rounding. arm64 emits `FABSS`. |
| `min` / `max` | AABB clamping, slab tests | **Yes.** The Go spec pins the total order in a table — *"negative zero is smaller than (non-negative) zero"*, *"if any argument is a NaN, the result is a NaN"*. arm64 emits `FMINS`. |
| `floor` / `ceil` / `round` | grid-aligned wall indexing (#285) | **Yes.** IEEE `roundToIntegral`: exactly specified, and the result is exactly representable. arm64 emits `FRINTMS`. |
| **`sin` `cos` `atan2` `pow` `exp`** | **not needed by §5** | **No** — see below. |

**`sqrt` deserves its line of working**, because the way `m` spells it looks
suspicious and is not. `m/vector.go:378` writes
`float32(math.Sqrt(float64(value)))`, which reads like a double rounding. It is not,
for two independent reasons. First, `generic.rules:1334` rewrites exactly this
expression to a single `Sqrt32` op, which lowers to a native single-precision square
root (`FSQRTS` on arm64, `SQRTSS` on amd64) — measured above — and IEEE 754 requires
that correctly rounded. Second, even if the rewrite did not fire, double rounding is
harmless *for square root specifically*: `float64` carries 53 bits and 53 ≥ 2·24 + 2,
which is the classical sufficient condition for a round-to-single of a
round-to-double square root to equal the correctly-rounded single result. Both paths
give the same bits. Go's own software fallback is documented as
`__ieee754_sqrt(x) // Return correctly rounded sqrt.` (`$GOROOT/src/math/sqrt.go:22`).

**The transcendentals are the ones that are not portable, and the `math` package says
so itself** — `$GOROOT/src/math/const.go:7`:

> Package math provides basic constants and mathematical functions.
>
> **This package does not guarantee bit-identical results across architectures.**

That disclaimer is the reason to check the list above rather than wave at it.
`Sin`, `Cos`, `Tan`, `Atan2`, `Pow`, `Exp`, `Log` and `Mod` have per-architecture
assembly implementations (s390x especially) and no correctly-rounded guarantee in
either IEEE 754 or Go. cog's `m` uses them — `m/matrix.go`, `m/quaternion.go`,
`m/scalar.go`, `m/color.go` — but only in rotation matrices, quaternion slerp, angle
wrapping and colour conversion. **None of those is on the physics path**, which is a
consequence of requirement 2 (2D on a plane, no rotation) rather than a coincidence.
If a `Vec2.Rotate` or an `Atan2`-based facing angle ever enters the solver, this row
of the table moves from "not needed" to "cost unbounded", and that is the trigger to
watch for.

## 6. Iteration order — say plainly which property replay needs

cog's `Store` is a sparse set with swap-remove, and its own source says what that
means — `ecs/store.go:181`:

> `remove` is swap-remove: the last row moves into the hole. That is what keeps the
> packed arrays dense and `len(owners)` exact, and it is why no dense index may be
> held across a mutation and **why iteration order is unspecified**.

Two further inputs feed the order, both established from the source:

- **The driver is the shortest Store** — `ecs/query.go:259`: `bind` picks the
  non-filter field with the fewest `owners` and walks *that* array. So a Query's
  iteration order is a function of which of its Component populations is currently
  smallest, and switches drivers when the populations cross.
- **Entity indices are recycled LIFO** — `ecs/entities.go:102`: `alloc` pops
  `free[len(free)-1]`. A plain stack, no map, no randomness.

Every one of those inputs is a pure function of the spawn/despawn/attach/detach
history. **So iteration order is deterministic given identical history, and
unspecified otherwise — and replay needs only the first.** A replay, by construction,
re-runs the same history from the same start state: it produces the same dense
arrays because it performed the same sequence of `add` and `remove` calls. Iteration
order therefore costs **nothing** for replay, in any of the three prices.

What it does forbid is the adjacent thing people reach for next. **A mid-game
snapshot is not a valid replay start point unless it captures the dense layout.** The
set of live entities and their component values is not sufficient state: two worlds
with the same entities in different dense rows iterate in different orders and
diverge immediately. If rollback ever arrives, a snapshot must serialise
`owners`/`dense` order (or a canonical re-sort must be applied on both sides), and
that is a real serialisation cost — but it is a cost of *snapshotting*, not of
replay from tick zero.

## 7. Concurrency — is one System enough?

`kernel/publication.go:56` fans a publication out to a goroutine per ready
subscriber, with a fast path:

```go
// One subscriber is the common case, and it has no ordering to resolve: run it
// on this goroutine and skip the scratch slices, result channel, and fan-out.
if len(plan.nodes) == 1 { … }
```

and otherwise `launch(node)` starts a goroutine for every node whose `dependsOn`
count has reached zero. The plan itself is built deterministically —
`kernel/subscription.go:102` walks a registration-order slice and uses maps only as
keyed sets, never ranging one to decide order — so the *plan* is the same on every
process start. What is not the same is the order in which those goroutines reach the
lock coordinator.

The distinction that matters: **cog's lock discipline gives mutual exclusion, not a
fixed order.** Two Systems that write the same Store can never overlap, but which one
goes first is whatever the scheduler grants. So:

- **Being one System does close the intra-physics case.** A single System runs its
  whole sub-step loop on one goroutine; nothing inside it reorders.
- **It does not close the inter-System case.** The physics System's position relative
  to every other writer of the same Components — the input System writing velocity,
  gameplay writing force — is unspecified unless it is pinned. It is pinned with
  `Registrar.Subscribe`'s `Before`/`After`, which the map already plans to use to say
  "physics after input, before rendering".
- **Nothing else reorders.** Nothing in `ecs` or the kernel dispatch path ranges a
  map to produce an order, and the entity free list is a stack.

Cost: **zero.** The edges are already in the design for reasons that have nothing to
do with determinism.

## 8. What a single fused operation costs downstream

Two probes, both run on amd64 with the single-rounding form emulated exactly in
`math/big`. Both are throwaway; they are reproduced here rather than committed.

**Per operation, the difference is exactly the size the map said it was.** Over 2²⁰
random `pos + vel*dt` at map scale (`pos` to ±512 m, `vel` to ±30 m/s, `dt` = 1/120 s),
fused and unfused differ in **0.06%** of cases, worst absolute difference
**3.05 × 10⁻⁵ m** — one ULP of the [256, 512) binade, 0.03 mm. Precision genuinely is
not the problem.

**Downstream, in a contact-rich scene, it is the whole map.** A miniature of §5 — 300
circles of radius 0.5 m in a 40 m box, penetration springs at stiffness 10, no
restitution, no drag, two sub-steps a tick at 60 Hz — run twice, once with every
`x*y+z` rounded twice and once with every `x*y+z` rounded once:

```
    1 ticks (   0.0 s): max separation 1.19209e-07 m
   10 ticks (   0.2 s): max separation 9.53674e-07 m
   60 ticks (   1.0 s): max separation 6.7435e-06 m
  300 ticks (   5.0 s): max separation 0.000140043 m
  600 ticks (  10.0 s): max separation 0.150457 m
 1800 ticks (  30.0 s): max separation 32.1253 m
 3600 ticks (  60.0 s): max separation 48.4997 m
```

One ULP at tick 1; 15 cm at 10 s; unrelated worlds at 30 s. The amplifier is the
`d2 >= 4r²` branch — a difference in the last bit flips a contact on or off, and from
there the two histories are different simulations, not the same simulation with an
error bar. A projectile hit/miss test is exactly the same branch, and one
flipped projectile is a gameplay divergence whatever the positions are doing. The
same run at a fifth of the density and with drag (64 bodies, drag 0.98) stays pinned
at 9.8 × 10⁻⁷ m for a full minute, so this is a property of contact density rather
than an inevitability — but the branch is the same branch.

The reading: **there is no partial credit.** Applying the barrier to "the important
parts" of the solver buys nothing, because the branch that amplifies is everywhere.
Price (c) is all-or-nothing.

## 9. The bill

### (a) Bit-identical across runs on one machine — free

Same binary, same machine, same inputs. Nothing in the toolchain varies per run;
instruction selection is fixed at compile time. Two things are needed and both are
already in the design:

1. **The coincidence nudge draws from a caller-supplied seeded source** — the map has
   already taken this. Note that Go's global `math/rand` auto-seeds since Go 1.20, so
   *any* accidental use of the package-level functions is a per-run divergence.
2. **`Before`/`After` on every System pair that writes the same Component.** The lock
   discipline gives exclusion, not order (§7). The map already plans these edges.

Not needed: any change to the arithmetic, any change to iteration, any change to the
scheduler. **Cost: zero.**

Caveat, and it is the one people trip on: this holds for *one binary*. Rebuilding with
a different toolchain version can change it — Go 1.25 is the live example (§2).

### (b) Across machines of one architecture — free in code, a lock in the build

Instruction selection depends on `GOARCH` **and** the microarchitecture level, so
"one architecture" is not a well-formed guarantee until the level is pinned:

- Pin `GOAMD64` explicitly (`v1` and `v3` disagree), or `GOARM64`/`GOARM`/`GOPPC64`
  for those ports.
- Pin the Go toolchain version, via the `go` and `toolchain` lines in `go.mod` plus
  `GOTOOLCHAIN`. Go 1.25 changed the amd64 answer; a future release can change
  another.
- Pin `GOEXPERIMENT` if anything ever sets it.
- Every replay participant must run a byte-identical binary. That is not a code
  requirement; it is a distribution requirement, and it is the one that quietly fails
  when someone runs `go run` locally against a shipped build.

**Cost: a CI lock and a documented build recipe. Zero lines of physics.** This is
worth writing down *now* even though determinism is not pursued, because it costs
nothing to record and it is the part that is hardest to reconstruct later.

### (c) Across architectures — one specific thing

Given (a) and (b), and given §4 (no reassociation), §5 (every needed operation
exactly specified) and §6 (iteration order costs nothing), **the entire residual
difference between amd64 and arm64 is FMA fusion.** The bill:

1. **Write every multiply-then-add as `float32(x*y) + z`, throughout `physics2d` and
   the response System.** In practice this means a single tiny helper —
   `func mad(x, y, z float32) float32 { return float32(x*y) + z }` — used everywhere,
   plus a rule that no exported `physics2d` function returns a bare product that a
   caller will add to something. The barrier must be *inside* the helper (§3); a call
   boundary is erased by inlining.
2. **Accept the throughput loss.** On arm64 the hot loop pays `FMULS` + `FADDS` where
   it could pay one `FMADDS`, and loses the extra accuracy of the single rounding.
   For a broadphase-plus-four-intersection-routines workload this is a small constant
   factor on the arithmetic, not on the algorithm — but it is a real cost, and it is
   paid on the platform the design is *least* likely to profile on.
3. **Add a test that fails when the barrier rots.** This is the part that makes the
   difference between a decision and an aspiration. The Go project enforces its own
   codegen expectations with `test/codegen`, which is not available to a module, but
   the equivalent is about twenty lines: shell out to
   `go build -gcflags=-S` with `GOARCH=arm64` over `physics2d`, and fail if the
   output contains `FMADD`, `FMSUB`, `FNMADD` or `FNMSUB`. It is cheap, it runs on any
   host, and it catches every future `x*y + z` on the day it is written.
4. **Optionally belt-and-braces the build** with `-gcflags=all=-d=fmahash=n`, which
   works today (§3) and also covers the standard library. Undocumented, no
   compatibility promise; a determinism CI job, not a shipping configuration.

**Cost: one helper, one discipline, one test, and one instruction on arm64's hot
path.** It is genuinely small — small enough that it would be defensible to take it
pre-emptively — but it is not free, and taking it pre-emptively would pay the arm64
throughput cost forever for an option the driving game has said it does not need. The
map's posture (record the price, take only the free things) is the right call, and
this is the price.

### What is *not* on the bill

For completeness, so a later reader does not go looking:

- Reassociation, distribution, `x+0`, `x-x`, contraction beyond FMA — Go does none of
  it (§4).
- `math` transcendentals — this design needs none (§5). This is contingent on
  requirement 2 holding.
- Iteration order — a function of history alone (§6). Snapshot-based rollback is a
  separate and larger cost.
- Goroutine scheduling — excluded by the lock discipline plus ordering edges the
  design already has (§7).
- `float32` precision as such. At the 512 m map bound a ULP is 0.03 mm, three orders
  of magnitude below the 6 mm coincidence nudge and the ~3 cm penetrations of §5.

## 10. The map's two findings, checked

Both **stand**, with one sharpening and one footnote.

- ***"`m.Vec2` is `float32`, and precision is not the problem here."*** Confirmed:
  `m/vector.go:7`, `type Vec2 struct{ X, Y float32 }`. Confirmed numerically: the ULP
  at the map bound is 3.05 × 10⁻⁵ m. **Footnote:** that figure is the ULP of the
  [256 m, 512 m) binade; at and above exactly 512 m it doubles to 0.061 mm. The
  conclusion is unaffected.
- ***"The determinism hazard is FMA fusion, not precision … whether it does is
  platform-dependent, so a seeded RNG and single-threaded execution are not
  sufficient for bit-identical cross-platform replay."*** Confirmed, and the
  concluding clause is exactly right. **Sharpening: "platform-dependent" understates
  it.** Fusion is *build-configuration*-dependent. The same `GOARCH=amd64` fuses at
  `GOAMD64=v3` and does not at `v1`, and the `v3` behaviour is new in Go 1.25. Two
  builds for the same platform, from the same source, on the same machine, can
  disagree. That moves part of the cost out of price (c) and into price (b), which is
  where it is easiest to forget.

## Sources

Primary, in order of weight.

- **The Go language specification**, §Floating-point operators and §Min and max —
  `$GOROOT/doc/go_spec.html` (go1.27.1), also at <https://go.dev/ref/spec>.
- **`cmd/compile` source**, go1.27.1: `internal/ssa/_gen/{AMD64,ARM64,PPC64,RISCV64,LOONG64,S390X,generic}.rules`;
  `internal/ssa/func.go:848`; `internal/ssagen/ssa.go:2710`; `internal/base/debug.go:42`;
  `internal/buildcfg/zbootstrap.go:8`.
- **`$GOROOT/test/codegen/floats.go:77`** — the Go project's own per-architecture FMA
  expectations.
- **`$GOROOT/src/math/const.go:7`** (no bit-identical guarantee) and
  **`src/math/sqrt.go:22`** (correctly rounded sqrt).
- **IEEE 754-2019**, §5.4.1 (correctly rounded: add, subtract, multiply, divide,
  squareRoot, fusedMultiplyAdd) and §5.5.1 (exact: negate, abs, copySign).
- **[golang/go#17895](https://github.com/golang/go/issues/17895)** — the proposal that
  put fusion in the spec.
- **[golang/go#71204](https://github.com/golang/go/issues/71204)** and the
  **[Go 1.25 release notes](https://go.dev/doc/go1.25)** — amd64 `GOAMD64=v3` fusion.
- **cog source**, this commit: `m/vector.go:7,378`; `ecs/store.go:181`;
  `ecs/query.go:259`; `ecs/entities.go:102`; `kernel/publication.go:56`;
  `kernel/subscription.go:102`.
- **`go help environment`** — `GOAMD64` valid values and default.
