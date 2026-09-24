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

ecsscene is a **Bundle**: it requires no Adapter, and contributes one,
`StorageReadMount`, which mounts its own shader - the debug shapes' - in
storage, the way model mounts the bundled PBR. The
vocabulary is in [`CONTEXT.md`](../../../CONTEXT.md) and the decision in
[ADR 0002](../../../docs/adr/0002-slots-extensions-and-bundles-as-declaration-roots.md).

## Packages

ecsscene has the alias-index root of
[`architecture.instructions.md`](../../../.github/instructions/architecture.instructions.md)
and [ADR 0003](../../../docs/adr/0003-roots-are-alias-indexes.md). Its
Components are plain data with no methods, every field exported.

- **`bundles/ecsscene`** is the root, and declares nothing: it aliases what
  `internal/` declares — the Components a game spawns
  (`Model`, `Mesh`, `Animation`, `Params`, `Material`, `Light`, `Camera` and
  the five debug shapes), `MaterialTag`, the camera, layer and pass
  vocabulary, the errors the recording System reports, `StorageReadMount`,
  `Name` and the ordering identities `LoadOnUpdate`, `RecordOnUpdate` and
  `DebugOnUpdate`. It is what a game's Systems import.
- **`bundles/ecsscene/internal`** is the plugin: everything the root aliases,
  its `New`, the registration of every Component, the load System behind
  `LoadOnUpdate` with the key scratch it writes, the recording System behind
  `RecordOnUpdate` with its scratch and its copy of scene's frame code, and
  the debug shapes' Systems. It never imports the root.
- **`bundles/ecsscene/ecssceneplugin`** exports only `New() kernel.Plugin`.
  ecsscene has no configuration, so there is no `Config`. Only composition
  roots and tests import it.

**The Components are registered by the plugin that defines their Go type.**
Their types are declared in `internal/`, which registers them under
`ecsscene.Name`. Every Store is owned
by `ecsscene`, so a game System that names one still has to declare `ecsscene`
as a dependency, and the ECS's coupling check keeps holding on Component data.

## Files

In the root, `doc.go` holds the package documentation, and the rest are
aliases: `id.go` of `Name`, `LoadOnUpdate`, `RecordOnUpdate` and
`DebugOnUpdate`, `components.go` of every Component, `types.go` of
`MaterialTag` and the vocabulary, `err.go` of the errors, `adapters.go` of
`StorageReadMount`, and `utils.go` forwards `Layer`. `internal/` declares them
in files of the same names, and `camera.go` the vocabulary. In `internal`, one System per file, named for it, beside the types
they share:

- `plugin.go` holds the plugin and its registration;
- `loadsystem.go` the load System, and `keyer.go` the one run's keying it
  drives;
- `keyscratch.go` the key scratch both Systems share, the Batch key and the
  material keys;
- `recordsystem.go` the recording System, and `scratch.go` its scratch, the
  Queries it and the bucketing name, and the per-pass flush;
- `batch.go` the bucketing of Entities into Batches and their animation
  blocks;
- `debugkind.go` `debugKind`, whose two methods are each shape's bake and
  change Systems, with their scratch and two Materials, and
  `debuggeometry.go` the shapes' geometry;
