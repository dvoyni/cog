# ecsscene

`github.com/dvoyni/cog/bundles/ecsscene` records Entities into `scene`. A game spawns an
Entity with a `Transform` and a `Model` naming a glTF path, and it is drawn —
nothing registered in advance, no manifest, no hash.

**It is a binding, and a binding is necessarily a third plugin.** `ecs` imports
nothing of `scene` and `scene` imports nothing of `ecs`, so what attaches them
is an ordinary plugin that imports both. A project not using the ECS does not
register it and schedules no ECS Systems.

**It is thin on purpose.** Its Components hold scene's own types — a
`scene.Transform`, a `scene.ModelRef`, a `scene.MeshRef`, `scene.ClipPlay`s,
`gfx.ParameterDescr`s, `scene.Pass`es — and its one System copies every matching
Entity into scene's op queue once a tick. What each field means is scene's
documentation, not this one's: [`../../scene/docs/README.md`](../../scene/docs/README.md).

[`../../ecs/docs/specs/ecs.md`](../../ecs/docs/specs/ecs.md) §Binding is the design
record. **[What a binding may not do](#what-a-binding-may-not-do) is the part to
read before writing a second one.**

ecsscene is a **Bundle**: it requires no Adapter and contributes none. The
vocabulary is in [`CONTEXT.md`](../../../CONTEXT.md) and the decision in
[ADR 0002](../../../docs/adr/0002-slots-extensions-and-bundles-as-declaration-roots.md).

## Packages

ecsscene has the declaration-root shape of
[`architecture.instructions.md`](../../../.github/instructions/architecture.instructions.md).
Its Components are plain data with no methods, so it has no `internal/types`.

- **`bundles/ecsscene`** is the root, and holds declarations only: the eight
  Components a game spawns (`Transform`, `Model`, `Mesh`, `Animation`,
  `Params`, `Material`, `Light`, `Camera`), `MaterialTag`, `MaxPlays`, `Name`
  and the ordering identity `RecordOnUpdate`. It declares no plugin, and it is
  what a game's Systems import.
- **`bundles/ecsscene/internal`** is the plugin: its `New`, the registration of
  every Component, the recording scratch and the one recording System behind
  `RecordOnUpdate`.
- **`bundles/ecsscene/ecssceneplugin`** exports only `New() kernel.Plugin`.
  ecsscene has no configuration, so there is no `Config`. Only composition
  roots and tests import it.

**The Components are still registered by the plugin that defines their Go
type.** Their types are declared in the root, and the plugin that registers
them ships inside the same Bundle, under `ecsscene.Name`. Every Store is owned
by `ecsscene`, so a game System that names one still has to declare `ecsscene`
as a dependency, and the ECS's coupling check keeps holding on Component data.

## Files

In the root, `doc.go` holds the package documentation, `id.go` `Name` and
`RecordOnUpdate`, and `types.go` every Component with `MaxPlays` and
`MaterialTag`. In `internal`, `plugin.go` holds the plugin and its
registration, and `systems.go` the recording System, its Queries and its
scratch.

## Dependencies

- Go packages: `app`, `ecs`, `gfx`, `kernel`, `m`, `scene`
- Plugin dependencies: `ecs`, `scene`
- Configuration: none — every Store reserves an internal default population,
  which is a hint and not a cap
- Events declared or published: none

## Composing

```go
kernel.New(config).WithPlugins(
    storageplugin.New(), diskstorageplugin.New(),
    inputplugin.New(), appplugin.New(), gfxplugin.New(), sceneplugin.New(), gogpuplugin.New(),
    ecsplugin.New(), ecssceneplugin.New(), game.New())
```

Only the composition root imports `ecssceneplugin`; a game's Systems import
`ecsscene`.

The binding takes no world. It declares `ecs` and `scene` as dependencies, so
both register first, and its Components and System reach the ecs plugin's
`*ecs.Entities` at registration through `kernel.Registrar.Dependency`.

## Components

```go
type Transform scene.Transform                 // required

type Model struct {
    Ref    scene.ModelRef                      // Path, Scene, Node
    Layers scene.LayerMask
}

type Mesh struct {                             // pointer-free
    Ref       scene.MeshRef
    Bounds    m.Vec4
    Layers    scene.LayerMask
    NeverCull bool
}

type Animation struct{ Plays [MaxPlays]scene.ClipPlay } // MaxPlays is 4
type Params    struct{ Values m.List[gfx.ParameterDescr] }
type Material  struct{ Tags m.List[MaterialTag] }

type MaterialTag struct {
    Tag    scene.PassTag
    Shader gfx.ShaderDescr
    State  gfx.MaterialState
    Params m.List[gfx.ParameterDescr]
}

type Light struct {                            // pointer-free
    Kind                                  scene.LightKind
    Color                                 m.Color
    Intensity, Range, InnerCone, OuterCone float32
    Layers                                scene.LayerMask
}

type Camera struct {
    ID                                      scene.CameraID
    Projection                              scene.ProjectionKind
    FovY, Height, Shear, Near, Far          float32
    CullMask                                scene.LayerMask
    SunDirection                            m.Vec3
    SunColor                                m.Color
    SunIntensity                            float32
    AmbientSky, AmbientGround               m.Color
    AmbientIntensity                        float32
    Passes                                  m.List[scene.Pass]
}
```

All eight are registered by this Bundle's plugin, in `internal`, because a
Component is registered by the plugin that defines its Go type — which is what
keeps cog's coupling check working on Component data. See
[Packages](#packages).

| Component | reaches scene as | notes |
| --- | --- | --- |
| `Transform` | the draw's, light's or camera's placement | **Required**: an Entity without one is not recorded. Defined *from* `scene.Transform`, not aliased, so the Store's Go type is this package's and a System naming it imports `ecsscene`. Convert with `scene.Transform(t)`. |
| `Model` | `OpQueue.Model(Layers, Ref.Path, …)` with `Ref.Scene` and `Ref.Node` | The path is a string. Loading, residency and a bad path's report are scene's. |
| `Mesh` | `OpQueue.Mesh(Layers, Ref, …)` | The ref comes from `LookupAccess.BakeMesh`. |
| `Animation` | `ModelDraw.Plays` | Optional. A play with an empty `Clip` is an unused slot. **Nothing here advances clip time**; that is the game's. |
| `Params` | `MeshDraw.Params`, `ModelDraw.OverrideParams` | Optional. On a model they merge by name over the file's materials, so `gfx.ColorParam("baseColorFactor", c)` tints it. |
| `Material` | `MeshDraw.Material`, `ModelDraw.Material` | Optional, any number of tags. **Absent is no material**: the bundled PBR for a mesh, the file's own for a model. Present with no tags is an empty non-nil material. |
| `Light` | `OpQueue.PointLight` or `SpotLight` | Position is the Transform's; a spot's direction is the Transform's rotation applied to −Z, the way `scene.LookAt` faces. |
| `Camera` | `OpQueue.Camera(ID, …)` | Placement is the Transform's; its scale is ignored, as scene ignores it. An empty `Passes` is scene's default pass. Two Cameras with one ID are scene's duplicate report. |

`Model`, `Mesh`, `Light` and `Camera` are what the System queries, each beside
`Transform`. `Animation`, `Params` and `Material` are **optional** and reached
through accessors, so an Entity without them pays one probe each: a Query
matches an Entity having *at least* the Components it names, so naming an
optional one would drop every Entity without it out of the walk.

`Mesh` and `Light` are pointer-free and keep the ECS's fast path. The rest hold a
string, a `List` or a `Blob` and give it up for their own Store only.

A spawn names whichever it means:

```go
type Crate struct {
    Place ecsscene.Transform
    Model ecsscene.Model
    Tint  ecsscene.Params
}

func spawnCrates(sp *ecs.Spawn[Crate]) {
    sp.New(Crate{
        Place: ecsscene.Transform(scene.At(0, 0, -5)),
        Model: ecsscene.Model{Ref: scene.ModelRef{Path: "models/crate.glb"}},
        Tint:  ecsscene.Params{Values: m.NewList(gfx.ColorParam("baseColorFactor", m.Color{R: 1, A: 1}))},
    })
}
```

## The recording System

It lives in `internal`, beside its Queries and its scratch:

```go
func record(
    models  *ecs.Query[modelQuery],   // Transform + Model
    meshes  *ecs.Query[meshQuery],    // Transform + Mesh
    lights  *ecs.Query[lightQuery],   // Transform + Light
    cameras *ecs.Query[cameraQuery],  // Transform + Camera
    animations *ecs.Get[ecsscene.Animation],
    params     *ecs.Get[ecsscene.Params],
    materials  *ecs.Get[ecsscene.Material],
    work *ecs.Write[*scratch],
    out  *ecs.Write[*scene.OpQueue],
)
```

That signature is the whole binding and its whole lock set: every Store read,
the scratch and scene's queue written, and nothing declared in a `Lock` func
anywhere.

**One recording System per bound plugin is the shape.** `*scene.OpQueue` is one
resource, so every recording System serialises against every other whatever
Components they read. A second one would cost a scheduling slot and could not
run concurrently anyway.

`ecsscene.RecordOnUpdate` is its subscription identity, declared in the root's
`id.go` and named verb plus event as scene's `FlushOnUpdate` and gfx's
`PresentOnUpdate` are. **It declares no ordering.** Scene subscribes
`scene.FlushOnUpdate` `Last`, so anything that does not ask to be last already
runs before it, and a draw recorded in a tick is in what that tick's flush
publishes. A game System that moves Transforms orders itself
`Before[ecsscene.RecordOnUpdate]()`.

## What a binding may not do

Stated as prohibitions, because they are what a second binding — physics, audio,
or a backend adopted from outside cog — has to keep true, and none of them has a
compiler behind it.

- **A Component holds no mutable indirection, transitively.** Enforced at
  registration. A string, an `assets.Blob` and an `m.List` are admitted; a bare
  slice is not. Scene's descriptors that keep a slice — `gfx.MaterialDescr`'s
  params, `CameraDescr.Passes` — are therefore spelled out as Component fields
  and rebuilt per draw, which is what `MaterialTag` is.
- **Copy a List out through `All()`; never hand the bound plugin its backing.**
  There is no `Raw()`, and a view of a Store's memory would be read by scene's
  flush after this System's locks are gone. The copy into reused scratch costs no
  allocation.
- **Anything a System keeps between calls is a resource.** The scratch is one,
  owned by this plugin and named in the signature, so it is in the lock set and
  the kernel keeps two holders apart. Nothing is captured in a closure.
- **Per-draw slices come from scratch allocated once, never from a stack
  array.** Scene's recording calls let their argument escape, so a local
  `[4]scene.ClipPlay` handed to one is a heap allocation per draw.
- **Reset scratch per draw, but not per material tag.** `gfx` keeps the params
  slice a descriptor is built around, so within one draw each tag's params are
  appended after the previous tag's and windowed with a full slice expression.
  Scene copies the whole material into its frame arenas at record, so the next
  draw may reuse everything.
- **Release what the scratch held once the frame is recorded.** It holds copies
  of Components — names, and `Blob` bytes that may be a texture's pixels — and a
  stale slot would keep a despawned Entity's data reachable.
- **Do not cache an Entity without checking liveness, and do not restructure the
  world from a recording System.** Recording holds `*Entities` for read, which is
  not the authority to retire anything.

## What it costs

Measured on a real `kernel.Engine` driven by a real `app.UpdateEvent`, with
`storage`, `gfx`, `scene`, `ecs`, this plugin and a game plugin composed beside
it — five subscribers to the tick. The frame has no camera and no resident
model: scene records a `Model` call, copying its plays, overrides and material
into its arenas, before it knows whether the path is resident, so this is the
binding's cost and not scene's decide-cull-pack. Every arm is 5 000 `Model`
Entities; *animated* blends two clips, *Params* is one colour, *Material* is two
pass tags with one float parameter each.

Both test binaries — this binding, and the manifest binding it replaces at
`d3631ff` — were built first and run alternately, ten rounds, medians.
AMD Ryzen 9 7950X3D, go1.27.1 windows/amd64.

| whole frame | ns/op | allocs/op | per Entity | manifest binding, ns/op |
| --- | --- | --- | --- | --- |
| nothing to record | 19 931 | **14** | — | 21 071 |
| 5 000 | 413 928 | **14** | 78.8 ns | 359 604 |
| 5 000 animated | 456 584 | **14** | +8.5 ns | 463 337 |
| 5 000 with `Params` | 570 824 | **14** | +31.4 ns | — |
| 5 000 with `Material` | 1 116 624 | **14** | +140.5 ns | — |

**Allocation-free.** Measured as a steady state over 3 000 frames rather than as
`allocs/op`, which rounds: **14.08, 14.02, 14.03, 14.03 and 14.05** objects a
frame for the five rows, so nothing scales with the Entity count. The figure is
the kernel's own per-publication and per-subscriber charge; a profile of the
steady state with every allocation sampled finds none in `ecsscene`, `ecs`,
`scene` or `gfx`. (The manifest binding's harness subscribed a second recorder
to scene's queue, which is why it sat at 15: a request blocked behind another
costs the kernel's scheduler a map entry.)

**Time is about 79 ns an Entity**, against the manifest binding's 68. The
difference is the price of the shape rather than of a lookup: a `Model` holds
strings, so its Query takes the ECS's per-field fill instead of the unrolled
one, and every Entity pays three accessor probes for `Animation`, `Params` and
`Material` whether it has them or not. Animation is cheaper than it was, because
there is no clip name to resolve.

**A `Material` costs 140 ns a draw**, most of it scene copying the tags and their
params into its arenas at record — copied once per draw, even when five thousand
Entities share one material, which is
[#314](https://github.com/dvoyni/cog/issues/314).

`-gcflags=-m` reports no `moved to heap` in this package's recording path; its
`append`s escape only when they grow a scratch backing, which the steady state
above never does.
