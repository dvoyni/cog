# Mux

Mux keeps a game's true state apart from what is shown of it: the model jumps to each new state at once, and timed overrides make what is shown catch up. It knows nothing of rendering, the ECS or networking. This glossary records its domain language; metronome's terms (track, beat, clock) are in [metronome/CONTEXT.md](../metronome/CONTEXT.md).

## Language

**Model**:
A game's true state as plain data, with no logic and no presentation state in it. An application may hold several at once, such as the menu's and the game's.
_Avoid_: State, world, store

**Path**:
The route through a model's type to one property, with a hole for each index or key on the way.
_Avoid_: Selector, binding, field path

**Hole**:
One place in a path filled by an index or a key.
_Avoid_: Wildcard, parameter, slot

**Address**:
A path with every hole filled, naming one property of one model.
_Avoid_: Key, handle, target

**Depth**:
How many holes an address of a model can hold, declared by the application with the model's timeline.
_Avoid_: Capacity, hole count, nesting

**Override**:
A timed replacement of one property's current value, over a span that may start in the future.
_Avoid_: Animation, effect

**Tween**:
An override that interpolates between two values over its span.
_Avoid_: Tweener, lerp, animation

**Hold**:
A tween whose two ends are one value, keeping a property at it over its span.
_Avoid_: Freeze, pin, constant

**Weight**:
An override's share of a property's current value while it overlaps others: none at its start and end, full at its midpoint.
_Avoid_: Influence, alpha, priority

**Sequence**:
Overrides an update places one after another or alongside each other, their starts worked out as they are placed.
_Avoid_: Chain, group, clip

**Easing**:
A registered curve, named by its number, giving the value between two numbers at a point of a span.
_Avoid_: Curve, ease function, tween function

**Interpolator**:
A value type's way of easing two of its values, component by component.
_Avoid_: Lerp, blender, mixer

**Callback**:
A function a timeline calls at a moment of its own, given the model to change.
_Avoid_: Cue, event, trigger

**Timeline**:
The overrides scheduled over a model, past, current and future.
_Avoid_: Track, schedule

**Current value**:
What one property of a model looks like at its timeline's current moment: the overrides active then, blended by weight, or the model's own value when none is.
_Avoid_: Shown value, view value, display value

**Sample**:
A timeline's active overrides evaluated at one moment, taken once per frame; current values are read from it.
_Avoid_: Snapshot, frame, pose, tick

**Update**:
What changes a model and schedules the overrides that show the change.
_Avoid_: Step, tick, reducer

**View**:
What draws a frame from current values.
_Avoid_: Renderer, presenter
