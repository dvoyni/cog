# mux

`github.com/dvoyni/cog/mux` keeps a game's true state apart from what is
shown of it: the model jumps to each new state at once, and timed overrides
make what is drawn catch up. It is its own Go module (Go 1.27) and depends on
nothing but the standard library and the [kernel](../kernel).

## Shape

- **`Plugin`**, from `NewPlugin()`, is mux. It is built empty: the plugins
  that use a model register it and its paths in their `Init`.
- A **model** is a plain Go struct of the game's, with no mux types in it,
  registered with `RegisterModel[M, D](configurator)`. `D` is its depth, an
  `[N]int64` the game declares: `type GameDepth [12]int64`. A kernel holds
  any number of models, such as the menu's and the game's.
- A **path**, `MakePath[M, T](configurator, "Squads[].Members[2].Position")`,
  names one property through the model's struct fields, arrays and slices,
  in any order. Each `[]` is a **hole**, which every read and override fills
  with an `int64`; a literal index leaves none. A path is compiled once, by
  reflection, and `path.Get(model, holes...)` reads the model's own value
  through it, allocating nothing, or answers `false` for an index outside its
  array or slice. A path holds no more holes than the model's depth.
- A path keeps no pointer into the model, so it reads a slice the model
  moved to a new array, and a copy of the model. A hole picks a slice's element by
  its index, so the game moves no element of a slice while an override
  addresses it: it frees elements into a free list, or marks them dead,
  rather than closing the gaps.
- **`Timeline[M, D]`** is the reading side of the model's timeline, and
  **`WriteTimeline[M, D]`**, from `ElevateTimeline`, its writing side. Both
  answer `Now`, the timeline's own time.
- An update changes the model and schedules a **tween** on the writing side,
  `Tween(path, start, from, to, length, easing, holes...)`, at an absolute
  start on the timeline's clock. A view reads `Current(path, model, holes...)`
  on the reading side: the value of the tween active at that address, or the
  model's own when none is. Each address of a path is tweened on its own.
- An **interpolator** eases two values of a type, component by component.
  Every float and integer kind has one built in, named types of those kinds
  included. A game registers one for its own value types, such as a vector,
  with `RegisterInterpolator[T](configurator, function)`; for a named type,
  such as an angle taking the shortest arc, it replaces the built-in one for
  that type alone.
- An **easing** is a curve named by its number, an `Easing`, so a tween
  stores only the number. mux registers `EasingLinear`, the zero `Easing`,
  and Robert Penner's curves, from `EasingQuadIn` to `EasingBounceInOut`,
  under numbers below 256. A game registers its own from 256, in any order,
  with `RegisterEasing(configurator, easing, function)`.
- **`Tasks[M, D]`** holds `Step`, which advances the timeline's time by the
  delta it is given, then samples every active override at the new time for
  `Current` to read.

## Kernel surface

- **Resources it installs:** a private registry, holding the kernel's
  easings and interpolators, and for each model a later plugin registers
  with `RegisterModel[M, D]`:
  - `*M`, the model: updates and `Step` write it, views and the AI read it;
  - `Timeline[M, D]`, its timeline: views read it, and updates reach its
    writing side with `ElevateTimeline` under its write lock;
  - `Tasks[M, D]`, read with `configurator.Resource[mux.Tasks[M, D]]()`,
    holding `Step`, which holds the write locks on `*M` and
    `Timeline[M, D]`. mux schedules it nowhere: the game schedules it once
    per rendered frame, with the frame's delta, before its views.
- **Resources it reads from other plugins:** none.
- **Events it publishes:** none.
- **Events it subscribes to:** none, so it has no `Handlers`.
- **Sockets it declares:** none.
- **Sockets it binds:** none.

A plugin that registers a model is listed after mux.

See [docs/specs/mux.md](docs/specs/mux.md) for the design mux is built to,
and [CONTEXT.md](CONTEXT.md) for its language.
