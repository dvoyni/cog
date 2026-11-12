# Mux

Mux keeps a game's true state apart from what is shown of it: the model jumps to each new state at once, and timed overrides make what is shown catch up. It knows nothing of rendering, the ECS or networking. This glossary records its domain language; metronome's terms (track, beat, clock) are in [metronome/CONTEXT.md](../metronome/CONTEXT.md).

## Language

**Model**:
A game's true state as plain data, with no logic and no presentation state in it. An application may hold several at once, such as the menu's and the game's.
_Avoid_: State, world, store

**Override**:
A timed replacement of one property's shown value, over a span that may start in the future.
_Avoid_: Animation, effect

**Tween**:
An override that interpolates between two values over its span.
_Avoid_: Tweener, lerp, animation

**Timeline**:
The overrides scheduled over a model, past, current and future.
_Avoid_: Track, sequence, schedule

**Shown value**:
What one property of a model looks like at a moment: the value of the override active then, or the model's own value when none is.
_Avoid_: View value, display value, current value

**Update**:
What changes a model and schedules the overrides that show the change.
_Avoid_: Step, tick, reducer

**View**:
What draws a frame from shown values.
_Avoid_: Renderer, presenter