- `shaderfs.go` the embedded mount, and `builtin/ecsscene/debug.wgsl` the
  debug shapes' shader, which includes model's published sources by their
  storage paths;
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
| `Params` | the Batch's gfx parameters, laid by name over every tag's, the material's numbers among them | Optional, and part of the Batch key, so `gfx.ColorParam("baseColorFactor", c)` tints a model or a mesh. |
| `Material` | one entry per pass tag, each **laid over** what the file provides | Optional, any number of tags. **Absent is no material**: the default scene shader over the file's own material, or over the bundled PBR's for a mesh. Present with no tags serves no pass. See [A Material overlays the file](#a-material-overlays-the-file). |
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
  model material's load-time key; under a `Material` it is that key and the
  `Material`'s content key together, because one `Material` over two file
  materials resolves to two materials. A `Mesh`'s is the `Material`'s alone,
  and zero is the bundled PBR a `Mesh` with no `Material` draws with. `Params`
  are hashed with `gfx.FingerprintParams`, and no parameters hash as zero.
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
- **It is the only ecsscene System that loads**, and it reads every Store it
  keys from. Beside it only the [debug shapes'](#debug-shapes) ten Systems hold
  `Write[*model.Lookup]`, to bake the shapes' meshes, and all ten run before
  it. Loading is exclusive; nothing else is.
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
   its material and its `Params` once, from the first Entity bucketed into it;
2. packs each light through `model.PackLight` and each camera's passes out of
   their `List`;
3. **for each camera and pass**, culls every instance by the camera's layer
   mask and the pass's frustum, and filters each Batch's survivors by whether
   its material serves the pass's tag;
4. **sorts** the opaque survivors by material and Batch and the blended ones
   back to front, and **packs one instanced draw per opaque Batch**, and one per
   blended instance, through `model`'s packers;
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
for a file's own material, so that a `Material` over a skinned and an unskinned
use of one mesh never shares group 2 bindings only one declares.

## A Material overlays the file

A `Material` changes how a draw is shaded without losing what the file says
about it. For **each primitive**, and for each tag, the recording System
resolves:

| Part | Resolved from |
| --- | --- |
| **shader** | the tag's `Shader`, or the default scene shader where it is zero — either way under the variant's `SCENE_SKIN` / `SCENE_MORPH` for this primitive's geometry |
| **params** | the primitive's own — its five textures and samplers, white and flat-normal defaults in empty slots, and its numbers, one per member of the `scenePbrMaterial` uniform block — overlaid by name with the default scene shader's `Params`, then the tag's; the `Params` Component rides on the draw, which gfx lays over all of them |
| **state** | the tag's `State`, or the file's (alpha mode, double-sided) where it is zero |

A mesh takes the same path: its "file" is the bundled PBR's ingredients, white
and flat in every slot, opaque, single-sided, white paint.

**ecsscene knows no shader.** It binds what describes the scene — the frame,
the instances, the animation block, the mesh records and, for deforming
geometry, the group 2 buffers — and hands a material's params to gfx, which
packs whatever the shader in effect declares by name. The bundled material's
numbers are that shader's uniform block; a shader of your own gets its
same-named members filled the same way, and one declaring none of them costs
nothing for their being there.

- **Nothing the file says is lost**, per primitive, so a model of several
  materials keeps each one's textures, numbers and state under one `Material`.
- **The variant is cog's**, whatever shader is in effect, so a caller's shader
  over a skinned model is compiled with `SCENE_SKIN` and gets the pose buffers,
  and over a static one declares nothing it is not given.
- **Replacing is a special case, not a second mode.** gfx matches params to
  bindings by name and ignores one no binding declares, so an outline shader
  declaring only its own bindings simply never reads the file's textures.
- **A zero `Shader` and a zero `State` are unset**, not values: the zero
  descriptor is no shader, and a tag wanting the zero state — `StateOverlay2D`
  — cannot say so.

**The default scene shader** is model's, set with
`model.LookupAccess.SetDefaultSceneShader(model.SceneShaderDescr{Source, Params})`
and unset by the zero descriptor. It feeds every `Model` and `Mesh` with no
`Material` and every tag that sets no shader, and its `Params` ride on every
draw under the Entity's own, so a scene-wide binding needs nothing per Entity.
Nothing is baked from it: materials are resolved from their ingredients every
frame, so setting it after models load takes effect on the next frame.

**An app shader is the bundled PBR plus a step.** It includes
`model.VertexStagePath` and `model.FragmentStagePath`, declares its own
bindings in group 3 — the one bind group the scene layout leaves free — and
writes an `fs_main` around `scenePbrFragment`. The material's numbers are the
one uniform block gfx allows a shader, and its own per-draw numbers are members
it adds to that block, composing the block from `model.MaterialProloguePath`,
its own fields over `model.MaterialFieldsPath` and `model.MaterialEpiloguePath`
before it includes the stages. Larger data rides in textures and samplers, or
in the one storage buffer the bundled shader's animated variant leaves of the
web floor's eight:

```wgsl
//#include builtin/scene/vertexstage.wgsl
//#include builtin/scene/fragmentstage.wgsl
@group(3) @binding(0) var sightDepths: texture_2d<f32>;

@fragment
fn fs_main(in: SceneVertexOut, @builtin(front_facing) ff: bool) -> @location(0) vec4<f32> {
    let lit = scenePbrFragment(in, ff);
    return vec4(mix(vec3(0.0), lit.rgb, sightVisibility(in.worldPosition)), lit.a);
}
```

**A binding nothing supplies does not cost the frame.** gfx fills an unfilled
texture with white and an unfilled sampler with its default, and refuses a
draw whose declared storage buffer nothing supplies, reporting
`gfx.ErrStorageBufferUnsupplied` once under the shader's label and the
binding's name. That draw is skipped, and the rest of the frame draws.

## Debug shapes

A box, a sphere, a plane, a line and a wire box, each a Component of its own:

```go
type DebugBox     struct{ Size m.Vec3; Color m.Color; Layers LayerMask }
type DebugSphere  struct{ Radius float32; Color m.Color; Layers LayerMask }
type DebugPlane   struct{ Normal m.Vec3; D float32; Size m.Vec2; Color m.Color; Layers LayerMask }
type DebugLine    struct{ From, To m.Vec3; Width float32; Color m.Color; Layers LayerMask }
type DebugWireBox struct{ Size m.Vec3; Width float32; Color m.Color; Layers LayerMask }
```

A shape is described in its Entity's local space and stands at the Entity's
`m.Transform`, whose scale multiplies on top. A plane is the points with
`Normal·p + D = 0`: its quad is centred at `-D` along the unit normal, one-sided,
and `Size.X` runs along world +X projected onto the plane, or +Z where the
normal is along X. A line is a box of `Width` by `Width` from `From` to `To`; a
wire box's twelve edges run half a `Width` past each corner so the corners
close.

**A shape is drawn as an ordinary Mesh, not by a path of its own.** Each kind
has two Systems, chained from `DebugOnUpdate` and all before `LoadOnUpdate`:

- **the bake System** walks the shapes with no `Mesh`, builds the geometry,
  bakes it through `model.LookupAccess.BakeMesh` and adds the `Mesh`, the
  `Params` holding `Color` as `baseColorFactor`, and the `Material`. The shape
  owns all three: a caller does not add or write them;
- **the change System** reads the shape's Hooks. A geometry edit rebakes the
  mesh in place with `UpdateMesh`, keeping the ref and freeing the buffers it
  replaces; a `Color` or `Layers` edit rewrites the `Params`, `Material` and the
  `Mesh`'s layers; removing the shape or despawning its Entity releases the
  mesh, and on a living Entity takes the three Components away. The refs live in
  the kind's scratch, because a despawn record has no `Mesh` left to read.

Both write `model.Lookup`, as the load System does, so the chain costs no
parallelism: none of the ten could overlap each other or it anyway. A game
System that spawns or edits shapes orders itself `Before[DebugOnUpdate]`.

**Every shape is self-lit.** The debug shader, `builtin/ecsscene/debug.wgsl`
in ecsscene's own mount, is the bundled vertex stage and a fragment stage
returning `baseColorFactor` untouched, so no light, sun or ambient reaches a
shape. An alpha below 1 draws through a blended `Material`;
at 1 the shape is opaque and batches.

**A shape with nothing to draw draws nothing and reports nothing**: a size or
radius not above zero, a zero `Width`, a line of no length, a plane with no
normal. It gets no `Mesh`, and the bake System checks it again each tick; a
drawable shape edited into one gives its mesh back.

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
- **Do not declare a placement Component beside `m.Transform`.** Two Components
  describing one position are unrelated to the scheduler, whose lock unit is the
  Component type: two Systems writing them run concurrently, and nothing reports
  that they disagree. A binding that keeps a position of its own, as physics
  keeps `Position` on its plane, copies it into `m.Transform` one way, in one
  System, and never back.

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
