# Turning a system signature into a conflict set

Research for [research: how other ECSes turn a system signature into a lock set](https://github.com/dvoyni/cog/issues/236), a ticket on the map [ecs: a plugin whose systems are plain funcs and whose locks come from components](https://github.com/dvoyni/cog/issues/180).

**Gathered 2026-09-11.** Primary sources throughout: `bevy_ecs` source read at tag **`v0.19.1`** (the latest release; `v0.19.1`, `v0.19.0`, `v0.18.1` are the three most recent tags), plus bevy's own PR bodies and review threads fetched from the GitHub API; the flecs manual from `SanderMertens/flecs@master`; EnTT's own `docs/md/entity.md`; and the Go ECS sources named below. Where a claim comes from a summary rather than a verbatim read, it says so.

**Licence posture:** every source here is MIT or MIT/Apache-2.0. Nothing below is copied code. Algorithms are described in prose; only short doc-comment and prose sentences are quoted, and each is attributed.

---

## The short version

- **Bevy's conflict set is two layers.** A raw `Access` (four bitsets over a dense `ComponentId` space: reads-and-writes, writes, archetypal, plus two "inverted" flags for `read_all`/`write_all`) *and*, orthogonal to it, a DNF filter formula: `Vec<AccessFilters>`, where each `AccessFilters` is a pair of bitsets, `with` and `without`. A `FilteredAccess` is one of each; a system holds a `FilteredAccessSet` — a `Vec<FilteredAccess>` (one per parameter) plus a precomputed union `Access` used as a coarse fast path.
- **Question 2's answer, exactly.** `With<T>` reduces to *setting bit T in the `with` bitset of every DNF clause, and contributing no read or write access at all*. `Without<T>` likewise sets bit T in `without`. Two clauses are declared mutually exclusive by one test: `!self.with.is_disjoint(&other.without) || !self.without.is_disjoint(&other.with)` — pure bitset intersection, no entity or archetype ever consulted. Two whole queries are disjoint iff **every** clause of one rules out **every** clause of the other.
- **The critical consequence for cog:** the filter is *not* reduced to anything that can become part of a lock key. It is reduced to a **pairwise predicate between two systems**. Bevy's runtime state is therefore a set of *running system indices*, not a table of held resources. `Access` for the same component can be simultaneously "write-locked by A" and "grantable to B" depending on who B is — which a reader-count/writer-set table keyed by component type cannot represent. (§2.4, §9)
- **Bevy deliberately gave up the finer-grained alternative.** Until bevy 0.16 (PR [#16885](https://github.com/bevyengine/bevy/pull/16885), merged 2025-05-05) the multi-threaded executor compared `ArchetypeComponentId` access *at runtime*, so two systems writing `Transform` over archetypes that happened not to overlap ran in parallel automatically. That was removed in favour of the static, more pessimistic component-level check, and users were told to add `Without` filters by hand. This is precisely the map's false-serialisation trade-off, and bevy chose the pessimistic side on purpose.
- **The check is computed once, not per frame.** `MultiThreadedExecutor::init` builds an n×n conflict matrix (one `FixedBitSet` of system indices per system). At run time admission is one bitset-disjointness test against the `running_systems` bitset. There is no per-frame access comparison and no lock table.
- **flecs does not schedule systems against each other at all.** Its pipeline is sequential; `multi_threaded` fans *one* system out across worker threads by slicing matched entities. The author's stated reason: individual systems are "too small & add too much scheduling overhead in large applications". Its read/write analysis exists for a different purpose — deciding where to insert **sync points** for deferred commands.
- **EnTT refuses to build a scheduler on purpose** and says so in its own docs: thread safety is "not something that users should want out of the box", and "users are completely responsible for synchronization whether required".
- **No Go ECS derives access from a function signature.** Two Go ECSes do auto-parallelise systems (`kongbong/ecsgo`, `zllangct/ecs`), but both take access by **explicit registration calls**, not from the signature, and neither uses filter-based disjointness. The mainstream Go ECSes (`mlange-42/arche`, `mlange-42/ark`, `yottahmd/donburi`) have no systems at all — "No systems. Just queries."

---

## 1. How the access set is derived

### 1.1 The three traits

Bevy stacks three levels of derivation. Each level is a trait with a method whose whole job is to *mutate an access accumulator*.

| Level | Trait | Method | Accumulator |
|---|---|---|---|
| One element of a query (`&T`, `&mut T`, `Option<&T>`, `With<T>`, `Has<T>`, `EntityMut`, …) | `WorldQuery` | `update_component_access(state, &mut FilteredAccess)` | `FilteredAccess` |
| One whole query | `QueryState` (built in `QueryState::new_uninitialized`) | — | `FilteredAccess` |
| One system parameter (`Query<..>`, `Res<T>`, `ResMut<T>`, `Local<T>`, `Commands`, `&World`, …) | `SystemParam` | `init_access(state, &mut SystemMeta, &mut FilteredAccessSet, &mut World)` | `FilteredAccessSet` |

`WorldQuery::update_component_access` is `unsafe` to implement: the safety contract is that the access you declare must cover the access you actually perform. That is the whole basis of soundness — the executor trusts these declarations absolutely.

Bevy does **not** reflect over the function signature at runtime. The parameter list is a Rust tuple type `F::Param`, and the derivation is monomorphised trait dispatch on that tuple. Tuples implement `SystemParam` by forwarding to each element in turn, which is why arbitrary arity works with no per-call allocation. There is also a `#[derive(SystemParam)]` macro so user-defined parameter structs participate.

### 1.2 What each element reduces to

Read from `crates/bevy_ecs/src/query/fetch.rs` and `query/filter.rs` at `v0.19.1`:

| Element | Effect on the accumulator |
|---|---|
| `&T` | `FilteredAccess::add_read(T)` — asserts not already written, then sets `read_and_writes[T]`, `required[T]`, **and `with[T]` in every clause** |
| `&mut T` / `Mut<T>` | `FilteredAccess::add_write(T)` — asserts not already read, sets `read_and_writes[T]`, `writes[T]`, `required[T]`, **and `with[T]` in every clause** |
| `Ref<T>` | same as `&T` |
| `Option<T>` | runs `T`'s derivation against a *clone*, then folds back **only the `Access`**, via `extend_access` — deliberately not the `with` filter |
| `Has<T>` | `add_archetypal(T)` only — no read, no write, no `with`/`without` |
| `With<T>` | `and_with(T)`: set bit T in `with` for every clause. **No access whatsoever.** |
| `Without<T>` | `and_without(T)`: set bit T in `without` for every clause. **No access whatsoever.** |
| `Allow<T>` | `add_archetypal(T)` only |
| `Added<T>` / `Changed<T>` | **`add_read(T)`** — these are *not* pure filters; they take a real read access (and therefore imply `with[T]`), and panic if T is already written in the same query |
| `Or<(A, B, …)>` | runs each sub-filter against a clone of the current accumulator, `append_or`s the resulting clause lists together, **and `extend_access`es each sub-filter's access in** |
| `EntityRef` | asserts nothing is written yet, then `read_all()` |
| `EntityMut` | asserts nothing is read yet, then `write_all()` |
| `FilteredEntityRef` / `FilteredEntityMut` / `EntityRefExcept` / `EntityMutExcept` | assert compatibility with what's accumulated, then extend by a dynamically-built `Access` |
| `()` | nothing |

Two of these are worth dwelling on.

**`&T` and `&mut T` imply `With<T>`.** This is why one-sided `Without` is enough to disjoin a pair of queries (§2.3). It is also why `FilteredAccess` carries a separate `required` bitset: `required` is the set that data access alone contributed, used by query transmutes, and it excludes `With<C>` filters and excludes anything inside `Option`.

**`Option<T>` deliberately breaks that implication**, and the type's own doc comment explains why, because it is the subtle case that makes the whole scheme sound. Quoting `FilteredAccess`'s doc comment verbatim:

> "Subtle: a `read` or `write` in `access` should not be considered to imply a `with` access. … consider `Query<Option<&T>>` this only has a `read` of `T` as doing otherwise would allow for queries to be considered disjoint when they shouldn't: `Query<(&mut T, Option<&U>)>` read/write `T`, read `U`, with `U`; `Query<&mut T, Without<U>>` read/write `T`, without `U` — from this we could reasonably conclude that the queries are disjoint but they aren't."
>
> — `crates/bevy_ecs/src/query/access.rs`, v0.19.1

And the converse, from the same comment: `Option<&T>` must still register a *read* of `T`, or `Query<&mut T>` and `Query<Option<&T>>` would be wrongly judged disjoint.

So the invariant is: **`with`/`without` are about which entities you touch; `read_and_writes`/`writes` are about which columns you touch. They are independent dimensions and neither subsumes the other.** That separation is the design.

### 1.3 Assembling a query

`QueryState::new_uninitialized` builds the data access and the filter access into **two separate fresh `FilteredAccess` values**, then `extend`s the filter one into the data one. `FilteredAccess::extend` is the AND operation: it unions the `Access` parts, unions `required`, and takes the **cartesian product** of the two clause lists (with a short-circuit when one side has exactly one clause, which is the common case). Its doc comment states the expansion rule:

> "Extending `Or<(With<A>, Without<B>)>` with `Or<(With<C>, Without<D>)>` will result in `Or<((With<A>, With<C>), (With<A>, Without<D>), (Without<B>, With<C>), (Without<B>, Without<D>))>`."

After that, the world's `DefaultQueryFilters` (the entity-disabling mechanism) gets to modify the access — so there is a world-global hook that can add clauses, not only the query type.

### 1.4 Assembling a system

`FunctionSystem::initialize(&mut World) -> FilteredAccessSet` creates an empty `FilteredAccessSet` and calls `F::Param::init_access` on it. Each parameter appends one or more `FilteredAccess` entries via `FilteredAccessSet::add`, which also folds the new access into the set's `combined_access` union.

Resources are modelled as components. `add_resource_read(id)` builds a `FilteredAccess`, adds a read of `id`, and then `and_with(IS_RESOURCE)` — a reserved `ComponentId` marker. That single trick makes resource access live in the same bitset space as component access while never being able to alias it: a resource clause always carries `with[IS_RESOURCE]`, a component clause never does, so the ruled-out test separates them automatically. (It is also why B0002's advice mentions `Without<IsResource>`.)

Parameters that take no world access still leave marks on `SystemMeta`:

| Parameter | Access | Side effect on `SystemMeta` |
|---|---|---|
| `Local<T>` | none | none |
| `Deferred<T>` (and therefore `Commands`) | none | `set_has_deferred()` |
| `ExclusiveMarker` (i.e. `&mut World` systems) | none | `set_exclusive()` |
| `NonSendMarker` | none | `set_non_send()` |
| `&World` | `read_all()`, panics if it conflicts with anything already accumulated | none |
| `DeferredWorld` | `write_all()`, asserts nothing is read yet | none |

`Commands` is a three-field aggregate: `(Deferred<CommandQueue>, &EntityAllocator, &Entities)`. The queue contributes **no component access at all** — only the deferred flag. This is the single most consequential fact about `Commands`: two systems that both spawn, despawn and insert components never conflict, because as far as the access analysis is concerned they touch nothing.

### 1.5 When it is computed

`ScheduleGraph::initialize_systems` calls `System::initialize(world)` for every system and every run condition, and stores the returned `FilteredAccessSet` **beside** the system (`SystemWithAccess { system, access }`). That happens during `Schedule::run` the first time a schedule runs after being modified, or on an explicit `Schedule::initialize`.

So "startup-time panic" is precisely: *the first time that particular schedule is run against that world*. It is not compile time, and it is not app-build time. A system added to a schedule that is never run is never checked. (This is also why bevy insists a system be initialised against the same `World` it was added to — `initialize` asserts on `world.id()`.)

---

## 2. How conflicts are decided — and what the filter is reduced to

### 2.1 The data structure, precisely

```
FilteredAccessSet
  combined_access : Access                 // union of all members, coarse fast path
  filtered_accesses : Vec<FilteredAccess>  // one per system parameter

FilteredAccess
  access      : Access
  required    : ComponentIdSet             // bitset; what data access alone demanded
  filter_sets : Vec<AccessFilters>         // DNF: an OR of ANDs

AccessFilters                              // one conjunctive clause
  with    : ComponentIdSet                 // bitset
  without : ComponentIdSet                 // bitset

Access
  read_and_writes : ComponentIdSet
  writes          : ComponentIdSet
  archetypal      : ComponentIdSet
  read_and_writes_inverted : bool          // true ⇒ the bitset is a complement
  writes_inverted          : bool
```

`ComponentIdSet` is a newtype over `fixedbitset::FixedBitSet`, indexed by `ComponentId`, which is a dense monotonically-assigned index over every component *and* resource type registered in the world.

`AccessFilters`'s own doc comment gives the semantics: it "matches entities that have *all* the components in the `with` filters and *none* of the components in the `without` filters"; `filter_sets` "will match an entity if *any* of the `AccessFilters` matches". Two sentinel values exist: `matches_everything()` is one empty clause (TRUE), `matches_nothing()` is the empty clause list (FALSE).

### 2.2 The three-level compatibility test

**Level 1 — raw `Access`.** `Access::is_compatible` is "we have a conflict if we write and they read or write, or if they write and we read or write", evaluated as bitset intersections in both directions, with the inverted flags handled by complement-aware union/difference helpers. Archetypal access is ignored here entirely: `Has<T>` and `Allow<T>` conflict with nothing.

**Level 2 — `FilteredAccess`.** Fast path: if the raw accesses are compatible, done. Otherwise fall through to the filter check. That check is, verbatim from the source's own comment:

> "Since the `filter_sets` array represents a Disjunctive Normal Form formula ("ORs of ANDs"), we need to make sure that each filter set (ANDs) rule out every filter set from the `other` instance."

and the per-clause test is a single line:

```
is_ruled_out_by(self, other) :=
      with    ∩ other.without  ≠ ∅
   OR without ∩ other.with     ≠ ∅
```

i.e. one clause rules out another iff one demands a component the other forbids. Whole queries are disjoint iff that holds for **all pairs** of clauses.

This is sound and, for individually-satisfiable clauses, complete: a conjunction of positive and negative literals over distinct propositional variables is unsatisfiable exactly when it contains both a literal and its negation. The one acknowledged gap is self-contradiction, which the source calls out:

> "Although not technically complete, we don't consider the case when `AccessFilters`'s `without` bitset contradicts its own `with` bitset (e.g. `(With<A>, Without<A>)`). Such query would be considered compatible with any other query, but as it's almost always an error, we ignore this case instead of treating such query as compatible with others."

(The comment's wording is confusing; the code's behaviour is the conservative one. A query that can never match any entity is still reported as conflicting. That is a false positive, not a soundness hole.)

**Level 3 — `FilteredAccessSet`.** Coarse check on `combined_access` first; if that passes, the two systems are compatible outright. Only if it fails does it do the O(params × params) all-pairs walk over `filtered_accesses`. The set-level `is_compatible` returns `false` on the **first** incompatible pair.

### 2.3 The worked example from the ticket

Let component ids be `P = Position`, `L = Player`.

| Query | `access` | clause `with` | clause `without` |
|---|---|---|---|
| `Query<&mut Position, With<Player>>` | write P | `{P, L}` | `{}` |
| `Query<&mut Position, Without<Player>>` | write P | `{P}` | `{L}` |

`with` contains `P` in both rows because `&mut Position` calls `add_write`, which calls `and_with(P)`. Level 1 fails: both write P. Level 2: `{P,L} ∩ {L} = {L} ≠ ∅` → ruled out. Symmetric direction also holds. One clause each, so all pairs are covered. **Disjoint.**

And the one-sided case, which is bevy's own migration example, verbatim from PR [#16885](https://github.com/bevyengine/bevy/pull/16885):

> "The schedule will now prevent systems from running in parallel if there *could* be an archetype that they conflict on, even if there aren't actually any. For example, these systems will now conflict even if no entity has both `Player` and `Enemy` components:
> ```rust
> fn player_system(query: Query<(&mut Transform, &Player)>) {}
> fn enemy_system(query: Query<(&mut Transform, &Enemy)>) {}
> ```
> To allow them to run in parallel, use `Without` filters, just as you would to allow both queries in a single system:
> ```rust
> // Either one of these changes alone would be enough
> fn player_system(query: Query<(&mut Transform, &Player), Without<Enemy>>) {}
> fn enemy_system(query: Query<(&mut Transform, &Enemy), Without<Player>>) {}
> ```"

"Either one alone would be enough" falls straight out of the rule: `&Player` already put `L` in the player query's `with`, so the enemy query's `without` needs only `{L}` for the intersection to be non-empty. The reverse direction of the test catches it symmetrically.

### 2.4 What the filter is reduced to — the load-bearing answer

**It is reduced to two `FixedBitSet`s per DNF clause, over the same dense `ComponentId` index space as the read/write sets, carried alongside them and never merged into them.**

The three properties that matter:

1. **A filter contributes nothing to the read/write sets.** `With<T>` and `Without<T>` touch only `with`/`without`. So the filter cannot be folded into a "which resources does this system lock" answer — it is not an access at all.
2. **The comparison is a relation between two systems, not a lookup against shared state.** `is_ruled_out_by` needs both sides. There is no value derivable from one system alone such that "has the same value ⇒ conflicts".
3. **Therefore compatibility is not transitive and not composable into a per-key counter.** Concretely: let A = `Query<&mut P, With<L>>`, B = `Query<&mut P, Without<L>>`, C = `Query<&mut P>`. A∥B is legal. A∥C is not. B∥C is not. If A is running, "P is write-locked" must simultaneously block C and admit B. A single writer-set entry keyed on `P` cannot express that.

Bevy's answer is to not have a lock table. Its runtime state is the set of *running system indices*, and admission is "does my precomputed conflict-set intersect the running set" (§5).

### 2.5 What filter-based disjointness cannot express

- **Anything about entities or values.** Two systems writing `Position` over disjoint spatial regions, disjoint id ranges, disjoint chunks, or "the half of the entities my previous pass selected" — none of these are expressible. The vocabulary is exactly: presence and absence of component types. If you want two writers of one component to run in parallel you must invent a marker component and put it in the archetype.
- **Actual archetype overlap.** Explicitly withdrawn; see §3.4.
- **Disjointness discovered by a run condition.** Run conditions have their own access and are checked *separately* (the executor keeps a second `condition_conflicting_systems` bitset per system precisely because a system can be skipped by a set condition yet still need access to evaluate its own conditions). A condition cannot make two systems disjoint.
- **Disjointness across a deferred boundary.** `Commands` has no access, so the analysis has nothing to say about it; correctness there rests entirely on the `ApplyDeferred` sync points, not on the conflict set.
- **Self-contradictory clauses**, treated conservatively (§2.2).
- **Anything that would need more than DNF-of-literals.** The representation is closed under AND (cartesian product) and OR (concatenation) but has no negation of a clause, so a filter language richer than `With`/`Without`/`Or` would either reduce into this shape or need a new comparison.
- **Scale limit on `Or`.** `extend` multiplies clause counts. Two `Or<(_, _, _)>`s in one query is 9 clauses; the pairwise system-vs-system check then costs 81 bitset pairs. Nothing caps this.

---

## 3. What happens when conflict detection fails

There are three separate mechanisms, catching three different things, at three different times.

### 3.1 Within one query — panic during `WorldQuery` derivation

`&mut T` asserts `!access.has_read(T)`; `&T` asserts `!access.has_write(T)`; `Added<T>`/`Changed<T>` assert `!has_write(T)`; `EntityRef` asserts `!has_any_write()`; `EntityMut` asserts `!has_any_read()`. So `Query<(&mut Position, &Position)>` panics while the query's access is being assembled, with a message naming the offending element. Note these fire in *declaration order* within the tuple, which is why `Mut<T>` carries its own copy of the assertion — to avoid an error message that says `&mut T` when the user wrote `Mut<T>`.

### 3.2 Within one system — `error[B0001]` and `error[B0002]`

`QueryState::init_access` calls `FilteredAccessSet::get_conflicts_single` against everything the system has declared so far. If non-empty:

> `error[B0001]: <query type> in system <name> accesses component(s) <list> in a way that conflicts with a previous system parameter. Consider using Without<T> to create disjoint Queries or merging conflicting Queries into a ParamSet.`

`Res<T>`/`ResMut<T>` produce the analogous `B0002`. `&World` and `DeferredWorld` have their own bespoke panics. Every one of these is a **panic**, not a `Result` — there is no way to run a bevy app with a mis-declared system.

Crucially this check uses the *same* filtered compatibility as the scheduler. So `fn s(a: Query<&mut P, With<L>>, b: Query<&mut P, Without<L>>)` is legal in one system, by exactly the mechanism of §2.3.

### 3.3 Across systems — the ambiguity detector

This catches something different: not unsoundness, but **non-determinism**. Two systems that conflict on access and have no ordering relation will be serialised by the executor, but in an arbitrary order that may differ run to run.

`ScheduleGraph::build_schedule` computes it once, in `SystemNodes::get_conflicting_systems`:

1. Take every pair of systems that is **disconnected** in the transitively-reduced flattened dependency DAG — i.e. pairs with no ordering between them in either direction.
2. Skip pairs joined by `.ambiguous_with(..)` or covered by `ambiguous_with_all`.
3. If either is an **exclusive** system, report the pair with an empty conflict list.
4. Otherwise test `FilteredAccessSet::is_compatible`; on failure, collect the conflicting `ComponentId`s, drop any in `World`'s `ignored_scheduling_ambiguities` set (populated by `allow_ambiguous_component::<T>` / `allow_ambiguous_resource::<T>`), and report what remains. If the conflict is `AccessConflicts::All` (e.g. two `Query<EntityMut>` systems) the pair is reported with no component list.

**What it misses.** Order-dependence that is not visible as component access: two systems that both use `Commands` to mutate the same entity; two systems communicating through a `NonSend` resource; anything a system does through an escape hatch. Also, by construction, anything the pair-generation step skips — a pair with an ordering edge is never examined even if the edge exists for an unrelated reason.

**What it over-reports.** Exclusive systems, unconditionally. And conflicts between systems that a user considers benign (commutative accumulation, order-irrelevant writes) — hence the two suppression mechanisms.

**What it costs, and whether it is on.** It is O(disconnected pairs) × the cost of `FilteredAccessSet::is_compatible`, once per schedule build. And it is **off by default**: `ScheduleBuildSettings::new()` sets `ambiguity_detection: LogLevel::Ignore` (contrast `hierarchy_detection: LogLevel::Warn`). A user must opt in to `Warn` or `Error`.

That default is the honest measure of the feature: bevy computes a correct ambiguity report and then does not show it, because on a real app it is noisy.

### 3.4 The failure mode bevy fixed by making the analysis *worse*

Before bevy 0.16, the executor's runtime check used `ArchetypeComponentId` — a (archetype, component) pair — so two systems writing `Transform` ran concurrently whenever no archetype actually carried both `Player` and `Enemy`. PR [#16885](https://github.com/bevyengine/bevy/pull/16885) ("Stop using `ArchetypeComponentId` in the executor", merged 2025-05-05) replaced it with the static component-level check. Its stated objective, verbatim:

> "Stop using `ArchetypeComponentId` in the executor. These IDs will grow even more quickly with relations, and the size may start to degrade performance."

> "Have systems expose their `FilteredAccessSet<ComponentId>`, and have the executor use that to determine which systems conflict. This can be determined statically, so determine all conflicts during initialization and only perform bit tests when running."

Reviewer `hymm` benchmarked update-schedule times (negative = the PR is slower), posted in the PR thread:

| entities | components | systems | PR (ms) | main (ms) | change |
|---|---|---|---|---|---|
| 50 000 | 100 | 100 | 1.93 | 1.9 | −1.55% |
| 50 000 | 100 | 1600 | 39.32 | 24.32 | **−38.15%** |
| 50 000 | 2000 | 100 | 0.138 | 0.169 | +22.46% |
| 50 000 | 2000 | 1600 | 2.92 | 3.17 | +8.56% |
| 1 000 000 | 100 | 100 | 62.63 | 62.93 | +0.48% |
| 1 000 000 | 100 | 1600 | 1750 | 1320 | **−24.57%** |
| 1 000 000 | 2000 | 100 | 4.03 | 6.28 | +55.83% |
| 1 000 000 | 2000 | 1600 | 112.91 | 131.56 | +16.52% |

and his own reading of it:

> "We see there are some regressions on the 100 components-1600 systems results. This is somewhat expected as these are a worst case senario for this pr. We end up with fewer systems that can run in parallel because of the more pessimistic access check. … We lose some parallelism, but gain some speed spawning system tasks due to cheaper checks when there's a large number of archetype components. … But this doesn't answer the question of 'What does a more realistic schedule look like?' I would expect conflicts to be low or nonexistant, since those should get detected as ambiguities between systems and then get ordered."

Maintainer `alice-i-cecile`'s reply is the most quotable sentence in the whole search, and it is a direct verdict on fine-grained system parallelism:

> "Based on my conversations with users, I think that in practice our current parallelism is too fine-grained for realistic projects. Swapping to single-threaded schedules for your main update loop shouldn't be speeding things up! Both the checks and the system task dispatch are too expensive for all but the heaviest systems."

Follow-up PR [#19143](https://github.com/bevyengine/bevy/pull/19143) then deleted `ArchetypeComponentId` outright — "Following #16885, they are no longer used by the engine, so we can stop spending time calculating them or space storing them" — removing `System::update_archetype_component_access` and `SystemParam::new_archetype`, and saving (per reviewer figures on 64-bit) 8 bytes per component per archetype, 8 per resource, and ≥128 bytes per system in `SystemMeta`.

One more thing from that thread, from the PR author: at the time, the ambiguity checker and the executor used *different* conflict tests, and he considered that a defect —

> "It currently ignores filters, so it reports things that I consider false positives."

In `v0.19.1` they have been unified: both `SystemNodes::get_conflicting_systems` and `MultiThreadedExecutor::init` call `FilteredAccessSet::is_compatible` on the same stored `FilteredAccessSet`. Worth noting as a design lesson: bevy briefly had two disagreeing notions of "conflicts".

---

## 4. Escape hatches

Each exists because static analysis gave a wrong or unusable answer, and each says something different about where.

### 4.1 `ParamSet` — "the analysis is right, I'll take responsibility"

`ParamSet<(P0, P1, …)>` lets a system hold parameters that *do* conflict, by giving out only one at a time (`p.p0()`, `p.p1()`, borrow-checked by Rust so two cannot be live together).

The mechanism in `init_access` is two passes:

1. For each member, run its `init_access` against a **clone** of the system's accumulated set — so a member is still checked against everything *outside* the ParamSet, and its panics still fire.
2. Then for each member, run `init_access` against a **fresh empty** `FilteredAccessSet` and `extend` the result into the system's set — so the members are never compared against *each other*.

The net access of the system is the union, so the *scheduler* still sees the full conflict set. `ParamSet` buys nothing at the schedule level; it only suppresses the intra-system check. The bevy B0001 doc states the contract plainly: "A `ParamSet` will let you have conflicting queries as a parameter, but you will still be responsible for not using them at the same time in your system."

**What this tells us:** the intra-system check is stricter than necessary in a specific, common case — a system that reads a query, finishes, then writes an overlapping query. The analysis has no notion of time within a system, so the escape hatch reintroduces it manually, guarded by the borrow checker rather than by the ECS.

### 4.2 Exclusive systems (`&mut World`) — "the analysis cannot describe what I do"

A system taking `&mut World` gets the `ExclusiveMarker` parameter, which registers **no access** and instead sets a flag on `SystemMeta`. The executor then treats it structurally: `can_run` refuses to start an exclusive system while `num_running_systems > 0`, and once one starts, nothing else is dispatched until it finishes. The ambiguity detector reports an exclusive system as conflicting with every disconnected system, with no component list.

**What this tells us:** the vocabulary has no way to say "everything, including structure". Rather than extend the vocabulary, bevy stepped outside it into a scheduler-level mode. `ApplyDeferred` is itself implemented this way — `NON_SEND | EXCLUSIVE`.

Note that `write_all()` exists and *would* express "all components", and `DeferredWorld` uses it. Exclusive is stronger: it also covers archetype structure, resource creation, and schedule mutation, which the component-id space cannot name.

### 4.3 `Commands` / `Deferred<T>` — "don't analyse it, defer it"

`Commands` registers no component access at all. Structural changes (spawn, despawn, insert, remove) are queued into a per-system `CommandQueue` and applied later by an `ApplyDeferred` exclusive system. With `auto_insert_apply_deferred: true` (the default in `ScheduleBuildSettings`), the schedule builder inserts those sync points automatically: between a system with deferred buffers and anything that depends on it, and at the end of the schedule.

**What this tells us:** structural mutation is the case where a static access set is *hopeless* — you cannot know at registration which components a command will add. Bevy's answer is not a bigger conflict set but a different execution model: make the mutation invisible until a barrier, so it needs no lock at all. The cost is a frame-latency semantics that users must learn, and a class of order-dependence the ambiguity detector cannot see (§3.3).

### 4.4 Suppression knobs

`.ambiguous_with(other)`, `.ambiguous_with_all()`, `allow_ambiguous_component::<T>`, `allow_ambiguous_resource::<T>`, and the `ambiguity_detection: LogLevel::Ignore` default. Five ways to say "I know, stop telling me". That count is itself a finding about how noisy a purely-type-level conflict report is on a real schedule.

---

## 5. Cost

### 5.1 Representation

A dense bitset over `ComponentId` throughout — `fixedbitset::FixedBitSet`, wrapped as `ComponentIdSet`, indexed by a monotonically-assigned id covering every registered component *and* resource. Per `FilteredAccess`: 3 bitsets in `Access` + `required` + 2×(clause count) filter bitsets. Per system: one `FilteredAccess` per parameter plus one union `Access`.

`read_all`/`write_all` are represented by the two `inverted` flags rather than by setting every bit, which is what lets `EntityMut` and `&mut World`-adjacent params exist without knowing the component count. The union and difference helpers are complement-aware.

### 5.2 The schedule is computed once — there is no per-frame check

`MultiThreadedExecutor::init` (run when the schedule is built or rebuilt, not per frame) does:

- For each ordered pair (i, j), i > j: test `FilteredAccessSet::is_compatible`. On failure set bit j in system i's `conflicting_systems` and bit i in system j's. **O(n²) compatibility tests**, each itself O(params² × clauses² × bitset words) in the worst case but almost always settled by the `combined_access` fast path.
- For each system i and each system j (full n², not triangular, because conditions are asymmetric): if any of i's run conditions conflicts with j's access, set bit j in i's `condition_conflicting_systems`.
- For each system set: the same, producing `set_condition_conflicting_systems`.

Result: per system, two `FixedBitSet`s of n bits — **n²/8 bytes × 2** plus the set-condition matrix. At 1600 systems that is roughly 320 KB per matrix. PR [#19477](https://github.com/bevyengine/bevy/pull/19477) ("Stop storing access for all systems") exists specifically to reduce copies of `FilteredAccessSet` by returning it from `System::initialize` rather than exposing it from a method on `System` — which is why, in `v0.19.1`, the access lives in `SystemWithAccess`/`ConditionWithAccess` beside the system rather than inside it.

### 5.3 Per-frame

`ExecutorState` holds `ready_systems`, `running_systems`, `completed_systems`, `skipped_systems`, `unapplied_systems` — all `FixedBitSet`s of n bits. Admission (`can_run`) is, in order:

1. exclusive and anything running → no;
2. `!is_send` and the main thread is busy → no;
3. for each not-yet-evaluated set condition: `set_condition_conflicting_systems[set] ∩ running_systems ≠ ∅` → no;
4. `condition_conflicting_systems ∩ running_systems ≠ ∅` → no;
5. not skipped and `conflicting_systems ∩ running_systems ≠ ∅` → no.

That is it. Bitset disjointness, ~n/64 words, no allocation, no map lookup, no comparison of access sets. Dispatch is driven off dependency counts (`num_dependencies_remaining`) with a re-scan loop when a system completes or is skipped.

The contrast with a lock-table scheduler is total: bevy pays O(n²) once and O(n/64) per admission; a lock table pays O(1) once and O(|lock set|) per admission plus per-release bookkeeping. Bevy moved *toward* the precomputed matrix (from the old per-frame archetype-component comparison) on performance grounds.

---

## 6. flecs

Sources: `docs/Systems.md` from `SanderMertens/flecs@master`, and the author's answers in [discussion #1590](https://github.com/SanderMertens/flecs/discussions/1590).

**flecs does not schedule systems against each other.** This is the single most important fact and it is the opposite of what the name "multithreaded systems" suggests. From `Systems.md` verbatim:

> "By default systems are created as single threaded. Single threaded systems are always ran on the main thread. Multithreaded systems are ran on all worker threads. The scheduler runs each multithreaded system on all threads, and divides the number of matched entities across the threads. The way entities are allocated to threads ensures that the same entity is always processed by the same thread, until the next sync point."

> "The way the scheduler ensures that the same entities are processed by the same threads is by slicing up the entities in a table into N slices, where N is the number of threads. For a table that has 1000 entities, the first thread will process entities 0..249, thread 2 250..499, thread 3 500..749 and thread 4 entities 750..999."

Sander Mertens, in discussion #1590 (as summarised by fetch, with the key clause quoted):

> "No, the Flecs scheduler does not schedule individual systems. It is as you said: each multithreaded system is run on all threads, and entities get divided across threads."

with the rationale that individual systems are "too small & add too much scheduling overhead in large applications".

This is the *chunk-parallel* model the cog map explicitly declines to specify — and flecs adopts it **instead of**, not in addition to, task-parallel systems.

**What flecs' read/write analysis is actually for.** It does analyse per-term `[in]`/`[out]`/`[inout]` annotations, but the output is not a parallel schedule; it is **sync point placement** for the deferred command queue:

> "In addition to a system query, pipelines also analyze the components that systems are reading and writing to determine where to insert sync points. During a sync point enqueued commands are ran, which ensures that systems after a sync point can see all mutations from before a sync point."

> "A pipeline tracks on a per-component basis whether commands could have been inserted for it, and when a component is being read. When a pipeline sees a read for a component for which commands could have been inserted, a sync point is inserted before the system that reads."

Three consequences worth recording:

- **The analysis is deliberately incomplete and needs manual annotation.** "Because Flecs can't see inside the implementation of a system, pipelines can't know for which components a system could insert commands. This means that by default a pipeline assumes that systems insert no commands." To get earlier merges you annotate a phantom term — `[out] Transform()` — meaning "I write Transform but do not match on it". This is the direct analogue of bevy's `Commands`-has-no-access gap, solved by hand-declaration rather than by a barrier.
- **Sync point placement is dynamic, not static.** "When a system is inactive (e.g. it doesn't match any entities) or is disabled, it will be ignored for sync point insertion." So the set of barriers can change frame to frame with the composition of the world.
- **Staging is what makes the parallelism safe, not locking.** During `progress()` the world is readonly and structural operations are enqueued per-thread. Quoting: "enqueueing operations makes it safe for multiple threads to iterate the same world without taking locks as thread gets its own command queue." Component *values* may still be written in readonly mode; only structural change is deferred. The `immediate` flag opts a system out of readonly mode, with restrictions (operations on the iterated entity must still be deferred), and `defer_suspend`/`defer_resume` are the finer-grained knob.

---

## 7. EnTT

Source: `docs/md/entity.md` in `skypjack/entt@master`, §Multithreading.

EnTT has no scheduler, no access declaration, and no conflict analysis, and its docs argue that this is correct rather than merely unfinished. Verbatim:

> "In general, the entire registry is not thread safe as it is. Thread safety is not something that users should want out of the box for several reasons. Just to mention one of them: performance."

> "As long as a thread iterates the entities that have the component `X` or assign and removes that component from a set of entities, another thread can safely do the same with components `Y` and `Z` and everything work like just fine."

> "Because of the few reasons mentioned above and many others not mentioned, users are completely responsible for synchronization whether required. On the other hand, they could get away with it without having to resort to particular expedients."

The position is: the *safety rule* is simple and type-level (disjoint component sets are independent; a shared component set may be read concurrently as long as nothing is assigned or removed), so the user can apply it without a framework enforcing it. The parallelism EnTT actually recommends is again **data-parallel**, not task-parallel — its view iterators satisfy the random-access iterator requirements so they can be handed to `std::for_each` with `std::execution::par_unseq`. That "can increase the throughput considerably, even without resorting to who knows what artifacts that are difficult to maintain over time."

EnTT does ship a scheduler, but it is a *cooperative process scheduler* for tick-driven process lifecycles (`docs/md/process.md`) — unrelated to system access or parallelism. The one concession to threading is the `ENTT_USE_ATOMIC` compile-time flag.

**The lesson for a map like this one:** the strongest argument against building the analysis is that a hand-written rule ("these two systems touch disjoint component sets") is cheap for a human to check and expensive for a framework to prove — and that a framework which proves it still cannot prove the cases users care about most.

---

## 8. Go

**No Go ECS derives a system's access set from its function signature.** Checked across the top Go ECS repositories by stars (GitHub search, `ecs entity component system language:Go`, 2026-09-11).

**The mainstream ones have no systems at all.** `mlange-42/arche` and `mlange-42/ark` both advertise, verbatim in their READMEs: "No systems. Just queries. Use your own structure (or the Tools)." Their companion modules (`arche-model`, `ark-tools`) add a scheduler, but it is an update-rate scheduler — running logic and UI systems at independent tick rates — not a parallelism scheduler. `yottahmd/donburi` likewise provides systems as a convention, not a conflict-analysed schedule.

**Two Go ECSes do auto-parallelise, and both take access by explicit registration.**

- **`kongbong/ecsgo`** (MIT, ~25 stars) is the closest analogue to the map's design. Its README claim is "Run systems in concurrently with analyzing dependency tree". But a system is `type SystemFn func(ctx *ExecutionContext) error` — a **fixed signature**. Access is declared by imperative generic calls on a `Query` value obtained from the system: `AddReadWriteComponent[T](q)`, `AddReadonlyComponent[T](q)`, `AddExcludeComponent[T](q)`, plus optional and at-least-one-of variants. The query stores `[]reflect.Type` lists. `executiongroup.go` builds a dependency graph by testing every pair (`for i; for j := i+1`) with `System.dependent`, then walks it with a `sync.WaitGroup` per edge.

  Its conflict rule is instructive by contrast with bevy's: `Query.dependent` walks the include, optional and at-least-one component lists, and declares a dependency if the other query is "interested" in the same type *unless both sides list it readonly*. **`excludeComponents` is never consulted.** So ecsgo has the `Without` vocabulary, uses it for matching, and does **not** use it for disjointness — the exact capability the cog map's false-serialisation ticket is asking about, present in the type system and absent from the scheduler.

- **`zllangct/ecs`** (~156 stars) also parallelises systems, also by explicit declaration: components of interest are registered in the system's `Init` event, and the README states (Chinese) that the dependency computation between systems is done at startup, not derived from signatures. A system cannot access components it did not declare.

So: the technique the map proposes — **reflect a plain Go func's parameter types into a lock set** — has no precedent in the Go ECS ecosystem. The two auto-parallel Go ECSes both chose explicit registration over signature derivation, and neither implements filter-based disjointness.

---

## 9. Facts bearing on cog's scheduler

Stated as findings, not a recommendation.

`kernel/scheduler.go` (read at `10882a7`) maintains `readers map[reflect.Type]int` and `writers map[reflect.Type]struct{}`, and grants a `lockRequest` when `compatible(req)` — no requested read is in `writers`, no requested write is in `writers` or has `readers > 0`. Dispatch is conflict-aware FIFO over a `pending` slice: a later request may pass a blocked one only if it `overlaps` none of the blocked requests' resources. All bookkeeping lives in one coordinator goroutine.

Against that, from the above:

1. **The `reflect.Type` key and the filter live in different places in bevy.** The component type is a `ComponentId` bit in `read_and_writes`/`writes`; the filter is a `ComponentId` bit in `with`/`without` on a separate structure. cog's scheduler has a place for the first and no place for the second.

2. **Filtered compatibility is a pairwise predicate, not a key-indexed state.** §2.4 gives the three-query counterexample (A = `With<L>` writer, B = `Without<L>` writer, C = unfiltered writer): whether "P is write-locked by A" blocks a request depends on who is requesting. A `writers[P]` entry, or any per-type counter, cannot carry that. Nor can `overlaps`, which is a set-intersection test over requested resource types.

3. **Bevy's runtime state is the running-system set, not a resource table.** It answers "may I start" with `conflicting_systems ∩ running_systems == ∅` against a matrix precomputed once. That shape assumes a *fixed, enumerable* set of tasks known at schedule-build time. cog's scheduler admits arbitrary `task`s at arbitrary times (`s.acquire` is an open channel), so there is no n to build an n×n matrix over unless the ECS maintains its own registry of systems separately from the kernel's lock requests.

4. **Bevy tried the runtime-precision route and abandoned it.** The `ArchetypeComponentId` executor is the only design surveyed that let two writers of one component run in parallel *without* a hand-written filter, and it was deleted in 0.16 as too expensive relative to what it bought, with the maintainer's judgement that "our current parallelism is too fine-grained for realistic projects" (§3.4). The replacement's user-facing cost was: add `Without` by hand, or serialise.

5. **The two competing models for "two systems write Position over disjoint entities" are filters (bevy) and chunking (flecs).** flecs' answer to the same problem is not to disjoin the systems but to split *one* system's entity range across threads, with staging instead of locking — and its author's stated reason for not scheduling systems against each other is that systems are too small a unit to be worth the scheduling overhead. The cog map already has this as a deliberate non-goal ("chunk-parallel execution is not specified by this map, and nothing may foreclose it"); flecs is evidence that it is the *substitute* for, not the complement to, system-level parallelism.

6. **`Commands` is the precedent for cog's "Uses folds a dispatched command's lock set into its caller".** Bevy went the opposite way: a deferred command buffer takes **no** access, and correctness comes from an exclusive `ApplyDeferred` barrier auto-inserted between a producer and its dependants. That trades lock-set width for frame latency and for a class of ordering bugs the ambiguity detector cannot see.

7. **Every conflict failure in bevy is a panic at first-run of the schedule, not a compile error and not a runtime error value.** The panics fire in `WorldQuery::update_component_access` (intra-query) and `SystemParam::init_access` (intra-system). There is no `Result` path. For cog this maps onto composition-time failure (`ErrMissingResource`, `ErrUndeclaredDependency`) rather than onto anything at `Lock` time.

8. **The straight-line-`Lock` rule has an analogue in bevy and it is satisfied structurally.** Bevy's derivation is a fixed sequence of trait calls over a statically-known tuple type — no loop over a runtime list, no conditional. The loop that cog's `.github/instructions/kernel.instructions.md` forbids is exactly the loop bevy avoids by monomorphisation. Bevy's escape from arbitrary arity without reflection is tuple `SystemParam` impls generated by a macro for arities 0..16 (`all_tuples!`), which is the same shape as the map's stated fallback of "fixed-arity generic constructors".

---

## Sources

Read directly (pinned):

- `bevyengine/bevy` @ `v0.19.1`, MIT/Apache-2.0 — `crates/bevy_ecs/src/query/access.rs`, `query/state.rs`, `query/filter.rs`, `query/fetch.rs`, `system/system_param.rs`, `system/function_system.rs`, `system/mod.rs`, `system/commands/mod.rs`, `schedule/schedule.rs`, `schedule/node.rs`, `schedule/executor/mod.rs`, `schedule/executor/multi_threaded.rs`.
- `SanderMertens/flecs` @ `master`, MIT — `docs/Systems.md`.
- `skypjack/entt` @ `master`, MIT — `docs/md/entity.md`.
- `kongbong/ecsgo` @ `main`, MIT — `system.go`, `executiongroup.go`, `README.md`.
- `mlange-42/arche` and `mlange-42/ark` @ `main`, MIT — `README.md`.
- `dvoyni/cog` @ `10882a7` — `kernel/scheduler.go`.

Fetched:

- bevy PR [#16885](https://github.com/bevyengine/bevy/pull/16885) — body and full comment thread via the GitHub API (verbatim).
- bevy PR [#19143](https://github.com/bevyengine/bevy/pull/19143), PR [#19477](https://github.com/bevyengine/bevy/pull/19477) — summarised via fetch, not read verbatim.
- [bevy.org/learn/errors/b0001](https://bevy.org/learn/errors/b0001/) — summarised via fetch.
- flecs [discussion #1590](https://github.com/SanderMertens/flecs/discussions/1590) — summarised via fetch; the two quoted clauses are verbatim from that summary and should be re-checked against the thread before being quoted in a spec.
- [zllangct/ecs](https://github.com/zllangct/ecs) README — summarised via fetch (Chinese source).
- GitHub repository search, `ecs entity component system language:Go`, sorted by stars, 2026-09-11.
