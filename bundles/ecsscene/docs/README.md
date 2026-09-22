# ecsscene

`github.com/dvoyni/cog/bundles/ecsscene` draws Entities. A game spawns an
Entity with an `m.Transform` and a `Model` naming a glTF path, and it is drawn —
nothing registered in advance, no manifest, no hash.

**It is a binding, and a binding is necessarily a third plugin.** `ecs` imports
nothing of `model` and `model` imports nothing of `ecs`, so what attaches them
is an ordinary plugin that imports both. A project not using the ECS does not
register it and schedules no ECS Systems.

**It is scene's path over Entities, and imports nothing of scene.** Its
Components wrap `model`'s values — a `model.ModelRef`, a `model.MeshRef`,
`model.ClipPlay`s, a `model.LightDescr` — beside `gfx.ParameterDescr`s and its
own copy of scene's camera, layer and pass vocabulary. Its load System keys
each changed Entity into a Batch, and its recording System draws the frame into
`gfx` itself, over its own copy of scene's arena, culling, sorting and
emission, through `model`'s packers and binding names. What each field means is
what it means to scene: [`../../scene/docs/README.md`](../../scene/docs/README.md).
**An app runs ecsscene or scene, never both.**

[`specs/ecsscene.md`](specs/ecsscene.md) is the design record: why each part is
shaped the way it is, what it is tested against, and the shapes that were
rejected. [`../../ecs/docs/specs/ecs.md`](../../ecs/docs/specs/ecs.md) §Binding
is the shape every binding of the ECS takes. **[What a binding may not
do](#what-a-binding-may-not-do) is the part to read before writing a second
one.**

ecsscene is a **Bundle**: it requires no Adapter and contributes none. The
vocabulary is in [`CONTEXT.md`](../../../CONTEXT.md) and the decision in
[ADR 0002](../../../docs/adr/0002-slots-extensions-and-bundles-as-declaration-roots.md).

## Packages

ecsscene has the declaration-root shape of
[`architecture.instructions.md`](../../../.github/instructions/architecture.instructions.md).
Its Components are plain data with no methods. `internal/types` declares only
the camera, layer and pass vocabulary the root aliases, because `Layer` forwards
there.

- **`bundles/ecsscene`** is the root, and holds declarations only: the seven
  Components a game spawns (`Model`, `Mesh`, `Animation`, `Params`,
  `Material`, `Light`, `Camera`), `MaterialTag`, the camera, layer and pass
  vocabulary, the errors the recording System reports, `Name` and the
  ordering identities `LoadOnUpdate` and `RecordOnUpdate`. It declares no
  plugin, and it is what a game's Systems import.
- **`bundles/ecsscene/internal`** is the plugin: its `New`, the registration of
  every Component, the load System behind `LoadOnUpdate` with the key scratch
  it writes, and the recording System behind `RecordOnUpdate` with its
  scratch and its copy of scene's frame code.
- **`bundles/ecsscene/ecssceneplugin`** exports only `New() kernel.Plugin`.
  ecsscene has no configuration, so there is no `Config`. Only composition
  roots and tests import it.

**The Components are still registered by the plugin that defines their Go
type.** Their types are declared in the root, and the plugin that registers
them ships inside the same Bundle, under `ecsscene.Name`. Every Store is owned
by `ecsscene`, so a game System that names one still has to declare `ecsscene`
as a dependency, and the ECS's coupling check keeps holding on Component data.

## Files

In the root, `doc.go` holds the package documentation, `id.go` `Name`,
`LoadOnUpdate` and `RecordOnUpdate`, `types.go` every Component with
`MaterialTag` and the vocabulary's aliases, `err.go` the errors, and `utils.go`
`Layer`. `internal/types/camera.go` declares the vocabulary. In `internal`:

- `plugin.go` holds the plugin and its registration;
- `load.go` the load System, the Batch key and the key scratch;
- `systems.go` the recording System, its Queries, its scratch and the per-pass
  flush;
- `batch.go` the bucketing of Entities into Batches and their animation
  blocks;
- `cull.go`, `sort.go`, `arena.go`, `draw.go`, `material.go`, `light.go` and
  `projection.go` its copy of scene's culling, sort keys, arena, emission,
  material table, light selection and pass resolution.

## Dependencies

- Go packages: `app`, `ecs`, `gfx`, `kernel`, `m`, `model`, `storage`
- Plugin dependencies: `ecs`, `model`, `gfx`, `storage`
- Configuration: none — every Store reserves an internal default population,
  which is a hint and not a cap
- Events declared or published: none

## Composing

```go
kernel.New(config).WithPlugins(
    storageplugin.New(), diskstorageplugin.New(),
    inputplugin.New(), appplugin.New(), gfxplugin.New(), modelplugin.New(), gogpuplugin.New(),
    ecsplugin.New(), ecssceneplugin.New(), game.New())
```

Only the composition root imports `ecssceneplugin`; a game's Systems import
`ecsscene`.

The binding takes no world. It declares `ecs`, `model` and `gfx` as
dependencies, so they register first, and its Components and Systems reach the
ecs plugin's `*ecs.Entities` at registration through
`kernel.Registrar.Dependency`. `sceneplugin` is not composed beside it.

## Components

```go
type Model struct {
    Ref    model.ModelRef                      // Path, Scene, Node
    Layers LayerMask
}

type Mesh struct {                             // pointer-free
    Ref       model.MeshRef
    Bounds    m.Vec4
    Layers    LayerMask
    NeverCull bool
}

type Animation struct{ Plays [model.MaxClipPlays]model.ClipPlay } // model.MaxClipPlays is 4
type Params    struct{ Values m.List[gfx.ParameterDescr] }
type Material  struct{ Tags m.List[MaterialTag] }

type MaterialTag struct {
    Tag    PassTag
    Shader gfx.ShaderDescr
    State  gfx.MaterialState
    Params m.List[gfx.ParameterDescr]
}

type Light struct {                            // pointer-free
    Descr  model.LightDescr                    // Position and Direction ignored
    Layers LayerMask
}

type Camera struct {
    ID                                      CameraID
    Projection                              ProjectionKind
    FovY, Height, Shear, Near, Far          float32
    CullMask                                LayerMask
    SunDirection                            m.Vec3
    SunColor                                m.Color
    SunIntensity                            float32
    AmbientSky, AmbientGround               m.Color
    AmbientIntensity                        float32
    Passes                                  m.List[Pass]
}
```

`CameraID`, `ProjectionKind`, `PassTag`, `Pass` and `LayerMask` (with
`LayersAll` and `Layer`) are ecsscene's own copies of scene's, with scene's
names, shapes and zero values. ecsscene imports nothing of scene.

All seven are registered by this Bundle's plugin, in `internal`, because a
Component is registered by the plugin that defines its Go type — which is what
keeps cog's coupling check working on Component data. See
[Packages](#packages).

| Component | draws as | notes |
| --- | --- | --- |
| `m.Transform` | the instance's, light's or camera's placement | **Required**, and the ecs plugin's: an Entity without one is not recorded. |
| `Model` | one instance per primitive of the view `Ref.Scene` and `Ref.Node` select in `Ref.Path` | The path is a string. The load System loads it; residency and a bad path's report are model's. |
| `Mesh` | one instance of `Ref` | The ref comes from `model.LookupAccess.BakeMesh`. |
| `Animation` | the Entity's own `sceneAnim` block | Optional. A play with an empty `Clip` is an unused slot. **Nothing here advances clip time**; that is the game's. |
| `Params` | the Batch's gfx parameters; on a model, also merged by name over the file's properties record | Optional, and part of the Batch key, so `gfx.ColorParam("baseColorFactor", c)` tints a model. |
| `Material` | the Batch's material, one entry per pass tag | Optional, any number of tags. **Absent is no material**: the bundled PBR for a mesh, the file's own for a model. Present with no tags serves no pass. |
| `Light` | one light of each pass whose camera draws its layers | Position is the Transform's; a spot's direction is the Transform's rotation applied to −Z, the way `m.LookAt` faces. |
| `Camera` | its passes, labelled `scene.camera<ID>.<tag>` | Placement is the Transform's; its scale is ignored. An empty `Passes` is scene's default pass. Two Cameras with one ID report `ErrCameraAlreadyRecorded`, and the first walked wins. |

`Model`, `Mesh`, `Light` and `Camera` are what the recording System queries,
each beside `m.Transform`. `Animation`, `Params` and `Material` are
**optional** and reached through accessors — `Animation` once per animated
Entity, `Params` and `Material` once per Batch — a Query
matches an Entity having *at least* the Components it names, so naming an
optional one would drop every Entity without it out of the walk.

`Mesh` and `Light` are pointer-free and keep the ECS's fast path. The rest hold a
string, a `List` or a `Blob` and give it up for their own Store only.

A spawn names whichever it means:

```go
type Crate struct {
    Place m.Transform
    Model ecsscene.Model
    Tint  ecsscene.Params
}

func spawnCrates(sp *ecs.Spawn[Crate]) {
    sp.New(Crate{
        Place: m.At(0, 0, -5),
        Model: ecsscene.Model{Ref: model.ModelRef{Path: "models/crate.glb"}},
        Tint:  ecsscene.Params{Values: m.NewList(gfx.ColorParam("baseColorFactor", m.Color{R: 1, A: 1}))},
    })
}
```

## The load System

It runs on what changed, and is ordered before the recording System:

```go
func load(
    k kernel.Kernel,
    modelHooks    *ecs.Hooks[ecsscene.Model, ecs.HookAll],
    meshHooks     *ecs.Hooks[ecsscene.Mesh, ecs.HookAll],
    materialHooks *ecs.Hooks[ecsscene.Material, ecs.HookAll],
    paramsHooks   *ecs.Hooks[ecsscene.Params, ecs.HookAll],
    models    *ecs.Get[ecsscene.Model],
    meshes    *ecs.Get[ecsscene.Mesh],
    materials *ecs.Get[ecsscene.Material],
    params    *ecs.Get[ecsscene.Params],
    lookup     *ecs.Write[*model.Lookup],
    filesystem *ecs.Read[storage.FileSystem],
    resources  *ecs.Write[*gfx.ResourceQueue],
    work       *ecs.Write[*keyScratch],
)
```

- **For each Entity a Hook names**, once however many name it, it resolves the
  `ModelRef` to a `ModelHandle`, loading the model if needed, and computes the
  Batch keys: (`ModelHandle`, primitive, material key, `Params` hash) for each
  primitive of a `Model`, and (`MeshRef`, material key, `Params` hash) for a
  `Mesh`. A primitive is named by the mesh it draws. The material key is the
  model material's load-time key, or the key of the `Material` override, and
  zero is the bundled PBR a `Mesh` with no `Material` draws with. `Params` are
  hashed with `gfx.FingerprintParams`, and no parameters hash as zero.
- **It stores both in its key scratch, keyed by Entity**, never in a
  Component: writing a Component would make it that Component's writer. An
  Entity that loses its `Model` and `Mesh`, or is despawned, leaves the scratch.
- **It drives model's bake and release queues.** Nothing else in an ecsscene
  app drains them, because scene's flush is not composed.
- **It ensures the bundled PBR** once the backend is up, and hands its four
  materials and the backend's readiness to the recording System through the
  key scratch, since the recording System holds the Lookup only for reading.
- **In a steady frame it walks no Entity and hashes nothing.** No Hook names
  anything, so what is left is draining two empty queues.
- **It is the only ecsscene System holding `Write[*model.Lookup]`**, and it
  reads every Store it keys from. Loading is exclusive; nothing else is.
- **A model that does not load stays unkeyed** until its Component changes
  again, because every load failure but a missing backend is cached. Entities
  touched before the backend is up wait for the first frame that has one.
- **`Model` and `Mesh` spell their tail padding out**, and model's `MeshRef`
  its padding after the source, because a Changed record is a difference in
  bytes.

A game System that spawns drawables or writes these Components orders itself
`Before[ecsscene.LoadOnUpdate]()`, so the change is keyed in the same tick.

## The recording System

It lives in `internal`, beside its Queries and its scratch:

```go
func record(
    k kernel.Kernel,
    models  *ecs.Query[modelQuery],   // m.Transform + Model
    meshes  *ecs.Query[meshQuery],    // m.Transform + Mesh
    lights  *ecs.Query[lightQuery],   // m.Transform + Light
    cameras *ecs.Query[cameraQuery],  // m.Transform + Camera
    animations *ecs.Get[ecsscene.Animation],
    params     *ecs.Get[ecsscene.Params],
    materials  *ecs.Get[ecsscene.Material],
    keys     *ecs.Read[*keyScratch],
    lookup   *ecs.Read[*model.Lookup],
    viewport *ecs.Read[*gfx.Viewport],
    work *ecs.Write[*scratch],
    out  *ecs.Write[*gfx.OpQueue],
)
```

That signature is its whole lock set: every Store, the load System's keys,
model's Lookup and the viewport read; its scratch and gfx's queue written; and
nothing declared in a `Lock` func anywhere. **It holds the Lookup only for
reading**, through `model.NewLookupReadAccess`, so it never loads: the load
System is the only ecsscene System that writes it.

Once a tick it:

1. **buckets** every placed `Model` and `Mesh` into the Batch its key names,
   reading the key the load System wrote into the key scratch. A `Model` is one
   instance per primitive of its view; each instance keeps its own world
   matrix, world sphere, layers and animation offset. A Batch resolves its mesh,
   its material, its properties record and its `Params` once, from the first
   Entity bucketed into it;
2. packs each light through `model.PackLight` and each camera's passes out of
   their `List`;
3. **for each camera and pass**, culls every instance by the camera's layer
   mask and the pass's frustum, and filters each Batch's survivors by whether
   its material serves the pass's tag;
4. **sorts** the opaque survivors by material and Batch and the blended ones
   back to front, and **packs one properties record and one instanced draw per
   opaque Batch**, and one per blended instance, through `model`'s packers;
5. emits the arenas and the passes into `*gfx.OpQueue`, bound under `model`'s
   binding names.

A frame before the backend is up, or with no window, is skipped whole, as
scene skips it.

**One recording System per bound plugin is the shape.** `*gfx.OpQueue` is one
resource, so every System that writes it serialises against every other,
whatever Components they read. It is the lock scene's flush held, and in an
ecsscene app scene's flush is not composed.

`ecsscene.RecordOnUpdate` is its subscription identity, declared in the root's
`id.go` and named verb plus event as gfx's `PresentOnUpdate` is. **It declares
no ordering beyond the load System's `Before`.** gfx subscribes
`gfx.PresentOnUpdate` `Last`, so anything that does not ask to be last already
runs before it, and what the recording System draws in a tick is in what that
tick presents. A game System that moves Transforms orders itself
`Before[ecsscene.RecordOnUpdate]()`.

**Passes are labelled `scene.camera<ID>.<tag>`**, scene's spelling, so the same
frame through either renderer reaches gfx with the same labels, and a HUD
reading `ArmFrameCmd` reads both.

**It reports scene's errors under its own names**: `ErrCameraAlreadyRecorded`,
`ErrCameraClipPlanesMissing`, `ErrCameraProjectionDegenerate`,
`ErrPassTargetUnsized`, `ErrColourlessPassWithoutDepth`,
`ErrColourlessPassClearsColour`, `ErrMaterialTagAlreadyServed` and
`ErrMeshCustomLayoutNeedsMaterial`, beside model's `ErrMeshUnavailable` and
the light packer's errors.

## Batches

**A Batch is one instanced draw per pass**: every Entity whose key is equal.
The key is the load System's, taken on change, so a steady frame never hashes a
material or a parameter.

- **What splits a Batch** is anything that changes the key: a different mesh or
  primitive, a different `Material`, or different `Params` values. **Equal
  `Params` batch**: 5 000 crates all tinted red are one Batch, and 5 000 crates
  tinted 5 000 ways are 5 000.
- **Not in the key:** layers, culling and the camera. They filter a Batch's
  instances per pass, so one Batch can draw a different subset in each pass.
- **Skinned and morphed Entities that share a mesh and material share a Batch.**
  Each instance carries its own `sceneAnim` offset, which the shader reads per
  instance.
- **Blended materials stay one draw per instance**, sorted back to front across
  Batches, as in scene. An alpha-tested cutout is opaque, so it batches.
- **Batching is always on**, with no Component to opt out. The only thing it
  changes that a game could observe is the order of opaque draws, which is
  unspecified anyway.

The recording System also splits by shader variant, which follows from the key
for a file's own material, so that an override `Material` over a skinned and an
unskinned use of one mesh never shares group 2 bindings only one declares.

## What a binding may not do

Stated as prohibitions, because they are what a second binding — physics, audio,
or a backend adopted from outside cog — has to keep true, and none of them has a
compiler behind it.

- **A Component holds no mutable indirection, transitively.** Enforced at
  registration. A string, an `assets.Blob` and an `m.List` are admitted; a bare
  slice is not. Scene's descriptors that keep a slice — `gfx.MaterialDescr`'s
  params, a camera's passes — are therefore spelled out as Component fields
  and rebuilt per Batch, which is what `MaterialTag` is.
- **Copy a List out through `All()`; never hand gfx its backing.** There is
  no `Raw()`, and a view of a Store's memory would be read after this System's
  locks are gone. The copy into reused scratch costs no allocation.
- **Anything a System keeps between calls is a resource.** The scratch is one,
  owned by this plugin and named in the signature, so it is in the lock set and
  the kernel keeps two holders apart. Nothing is captured in a closure.
- **Per-frame slices come from scratch allocated once, never from a stack
  array.**
- **Reset scratch per frame, not per Batch or per material tag.** A material
  descriptor keeps the params slice it is built around until gfx copies it at
  the draw, so each Batch's material and `Params` are appended after the
  previous ones and windowed with a full slice expression, and nothing is
  overwritten before the frame is emitted.
- **Release what the scratch held once the frame is emitted.** It holds copies
  of Components — names, and `Blob` bytes that may be a texture's pixels — and a
  stale slot would keep a despawned Entity's data reachable.
- **Do not cache an Entity without checking liveness, and do not restructure the
  world from a recording System.** Recording holds `*Entities` for read, which is
  not the authority to retire anything.

## What it costs

Measured by the five frame benches in `internal/recordbench_test.go` on a real
`kernel.Engine` driven by `app.UpdateEvent` and `app.RenderEvent`: a camera
over a 100-column grid, the model resident, and gfx replaying the frame into a
backend that is `Ready` and discards it. Every arm is 5 000 `Model` Entities;
*animated* blends two clips, *Params* is one colour for all, *Material* is two
pass tags with one float parameter each.

Two test binaries were built and run interleaved, 8 rounds, each arm its own
process at `-test.benchtime 1s`, with the arm order rotated and the first
binary alternated. *Before* is the proxy into `scene.OpQueue` at `de8e5f9`;
*after* is this recording System. Means, AMD Ryzen 9 7950X3D, go1.27.1
windows/amd64.

| whole frame | before, ns/op | after, ns/op | after, allocs/op |
| --- | ---: | ---: | ---: |
| nothing to record | 31 687 | 28 103 | 18 |
| 5 000 | 14 162 122 | 1 105 657 | 19 |
| 5 000 animated | 17 135 613 | 1 482 363 | 20 |
| 5 000 with `Params` | 15 283 788 | 1 126 376 | 19 |
| 5 000 with `Material` | 9 761 146 | 1 122 856 | 18 |

**The call shape was the cost.** Every arm is one Batch, so a frame is one
instanced draw where the proxy made 5 000, and what is left is about 220 ns an
Entity: the walk, the key lookup, the cull and packing its instance record.
**Allocations no longer scale with the population**: the proxy's one gfx
shader label per draw is now one per Batch.

`TestRecordingAllocatesNothingPerEntity` still holds its bar over 3 000-frame
steady states. Its harness uses a backend that never comes up, so since the
recording System draws into gfx itself it now skips those frames whole, as
scene's flush did; the drawn frame's allocations are the benches' figures
above.
