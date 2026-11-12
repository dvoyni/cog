# Prior art: overrides over a model

Research for [#774](https://github.com/dvoyni/cog/issues/774), on the map [#773](https://github.com/dvoyni/cog/issues/773). It asks how existing tween and animation systems bind a tween to a property, and what they do about sequencing, collisions, removal and allocation. It records what they do and decides nothing for mux. Terms are from [mux/CONTEXT.md](../../CONTEXT.md).

Sources were read on 2026-11-12, with source links pinned to these commits: Godot `0694588`, DOTween `bfb02b0`, elm-animator `12ca2ee` (tag 2.0.0), mgold/elm-animation `cb278e8`, elm-community/easing-functions `d320147`, tanema/gween `e874ee3`, setanarut/tween `f0e495d`, gopackage/tween `b2cb17e`.

## Summary

| System | Target named by | Sequence as data? | Second tween on the same property | Target gone mid-tween | Allocation |
|---|---|---|---|---|---|
| Godot `Tween` | object ID + `NodePath` string (`"position:x"`) | no: steps are lists of ref-counted tweener objects | no detection; both write each frame, so the last one created wins | object ID is looked up each step; a missing one finishes its tweener quietly; `bind_node` kills the tween | heap objects per tween and per tweener; `Variant` math each frame |
| DOTween | getter/setter closures (`DOTween.To`) or shortcuts on the object | no: nested tween objects at time positions | no detection; they fight unless you use "Blendable" tweens or kill by target/id | Safe Mode catches the exception from the getter/setter and kills the tween; `SetLink` kills it with its GameObject | pooled tween objects (default capacity 200 + 50); closures per tween |
| elm-animator | none: a `Timeline state` *replaces* a model field | yes: `List (Step state)` of waits and transitions to states | one timeline per field; `interrupt` replaces the schedule, `queue` appends; motion keeps velocity | not applicable: the timeline is a value in the model | immutable records, rebuilt on each update (GC'd) |
| mgold/elm-animation | none: an `Animation` value the program samples | no chaining; `delay` only | `retarget` keeps velocity and duration | not applicable | immutable record per change |
| tanema/gween | none: `Update` returns a `float32` the caller writes | `Sequence` of `*Tween`, run one after another | not applicable: no registry | not applicable | one `*Tween` per `New`; `Sequence.Update` may `append` |
| setanarut/tween | none: the result goes in a `Value` field | `Sequence` of `*Tween`; tweens are JSON-serialisable, easing by name | not applicable | not applicable | one `*Tween` per `NewTween`; nothing per frame |
| gopackage/tween | an `Updater` interface | none | not applicable | not applicable | one goroutine, two channels and a ticker per tween |

## Godot `Tween` (4.x)

- **Target.** `tween_property(object, property: NodePath, final_val, duration)`. The path is parsed into subnames once. The start value comes from `get_indexed`, and each step writes `target_instance->set_indexed(property, tween->interpolate_variant(...))`. The target is stored as an `ObjectID`, not a pointer. Components are reachable as `"position:x"`. ([tween.cpp#L100](https://github.com/godotengine/godot/blob/069458825a8e4a01938a176372c0709ab6fd9dc3/scene/animation/tween.cpp#L100), [#L640](https://github.com/godotengine/godot/blob/069458825a8e4a01938a176372c0709ab6fd9dc3/scene/animation/tween.cpp#L640), [Tween docs](https://docs.godotengine.org/en/stable/classes/class_tween.html))
- **Sequence.** A tween is `LocalVector<List<Ref<Tweener>>> tweeners`: a list of steps, each a list of tweeners run in parallel. `append` adds to the current step when `parallel()` or `set_parallel(true)` is in force, and opens a new step otherwise (`chain()`). `step()` runs the tweeners of the current step, emits `step_finished` when none is left active, then moves on, repeating for `set_loops`. Tweeners are ref-counted objects (property, interval, callback, method, subtween), not data, and "Tweens are not designed to be reused and trying to do so results in an undefined behavior". ([tween.h#L116](https://github.com/godotengine/godot/blob/069458825a8e4a01938a176372c0709ab6fd9dc3/scene/animation/tween.h#L116), [tween.cpp#L182](https://github.com/godotengine/godot/blob/069458825a8e4a01938a176372c0709ab6fd9dc3/scene/animation/tween.cpp#L182), [#L335](https://github.com/godotengine/godot/blob/069458825a8e4a01938a176372c0709ab6fd9dc3/scene/animation/tween.cpp#L335), [docs](https://docs.godotengine.org/en/stable/classes/class_tween.html))
- **Collision.** No detection. The docs say: "You should avoid using more than one Tween per object's property. If two or more tweens animate one property at the same time, the last one created will take priority and assign the final value." ([docs](https://docs.godotengine.org/en/stable/classes/class_tween.html))
- **Target gone.** Each `PropertyTweener::step` resolves `ObjectDB::get_instance(target)`. When that fails, it calls `_finish()` and returns, and the tween carries on with its other steps. A ref-counted target is kept alive through `ref_copy`. With `bind_node(node)`, the tween pauses while the node is outside the tree and "will be automatically killed when the bound object is freed". ([tween.cpp#L640](https://github.com/godotengine/godot/blob/069458825a8e4a01938a176372c0709ab6fd9dc3/scene/animation/tween.cpp#L640), [#L238](https://github.com/godotengine/godot/blob/069458825a8e4a01938a176372c0709ab6fd9dc3/scene/animation/tween.cpp#L238), [docs](https://docs.godotengine.org/en/stable/classes/class_tween.html))
- **Time.** `pause`/`play`, `set_speed_scale`, `set_ignore_time_scale`, an idle or physics process mode, and `custom_step(delta)` to advance by hand, for example past the end to finish at once. ([docs](https://docs.godotengine.org/en/stable/classes/class_tween.html))
- **Allocation.** Each `create_tween` makes a `RefCounted` tween and puts it in `SceneTree`'s `List<Ref<Tween>> tweens`, a linked list with one node per tween. Each `tween_*` call allocates a tweener. Each frame, `process_tweens` walks that list ([scene_tree.cpp#L826](https://github.com/godotengine/godot/blob/069458825a8e4a01938a176372c0709ab6fd9dc3/scene/main/scene_tree.cpp#L826)) and interpolates `Variant`s. `Variant` keeps values of up to four `real_t` inline, and points to storage for `Transform2D`, `AABB` and larger ([variant.h#L259](https://github.com/godotengine/godot/blob/069458825a8e4a01938a176372c0709ab6fd9dc3/core/variant/variant.h#L259)).
- **Beside it.** Godot's `AnimationPlayer` plays an `Animation` resource, whose tracks address properties by `NodePath` and hold timed keys as data (the docs call animations "just data containers"). That is the plain-data counterpart of `Tween`, which is built in code. ([Animation docs](https://docs.godotengine.org/en/stable/classes/class_animation.html))

## DOTween (Unity, C#)

- **Target.** The generic form takes two closures: `DOTween.To(()=> myFloat, x=> myFloat = x, 52, 1)`. Shortcuts such as `transform.DOMove(...)` "automatically set the object itself as the tween target", which is then a filter key for `Kill`, `Pause` and the rest. `SetId` gives an int or string key, which is faster to filter by. The site claims it "doesn't use any strings nor Reflection at all". ([documentation](https://dotween.demigiant.com/documentation.php), [home](https://dotween.demigiant.com/))
- **Sequence.** `Sequence` has `Append`, `Join`, `Insert(atPosition, tween)`, `Prepend`, `AppendInterval` and `AppendCallback`. Internally it is a `List<ABSSequentiable>` of tweens and callbacks, each with a `sequencedPosition`, stably sorted by position. To go to a position it walks the items whose span covers it. Sequences nest to any depth, and a nested tween "can be nested only inside a single other Sequence" and can no longer be controlled on its own. Steps are live tween objects with closures, not data. ([Sequence.cs#L23](https://github.com/Demigiant/dotween/blob/bfb02b05551a45204352dc43bdc15b882147891a/_DOTween.Assembly/DOTween/Sequence.cs#L23), [documentation](https://dotween.demigiant.com/documentation.php))
- **Collision.** Nothing kills an earlier tween automatically. The convention is to kill by target or id before starting a new tween. The "Blendable" shortcuts are the explicit alternative: `DOBlendableMoveBy` tweens relatively "in a way that allows other DOBlendableMove tweens to work together on the same target, instead than fight each other as multiple DOMove would do". ([DOTween.XML doc comment](https://github.com/Demigiant/dotween/blob/bfb02b05551a45204352dc43bdc15b882147891a/UnityExamples.Unity5/Assets/Plugins/Demigiant/DOTween/DOTween.XML#L2136))
- **Target gone.** With `useSafeMode`, `ApplyTween` wraps the plugin's `EvaluateAndApply(getter, setter, ...)` in `try/catch`. On an exception ("Target/field doesn't exist anymore: kill tween") it logs, counts the case in `safeModeReport`, and returns true, which kills the tween. Startup failures are caught the same way. Without Safe Mode you must kill tweens yourself before destroying their targets. `SetLink(gameObject)` kills a tween when its GameObject is destroyed. ([TweenerCore.cs#L255](https://github.com/Demigiant/dotween/blob/bfb02b05551a45204352dc43bdc15b882147891a/_DOTween.Assembly/DOTween/Core/TweenerCore.cs#L255), [Tweener.cs#L131](https://github.com/Demigiant/dotween/blob/bfb02b05551a45204352dc43bdc15b882147891a/_DOTween.Assembly/DOTween/Tweener.cs#L131), [documentation](https://dotween.demigiant.com/documentation.php))
- **Time.** There is a per-tween `timeScale` and a global `DOTween.timeScale`. `SetUpdate` chooses Normal, Late, Fixed or Manual updates, the last driven by `DOTween.ManualUpdate`. `Goto(position)`, `Complete`, `Rewind` and `SmoothRewind` move a tween in time. ([documentation](https://dotween.demigiant.com/documentation.php))
- **Allocation.** `TweenManager` keeps fixed arrays: `_activeTweens` (tweeners + sequences) and `_pooledTweeners`. `GetTweener` takes a pooled one or makes a new one. Capacity defaults to "200 Tweeners and 50 Sequences" and grows with `Array.Resize` when exceeded, or is preset with `SetTweensCapacity`. Recycling (`recycleAllByDefault`, `SetRecyclable`) is opt-in, with the warning that a held reference may then point at a different, live tween. The getter/setter lambdas are allocated by each call site. The docs mark several list-returning queries as allocating. ([TweenManager.cs#L36](https://github.com/Demigiant/dotween/blob/bfb02b05551a45204352dc43bdc15b882147891a/_DOTween.Assembly/DOTween/Core/TweenManager.cs#L36), [#L72](https://github.com/Demigiant/dotween/blob/bfb02b05551a45204352dc43bdc15b882147891a/_DOTween.Assembly/DOTween/Core/TweenManager.cs#L72), [documentation](https://dotween.demigiant.com/documentation.php))

## Elm: elm-animator, elm-animation, easing-functions

### mdgriffith/elm-animator (2.0.0, the current successor)

- **Target.** There is none. A field's type changes from `Bool` to `Timeline Bool`: "You could replace that with a `Timeline.Timeline Bool`". The timeline holds *states*, not interpolated values. A value comes out at view time through a mapping function: `Animator.Value.float : Timeline state -> (state -> Movement) -> Float`, likewise `color`, `xy` and `xyz`. So interpolation belongs to the reader, not the stored schedule. ([Timeline.elm](https://github.com/mdgriffith/elm-animator/blob/12ca2ee7eba6af39bf8a4b75a6845db2f5d6ec70/src/Animator/Timeline.elm), [Value.elm](https://github.com/mdgriffith/elm-animator/blob/12ca2ee7eba6af39bf8a4b75a6845db2f5d6ec70/src/Animator/Value.elm))
- **Model and shown value.** `current` returns the new state as soon as it is scheduled, `arrived` returns it once the transition has finished, and `previous`, `upcoming` and `progress` answer the rest. This is the same split as mux's model value and shown value, but it lives inside one field. ([Timeline.elm](https://github.com/mdgriffith/elm-animator/blob/12ca2ee7eba6af39bf8a4b75a6845db2f5d6ec70/src/Animator/Timeline.elm))
- **Sequence.** `interrupt [ wait (ms 300), transitionTo (ms 1000) Griffyndor, transitionTo (ms 500) Slytherin ]`. `Step state` is a union of `Wait Duration` and `TransitionTo Duration state`, so it is plain data. Both `queue` and `interrupt` are recorded as pending and folded into the schedule on the next `update now`. ([Timeline.elm#L327](https://github.com/mdgriffith/elm-animator/blob/12ca2ee7eba6af39bf8a4b75a6845db2f5d6ec70/src/Animator/Timeline.elm#L327))
- **Collision.** Each field has one timeline, so a collision is a scheduling choice. `queue` means "Wait until the current timeline is **finished** and then continue with these new steps". `interrupt` replaces the schedule: "If several interruptions are requested before the next `update`, the latest nonempty schedule wins. An initial `wait` lets existing motion continue until the replacement begins." The renderer handles "interruptions from their sampled positions", and a timeline is advised "when spring interruptions need to preserve incoming velocity". ([Timeline.elm#L345](https://github.com/mdgriffith/elm-animator/blob/12ca2ee7eba6af39bf8a4b75a6845db2f5d6ec70/src/Animator/Timeline.elm#L345), [Animator.elm](https://github.com/mdgriffith/elm-animator/blob/12ca2ee7eba6af39bf8a4b75a6845db2f5d6ec70/src/Animator.elm))
- **Removal of the past.** `update` runs `clean`, which garbage-collects history (`collectHistory`) but keeps a lookback window, `maxDelay = 5 s`, because "View functions may request any delay up to this duration after `update` has already run". `TimelineDetails` holds `initial` ("the state preceding retained history"), `now`, `delay`, `scale`, `events`, `queued`, `interruption` and `running`. ([InternalAnim/Timeline.elm#L74](https://github.com/mdgriffith/elm-animator/blob/12ca2ee7eba6af39bf8a4b75a6845db2f5d6ec70/src/InternalAnim/Timeline.elm#L74), [#L209](https://github.com/mdgriffith/elm-animator/blob/12ca2ee7eba6af39bf8a4b75a6845db2f5d6ec70/src/InternalAnim/Timeline.elm#L209))
- **Target gone.** Not applicable: the timeline is part of the model and goes with it.
- **Time.** The caller passes `now` to `update`. `scale` scales durations, `delay` delays events, and `isRunning` controls the frame subscription.
- **Allocation.** Elm values are immutable, so every `update` builds new records and lists, and the JS garbage collector reclaims them. The library makes no per-frame allocation bound.

### mgold/elm-animation

- **Target.** There is none. `Animation` is "an opaque type that represents a time-varying number". `animate clock anim` samples it, and "You are responsible for maintaining the clock". Its record holds `start`, `delay_`, duration or speed, `ramp`, `ease_`, `from_` and `to_`. ([Animation.elm](https://github.com/mgold/elm-animation/blob/cb278e818777ff19714aaa57d97f4f121acba815/src/Animation.elm))
- **Sequence.** There is no chaining. `delay` is the only offset.
- **Collision.** `retarget` handles it. A running animation keeps its current velocity as `ramp` and its average speed, so the turn is smooth. A scheduled one only changes its target. A finished one starts from its old target. To retarget x and y together, the docs advise syncing durations, for example to `timeRemaining`. ([Animation.elm](https://github.com/mgold/elm-animation/blob/cb278e818777ff19714aaa57d97f4f121acba815/src/Animation.elm))
- **Easing** is a function value, `Float -> Float`, so an `Animation` is not serialisable.

### elm-community/easing-functions

`type alias Easing = Float -> Float`, with combinators `reverse`, `flip`, `inOut` and `bezier`. Easing is a function value, not an enum. ([Ease.elm](https://github.com/elm-community/easing-functions/blob/d320147d67703bd1bd579defb002e41454be670e/src/Ease.elm))

## Go libraries

No tween library made for Ebitengine turned up; these are the general Go ones a search finds, and Ebitengine games can use any of them since they are plain Go.

### tanema/gween (port of kikito's tween.lua)

- **Target.** There is none. `Tween{duration, time, begin, end, change float32; Overflow float32; easing ease.TweenFunc; reverse bool}`. `Update(dt) (current float32, isFinished bool)` returns the value, and the caller writes it where it belongs. `Set(time)` scrubs to an absolute time. ([gween.go](https://github.com/tanema/gween/blob/e874ee3ae01a9c2d061620ec7e9217c9dd535243/gween.go))
- **Easing** is `type TweenFunc func(t, b, c, d float32) float32`, a func value, so a `Tween` is not serialisable. ([ease/easing_functions.go#L15](https://github.com/tanema/gween/blob/e874ee3ae01a9c2d061620ec7e9217c9dd535243/ease/easing_functions.go#L15))
- **Sequence.** `Sequence{Tweens []*Tween; index; yoyo; reverse; loop; loopRemaining}` runs one tween at a time and carries the overflow into the next. It cannot run tweens in parallel. ([sequence.go](https://github.com/tanema/gween/blob/e874ee3ae01a9c2d061620ec7e9217c9dd535243/sequence.go))
- **Collision and target gone.** Not applicable: there is no registry and no target.
- **Allocation.** `New` returns a `*Tween`. `Tween.Update` allocates nothing. `Sequence.Update` declares `var completed []int` and `append`s to it as tweens finish within a call, so a frame that crosses a step boundary allocates. ([sequence.go#L44](https://github.com/tanema/gween/blob/e874ee3ae01a9c2d061620ec7e9217c9dd535243/sequence.go#L44))

### setanarut/tween (a gween derivative, `time.Duration` based)

- `Tween` has exported fields `Value, Delay, Begin, End, Duration, Time, Overflow, Reversed, Yoyo`, `EasingFunc TweenFunc \`json:"-"\`` and `EaseName string`. `NewTween` looks the name up in `EaseMap map[string]TweenFunc`. `UnmarshalJSON` restores the func from `EaseName` and the delay state from `Time`. This is the one surveyed library whose tween is meant to round-trip as data, with easing named rather than stored as a func. ([tween.go#L16](https://github.com/setanarut/tween/blob/f0e495d66500672fba895ee5453489a9d472ea70/tween.go#L16), [#L59](https://github.com/setanarut/tween/blob/f0e495d66500672fba895ee5453489a9d472ea70/tween.go#L59))
- `Sequence{Tweens []*Tween; Yoyo; IsReversed; Index; Loop; LoopRemaining; Value; ...}` is sequential only. Its only `append` is in `Add`, so `Update` does not allocate. It has no target and no collision handling. ([sequence.go](https://github.com/setanarut/tween/blob/f0e495d66500672fba895ee5453489a9d472ea70/sequence.go))

### gopackage/tween

`Engine{Duration, Framerate, Transition TransitionFunc, Updater Updater}`. `Start` launches a goroutine with a `time.Ticker` and `make(chan int)`, and calls `Updater.Update(Frame)` on its own clock. It has no sequences. Driving itself on wall time on another goroutine does not fit a game loop's own time. ([tween.go](https://github.com/gopackage/tween/blob/b2cb17ef32de9029f35760476110ee17f233f4b3/tween.go))

## What this means for mux's open questions

These are options the survey shows, not decisions.

**Addressing a property** (target naming)

- A path string resolved once at registration, as Godot's `NodePath` is. Godot then still writes by name each frame through `set_indexed`. mux could resolve it to an offset or index at registration and keep only that in the override.
- Accessor closures, as DOTween uses. These are ruled out for the timeline by "no func values", but could live in a registered kind and be referred to by id.
- No target at all, as in gween and Elm: the override yields a value and something else decides where it goes. In elm-animator, the stored schedule holds states, and a mapping supplied at read time turns them into values.

**Sequencing**

- Steps placed at absolute positions (DOTween's `Insert(atPosition)`, sorted by `sequencedPosition`) suit "overrides that may start in the future, scheduled up front".
- Godot's step lists (each step a parallel group) and gween's strictly sequential `[]*Tween` are relative orderings, not times.
- elm-animator's `List (Step state)` of `Wait` / `TransitionTo` is the closest to plain data, together with setanarut's JSON-round-tripping tween with a named easing.

**Two overrides on one property**

- Last created wins, with no detection (Godot, DOTween by default).
- Kill the earlier one by target or id (DOTween's convention).
- Combine them additively (DOTween's Blendable).
- One schedule per property, with an explicit `queue` (after the current one ends) or `interrupt` (replace, latest wins, optionally starting from the sampled value and velocity), as elm-animator and `retarget` do.

**Target removed mid-override**

- Check an ID each step and drop the override quietly (Godot `ObjectID`).
- Catch the failure and kill it (DOTween Safe Mode).
- Bind the override's life to an owner (Godot `bind_node`, DOTween `SetLink`).
- Keep the schedule inside the model, so it goes with the property (Elm).

**Clearing the past**

- Remove an override when it completes (Godot, DOTween `autoKill`).
- Keep it until removed (DOTween `SetAutoKill(false)` for rewinds).
- Garbage-collect history but keep a lookback window for readers that sample late (elm-animator's 5 s `maxDelay`). Which of these fits depends on how far back skipping or rewinding must reach.

**Time**

- Every system offers a time scale plus a manual step (Godot `custom_step`, DOTween `ManualUpdate`/`Goto`, elm-animator `update now`, gween `Set(time)`).
- Absolute-time sampling (`Goto`, `Set`, Elm's `now`) makes pause, skip and fast-forward one question of which time is read. Delta stepping (`Update(dt)`) makes them a question of what delta is fed.

**Allocation**

- None of the surveyed systems states or measures a zero-allocation-per-frame bound.
- The patterns on offer are a fixed-capacity pool that grows when exceeded (DOTween) and per-tween heap objects (Godot, gween), plus incidental per-frame allocations to avoid (gween's `completed` slice; Godot's linked-list nodes are per tween, not per frame).
