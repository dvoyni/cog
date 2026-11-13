# mux

mux keeps a game's true state apart from what is shown of it: the model jumps to each new state at once, and timed overrides make what is drawn catch up. This spec is the destination of the map [mux: a plain-data model that jumps, and a view that catches up through timed overrides](https://github.com/dvoyni/cog/issues/773); every decision in it is recorded, with its reasons and the shapes rejected, in the resolution of the ticket it names. Its language is [mux/CONTEXT.md](../../CONTEXT.md).

## Problem Statement

An ECS is too much for many games. A turn-based or strategy game has a small, discrete state that changes when a player or the AI acts, not every frame, yet what is drawn has to move smoothly between those states: a unit walks from one tile to the next over a second, a health bar drains, a dead unit fades out.

Written by hand, that smoothing ends up inside the game state: positions halfway between tiles, fade timers, "is animating" flags. The state then stops being the truth of the game. The AI cannot be handed it mid-animation, a save taken during a walk records a unit between tiles, and a message sent over the network carries presentation the other side does not need. Each animation is also its own code, so sequencing a walk, an attack and a health drop becomes a tangle of timers, and none of it is checked for allocation on the frame's hot path.

## Solution

A game keeps its state as a **model**: a plain Go struct of data, no logic, no presentation. An **update** changes the model at once and schedules **overrides** on a **timeline**: timed replacements of a property's **current value**. A click moving a unit from A to B over a second sets its position in the model to B and schedules a **tween** of that position from A to B, starting now, lasting one second. A **view** draws from current values: per property, the overrides active at the timeline's current moment, blended by **weight**, or the model's own value when none is.

The model is the true state at every moment, so it can go to the AI, a save or the network while animations play, and the next update simply adds more overrides. A property is named by a **path**, a string compiled once at registration (`"Units[].Position"`), with a **hole** for each index or key on the way; a path with its holes filled is an **address**. Removal and appearance need no special machinery: a dead unit stays in the model, marked dead by the game, while its fade plays, and a **callback** at the fade's end purges it.

mux is a kernel plugin with a registry per kernel. Each registered model is two resources, the model and its timeline, so the AI reads the model alongside the views drawing it. The game steps each timeline once per rendered frame, and nothing on a frame's path allocates.

## User Stories

### Models and paths

1. As a game developer, I want my game state to be a plain Go struct with no mux types in it, so that it stays mine and reads like any other data.
2. As a game developer, I want to register a model with the kernel once, so that its updates, views and AI reach it as kernel resources.
3. As a game developer, I want several models in one kernel, such as the menu's and the game's, so that each part of the game animates on its own.
4. As a game developer, I want to name a property with a path string such as `"Units[].Position"`, so that I write no accessor per property.
5. As a game developer, I want a path checked when it is registered, so that a typo or a wrong type fails at startup, not in the middle of a game.
6. As a game developer, I want paths through nested structs, arrays, slices and maps in any order, so that my model can have the shape the game needs.
7. As a game developer, I want a literal index in a path (`"Players[0].Gold"`), so that a fixed element needs no hole.
8. As a game developer, I want a path through a map with string keys, or any comparable key, with nothing registered for the map, so that I can key units by name or by my own id type and write only the path.
9. As a game developer, I want a path rejected at registration when it crosses a pointer or an interface, so that my model stays plain data.
10. As a game developer, I want two paths of one model that overlap rejected at registration, so that two overrides never fight over one field.
11. As a game developer, I want to choose how deep a model's holes can nest by declaring a depth type of any length, so that a deep model is possible and a shallow one pays nothing for depth it does not use.
12. As a game developer, I want a wrong number of holes in a call to panic with the path and the count, so that the mistake is plain.
13. As a game developer, I want to read the model's own value through a path, with an answer of false when a hole misses, so that a view can tell a removed element from a present one.

### Scheduling

