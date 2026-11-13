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
  names one property through the model's struct fields, arrays, slices and
  maps, in any order. Each `[]` is a **hole**, which every read and override
  fills with an `int64`; a literal index leaves none. A path is compiled
  once, by reflection, and `path.Get(model, holes...)` reads the model's own
  value through it, allocating nothing, or answers `false` for an index
  outside its array or slice, or a key its map lacks. A path holds no more
  holes than the model's depth.
- A path keeps no pointer into the model, so it reads a slice the model
  moved to a new array, a map the model replaced, and a copy of the model. A
  hole picks a slice's element by its index, so the game moves no element of
  a slice while an override addresses it: it frees elements into a free
  list, or marks them dead, rather than closing the gaps.
- A map may be keyed by any comparable type, with nothing registered for it.
  An integer key is its own hole. Any other key is **interned** in the
  model's timeline by an update, `writeTimeline.Intern(key)`, which answers
  its index, the hole that reaches its element; a view looks the index up
  with `timeline.Index(key)`, which writes nothing. Indices live one run, so
  a game interns every key again after loading.
- A map step reaches the runtime's own map access, `reflect.mapaccess`,
  through `//go:linkname`, and reads a map type's runtime descriptor out of
  its `reflect.Type`; `All` walks a map through a `reflect.MapIter`, reading
  each entry's key and element in place out of its layout. All three lie
  outside Go's compatibility promise. Tests hold every map step to a typed
  lookup over the same map, and `All` to each map's entries, so a change in
  any fails them.
- **`Timeline[M, D]`** is the reading side of the model's timeline, and
  **`WriteTimeline[M, D]`**, from `ElevateTimeline`, its writing side. Both
  answer `Now`, the timeline's own time.
- An update changes the model and schedules a **tween** on the writing side,
  `Tween(path, start, from, to, length, easing, holes...)`, at an absolute
  start on the timeline's clock. A view reads `Current(path, model, holes...)`
  on the reading side: the blend of the overrides active at that address, or
  the model's own when none is. Each address of a path is tweened on its own.
  `All(path, model, fixedHoles...)` walks every element of a path's last
  hole at once, the holes before it fixed, yielding each element's hole and
  current value: an array or a slice in index order, a map in Go's map
  order. Ranging over it allocates nothing.
- Overrides that overlap at one address **blend by weight**: each weighs
  nothing at its start and end and fully at its midpoint, and the current
  value is their weighted average, through the type's interpolator, so a new
  tween over a running one takes over with no jump. A type with no
  interpolator shows the override that weighs most.
- A **hold**, `Hold(path, start, value, length, holes...)`, keeps a
  property's current value at one value over its span. It calls no
  interpolator to make its value, so it holds a property of any type, such as
  a `bool` or a struct. `End(path, holes...)` answers when an address is next
  free: the end of its last override, or `Now`. An update that starts its
  overrides there queues them after whatever plays. `Cancel(path, holes...)`
  removes every override on an address, running or yet to start, and its
  current value is the model's own from the next `Step`.
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
