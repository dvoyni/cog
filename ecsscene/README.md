# ecsscene

`github.com/dvoyni/cog/ecsscene` draws the world: it records every Entity
carrying a `Transform` and a `Drawable` into `scene`'s frame-local queue, once a
tick, in one System.

**It is a binding, and a binding is necessarily a third plugin.** `ecs` imports
only `kernel`; `scene` imports nothing of `ecs`. Neither can know about the
other, so what attaches them is an ordinary plugin that imports both — and that
is what "there is no binding mechanism" means in practice. The consequence worth
stating: **a project not using the ECS schedules no ECS Systems**, because it
simply does not register this plugin.

[`../ecs/docs/specs/ecs.md`](../ecs/docs/specs/ecs.md) §Binding is the design
record. This README is the API, and **[What this plugin may not do](#what-this-plugin-may-not-do)
is the part to read before writing a second binding**: a backend adopted from
outside cog will not have been written with those rules in mind.

## Files

`contract.go` holds the package documentation, the two hash types and the three
Components; `config.go` the manifest and its `With*` setters; `resources.go` the
public `Manifest` alias; `resourcesimpl.go` the two name tables behind it and the
play scratch builder; `plugin.go` the plugin, the Query and the one recording
System.

## Dependencies

- Go packages: `app`, `ecs`, `kernel`, `m`, `scene`
- Plugin dependencies: `ecs`, `scene`
- Configuration: `ecsscene.Config` — the manifest, and the Store population hint
- Events declared or published: none

## Composing

```go
world := ecs.NewEntities(4096)

config := map[kernel.PluginName]any{
    storage.Name: storage.DefaultConfig("my-game").WithReadDiskFS("res"),
    ecsscene.Name: ecsscene.DefaultConfig().
        WithModel("crate", "models/props/crate.glb").
        WithClip("walk", "Walk").
        WithDrawables(4096),
}

kernel.New(config).WithPlugins(
    storage.New(), input.New(), gfx.New(), scene.New(), wgpu.New(),
    ecs.Plugin(world), ecsscene.New(world), game.New(world))
```

Register `ecs` and `scene` before it. The world handle is threaded through the
constructor because Component registration needs it at registration, where no
handler is running and no resource value may be read.

## Components

```go
type Transform struct {
    Position m.Vec3
    Rotation m.Quat
    Scale    float32 // zero means 1
}

type Drawable struct {
    Model  ModelHash
    Layers scene.LayerMask // zero reads as every layer
}

type Animation struct {
    Plays [MaxPlays]Play  // MaxPlays is 4, which is scene's own cap
}

type Play struct {
    Clip   ClipHash
    Time   float32
    Weight float32
    Loop   bool
}
```

The three are registered by this plugin, because a Component is registered by the
plugin that defines its Go type — which is what keeps cog's coupling check
working on Component data.

`Transform` is `scene.Transform` **without the one field that would break the
binding**: `scene.Transform.Matrix` is a `*m.Mat4`, and a matrix pointing into a
Component Store would be read after this System's locks are gone. The
prohibition is therefore structural rather than remembered, and the pointer-free
rule the ECS checks at registration would have refused the field anyway.
Non-uniform scale is not expressible for the same reason — that is scene's trade,
not the binding's.

`Animation` is **optional**, and is reached through an Accessor rather than named
in the Query: a Query matches an Entity having *at least* the Components it
names, so naming `Animation` there would drop every unanimated drawable out of
the walk. A second System for those would serialise against this one anyway.

A spawn names whichever of them it means:

```go
type Crate struct {
    Place ecsscene.Transform
    Draw  ecsscene.Drawable
}

var crate = ecs.HashOf[ecsscene.ModelHash]("crate")   // package level, at init

func spawnCrates(sp *ecs.Spawn[Crate]) {
    sp.New(Crate{Place: ecsscene.Transform{Scale: 1}, Draw: ecsscene.Drawable{Model: crate}})
}
```

## The manifest

```go
func (c Config) WithModel(name, path string) Config
func (c Config) WithClip(name, clip string) Config
func (c Config) WithDrawables(count int) Config

func (m *Manifest) Model(name ModelHash) (string, bool)
func (m *Manifest) Clip(name ClipHash) (string, bool)
func (m *Manifest) ModelText(name ModelHash) (string, bool)
func (m *Manifest) ClipText(name ClipHash) (string, bool)
```

A Component names a model by the **hash of a name**, never by the path: a path is
a string and a Component holds no pointers, and a hash is the same number in
every process and every run. `Manifest` is the reverse half, and **where it lives
is the whole point** — it belongs to the plugin that resolves names, is read once
per draw under a lock the recording System holds anyway, and has no lock of its
own. One System declares it, rather than every System that ever assigns a model
to an Entity.

It is **filled from `Config` during Registration and never again**, which is why
nothing here mutates it: a mutator would be reachable through a read handle, and
a read-locked System holding a mutator is a race the kernel cannot see. A game
that discovers its assets at runtime builds the `Config` before `kernel.New`,
which is where the plugin set is fixed anyway.

A name nobody registered resolves to nothing, draws nothing and is not reported:
an unset Component field is `ecs.NoHash`, which is the ordinary state of a
drawable being assembled. A **collision** — two names on one hash — fails
composition rather than silently drawing the wrong model.

## The recording System

```go
func(q *ecs.Query[drawQuery], animations *ecs.Get[Animation],
     names *ecs.Read[*Manifest], out *ecs.Write[*scene.OpQueue])
```

That signature is the whole binding: the Components, an Accessor for the optional
one, the manifest read, and scene's queue written. Nothing declares a lock
anywhere — the parameter types are the declaration, derived once at
registration. `RecordEventHandler` is the subscription's identity type, exported
so a System that moves drawables can order itself `Before` it.

**One recording System per bound plugin is the shape.** `*scene.OpQueue` is one
resource, so every recording System serialises against every other whatever
Components they read — the ECS's per-Store granularity buys nothing there, and
that is a property of scene's API rather than of the ECS. A second System would
cost a scheduling slot and could not run concurrently anyway.

### Ordering needs nothing new

The System declares **no** `First`, `Last`, `Before` or `After`. Scene subscribes
its flush `.Last().Before[gfx.UpdateEventHandler]()`, so anything that does not
ask to be last is in the ordinary phase and already runs before it. A draw
recorded in a tick is in the recording that same tick's flush publishes, and
`TestTheRecordingSystemNeedsNoOrderingVocabulary` asserts the edge the engine
derived without either side declaring it.

## What this plugin may not do

Stated as prohibitions, because they are what a second binding — physics, audio,
or a backend adopted from outside cog — has to keep true, and none of them has a
compiler behind it.

- **A Component holds no pointer, transitively.** Enforced at registration, where
  the type is named and nothing has been stored yet.
- **Never hand the bound plugin a pointer into a Store.**
  `scene.ModelDraw.Transform.Matrix` is a `*m.Mat4` retained by value in scene's
  record until the flush — and the flush is a *different* System, running after
  the recording System's locks are gone, so such a matrix would be read
  unlocked. Use the TRS form, or point into scratch that outlives the frame.
- **Variable-length draw data is not a Component.** Play lists, morph weights and
  override params are built in System-owned scratch and rebuilt each frame;
  scene copies each into its own arena at record time and says so, so the caller
  may reuse the backing the moment the call returns. **Scratch captured in a
  closure is safe for one System only** — two Systems sharing it have no lock
  between them. This one is safe because it has a single holder and because the
  System takes scene's queue for write, so the lock that orders the queue orders
  the scratch with it. If in doubt make it a resource, which puts it in the lock
  set.
- **Do not cache an Entity without checking liveness, and do not restructure the
  world from inside another Entity's iteration.** Recording does neither: it
  holds `*Entities` for read, which is not the authority to retire anything.

## What it costs

Measured on a real `kernel.Engine` driven by a real `app.UpdateEvent`, with
`storage`, `gfx`, `scene`, `ecs`, this plugin and a recorder composed beside it —
six subscribers to the tick. AMD Ryzen 9 7950X3D, go1.27.1 windows/amd64.

| whole frame | ns/op | allocs/op |
| --- | --- | --- |
| nothing to record | 22 378 | **15** |
| 100 drawables | 30 331 | **15** |
| 1 000 drawables | 97 578 | **15** |
| 5 000 drawables | 384 654 | **15** |
| 1 000 drawables, each blending two clips | 116 232 | **15** |
| 5 000 drawables, each blending two clips | 478 851 | **15** |

**Allocation-free**: the count is identical with nothing to record and with five
thousand drawables, and identical again with every one of them animated, so
nothing in the binding scales with the entity count. Measured as a steady state
over 10 000 frames rather than as `allocs/op`, which rounds:
**15.07 / 15.03 / 15.02** objects a frame at 0, 5 000 and 5 000 animated. The
figure is the engine's own per-publication and per-subscriber charge; the binding
adds nothing to it.

Time is **linear at about 72 ns a drawable** — 73.9 ns at 500 and 71.5 ns at
5 000, measured against the same frame with nothing to record. Blending two
clips adds about 19 ns a drawable, which is the accessor probe, the two clip
lookups and the scratch rebuild.

The frame above records but does not draw: what a resident model costs once
scene decides, culls, sorts and packs it is scene's number, and it is large —
**1.60 ms at 1 000 and 10.26 ms at 5 000** drawables, still at 15 allocations.
Three quarters of the per-draw part of that is scene re-resolving a model path
per draw per frame, 46.9 ns against 0.54 ns for a dense index. That is
[#263](https://github.com/dvoyni/cog/issues/263), it is scene's own hot path, and
the binding is correct and allocation-free without it.

`-gcflags=-m` reports exactly one `moved to heap` in this package: the play
scratch, which is allocated once at registration and is the point. The System
body inlines the manifest lookup, the play rebuild and the Query's `All`.
