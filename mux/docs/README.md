# mux guide

How to use `github.com/dvoyni/cog/mux`, with examples: a game wired end to
end, from the model it registers to the views that draw it. For the reasons
behind each choice, see [the design](specs/mux.md); its language is
[CONTEXT.md](../CONTEXT.md). It assumes the
[kernel guide](../../kernel/docs/README.md), and the
[gfx guide](../../gfx/docs/README.md) for the render beat.

- [Concepts](#concepts)
- [Registering a model](#registering-a-model)
- [Stepping on the render beat](#stepping-on-the-render-beat)
- [Updates](#updates)
- [Views and the AI](#views-and-the-ai)
- [Removal and appearance](#removal-and-appearance)
- [Keys](#keys)
- [Time](#time)
- [Performance](#performance)

## Concepts

```
Kernel
  ├─ mux plugin ── the registry RegisterModel finds
  └─ model Game
       ├─ *Game                       ── a resource: the true state
       ├─ Timeline[Game, GameDepth]   ── a resource: overrides, sample, time
       └─ Tasks[Game, GameDepth]      ── Step, which the game schedules
```

| Term | What it is |
|---|---|
| model | A plain struct of the game's data, with no mux types in it. Updates change it at once. |
| path | `MakePath[M, T](c, "Units[].Position")`: one property, compiled once. Each `[]` is a hole. |
| override | A **tween** from one value to another, or a **hold** of one value, over a span of the timeline's time. |
| current value | What a view draws: the blend of the overrides active at an address, or the model's own value. |
| `Step` | Advances the timeline's time, samples every active override, and calls the callbacks that came due. |
| callback | A function the timeline calls once at a moment of its own, given the model and the writing side. |

An **update** changes the model to the new state and schedules overrides
from the state shown to it. A **view** reads current values. The model is
the true state throughout, so the AI, a save or the network reads it while
the animations play.

## Registering a model

The game declares its model, a depth for its holes, and registers both from
the `Init` of a plugin listed after mux. A path is compiled once there, and
may live in the plugin's fields:

```go
// Game is the model: plain data, no logic, no presentation.
type Game struct {
	Units []Unit
	// FreeUnits holds the slots of Units purged, which a new unit takes, so
	// no unit moves while it is overridden.
	FreeUnits []int64
	// Squads is keyed by name, which the timeline interns.
	Squads map[SquadName]Squad
}

type Unit struct {
	Position Vec2
	Opacity  float32
	Dead     bool
}

type SquadName string

type Squad struct{ Morale float32 }

type Vec2 struct{ X, Y float32 }

// GameDepth holds a path's holes: the game's deepest path has four.
type GameDepth [4]int64

type gamePlugin struct {
	unitPosition mux.Path[Game, Vec2]
	unitOpacity  mux.Path[Game, float32]
	squadMorale  mux.Path[Game, float32]
	moveUnit     *kernel.Task[MoveOrder, kernel.NoResult]
	killUnit     *kernel.Task[KillOrder, kernel.NoResult]
	drawUnits    *kernel.Task[gfx.DrawEvent, kernel.NoResult]
	planTurn     *kernel.Task[kernel.NoArg, kernel.NoResult]
}

func (p *gamePlugin) Init(c *kernel.Configurator, config any) error {
	mux.RegisterModel[Game, GameDepth](c)
	mux.RegisterInterpolator(c, func(a, b Vec2, t float32, ease func(a, b, t float32) float32) Vec2 {
		return Vec2{X: ease(a.X, b.X, t), Y: ease(a.Y, b.Y, t)}
	})
	p.unitPosition = mux.MakePath[Game, Vec2](c, "Units[].Position")
	p.unitOpacity = mux.MakePath[Game, float32](c, "Units[].Opacity")
	p.squadMorale = mux.MakePath[Game, float32](c, "Squads[].Morale")
	p.moveUnit = c.RegisterTask2(p.move)
	p.killUnit = c.RegisterTask2(p.kill)
	p.drawUnits = c.RegisterTask2(p.draw)
	p.planTurn = c.RegisterTask1(p.plan)
	p.subscribeStep(c)
	return nil
}
```

A path that crosses a pointer or an interface, overlaps another path, has
more holes than the depth, or names a field the model lacks panics here, at
spawn. Floats and integers tween with nothing registered; `Vec2` needs its
interpolator.

## Stepping on the render beat

mux subscribes to nothing: the game steps each timeline once per rendered
frame, by the beat's delta. Its handler of `gfx.RenderEvent` is ordered
before gfx's `Draw` and returns `ex.Join(in, step.Handle())`. gfx schedules
its acquire after that, and `DrawEvent.Ready` is the acquire, so every view
recording on `DrawEvent`, in any plugin, reads the sample `Step` took:

```go
func (p *gamePlugin) subscribeStep(c *kernel.Configurator) {
	tasks, _ := c.Resource[mux.Tasks[Game, GameDepth]]()
	stepGame := tasks.Step
	handlers, _ := c.Resource[gfx.Handlers]()
	stepping := c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, beat gfx.RenderEvent) (gfx.RenderEvent, kernel.JobHandle) {
		step := ex.Schedule(stepGame, beat.Delta)
		return beat, ex.Join(in, step.Handle())
	})
	c.OrderSubscriptions(stepping, handlers.Draw)
}
```

`Step` holds the write locks on `*Game` and the timeline. `RenderEvent`
beats on while gfx recovers a lost device, so the timeline's time goes on.

## Updates

An update holds the write locks on the model and the timeline. It changes
the model to the new state at once, and tweens the property it changed
from the value shown to the new one. Here a click moves a unit over a
second:

```go
// MoveOrder is an event: pointer-free, as the kernel's events are.
type MoveOrder struct {
	Unit int64
	To   Vec2
}

func (p *gamePlugin) move(k *kernel.Kernel, order MoveOrder, game kernel.Write[*Game], timeline kernel.Write[mux.Timeline[Game, GameDepth]]) kernel.NoResult {
	unit := &game.Get().Units[order.Unit]
	writeTimeline := mux.ElevateTimeline(timeline)
	// The walk starts where the unit is shown, which may be mid-walk, and
	// queues after whatever plays on its position.
	from, _ := timeline.Get().Current(p.unitPosition, game.Get(), order.Unit)
	start := writeTimeline.End(p.unitPosition, order.Unit)
	unit.Position = order.To
	writeTimeline.Tween(p.unitPosition, start, from, order.To, time.Second, mux.EasingQuadInOut, order.Unit)
	return kernel.NoResult{}
}
```

- **The model holds the destination** as soon as the update ends: the AI
  reads it there, mid-walk.
- **Starts are absolute**, on the timeline's own clock: `Now()` for at once,
  `End(path, holes...)` to queue after what plays at an address, or any
  moment to come, so a whole turn is laid out up front.
- **A tween over a running one blends** with it by weight, so a quick second
  order takes over with no jump.
- **A hold**, `Hold(path, start, value, length, holes...)`, keeps a value
  over a span, of any type, such as the old health until the hit lands.
- **A sequence**, `MakeSequence(start)`, works out the starts of what it
  places: `Tween`, `Hold` and `Call` after everything placed so far, `With`
  alongside what was placed last, `Wait` a gap.
- **`Cancel(path, holes...)`** removes every override at an address, and the
  model's own value shows from the next `Step`. It finds them, as `End`
  does, by binary search, so it costs the log of the overrides on the path.
  The overrides scheduled since the last `Step` are searched too while they
  were scheduled in hole order, as an update moving its units in index
  order schedules them; once one was scheduled out of order, they are
  walked until the next `Step`.

## Views and the AI

A view holds the read locks on the model and the timeline, so it cannot
schedule. It reads current values: `Current(path, model, holes...)` for one
address, or `All(path, model, fixedHoles...)` to walk every element of a
path's last hole at once. Ranging over `All` allocates nothing:

```go
func (p *gamePlugin) draw(k *kernel.Kernel, e gfx.DrawEvent, game kernel.Read[*Game], timeline kernel.Read[mux.Timeline[Game, GameDepth]]) kernel.NoResult {
	view := timeline.Get()
	model := game.Get()
	for unit, position := range view.All(p.unitPosition, model) {
		opacity, _ := view.Current(p.unitOpacity, model, unit)
		drawUnit(e, position, opacity)
	}
	return kernel.NoResult{}
}
```

It records on `DrawEvent`, after `e.Ready`, and is joined into the out
dependency, as every recording task is:

```go
c.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, e gfx.DrawEvent) (gfx.DrawEvent, kernel.JobHandle) {
	drawn := ex.Schedule(p.drawUnits, e, e.Ready)
	return e, ex.Join(in, drawn.Handle())
})
```

The AI reads the model alone, the true state, never a current value:

```go
func (p *gamePlugin) plan(k *kernel.Kernel, _ kernel.NoArg, game kernel.Read[*Game]) kernel.NoResult {
	for _, unit := range game.Get().Units {
		if !unit.Dead {
			// ... plan from unit.Position, where the unit is headed.
		}
	}
	return kernel.NoResult{}
}
```

Their lock sets:

| Job | Locks |
|---|---|
| update | write `*Game`, write `Timeline` |
| `Step` | write `*Game`, write `Timeline` |
| view | read `*Game`, read `Timeline` |
| AI | read `*Game` |

The AI's read lock shares with the view's, so planning runs alongside the
drawing. It conflicts only with updates and `Step`.

- **One frame, one instant.** Every view reads the sample `Step` took, so no
  two views show a unit at two moments.
- **A quiet path costs the model read.** `Current` on a path with nothing
  sampled reads the model alone.
- **`Get(model, holes...)`** reads the model's own value through a path, and
  answers `false` for a missed hole: a slice index past its end, or a map
  key absent.
- **The model's read lock is a convention.** A view holding `*Game` could
  write through it; the timeline's read side cannot write.

## Removal and appearance

A unit that dies stays in the model, marked dead by the game's own flag,
while its fade plays. A callback at the fade's end purges it, freeing its
slot. A slot is never closed up: holes are stable slots, and the overrides
on other units keep their addresses.

```go
type KillOrder struct{ Unit int64 }

func (p *gamePlugin) kill(k *kernel.Kernel, order KillOrder, game kernel.Write[*Game], timeline kernel.Write[mux.Timeline[Game, GameDepth]]) kernel.NoResult {
	game.Get().Units[order.Unit].Dead = true
	writeTimeline := mux.ElevateTimeline(timeline)
	fadeStart := writeTimeline.End(p.unitOpacity, order.Unit)
	writeTimeline.Tween(p.unitOpacity, fadeStart, 1, 0, time.Second, mux.EasingLinear, order.Unit)
	slot := order.Unit
	writeTimeline.Call(fadeStart+time.Second, func(game *Game, _ mux.WriteTimeline[Game, GameDepth]) {
		game.Units[slot] = Unit{}
		game.FreeUnits = append(game.FreeUnits, slot)
	})
	return kernel.NoResult{}
}
```

- **The model tells the truth at once:** the unit is dead, and the AI skips
  it. Only the view goes on drawing it, fading.
- **Each callback is called exactly once, in time order**, even when a skip
  or a fast-forward passes it, so a skipped death still purges its unit.
- **`Call` allocates its closure**, once; `CancelCall(index)` drops a
  callback still to come, by the index `Call` answered. Both cost in
  proportion to the callbacks pending: little with tens, more with
  thousands.
- **Callbacks are not saved.** A timeline loaded has none pending.

A unit that appears needs nothing new. An update takes a free slot, or
appends one, and tweens its entrance; a hold keeps it hidden until then:

```go
slot := int64(len(model.Units))
if last := len(model.FreeUnits) - 1; last >= 0 {
	slot = model.FreeUnits[last]
	model.FreeUnits = model.FreeUnits[:last]
	model.Units[slot] = Unit{Position: at, Opacity: 1}
} else {
	model.Units = append(model.Units, Unit{Position: at, Opacity: 1})
}
writeTimeline.Hold(p.unitOpacity, writeTimeline.Now(), 0, time.Second, slot)
writeTimeline.Tween(p.unitOpacity, writeTimeline.Now()+time.Second, 0, 1, time.Second, mux.EasingLinear, slot)
```

## Keys

A hole is an `int64`. A map keyed by an integer type takes its keys as
holes. Any other key is **interned**: the timeline gives it an index, which
is its hole.

- **An update interns a key when it adds the element**, holding the write
  locks on the model and the timeline:

  ```go
  game.Get().Squads[name] = Squad{Morale: 1}
  mux.ElevateTimeline(timeline).Intern(name)
  ```

- **A view looks an index up** with `Index`, which writes nothing, so it
  holds the read lock:

  ```go
  if squad, found := timeline.Get().Index(name); found {
  	morale, _ := timeline.Get().Current(p.squadMorale, game.Get(), squad)
  	drawMorale(e, name, morale)
  }
  ```

- **`All` over a map yields interned indices** and skips a key never
  interned, as no hole reaches its element.
- **Indices live one run, so a game interns every key after loading.** A
  save holds the model's keys, not their indices:

  ```go
  func (p *gamePlugin) load(k *kernel.Kernel, _ kernel.NoArg, game kernel.Write[*Game], timeline kernel.Write[mux.Timeline[Game, GameDepth]]) kernel.NoResult {
  	*game.Get() = readSave()
  	writeTimeline := mux.ElevateTimeline(timeline)
  	for name := range game.Get().Squads {
  		writeTimeline.Intern(name)
  	}
  	return kernel.NoResult{}
  }
  ```

A key interned once keeps its index for the run: tables never shrink, and
interning a known key allocates nothing.

## Time

A timeline's time is its own: only `Step`, by its delta times the speed,
and `Skip` move it, so a test or a replay steps it deterministically.

```go
writeTimeline.Pause()      // the game's animations stop behind the menu
writeTimeline.Resume()     // and go on
writeTimeline.SetSpeed(4)  // the AI's turn plays four times faster
writeTimeline.Skip()       // "skip animations": the next Step finishes all
```

Each model's timeline has its own controls, so the menu's plays while the
game's is paused. `Now()` is on both sides: an update starts at it, and the
next frame samples there.

## Performance

Nothing on a frame's path allocates once mux's stores have grown: `Step`,
`Current`, `All`, `Index` and `Get`, and in steady state `Tween`, `Hold`,
a sequence, `Cancel`, `CancelCall` and `Intern` of a known key. `Call`
allocates only its closure. Tests hold every one of them to it in plain
`go test`. The costs per element, against hand-written loops, are in the
design's [Performance](specs/mux.md#performance) section.
