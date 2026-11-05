# metronome

`github.com/dvoyni/cog/metronome` paces a Cog kernel over time. It is its own
Go module (Go 1.27) and depends on nothing but the standard library and
[kernel](../kernel).

## Features

- **Tracks the application declares.** A track publishes its own event type
  as beats, on its own goroutine, so a slow handler on one track delays no
  other. A server declares an update track, a client an update and a render
  track, or more. metronome knows nothing of rendering, windows or the ECS.
- **Fixed-step and variable pacing.** A fixed-step track beats on deadlines
  computed from the clock's epoch, never summed from sleeps, and catches up
  after a stall within a debt cap, reporting the time it drops. A variable
  track beats as often as its work and its settings allow, which covers
  vsync, an fps limit and uncapped.
- **Backpressure from the kernel.** A beat stays in flight until its
  publication's out dependency finishes, and a track starts a new beat only
  while fewer than its limit are in flight.
- **Interpolation.** A track can interpolate against a fixed-step track,
  each beat carrying the tick whose state it reads and how far past it falls.
- **Replaceable time.** metronome reads time only through its `Clock`. The
  default is wall time; `ManualClock` moves only when told, for tests and
  deterministic runs, and a custom clock can be driven by anything, such as
  an agent.
- **Zero allocations per beat**, which the tests assert.

## Shape

- **`Plugin`** is the kernel plugin, from `NewPlugin()`. It installs no
  resource.
- **`Config`** is its part of the kernel's config: the `Clock` and the
  `Tracks`, each made by `NewTrack[E](settings)`.
- **`Beat`** is the header every track event embeds: `Tick`, `Delta`, `Lost`,
  `TimeFromStart`, `Alpha`, `InterpolatedTick`. The rest of the event is the
  application's payload. `UpdateEvent` is the one event metronome ships.
- **`Settings`** paces a track, with the presets `FixedStep(hz)` and
  `Variable()`.

## Kernel surface

- **Resources it installs:** none; nothing reaches metronome but its
  `Config`.
- **Resources it reads from other plugins:** none.
- **Events it publishes:** each track's event, a beat at a time, on the
  track's own goroutine: the types declared in `Config.Tracks`, which
  embed `Beat`. `UpdateEvent` is the one it ships.
- **Events it subscribes to:** none, so it has no `Handlers`.
- **Sockets it declares:** none.
- **Sockets it binds:** none.

metronome is listed first, before the plugins handling its beats, so it
stops last.

See [docs/README.md](docs/README.md) for the guide with examples, and
[docs/design.md](docs/design.md) for the design and why it is this way.