14. As a game developer, I want an update to change the model and schedule overrides in one place, so that the true state and its presentation never disagree about what happened.
15. As a game developer, I want to schedule a tween from one value to another over a length, with an easing, starting at a moment I choose, so that a property moves as I intend.
16. As a game developer, I want to schedule a tween to start in the future, so that a whole turn's animation is laid out up front.
17. As a game developer, I want to schedule a hold, keeping a property at one value over a span, so that the old health stays shown until the hit lands.
18. As a game developer, I want a hold to work on any type, including a `bool` or a string, so that I can keep a unit visible or a label unchanged without an interpolator.
19. As a game developer, I want a sequence builder that places overrides one after another, alongside each other, and after a wait, so that "walk, then attack while turning, then drain health" reads as it plays.
20. As a game developer, I want to ask when an address's last override ends, so that another update can queue after it.
21. As a game developer, I want to cancel every override on an address, so that a plan that changed stops showing.
22. As a game developer, I want the type of the value checked at compile time when I schedule on a path, so that a `float32` tween on a `Vec2` property does not build.
23. As a game developer, I want to schedule on map elements by interning their keys in the timeline, so that a hole is always an integer and costs nothing to compare.

### Easing and interpolation

24. As a game developer, I want the standard easing curves built in, numbered, with Linear as the zero value, so that most tweens need nothing registered.
25. As a game developer, I want to register my own easing under a number of my choosing, so that the order of registration does not matter and a saved timeline's easings keep their meaning.
26. As a game developer, I want to register one interpolator per value type that eases a value component by component, so that a `Vec2` or a colour tweens with any easing.
27. As a game developer, I want floats and integers to tween with no registration, so that the common case is free.
28. As a game developer, I want a property that needs different interpolation, such as an angle taking the shortest arc, to get it from its own named type, so that the rule lives with the type.

### Blending

29. As a game developer, I want overrides that overlap on one address to blend instead of one cutting the other off, so that a quick second move does not make a unit jump.
30. As a game developer, I want a lone override shown exactly as it is, so that blending never dilutes the common case.
31. As a game developer, I want an override to leave the timeline without a visible jump, so that the end of an animation is invisible.
32. As a game developer, I want a type with no interpolator to show the override of the highest weight, so that a `bool` or an enum still behaves sensibly when overrides overlap.

### Reading current values

33. As a game developer, I want to ask a property's current value by path and holes, so that a view draws what the player should see.
34. As a game developer, I want to walk every element of a collection with its current value in one loop, so that drawing every unit is one `range`.
35. As a game developer, I want walking a slice to cost about as much as a plain loop, so that drawing thousands of units stays cheap.
36. As a game developer, I want every property of one frame to be current at the same instant, so that two views never show a unit at two different moments.
37. As a game developer, I want a property with no overrides on its path to cost only the model read, so that quiet parts of the model cost nothing extra.
38. As a game developer, I want a view to look up a map element's interned key without being able to intern, so that a view never writes the timeline.

### Removal and appearance

39. As a game developer, I want a removed unit to stay in my model, marked dead, while its fade plays, so that the view keeps drawing it and the model still tells the truth: it is dead.
40. As a game developer, I want to schedule a callback at a moment of the timeline, so that the dead unit is purged from the model exactly when its fade ends.
41. As a game developer, I want a callback given the model and the timeline's writing side, so that it can change the model and schedule more overrides, as an update does.
42. As a game developer, I want a callback to capture whatever it needs, so that it can also play a sound or start an effect.
43. As a game developer, I want every callback called exactly once, in time order, even when a skip or a fast-forward jumps past it, so that a skipped death still purges its unit.
44. As a game developer, I want to cancel a callback still to come, so that a plan that changed does not fire it.
45. As a game developer, I want a new unit to fade in or arrive from off screen with ordinary tweens, and to stay hidden until its entrance by a hold, so that appearing needs nothing special.

### Time

46. As a game developer, I want each timeline to keep its own time, advanced once per frame by the frame's delta, so that animations follow the frames drawn.
47. As a game developer, I want to pause and resume a timeline, so that the game's animations stop while the menu's keep playing.
48. As a game developer, I want to set a timeline's speed, so that the AI's turns can play four times faster.
49. As a game developer, I want to skip a timeline to the end of its last override, so that "skip animations" finishes everything at once.
50. As a game developer, I want an update to read the timeline's current time, so that "now" is the same moment the next frame samples.
51. As a game developer, I want a timeline's time to keep running while the GPU device recovers from a loss, so that animations do not stall behind a driver problem.
52. As a game developer, I want a timeline's time to depend only on the deltas it is stepped by, so that a test or a replay is deterministic.

