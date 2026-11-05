# metronome guide

How to use `github.com/dvoyni/cog/metronome`, with examples. For the reasons
behind each choice, see [design.md](design.md). It assumes the
[kernel guide](../../kernel/docs/README.md).

- [Concepts](#concepts)
- [Quick start](#quick-start)
- [Tracks](#tracks)
- [Fixed-step tracks](#fixed-step-tracks)
- [Variable tracks](#variable-tracks)
- [In flight](#in-flight)
- [Interpolation](#interpolation)
- [Clocks](#clocks)
- [Testing with a ManualClock](#testing-with-a-manualclock)
- [Lifecycle](#lifecycle)
- [Rules](#rules)

## Concepts

```
Clock ── Now, a Timer per track, the wait callback
  └─ metronome ── one goroutine per track
       ├─ update track ── UpdateEvent beats ──▶ handlers ──▶ jobs
       └─ render track ── RenderEvent beats ──▶ handlers ──▶ jobs
```

| Type | What it is |
|---|---|
| `Plugin` | The kernel plugin, from `NewPlugin()`. It installs no resource: nothing else reaches it. |
| `Config` | metronome's part of the kernel's config, under `PluginName`: its `Clock` and its `Tracks`. |
| `Track` | A declared track, from `NewTrack[E](settings)`: its event type and its pacing. |
| `Settings` | A track's pacing. Presets `FixedStep(hz)` and `Variable()`. |
| `Beat` | The header every track event embeds, stamped by metronome on each beat. |
| `UpdateEvent` | The one event metronome ships: `struct{ Beat }`, for an update track. |
| `Clock` / `Timer` | Where time comes from, and how a track waits for its next deadline. |
| `WallClock` | The default clock: monotonic time, with the epoch at `Start`. |
| `ManualClock` | A clock that moves only when told: `Advance`, `Jump`, `Wait`. |

A **beat** is one publication of a track's event. Each track runs on its own
goroutine: it waits on its timer, publishes a beat, and publishes the next
one once its pacing allows. The beat's handlers schedule jobs as for any
kernel event.

## Quick start

A game plugin advances a world on a 10 Hz update track and ends the
application after three ticks. metronome is configured, not written to: the
track is declared in its `Config`.

```go
package main

import (
	"fmt"
	"runtime"
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/metronome"
)

const gameName kernel.PluginName = "game"

// GameConfig is gamePlugin's part of the kernel's config.
type GameConfig struct {
	Supervisor *kernel.Supervisor
	Ticks      uint64
}

// World is the state the update track advances.
type World struct{ Elapsed time.Duration }

// gamePlugin advances the world on every update beat, and ends the
// application after the configured number of ticks.
type gamePlugin struct {
	config *GameConfig
	update *kernel.Task[metronome.UpdateEvent, kernel.NoResult]
}

func (p *gamePlugin) Name() kernel.PluginName { return gameName }

func (p *gamePlugin) Init(c *kernel.Configurator, config any) error {
	p.config = kernel.PluginConfigOrDefault[GameConfig](config)
	c.InitResource(World{})
	p.update = c.RegisterTask(func(access *kernel.Access) kernel.TaskBody[metronome.UpdateEvent, kernel.NoResult] {
		world := access.Write[World]()
		return func(k *kernel.Kernel, beat metronome.UpdateEvent) kernel.NoResult {
			w := world.Get()
			w.Elapsed += beat.Delta
			world.Set(w)
			k.Logger().LogDebug("update", "tick", beat.Tick, "elapsed", w.Elapsed)
			if beat.Tick == p.config.Ticks {
				p.config.Supervisor.Quit()
			}
			return kernel.NoResult{}
		}
	})
	c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, beat metronome.UpdateEvent) (metronome.UpdateEvent, kernel.JobHandle) {
		// Returning the job keeps the beat in flight until it finishes.
		return beat, ex.Schedule(p.update, beat, in).Handle()
	})
	return nil
}

func (p *gamePlugin) Start(k *kernel.Kernel) error { return nil }
func (p *gamePlugin) Stop(k *kernel.Kernel) error  { return nil }
func (p *gamePlugin) Destroy() error               { return nil }

func init() { runtime.LockOSThread() }

func main() {
	s := kernel.NewSupervisor(nil)
	config := map[kernel.PluginName]any{
		metronome.PluginName: &metronome.Config{
			Tracks: []metronome.Track{
				metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(10)),
			},
		},
		gameName: &GameConfig{Supervisor: s, Ticks: 3},
	}
	// metronome is listed first, so it stops last.
	if _, err := s.Spawn(config, runtime.GOMAXPROCS(0), metronome.NewPlugin(), &gamePlugin{}); err != nil {
		panic(err)
	}
	for s.WaitMainThread() {
		s.TickMainThread()
	}
	fmt.Println("done")
}
```

Output, one line every 100ms:

```
[kernel 1] update tick=1 elapsed=100ms
[kernel 1] update tick=2 elapsed=200ms
[kernel 1] update tick=3 elapsed=300ms
done
```

## Tracks

A track is declared by its event type, a struct that embeds `metronome.Beat`.
Whatever else it holds is the payload, zero as published, for the first
handler to fill:

```go
type RenderEvent struct {
	metronome.Beat
	Ready kernel.JobHandle // the first handler sets it
}

&metronome.Config{
	Tracks: []metronome.Track{
		metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(60)),
		metronome.NewTrack[RenderEvent](metronome.Variable()),
	},
}
```

There are no built-in tracks: a server declares only an update track, a
client an update and a render track, or more. A track event is a kernel
event, so it is pointer-free.

A track's event type is its identity. Two tracks of one event type panic at
`Init`, and a negative setting panics at `NewTrack`.

metronome stamps the header on every beat:

| Field | On a fixed-step track | On a variable track |
|---|---|---|
| `Tick` | The beat's number on its track, from 1, never reset. | Same. |
| `Delta` | The step. | Clock time since the previous beat, clamped to `MaxDelta`. |
| `Lost` | Steps dropped past the debt cap just before this beat; 0 on pace. | Time clamped away by `MaxDelta`. |
| `TimeFromStart` | The tick's deadline from the epoch. | The publish time from the epoch. |
| `Alpha`, `InterpolatedTick` | See [Interpolation](#interpolation). | Same. |

On every track `TimeFromStart` is the sum of `Delta + Lost` over the
track's beats. An event published by hand has a zero header.

## Fixed-step tracks

`FixedStep(hz)` is a track with `Step` set to `time.Second / hz` (hz zero is
60) and one beat in flight. Tick n is due at the epoch + n × `Step`, computed,
never summed from sleeps, so a fixed-step track does not drift.

When the track falls behind, by a stalled handler or a slow machine, the steps
it has passed and not published are its **debt**. It publishes owed steps
back-to-back, each with `Delta` the step, so a simulation advances in the
steps it expects. `MaxDebt` (zero is 4) caps the debt: steps owed past the cap
are dropped, get no tick, and their time is the next beat's `Lost`. A beat
with a non-zero `Lost` is where the game knows it skipped time.

```go
settings := metronome.FixedStep(30)
settings.MaxDebt = 2 // after a stall, catch up at most 2 steps
```

## Variable tracks

A track with `Step` zero is variable: each beat stands for the time since the
previous one. `Variable()` keeps one beat in flight, so a beat starts once
the previous one has finished. Two settings shape it:

- **`MinInterval`** is the least time between two beats; zero is none.
- **`MaxDelta`** clamps `Delta` after a stall; zero is no clamp. The time
  clamped away is the beat's `Lost`.

The usual render modes are combinations of these, not modes of their own:

| Mode | Settings |
|---|---|
| vsync | `MaxInFlight: 2`, no `MinInterval`. A FIFO acquire blocks: the next beat's acquire waits for the display to free an image, holding that beat in flight, and the display paces the track. |
| limit(fps) | `MinInterval: time.Second / fps`, plus a `MaxInFlight`. |
| uncapped | A `MaxInFlight` alone. |

```go
vsync := metronome.Variable()
vsync.MaxInFlight = 2
vsync.MaxDelta = 250 * time.Millisecond
```

## In flight

A beat is **in flight** until its publication's out dependency finishes: the
job handle the last handler returns. A track starts a new beat only while
fewer than `MaxInFlight` of its beats are in flight (zero is no limit). This
is how the kernel's work paces a track: a render track whose acquire blocks
holds its beat in flight, and the next waits.

So a handler returns the work a beat started in its out dependency. One that
schedules a pinned job joins it in, or the in-flight limit does not see it,
and each beat queues another pinned job behind a blocked main thread:

```go
c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, beat RenderEvent) (RenderEvent, kernel.JobHandle) {
	present := ex.Schedule(p.present, beat, in) // pinned to the main thread
	return beat, ex.Join(in, present.Handle())
})
```

## Interpolation

A track can interpolate against a fixed-step **reference track**, named by
its event type in `InterpolateAgainst`. Each beat then carries:

- **`InterpolatedTick`**, the reference track's last tick published before
  the beat. The beat's tasks read that tick's state: locks order them after
  the writes its publication scheduled.
- **`Alpha`**, in [0,1), how far past that tick's deadline the beat falls,
  in reference steps: blend the tick's predecessor into it by `Alpha`. While
  the reference track is in debt, `Alpha` is the largest `float32` below 1,
  so the beat shows the newest state.

```go
render := metronome.Variable()
render.MaxInFlight = 2
render.InterpolateAgainst = reflect.TypeFor[metronome.UpdateEvent]()

&metronome.Config{
	Tracks: []metronome.Track{
		metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(20)),
		metronome.NewTrack[RenderEvent](render),
	},
}
```

A reference that is not a declared fixed-step track panics at `Start`.

## Clocks

metronome reads time only through its `Clock`:

```go
type Clock interface {
	Now() time.Duration // since the epoch
	NewTimer() Timer    // one per track
	SetWaitCallback(wait func(ctx context.Context) error)
}

type Timer interface {
	Reset(at time.Duration) // fire once Now has reached at; the clock decides when that is
	C() <-chan struct{}
	Stop()
}
```

The **epoch** belongs to the clock: it is where `Now` reads zero, and every
track measures its deadlines from it.

- **`WallClock`** is the default when `Config.Clock` is nil, and can be made
  with `NewWallClock()`. It reads monotonic time with the epoch at the
  plugin's `Start`, backs each timer with one reused `time.Timer`, and fires
  a deadline already past at once.
- **`ManualClock`**, from `NewManualClock()`, starts at 0 and moves only when
  told. See the next section.
- **A custom clock** may fire on time, late, on an outside event such as an
  agent's MCP call, or never; the game sees its time and cannot tell.

`SetWaitCallback` is how a clock that moves time when told learns when its
work is done. metronome hands it a `wait` func at `Start`, and nil at `Stop`;
`wait(ctx)` returns once every track has **settled**, none with a beat in
flight or owing one, or with `ctx`'s error. A clock that fires a deadline for
an agent calls it before answering; `WallClock` ignores it. A clock serves
one metronome.

## Testing with a ManualClock

A `ManualClock` makes a kernel's pacing deterministic. Give it in the config
and drive it from the test:

```go
clock := metronome.NewManualClock()
config := map[kernel.PluginName]any{
	metronome.PluginName: &metronome.Config{
		Clock:  clock,
		Tracks: []metronome.Track{metronome.NewTrack[metronome.UpdateEvent](metronome.FixedStep(10))},
	},
}
// spawn metronome.NewPlugin() and the plugins under test with config

clock.Advance(300 * time.Millisecond) // ticks 1–3, each settled before the next
clock.Jump(time.Second)               // 10 steps owed: 4 published, 6 lost
if err := clock.Wait(t.Context()); err != nil {
	t.Fatal(err)
}
```

- **`Advance(d)`** walks deadline by deadline across every track, letting
  each beat due publish and settle before moving on. Work takes no manual
  time, so no debt builds and nothing drops.
- **`Jump(d)`** moves to the target at once, as a stall would, so debt,
  `MaxDebt` and `Lost` can be tested. It does not wait.
- **`Wait(ctx)`** returns once every track has settled.
- A variable track beats at most once per `Advance` or `Jump`, at the target
  time.

## Lifecycle

- **`Start`** starts the clock's epoch and one goroutine per track, and hands
  the clock its wait callback.
- **`Stop`** takes the callback back, then signals every track goroutine and
  joins them, so no beat is published after it returns. It does not wait on
  beats in flight: the kernel finishes the jobs it has.
- **List metronome first,** before the plugins handling its beats. It then
  stops last, and a few beats per track may still publish while their
  `Stop`s run, so a plugin's `Stop` only halts its own activity and releases
  surfaces, devices and windows in `Destroy`, after the kernel's last job.
- **No quit API.** Ending the application is the supervisor's: a handler
  calls `Supervisor.Quit` or `Destroy`.

## Rules

- **Tracks are config.** Every track and its settings is declared in
  `Config.Tracks`; no plugin reaches metronome to add one.
- **Return the beat's work.** A handler's out dependency holds every job the
  beat started, pinned jobs joined in with `ex.Join`, or the in-flight limit
  cannot see it.
- **Composition errors panic.** A config of another type than `Config`, two
  tracks of one event type, a negative setting, or a reference that is not a
  fixed-step track.
- **Zero allocations per beat.** Publishing a beat and waiting on it allocate
  nothing; the tests assert it.
