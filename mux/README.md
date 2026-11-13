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
- A **path**, `MakePath[M, T](configurator, "Hero.Position")`, names one
  property through the model's struct fields. It is compiled once, by
  reflection, and `path.Get(model)` reads the model's own value through it,
  allocating nothing.
- **`Timeline[M, D]`** is the reading side of the model's timeline, and
  **`WriteTimeline[M, D]`**, from `ElevateTimeline`, its writing side. Both
  answer `Now`, the timeline's own time.
- An update changes the model and schedules a **tween** on the writing side,
  `Tween(path, start, from, to, length, easing)`, at an absolute start on the
  timeline's clock. A view reads `Current(path, model)` on the reading side:
  the active tween's value, or the model's own when none is. Only `float32`
  properties tween yet, and only by `EasingLinear`.
- **`Tasks[M, D]`** holds `Step`, which advances the timeline's time by the
  delta it is given, then samples every active override at the new time for
  `Current` to read.

## Kernel surface

- **Resources it installs:** a private registry, and for each model a later
  plugin registers with `RegisterModel[M, D]`:
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