### The kernel

53. As a game developer, I want the AI to read the model on another job while the views draw, so that planning does not stall rendering.
54. As a game developer, I want reaching the model or the timeline to cost one pointer copy however large they are, so that a big model costs nothing per job.
55. As a game developer, I want the timeline's read handle to offer only reads, and its writes reached only through a write handle, so that a view cannot schedule by mistake.
56. As a game developer, I want to step a model's timeline from my handler of the render beat, ordered before gfx's draw, so that every view in every plugin comes after the step without waiting on one another.
57. As a game developer, I want mux to know nothing of rendering or of which track I step on, so that I decide how my game is paced.
58. As a game developer, I want a view on top of ecs to take the model and the timeline beside ecs's own parameters, so that mux and ecs work together without either changing.
59. As a plugin author, I want mux's registry to be per kernel, so that two kernels never share easings, interpolators or keys.
60. As a plugin author, I want registration after the kernel's `Init`, or before mux's plugin, to panic, so that a composition mistake is caught at spawn.

### Plain data

61. As a game developer, I want the model to stay plain data I can copy, save or send at any moment, so that the AI, saves and the network see the true state mid-animation.
62. As a game developer, I want the timeline's overrides to be plain data too, with easings and paths saved by name, so that a save can carry the animations still playing.
63. As a game developer, I want to know that pending callbacks are not saved, so that I do not rely on them across a load.

### Performance

64. As a game developer, I want nothing on a frame's path to allocate once mux's stores have grown, so that animations never feed the garbage collector.
65. As a game developer, I want scheduling to allocate only while a store grows, and a callback only its own closure, so that a turn's updates are cheap too.
66. As a maintainer, I want the zero-allocation claim enforced by tests in plain `go test`, so that a change that breaks it fails at once.
67. As a maintainer, I want per-element benchmarks against hand-written equivalents, with a baseline for `benchstat`, so that a slowdown shows before it ships.

## Implementation Decisions

### The module

- mux is its own Go module (Go 1.27) in the workspace, depending on the kernel alone: not on metronome, gfx or ecs. Its context is already in the context map.
- It has the shared Go rules and the project's plugin rules: `NewPlugin()` with no parameters, a README with a **Kernel surface** section, `resources.go` and the rest.
- It depends on three parts of the runtime outside Go's compatibility promise:
  - four map accesses, each reached through `//go:linkname` and declared `//go:noescape`: `runtime.mapaccess2_fast32`, `runtime.mapaccess2_fast64` and `runtime.mapaccess2_faststr`, which a typed lookup takes for a map whose keys compare as 4 or 8 bytes of plain memory or as a string and whose elements are 128 bytes or less, and `reflect.mapaccess` for every other map. The runtime keeps each for outside packages and pledges its signature unchanged: "Do not remove or change the type signature. See [go.dev/issue/67401](https://go.dev/issue/67401)." It kept them through the map rewrite of Go 1.24;
  - a `reflect.Type`'s type descriptor, read out of its interface;
  - the layout of a `reflect.MapIter`, which `All` holds on the stack to walk a map: the runtime's iterator follows the map's `Value` in it, and the iterator's first two fields point at the entry's key and element, as the compiler's own range over a map reads them. The public routes do not do: `MapIter.Key` and `Value`, and `SetIterKey` and `SetIterValue`, copy every entry, and the iterator the runtime keeps for outside packages, `reflect.mapiterinit`, allocates on every start.

  A test checks every map step against a typed lookup over the same map, and `All` over maps of every kind of key against their entries, so a change in any of them fails a test.

### The plugin and its registry

