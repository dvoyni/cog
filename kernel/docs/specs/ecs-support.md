# kernel support for `ecs` — specification

This document specifies what `github.com/dvoyni/cog/kernel` gains so that
[the `ecs` plugin](../../../ecs/docs/specs/ecs.md) can exist, and what each
addition costs the guarantees kernel makes today.

**The answer is one code addition and one documentation correction.** That is a
much smaller delta than the effort opened expecting, and the reason is worth
stating first: the whole decided design — reflective handler builder,
per-Component stores, structural change, two concurrent Systems, three plugins
composing against each other — **was built and run on an unmodified kernel**.
The prototype commit touches nine files and none of them is under `kernel/`
([What kernel gains, and what each addition
costs](https://github.com/dvoyni/cog/issues/244)).

Six items follow. Four of them are "no change", and they are here because each
was a live proposal that would have weakened something, so a future reader
should find the reason rather than the absence. Each states which current
guarantee it weakens and what replaces it.

---

## 1. No resource factory

**No change. `ResourceFactory func(reflect.Type) any` is not needed and should
not be added.**

A Component store is a kernel resource keyed on the Component's Go type, and the
`ecs` plugin does not know the app's Component types — which looked like a
factory-shaped hole. It is not. Component stores are created by **explicit
generic registration in the owning plugin**:

```go
func RegisterComponent[C any](r *kernel.Registrar, en *Entities, ids uint32) *Store[C] {
	s := NewStore[C](ids)
	r.InitResource[*Store[C]](s)   // an ordinary resource, with an ordinary owner
	// … enrol with Entities, bake the per-type closures …
	return s
}
```

Two facts make that safe rather than merely workable.

**Registration order already guarantees the store exists first.**
`orderPlugins` topologically sorts by declared dependency, so a plugin registers
strictly after every plugin it depends on. A System over `game.Health` lives in
a plugin that imports `game.Health`, therefore depends on that plugin, therefore
registers after it. There is no window in which a Query can name a store that
does not exist yet.

**The failure mode is a composition failure, and `ecs` catches it before kernel
can.** A Query naming an unregistered Component fails at registration, and
`callPluginBoundary` turns it into a composition error naming the plugin:

```
plugin "systems" panicked in Register: ecs: Query proto.GuardedQ names
unregistered Component proto.Guarded
```

That is strictly better than the `ErrMissingResource{*ecs.Store[…]}` kernel
would otherwise report at `finalize`, because it names the **Component** and the
**Query** rather than a store type the user never wrote.

**Cost: none. No guarantee weakens, because nothing changes.** The questions
that hang off a factory — what replaces `ErrMissingResource` for a type the
factory claims, what happens when the factory declines, what `Describe` reports
for a resource that appeared from nowhere — do not arise.

---

## 2. A Component store is an ordinary resource

**No change, and this is a deliberate constraint on the implementation rather
than an observation.**

A Component store is a resource of type `*ecs.Store[C]`, created by
`Registrar.InitResource` from `ecs.RegisterComponent[C]`, **owned by the calling
plugin**, and reported by `Describe` like any other. **No kernel concept of
"component" exists, and none should.** `*ecs.Store[C]` is self-describing and is
already reported with its owner, so `Describe` needs no change for it.

The store must be reached as a **pointer**: a value store makes
`kernel.Write[T].Get()` return a copy and **silently discard every mutation**.
That is a property of kernel's existing handle contract, not a new rule, and it
is recorded here because it is the one place that contract has a sharp edge for
this consumer.

One consequence for kernel's own vocabulary: **`Resource` keeps its meaning and
gains no ECS-local sense.** What other engines call a resource is exactly a cog
resource, and a second meaning of the word would collide in every architecture
sentence. `CONTEXT.md` records the avoidance.

**Cost: none.**

---

## 3. The coupling check applies to Component stores, unchanged

**No change, and the fact that it is not weakened is the decision.**

`InitResource` records `cell.owner = r.owner`, so a Store is owned by the plugin
that declared its Component, and `ErrUndeclaredDependency` fires when a handler
locks a resource owned by a plugin that is neither the owner nor a transitive
declared dependency. Applied to Component stores that means:

```
plugin "systems" locks resource *ecs.Store[protoecs.Guarded] owned by
"components" without declaring it as a dependency
```

The counterfactual was built and composed to price it. Under ownership by `ecs`,
**the same System composes with no dependency at all** — because once every
store has the same owner and every plugin with a System already depends on
`ecs`, the check **can never fire on a Component again**. It would not become
lenient; it would become **vacuous**, and the check that exists to catch "plugin
A quietly reads plugin B's data" would stop seeing the majority of an ECS app's
data. It would also collapse `ErrDuplicateRegistration`'s diagnostic, which
could then only ever name `ecs` twice.

So the rule the ECS spec carries, and which this delta exists to justify:
**register a Component in the plugin that defines its Go type.**

**Cost: none to kernel; a real cost to the app.** The coupling check now fires
on Component access, which is new surface for a game that scatters Components
across plugins. That is the check doing its job — the Go import graph already
forces the same edge, since a System cannot name `B.Health` in a Query struct
without importing `B` — and it arrives as a composition error at startup naming
the offending plugin, never as a silent race.

One wart worth knowing before it is met in a log: an instantiated generic
renders its type argument with the full import path, so the error reads
`*ecs.Store[github.com/dvoyni/nox/game.Health]`. Verbose, unambiguous, and not
worth a kernel change to prettify.

---

## 4. No non-generic `ResourceAccess` entry point

**No change. There is no `GetReadType(reflect.Type) Read[any]`, no untyped
handle, and no per-access type assertion on the hot path.**

`GetRead[T]`/`GetWrite[T]` are generic methods needing a compile-time `T`, and
the handler builder has a `reflect.Type` instead — and **a generic cannot be
instantiated from a `reflect.Type`** (`t (local variable) is not a type`;
verified, along with the two facts around it: an interface method may not have
type parameters, and generic methods on concrete types are what let `GetRead[T]`
exist at all). The inference was that `ResourceAccess` therefore needs a
non-generic entry point.

It does not. The resolution is to **never need the instantiation**:
`RegisterComponent[C]` is generic, so it bakes the per-type closures at
registration and stores them under `reflect.TypeFor[C]()`:

```go
declareRead: func(a kernel.ResourceAccess) func() *storeCore {
	h := a.GetRead[*Store[C]]()          // an ordinary generic call; C is static here
	return func() *storeCore { return &h.Get().storeCore }
},
```

Registration-time reflection looks the closure up by `reflect.Type` and invokes
it with the live `ResourceAccess`. The generic call is written once, by the app,
where `C` is a compile-time type. The assertion that remains is
`resource.value.(*Store[C])` inside the baked closure — pointer-shaped and free
at **0.61 ns**, exactly as an ordinary handler's.

**And the hole the proposal feared cannot open.** The question was whether a
reflective entry point would let a plugin lock anything it can name a
`reflect.Type` for, bypassing a check the generic form enforces. **The generic
form enforces nothing a reflective one would not**: `GetRead[T]` is
`reflect.TypeFor[T]()` followed by the same map lookup, and ownership is checked
at `finalize` against the resulting set, not at the call. The generic form's
real guarantee is that you must be able to *name* the type in Go source, which
is a coupling the import graph already records. Since nothing is being added
this stays hypothetical — but adding one later would cost nothing in safety,
only the ability to grep for who locks what.

**Cost: none.**

---

## 5. The `Lock` rule is amended: deterministic, total and final

**A documentation change to `.github/instructions/kernel.instructions.md`, and
the only guarantee this delta modifies.**

The current rule forbids by *syntax* what it means to forbid by *property*, and
a builder walking a func's parameter types is exactly the loop it forbids. The
rule is not wrong; it is written for hand-authored locks. Two facts sharpen the
amendment:

- **`Lock` runs exactly once.** `Subscribe` and `HandleCommand` call `factory()`
  and then `lock(*access)` immediately, at registration. "Reused forever"
  describes the resulting **set**, not repeated calls — so "every `GetRead` must
  run unconditionally" was never enforcing per-call consistency, because there
  is no second call.
- **What actually matters is that the body cannot reach a lock the set omits.**
  There is no runtime guard on `Get`/`Set`; an undeclared lock is a silent data
  race.

Drop-in replacement for the section titled "Keep `Lock` Straight-Line":

> ### Keep `Lock` Deterministic and Total
>
> `Lock` binds handles and nothing else, and it runs **once**, during
> registration: `Subscribe` and `HandleCommand` call it immediately and keep the
> lock set it produces for the engine lifetime. Three guarantees are what the
> rule is about.
>
> - **Deterministic.** The set is fixed by the end of registration and never
>   changes. It may depend on types and on registration-time configuration; it
>   must not depend on anything observable only while the engine runs — no
>   clock, no resource value, no entity count, no global mutable state.
> - **Total.** Every handle the handler body can reach is bound here. There is
>   no runtime guard on `Get`/`Set`, so a lock the body takes that `Lock` did
>   not declare is a silent data race, not an error.
> - **Final.** No lock is acquired after registration. A handler never widens
>   its own set; it declares what it dispatches with `Uses` and composition
>   folds those locks in.
>
> **Hand-written `Lock` bodies stay straight-line** — no branching, no loops, no
> work — because that is the cheapest way to satisfy all three and the only one
> a reviewer can check by eye. Conditional logic belongs in the body, where
> early returns are fine.
>
> **A generated `Lock` may loop**, and the `ecs` handler builder does: it walks
> a System's parameter list and declares one lock per Component named there.
> That satisfies all three — deterministic, because the parameter list is fixed
> by the System's Go signature; total, because the body can reach nothing its
> parameters do not name; final, because the walk happens inside the single
> registration-time call. What violates the rule is a loop whose **extent is not
> fixed by types**: over a slice a later run could size differently, over the
> entities alive at registration, or over anything read from the world.
>
> A generated `Lock` must be able to point at the type-level function that
> produces its set. If you cannot name that function, the loop is not generated,
> it is conditional.

**Cost: the rule gets longer and stops being checkable by grep.** What replaces
`grep -c 'if' lock.go` is that generated locks are produced in exactly one place
— `ecs.ToHandler` — so the property is reviewed once rather than per handler.
**`ecs` is the only sanctioned user of the generated-lock exemption.**

---

## 6. `Describe` gains a conflict report

**The one code addition, and it is not this delta's invention** — it is what
[Two Systems, one component, disjoint
entities](https://github.com/dvoyni/cog/issues/241) chose instead of solving
false serialisation. That ticket accepted the serialisation and made it
**visible**; the visibility half is a kernel change.

What is already there: `Describe()` emits fully-resolved `Reads`, `Writes` and
`Uses` per command and per subscription, read straight from the frozen `access`
sets with the transitive `Uses` closure already folded in by `resolveUses`.
`Dump()` renders them as a table and its own doc comment calls it *"the only
place the whole coupling map can be seen at once."*

**What is missing is any pairwise conflict computation.** There is none anywhere
in `kernel/`. The addition is one registration-time pass over data that is
already immutable when `finalize` returns — no new reflection, no new resource
walk, nothing on the hot path — producing three views:

- **Per-handler-pair conflicts** — which handlers can never overlap, and on
  which resource.
- **Per-resource contention** — which resources serialise the most, turning "the
  frame is slow" into "`Position` is why".
- **Per-phase serialisation** — whether a phase went effectively
  single-threaded. This is also the widest-lock warning the ECS's own hazard
  needs, so one addition discharges both.

Three constraints on it.

**It belongs in `kernel`, not in `ecs`**, because it needs no ECS knowledge and
should serve plain subscriptions and commands equally.

**It reports, never errors and never panics.** Bevy's equivalent ships **off by
default** (`ambiguity_detection: LogLevel::Ignore`) with four suppression knobs,
which is the honest measure of how noisy a type-level conflict report is on a
real schedule. cog reports and lets the reader judge.

**Rank, do not enumerate — and the ECS is why.** Every System declares
`read{*Entities}` and every structural change declares `write{*Entities}`, so a
raw pairwise dump says *"every System conflicts with every spawning or
despawning System"*: true, unavoidable under the ECS's single-lock Despawn, and
useless as a list. **Per-resource contention is the shape that survives contact
with an ECS.** It puts `*Entities` at the top, correctly, and names the
Component stores underneath it.

**Cost.** No hot-path cost and no weakened guarantee: it is a pure function of
data that is immutable once `finalize` returns. **The API cost is additive** —
`ArchitectureDescription` gains a field. cog has never been tagged and the
struct is a detached report, so nothing breaks.

---

## Two constants this delta records rather than changes

Both bound what any future kernel affordance in this area may cost, and both say
the same thing: **the scheduler round-trip, not the work, is what dominates a
small task.**

**A scheduled task costs ~2.2 µs.** Eight Systems doing ~0.04 µs of work each
still cost 17.45 µs a frame.

**A `Uses` dispatch costs 1113 ns**, against **0.49 ns** for the same mutation
called directly — 2270×, at **zero allocations**. The zero identifies the cause:
a subscription's Kernel sets `bounded: true`, so the dispatcher skips the
per-call context setup that would itself cost 169 ns and 4 allocs when an
unbounded caller does take it. What remains is the round-trip to the coordinator
goroutine, **paid even though a declared nested dispatch requests no locks at
all (`noLocks, noLocks`) and therefore cannot block.**

The consequence is that **command dispatch is unusable on any per-entity hot
path**, whoever writes it. It is why the ECS ended up with no commands: a Spawn
became a handle declared in the System's signature instead, at 26.0 ns for a
four-Component bundle.

**If kernel ever wants a fast path here**, the shape the measurement suggests is
one that skips `scheduler.execute` when the request is empty, and these are the
two numbers to beat. What currently forces the round-trip is that
`scheduler.acquire` is an open channel with no enumerable task set. **That is
not proposed here** — no consumer needs it, since the ECS routed around it — and
it is recorded so the next proposal starts from the measurement.

---

## Gap: what a bad System signature should be

`ecs.ToHandler` must reject a parameter it does not recognise, and the two
tickets that touched this do not agree on how.

[How a System declares what it touches](https://github.com/dvoyni/cog/issues/238)
required a **reported** composition error naming the System's type, joining
`ErrMissingResource` and friends, explicitly *"rather than surfacing as
`ErrPluginPanic` with a stack"*, on the grounds that this is a mistake every new
user makes once. [What kernel gains](https://github.com/dvoyni/cog/issues/244)
concluded kernel gains no new error type and no other API. The prototype does
the third thing: it panics, and `callPluginBoundary` converts that into
`ErrPluginPanic`.

Those cannot all hold. `registry.errs` is private to `kernel` and is appended to
only from `Registrar` methods, so a plugin-side builder cannot join that list
without kernel growing a way to report — which is an addition item 1 through 4
otherwise avoid. And `ErrPluginPanic` differs from what #238 asked for in two
ways that matter: it carries a **stack**, and `WithPlugins` **fails composition
at the first one**, so a project with three bad signatures fixes them one per
run.

The reading this document does **not** take is that the prototype's behaviour
settles it, because the prototype was never asked this question.

**What would settle it:** decide whether `Registrar` gains a narrow
`ReportError(error)` — additive, no hot-path cost, and it would let `ecs` emit a
sentence rather than a stack and let composition collect every bad signature in
one run — or whether `ErrPluginPanic` is accepted as the answer and #238's
requirement is formally withdrawn. It is a small decision with a real
ergonomic difference, and it is a **new ticket**, not something to settle while
assembling a spec.

---

## What kernel does not gain

Stated so a future reader does not re-propose them, and so the implementation
does not add them speculatively:

- **No `ResourceFactory`** (item 1).
- **No non-generic `ResourceAccess`** (item 4).
- **No barrier, no group locks, no "lock everything" affordance.** Structural
  change needs nothing: `Entities` holding a reference to every Store gives a
  Despawn coverage of all of them under a `Lock` that is a *single
  unconditional* `GetWrite[*Entities]()`, so the straight-line rule is satisfied
  trivially rather than strained. The alternative that *would* have needed a
  kernel affordance — a handler enumerating 85 registered Stores in its `Lock` —
  is exactly what this replaced.
- **No new error type**, subject to the Gap above.
- **No ownership escape hatch.**
- **No `Describe` change for Component stores** beyond item 6's report;
  `*ecs.Store[C]` is self-describing.
- **No `Elem()` normalisation of pointer-ness in resource keying.** It was
  tempting, because `Read[World]` and `Write[*World]` being unrelated cells is
  what made a `World` resource silently unsound. The ECS's answer is the
  discipline instead — **one spelling per resource, everywhere** — because
  normalising would change the meaning of every existing declaration in the
  tree for a hazard one convention avoids.

---

## Evidence

`coupling_test.go` on `proto/ecs-zero-alloc`, beside the zero-allocation
prototype. Five composition tests, all passing, and they are what items 1–3 rest
on:

| test | what it establishes |
| --- | --- |
| `TestSystemComposesWhenItDeclaresTheComponentsPlugin` | caller-owns composes; `Describe` reports the store and its owner |
| `TestSystemFailsCompositionWhenItDoesNotDeclareTheComponentsPlugin` | the coupling check fires on a Component store, with the exact message |
| `TestOwnershipByEcsMakesTheCouplingCheckVacuous` | the counterfactual: ownership by `ecs` removes the error entirely |
| `TestDuplicateComponentRegistrationIsCaught` | double registration names both plugins |
| `TestQueryOverAnUnregisteredComponentFailsComposition` | a missing store is a composition failure naming the Component, not a runtime one |

go1.27.1 windows/amd64, AMD Ryzen 9 7950X3D. `go vet` clean, stable at
`-count=10`. **The race detector cannot build in that environment**, so
`-count=10` was substituted; an implementation must run under `-race` in CI.
Thrown away with the rest of the prototype once this document is merged.