([mux: mux on the kernel](https://github.com/dvoyni/cog/issues/780))

- `mux.NewPlugin()` installs a private registry per kernel and is listed before the plugins that use it. It takes no `Config`: it has nothing to configure.
- A later plugin's `Init` registers through the configurator, as ecs's `RegisterWorld` reaches its registry:
  - `RegisterEasing(configurator, easing, function)`
  - `RegisterInterpolator[T](configurator, function)`
  - `RegisterModel[M, D](configurator)`
  - `MakePath[M, T](configurator, path)`
- Registering before mux's plugin, after `Init`, twice, or invalidly is a composition error and panics.
- **Kernel surface**: the resources it installs are the private registry, and per registered model the model `*M`, its `Timeline[M, D]` and its `Tasks[M, D]`. It publishes and subscribes to no events, so it has no `Handlers`, and it declares and binds no sockets.

### Models

([mux: mux on the kernel](https://github.com/dvoyni/cog/issues/780), [mux: how a task reaches a model and its timeline](https://github.com/dvoyni/cog/issues/784))

- `RegisterModel[M, D]` installs three resources: the model as `*M`, its `Timeline[M, D]`, and `Tasks[M, D]{Step}`.
- The model is installed by pointer, after `*platform.WindowManager`, so `Get()` copies one word whatever it holds. The read lock on its fields is a convention, as it is for anything in a slice either way.
- There is one timeline per model. Several models are several registrations of distinct types.
- The depth `D` is any `[N]int64`, declared by the game (`type GameDepth [12]int64`). It is constrained only to `comparable`, and registration checks through reflection that it is an `int64` array and records N.

### Paths

([mux: how an override addresses a property](https://github.com/dvoyni/cog/issues/776))

- A path is a string compiled by reflection once, at registration, into steps. Reflection runs nowhere else.
- Each `[]` is a hole, and reflection tells the step kinds apart:
  - a **field** adds its offset;
  - an **array** checks the index against its length and adds index times element size;
  - a **slice** reads its header, checks the index against its length, and jumps into its backing array;
  - a **map** looks the key up through the runtime's own map access, the one the compiler picks for a typed lookup of the same map, which answers a pointer into the map's slot or nil, and walks the remaining steps from that pointer, copying only the property out. No map type is registered: reflection finds each map along a path, with its key and element types.

  Steps mix in any order. A literal index compiles into the path and leaves no hole. A map's literal index is its key, so only a map of integer keys takes one: an interned index lives one run.
- Steps keep offsets, sizes, and the runtime's type descriptor and key table of each map they cross, never a pointer into the model, and every read starts from the model's base. A reallocated slice, a replaced map or a copy of the model therefore reads correctly.
- Registration rejects:
  - a pointer or an interface anywhere along a path;
  - two paths of one model where one is a prefix of the other;
  - a path with more holes than the depth.
- A path is a typed value, `Path[M, T]`, holding a pointer to its compiled steps. They are immutable once the kernel starts, so a path may live in a plugin's field or a package variable.
- `path.Get(model *M, holes ...int64) (T, bool)` answers the model's own value. It answers `false` when a hole misses: an index past a slice's end, or a key absent from a map.
- Paths are read-only. Nothing writes the model through a path; only updates and callbacks change the model.

### Holes, addresses and keys

([mux: how an override addresses a property](https://github.com/dvoyni/cog/issues/776), [mux: mux on the kernel](https://github.com/dvoyni/cog/issues/780))

- Holes are `int64`s passed variadically. The variadic slice stays on the stack. Their count is checked at run time, and a mismatch panics with the path and the count.
- **A slice hole is a stable slot.** A model does not move elements while they are overridden: it uses a free list or tombstones, not compaction. Array indices are stable anyway, and collections that churn are maps by id.
- An integer map key goes in as it is. Any other comparable key is interned in the model's timeline:
  - `WriteTimeline.Intern(k)` gives it the timeline's next index; only timeline writers call it.
  - `Timeline.Index(k) (int64, bool)` only looks a key up, so views call it under the read lock. A key never interned cannot have an override.
  - `path.Get` reads the interned keys holding only the model's read lock, as the AI does, so `Intern` is called holding the model's write lock too, as updates and callbacks do.
- Key tables are per timeline, per key type: a forward map, and a back slice that a map step reads to turn an index back into its key. They never shrink.
- Compiling a path makes, through reflection, the tables for the key type of each map it crosses whose keys are not integers; `Intern` and `Index` read them back typed. A key type no path's map uses panics in `Intern` and `Index`, as `Intern("bob")` does when the map is `map[UnitName]Unit`.
- An address is `{path id, holes D}` inside the timeline: comparable, a Go map key, no allocation. User code holds paths and hole values, never addresses.
- Path ids and interned indices live only within one run. Saving writes path strings and keys.

### Easings and interpolators

([mux: how an override addresses a property](https://github.com/dvoyni/cog/issues/776), [mux: timeline semantics](https://github.com/dvoyni/cog/issues/778))

- `type Easing uint16` names a function `func(a, b, t float32) float32` in an array.
  - `RegisterEasing` takes both the number and the function, so order does not matter, and a number registered twice panics.
  - Numbers 0 to 255 are reserved for mux, which registers the 31 Penner curves: Linear, then Quad, Cubic, Quart, Quint, Sine, Expo, Circ, Back, Elastic and Bounce, each In, Out and InOut. `Easing(0)` is Linear. A game registers from 256.
- An interpolator belongs to a value type: `RegisterInterpolator[T](configurator, func(a, b T, t float32, ease func(a, b, t float32) float32) T)` eases two values component by component.
  - Float and integer kinds are built in.
  - A property needing other behaviour gets a named type of its own.
  - A tween stores only its easing's number, so the timeline stays plain data.

### The timeline's two sides

([mux: how a task reaches a model and its timeline](https://github.com/dvoyni/cog/issues/784))

- `Timeline[M, D]`, the resource, is a struct holding one private pointer, with reads only: `Current`, `All`, `Index`, `Now`. `Get()` copies the pointer, and nothing can be asserted into writing.
- `ElevateTimeline(kernel.Write[Timeline[M, D]]) WriteTimeline[M, D]` gives the writing side, which is no resource:
  - scheduling: `Tween`, `Hold`, `Call`, `MakeSequence`, `End`;
  - removal: `Intern`, `Cancel`, `CancelCall`;
  - time: `Pause`, `Resume`, `SetSpeed`, `Skip`, and `Now`.

  It sits in `resources.go` between the two types.
- The reading side is a struct, not the interface that the potential rule on read-only resources asks for, because Go 1.27 interfaces cannot declare generic methods and `Current`, `All` and `Index` are generic.

### Scheduling

([mux: timeline semantics](https://github.com/dvoyni/cog/issues/778), [mux: showing what the model no longer holds](https://github.com/dvoyni/cog/issues/779))

- Every override stores an absolute start and a length, in `time.Duration` on the timeline's own clock. A length must be positive, and a zero length panics. A start may lie in the past, so the override begins partway through. Nothing links overrides to each other.
- Positional arguments throughout, the value type checked at compile time through `Path[M, T]` (Go 1.27 generic methods):
  - `Tween(path, start, from, to, length, easing, holes...)`: both ends are explicit.
  - `Hold(path, start, value, length, holes...)`: a tween whose two ends are one value, with Linear easing. It calls no interpolator to make its value, so it works on any type.

  These are the only kinds of override.
- `MakeSequence(start)` answers a `Sequence[M, D]`, a builder held on the stack, storing nothing of its own: what it places are ordinary overrides and callbacks, scheduled as it places them. Its methods take the same arguments without the start, which it fills in:
  - `Tween`, `Hold` and `Call` start after everything placed so far, and `Call` answers the callback's index;
  - `With` starts a tween alongside what was placed last, or at the sequence's start when nothing was;
  - `Wait(length)` adds a gap, and a negative length panics. A `With` after it still starts alongside what was placed last.

  What is placed next starts when everything placed so far has ended, the waits after it included. A callback has no length, so a tween placed with `With` after it starts with it.
- `End(path, holes...)` answers when an address is next free: the end of its last override, or `Now` when none is left or the last has ended. `MakeSequence(End(path, holes...))` therefore queues after whatever plays and never starts in the past.
- `Cancel(path, holes...)` removes every override on an address, running or yet to start. From the next `Step` on, the current value is the model's, which may jump: `Current` reads only the sample, as it does for an override just scheduled.

### Blending and leaving

([mux: timeline semantics](https://github.com/dvoyni/cog/issues/778))

- Every active override on an address has the weight `w = 1 − |2p − 1|`, where `p` is its progress through its span.
- The current value is their normalized weighted average, folded through the type's interpolator with Linear easing. Starting from the first override's value, each next one gives `W += wₖ` and then `current = interpolator(current, vₖ, wₖ/W, Linear)`. A lone override normalizes to full weight.
- When every weight is 0, the latest start wins. A type with no interpolator takes the override of the highest weight, ties going to the latest start. Of overrides that start together, the one scheduled last wins.
- An override leaves at the first sample at or past its end, where its weight is 0.

### Callbacks

([mux: showing what the model no longer holds](https://github.com/dvoyni/cog/issues/779), [mux: how a task reaches a model and its timeline](https://github.com/dvoyni/cog/issues/784))

- `Call(start, func(model *M, timeline WriteTimeline[M, D])) int` stores the closure in the timeline's cache and returns its index. The timeline stores only `{start, index}`.
- The closure allocates once, when it is scheduled. It may capture anything; what it reaches beyond its arguments is the game's.
- `CancelCall(index)` drops a callback still to come. A callback leaves the cache when it is called, and its index is reused.
  - Once its callback is called or cancelled, an index is free, so `CancelCall` there drops nothing, unless the next `Call` has answered it for another callback, which it then drops. A game cancels only a callback it knows is still to come; `Call`'s and `CancelCall`'s docs say so.
  - An index `Call` never answered panics, as does a nil function given to `Call`.
- Each callback is called exactly once, in time order, with ties in the order scheduled. That includes callbacks a skip or fast-forward passes; one scheduled in the past is called at the next step.
- A callback leaves the schedule before it is called. One it schedules at or before `Now` is called in the same `Step`, in its turn, so a skip calls every callback it passes, those scheduled along the way included. A callback that keeps scheduling another at `Now` therefore never lets `Step` end, which `Call`'s doc says.
- Callbacks do not survive saving. A loaded timeline has none pending.

### Removal and appearance

([mux: showing what the model no longer holds](https://github.com/dvoyni/cog/issues/779))

- A removed element stays in the model, marked dead by the game's own flag, while its fade plays as ordinary tweens. A callback at the fade's end purges it: a slice frees the slot into a free list, and a map deletes the key.
- An element that appears needs nothing new: entrance tweens run on its fields, and a hold on a field covers any wait before its entrance.

### Reading current values

([mux: how a view reads current values](https://github.com/dvoyni/cog/issues/777))

- `Timeline.Current(path, model *M, holes...) (T, bool)` answers the blend of the address's overrides in the sample, found by a binary search over its holes, or `path.Get`'s value when none is active. A count of active overrides per path skips the lookup on a path with none.
- `Timeline.All(path, model *M, fixedHoles...) iter.Seq2[int64, T]` walks the path's last hole, with the holes before it passed fixed:
  - over a slice or array, in index order, merged with the sample's sorted overrides;
  - over a map, in Go's map order, with a lookup per entry, yielding interned indices, or the keys themselves when they are integers. An element whose key was never interned is skipped: no path reaches it, since a key's hole is its index. An update interns each key when it adds the element, and a game interns every key after loading, since indices live one run.
  - Each element yields its current value as `Current` answers it. One with none, where an index after the last hole misses and nothing is sampled there, is skipped.
- `All` stays a one-line wrapper over a walk that is never inlined, so the iterator never escapes and allocates nothing, also on `break`. A view ranges over it directly.

### Stepping, sampling and time

([mux: how a view reads current values](https://github.com/dvoyni/cog/issues/777), [mux: mux on the kernel](https://github.com/dvoyni/cog/issues/780), [mux: how a task reaches a model and its timeline](https://github.com/dvoyni/cog/issues/784))

- `Tasks[M, D]{Step}` is read with `configurator.Resource[Tasks[M, D]]()`. `Step` takes the beat's delta as its argument and holds the write locks on `*M` and the timeline. In one job it:
  1. advances the timeline's time by delta times speed (nothing while paused);
  2. evaluates every active override into the sample, kept per path and sorted by holes;
  3. retires ended overrides;
  4. calls the callbacks that came due, in time order.
- A path's overrides are kept sorted by holes, comparing the first hole before the second, and at one address in the order scheduled ([mux: the sample sorted by holes, so many addresses on one path stay cheap to step and read](https://github.com/dvoyni/cog/issues/816)):
  - `Tween` and `Hold` append, and mark the path when the new override's address sorts before the last one's;
  - `Step` first retires a marked path's ended overrides, then stable-sorts the rest in place, before it samples them;
  - so the sample comes out sorted in one pass, an address's overrides lie side by side for blending, and `Cancel` keeps the order as it removes.
- mux subscribes to nothing. The game steps on `gfx.RenderEvent`, in a handler ordered before gfx's `Draw` with `OrderSubscriptions`, returning `ex.Join(in, step.Handle())`. gfx schedules its acquire after `in`, and `DrawEvent.Ready` is that acquire, so every `DrawEvent` handler's views, in any plugin, come after `Step`.
- `RenderEvent` beats on while gfx recovers a lost device, so time goes on.
- A timeline's time is its own, moved only by `Step` and `Skip`. It depends on nothing but the deltas and the time controls applied, so it is deterministic under any clock, and every frame's sample matches the beat it draws.
- Time controls:
  - `Now()` is on both sides;
  - `Pause()`, `Resume()` and `SetSpeed(f)` are per timeline. A paused `Step` leaves `Now` where it is but still samples, so an override scheduled or cancelled while paused shows from the next `Step`, and a callback scheduled at or before `Now` is called there. A paused timeline keeps the speed set for its resume. A speed of 0 stops time as a pause does; a negative, infinite or NaN speed panics.
  - `Skip()` moves `Now` to the end of the timeline's last override to end, or leaves it when none ends later. `WriteTimeline` holds no model, so the next `Step` does the rest: it samples there, finishing every override, and calls every callback `Skip` passed, in order, those they schedule along the way included. A callback after the last end stays to come, and overrides a callback schedules from the new `Now` play on.

### Locks

([mux: mux on the kernel](https://github.com/dvoyni/cog/issues/780))

| Job | Locks |
|---|---|
| update | write `*M`, write `Timeline` |
| AI | read `*M` |
| `Step` | write `*M`, write `Timeline` |
| view | read `*M`, read `Timeline` |

- An AI reads the model alongside the views drawing it.
- A view on top of ecs mixes `kernel.Read` with ecs's own parameters in one lock set, and no ecs lock conflicts with mux's ([mux research: a view on top of ecs](https://github.com/dvoyni/cog/issues/782)).

## Testing Decisions

([mux: the zero-allocation bar](https://github.com/dvoyni/cog/issues/783))

- **One seam: a kernel spawned through the `Supervisor`**, as kernel tests reach a kernel the way plugins do.
  - The kernel gets mux's plugin and a test plugin. In its `Init`, the test plugin registers a model, its paths, an easing and an interpolator.
  - Tests drive updates, `Step` with chosen deltas passed as its argument, and views.
  - Tests assert current values, the model's state after callbacks, the order and count of callback calls, and the panics of composition errors.
  - Nothing internal is called to stand in for a lifecycle step.
- **No metronome or gfx in the tests.** A timeline's time moves only by `Step`'s deltas, so every test is deterministic. Stepping on `gfx.RenderEvent` is the game's wiring: documented, not tested here.
- A good test checks behaviour through that seam. Examples:
  - a tween's current value at chosen moments;
  - two overlapping tweens blending continuously across the second's start;
  - an override leaving without a jump;
  - a skipped callback still called once, in order;
  - a missed hole answering `false`;
  - a path registered through a pointer panicking;
  - a map step reading through the access a typed lookup of its map takes, and answering as that lookup does, through each access: for string keys, 4- and 8-byte integer and struct keys, padded struct keys, and keys or elements too large for a fast access, present, absent, deleted and in a nil map.
- **The zero-allocation gate**: `testing.AllocsPerRun(...) == 0`, in plain `go test`, once each store has grown to working size.
  - Per frame: `Step` (advancing, sampling and blending, retiring, calling due callbacks whose own bodies allocate nothing); `Current` (an active override, a path with overrides but not this address, a path with none); `All` over a slice and over a map; `Index` (hit and miss); `Get` through a slice step and a map step.
  - Steady state: 0 for `Tween`, `Hold`, building a `Sequence`, `Cancel`, `CancelCall`, and `Intern` of a known key. `Intern` of a new key allocates only while its tables grow. `Call` allocates exactly its closure's own allocation.
- **Benchmarks** time each per element, against a hand-written equivalent where one exists (a direct field read, a plain loop). The cases:
  - counts of 16, 256 and 4096 overrides and elements, with the view case at 4096 units, a quarter of them overridden;
  - depths `[1]`, `[4]` and `[12]int64`;
  - 1, 2 and 4 overlapping overrides per address.

  The numbers go in the design's *Performance* section. Ten runs are kept as a baseline, and a change is checked with `benchstat`. There is no numeric time bar.
- **Prior art**:
  - ecs's tests, which spawn through the supervisor, mix `kernel.Read` with ecs parameters and assert zero allocations;
  - ecs's benchmark baseline and its *Performance* section;
  - the measurements of [mux research: what binding a property costs in Go](https://github.com/dvoyni/cog/issues/775).

## Out of Scope

- Continuous real-time simulation, client prediction and rollback: mux targets turn-based and strategy games.
- Saving and networking themselves: mux only keeps the model and the timeline plain data they can use.
- A bridge that writes ecs components: working on top of ecs is a requirement, not a component of this effort.
- Copying a model, for an AI or anything else: mux knows only the models registered with it.
- Ghosts (an element kept visible by mux after the model deleted it): the model keeps the dead element instead.
- Kinds of override other than the tween and its hold, built in or of a game's own.
- A cursor or applied copy of the model for views; crossfade or additive blending.

## Further Notes

- **Measurements behind the decisions**:
  - an unsafe offset path reads in 2.7 to 3.5 ns, against 0.6 ns direct;
  - a Go map lookup by address costs 13 to 20 ns, against 2 to 5 ns for the merge walk;
  - a map step costs 8 to 11 ns through a typed lookup, and 9 to 12 ns through the runtime's map access found by reflection, with no allocation; `reflect.Value.MapIndex` costs 47 ns and two allocations, which rules it out;
  - variadic holes do not allocate, and neither does an `iter.Seq2` returned by a one-line wrapper.

  ([findings](https://github.com/dvoyni/cog/blob/research/mux-binding-cost/mux/docs/research/binding-cost.md); the iterator and variadic shapes were measured while resolving the reading and addressing tickets, and the runtime's map access while breaking this spec into tickets.)
- **Prior art for the timeline**: [findings](https://github.com/dvoyni/cog/blob/research/mux-prior-art/mux/docs/research/prior-art.md) from Godot, DOTween, elm-animator and the Go tween libraries. None measures zero allocation.
- **The blend is the simplest for now.** While a long override is mid-flight, a short newer one is diluted: a 0.4 s dodge over a 2 s walk shows about halfway. Crossfades and additive offsets were weighed and set aside.
- **The model's read lock is a convention.** A view holding `kernel.Read[*M]` could write through the pointer, as it could through any slice of a copy. The timeline's read side, by contrast, cannot write.
- **Writing ecs components from current values** marks every row written as changed, which a world that observes changes pays for ([findings](https://github.com/dvoyni/cog/blob/research/mux-on-ecs/mux/docs/research/view-on-ecs.md)).
- **A deep depth slows every lookup**, since a Go map hashes the whole address.
