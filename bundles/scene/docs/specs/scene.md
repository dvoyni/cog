# scene — specification

`github.com/dvoyni/cog/bundles/scene` records declarative 3D draws — cameras, glTF
models, buffer-built meshes, lights — and translates them into `gfx` passes and
draws at the end of each simulation update. It is the sibling of `canvas`: same
frame-local `OpQueue` shape, same persistent `Lookup` behind a scoped access
facade, same lazy path-based loading through `storage`. Canvas draws 2D over,
under, and between scene cameras in one ordered frame.

This document is the specification the implementation is judged against. It is
assembled from the resolved tickets of
[Scene plugin: declarative 3D rendering](https://github.com/dvoyni/cog/issues/1);
every section cites the tickets it came from. Nothing is decided here — where a
gap was found while assembling, it is marked **Gap** and filed as its own ticket.

The plugin does not exist yet. Neither do the `gfx`, `gogpu`, `m` and `canvas`
changes it depends on; those are specified here as checklists
([Required engine changes](#required-engine-changes)), because scene cannot be
correct without them.

> **Amended by [#339](https://github.com/dvoyni/cog/issues/339).** scene became
> a Bundle under
> [ADR 0001](../../../../docs/adr/0001-bundles-slots-ports-and-adapters.md), shaped
> as a contract root, `sceneimpl` and `internal/`. The contract this document
> specifies is unchanged, and so is every name a recorder writes against:
> `OpQueue` and its recording methods, `Lookup`, `LookupAccess` and its queries,
> `Transform`, `CameraDescr`, `Pass`, `Material`, `MeshDraw`, `ModelDraw`, the
> `Err*` types and the coordinate helpers all stay in the root,
> `bundles/scene`. What moved is where the code lives. The plugin is
> `sceneimpl.New()`, configured by `sceneimpl.Config`, where a zero field takes
> its default. Its flush, the cull, the sort, the material table, the light
> selection, the model expansion and the load handlers are in
> `bundles/scene/sceneimpl`, and the built-in WGSL is embedded and mounted from
> `bundles/scene/sceneimpl/builtin/scene/`. `OpQueue` with its recording and
> consume sides, the `Lookup` with its model table, the glTF loader, the packing
> and the bundled PBR are declared in `bundles/scene/internal` and aliased or
> wrapped in the root, concrete as before, so no per-instance call goes through
> an interface. The flush's subscription identity, `UpdateEventHandler` below, is
> now `scene.FlushOnUpdate`. The file paths and line numbers cited below are as
> they were when this was written. `Transform` is no longer scene's: it is
> `m.Transform` since [#468](https://github.com/dvoyni/cog/issues/468).
>
> **Amended by [#361](https://github.com/dvoyni/cog/issues/361).** scene moved
> to the declaration-root shape of
> [ADR 0002](../../../../docs/adr/0002-slots-extensions-and-bundles-as-declaration-roots.md).
> Every name a recorder writes against is still spelled `scene.X`: the recording
> vocabulary, `LookupAccess` and the inspection views are aliases in the root's
> `types.go`, `OpQueue` and `Lookup` in its `resources.go`, `Config` in its
> `config.go`, and `At`, `LookAt`, `Layer`, `NewLookup`, `NewLookupAccess` and
> the four coordinate helpers are forwarders in its `utils.go`. `At` and
> `LookAt` are `m`'s since [#468](https://github.com/dvoyni/cog/issues/468):
> `m.At` and `m.LookAt`, with no forwarder in scene. The `Err*` types
> stay in its `err.go`. What `bundles/scene/internal` held — `OpQueue` with its
> recording and consume sides, the `Lookup` with its model table, the glTF
> loader, the packing, the bundled PBR — moved to `bundles/scene/internal/types`,
> and so did the coordinate maths. The plugin — the flush, the cull, the sort,
> the material table, the light selection, the model expansion and the load
> handlers — moved from `sceneimpl` to `bundles/scene/internal`, and the
> built-in WGSL is embedded from `bundles/scene/internal/builtin/scene/`. It is
> constructed with `sceneplugin.New()` and configured with `scene.Config`, whose
> zero value is the default.
>
> **Amended by [#532](https://github.com/dvoyni/cog/issues/532).** The built-in
> WGSL moved to `bundles/model/internal/builtin/scene/`, and the model plugin
> mounts it; its storage paths are unchanged.
>
> **Amended by [#539](https://github.com/dvoyni/cog/issues/539).** This document
> now specifies the renderer only: the queue, cameras and passes, scene's
> materials and pass tags, the draw calls, light culling, sorting, culling and
> batching, and the coordinate helpers. Everything a model file can contain is
> `bundles/model`'s, and so is its specification: the glTF loader and
> residency, mesh baking, animation, the lights' packing and cap, the bundled
> PBR material, the Lookup facade and the shader-side contract moved to
> [`model.md`](../../../model/docs/specs/model.md#the-model-contract-moved-from-scenemd),
> and the vertex and mesh layouts are in
> [`mesh.md`](../../../model/docs/specs/mesh.md). Each of those sections keeps a
> stub here saying what scene still does in that area. The names are spelled
> as they now are: `model.MeshRef`, `model.ClipPlay`, `model.LightDescr`,
> `*model.Lookup` and the rest have no alias in scene's root.

---

## Contents

- [Plugin](#plugin) · [Resources](#resources) · [Recording API](#recording-api)
- [Cameras and passes](#cameras-and-passes) · [Materials and pass tags](#materials-and-pass-tags)
- [glTF models](#gltf-models) · [Buffer-built meshes](#buffer-built-meshes)
- [Animation](#animation) · [Lights](#lights) · [Bundled PBR material](#bundled-pbr-material)
- [Sorting, culling, and batching](#sorting-culling-and-batching) · [Lookup facade](#lookup-facade)
- [Coordinate helpers](#coordinate-helpers) · [Shader-side contract](#shader-side-contract)
- [Required engine changes](#required-engine-changes) · [Extension points](#extension-points)
- [Demos and acceptance](#demos-and-acceptance) · [Out of scope](#out-of-scope)

---

## Plugin

- Name: `scene.Name` (`"scene"`)
- Constructor: `sceneplugin.New() kernel.Plugin` (amended by #361; #339 made it `sceneimpl.New()`, and before that it was `scene.New() *scene.Plugin`)
- Plugin dependencies: `gfx`, `storage`, `model` (amended by #530)
- Go package dependencies: `app`, `gfx`, `kernel`, `m`, `model`, `storage`
  (amended by #539: the glTF library is `model`'s alone)
- Events declared or published: none

```go
kernel.New(map[kernel.PluginName]any{
	model.Name: model.Config{PoseSampleRate: 60},
})
```

`model.Config` is the configuration type, and a zero field takes its default
(amended by #530, which moved the Lookup and its configuration to the model
plugin, and scene takes none of its own; #539 deleted the `scene.Config` alias.
#361 had it
as `scene.Config`, #339 moved it to `sceneimpl.Config`, and before that it was
`scene.Config` with `scene.DefaultConfig()`). The plugin
implements `Name`, `Dependencies`, and `Register` for the kernel lifecycle. The
bundled shader's sources are model's, which contributes them to storage as a
read mount through `storage.ReadMountPort` (amended by #532; scene contributed
them itself before).
Register `storage` and `model` before `scene`. A typical order is `storage`,
`input`, `gfx`, `canvas`, `model`, `scene`, then the system driver.

`PoseSampleRate` is the global animation bake rate in Hz, default 60. It is the
only configurable number in the plugin; see
[Animation](#animation)
([Baked pose buffer and skinning contract](https://github.com/dvoyni/cog/issues/15)).

### Adapter requirement

Scene reads storage buffers from the **vertex stage**, which WebGPU guarantees
only on a **core** adapter — compatibility mode defaults
`maxStorageBuffersInVertexStage` to 0. The binding cog runs on **cannot express
compatibility mode at all**: `RequestAdapterOptions` carries `PowerPreference`,
`ForceFallbackAdapter` and `CompatibleSurface` and nothing else, and the browser
path forwards at most `powerPreference` and `forceFallbackAdapter`. Omitting
`featureLevel` means core, so a compat-only device already fails
`requestAdapter()` and gets no WebGPU at all rather than a degraded scene. Stated
this way deliberately: this is not a choice cog makes and could later unmake — it
is a capability the binding does not have. The result is consistent with the
map's no-fallback rule, but the failure is total
([wgpu backend capabilities inventory](https://github.com/dvoyni/cog/issues/4),
[GPU skinning and morph techniques survey](https://github.com/dvoyni/cog/issues/6)).

Design to **browser spec defaults**, not to desktop's reported hardware limits:
8 storage buffers per shader stage, 128 MiB per binding, 256 MiB per buffer, 4
bind groups, 64 KiB uniform. A native device reports hardware limits, so a
desktop run will **not** catch a web limit violation; gfx therefore checks every
reflected shader against `gfx.DefaultLimits()` so it fails loudly on desktop
instead. There is no build gate on that check ([wgpu backend capabilities inventory](https://github.com/dvoyni/cog/issues/4)).

---

## Resources

- `*OpQueue` — frame-local recording surface. Scene consumes and resets it on
  `app.UpdateEvent`.
- `*model.Lookup` is not scene's (amended by #530 and #539). It is the single
  persistent resource holding resident models, baked pose and morph buffers,
  the texture cache, buffer-built meshes, and the built-in unit meshes, and the
  model plugin registers it. See
  [model.md §Lookup facade](../../../model/docs/specs/model.md#lookup-facade).

Gameplay normally writes only `*OpQueue`. Sizing, naming, residency, mesh baking
and unloading go through `*model.Lookup` via `model`'s facades; scene's flush
also writes `*model.Lookup` to load what a frame draws and to apply deferred
bakes and unloads.

### Event subscribed

`scene.FlushOnUpdate` (amended by #339; was `UpdateEventHandler`) subscribes to `app.UpdateEvent`. It writes the scene
`*OpQueue` and `*model.Lookup`, reads `gfx.Viewport`, and writes `gfx.OpQueue` and
`gfx.ResourceQueue`. It is ordered `Last()` but explicitly before
`gfx.PresentOnUpdate`, exactly as canvas is: gameplay records first, canvas
and scene emit graphics draws second, gfx presents last.

**Everything scene decides happens in that flush, on the update thread** —
projection resolve, frustum culling, light culling, sorting, instance packing,
buffer uploads. Scene never runs on the render thread: gfx renders on
`app.RenderEvent` from a latest-wins snapshot taken by `present` on
`app.UpdateEvent`, so there is no mechanism for it, and no need for one, because
a frustum needs **aspect**, not pixel size, and `gfx.Viewport` already carries
the exact aspect on the update thread
([Draw sorting, culling, and batching](https://github.com/dvoyni/cog/issues/20),
overturning the render-thread placement in
[Camera model](https://github.com/dvoyni/cog/issues/13) and
[Lighting model and limits](https://github.com/dvoyni/cog/issues/17)).

The one cost: a screen-targeted camera's aspect is up to one frame stale during
a window resize, which can mis-cull only something touching the frustum edge for
that frame.

---

## Recording API

Bind `access.GetWrite[*scene.OpQueue]()` in the recording subscription's `Lock`
and call ([Scene recording API sketch](https://github.com/dvoyni/cog/issues/12)):

```go
func (q *OpQueue) Camera(id CameraID, descr CameraDescr)
func (q *OpQueue) Model(layers LayerMask, path string, draw ModelDraw)
func (q *OpQueue) Mesh(layers LayerMask, mesh model.MeshRef, draw MeshDraw)
func (q *OpQueue) PointLight(layers LayerMask, light model.LightDescr)
func (q *OpQueue) SpotLight(layers LayerMask, light model.LightDescr)

func (q *OpQueue) Box(layers LayerMask, transform m.Transform, color m.Color)
func (q *OpQueue) Sphere(layers LayerMask, center m.Vec3, radius float32, color m.Color)
func (q *OpQueue) Plane(layers LayerMask, center m.Vec3, size m.Vec2, color m.Color)
func (q *OpQueue) Line3D(layers LayerMask, start, end m.Vec3, thickness float32, color m.Color)
func (q *OpQueue) WireBox(layers LayerMask, center, size m.Vec3, thickness float32, color m.Color)

func (q *OpQueue) TemporaryMesh[TVertex model.VertexLayout](vertices []TVertex, indices []uint32, topology gfx.PrimitiveTopology) model.MeshRef

func (q *OpQueue) Reset()
func (q *OpQueue) OpCount() int
func (q *OpQueue) Ops(dst []Op) []Op
func (q *OpQueue) Passes(dst []PassView) []PassView
```

**Amended by [scene: multi-pass, pass tags and multi-tag materials](https://github.com/dvoyni/cog/issues/81):
there is no `scene.OpQueue.TemporaryTarget`.** A temporary target's texture id is minted by the gfx
backend through `gfx.OpQueue`, which a scene recorder does not hold, so a passthrough on scene's
queue would either reach into a resource it has not locked or hand back a target with no id in it.
Call `gfx.OpQueue.TemporaryTarget(w, h, format)` and pass the target handle into `Pass.Target`, which
takes it untouched; a recorder doing that locks both queues and orders itself before scene's flush.
The same call returns the texture to sample it back with
([#111](https://github.com/dvoyni/cog/issues/111)).
`TemporaryMesh` is unaffected — scene bakes meshes at flush through the resource queue it holds.

The floor of the API is two statements:

```go
q.Camera(cameraMain, scene.CameraDescr{
    Transform: m.LookAt(m.Vec3{X: 3, Y: 2, Z: 4}, m.Vec3{}, m.Vec3{Y: 1}),
    FovY:      1.0472,
    Near:      0.1, Far: 100,
    SunDirection: m.Vec3{X: -0.3, Y: -1, Z: -0.2},
    SunColor:     m.NewColorSrgb(1, 1, 1, 1),
})
q.Box(0, m.At(0, 0, 0), m.NewColorSrgb(0.42, 0.71, 0.94, 1))
```

**Every slice field on every descriptor is borrowed for the duration of the
call.** Scene copies into its frame arena before returning, so a hot-loop caller
reuses one backing array. That includes a draw's `Material` — its tag entries
and each entry's parameters — but not the bytes a parameter carries, which are
`assets.Blob`s and static by contract; see [Materials are copied at
record](#materials-are-copied-at-record).

### Transform

scene places everything with `m.Transform` — a position, a rotation and a
per-axis scale — built with `m.At` and `m.LookAt` and adjusted with `WithScale`
and `WithRotation`, all in `libs/m/transform.go`. scene declares no placement
type of its own and imports nothing of `ecs` to take it.

The zero value is the identity. `Scale` is **per axis**, and only an all-zero
`Scale` reads as the identity; **a partly zero scale is taken literally**. So
`m.Vec3{X: 2}` collapses the draw onto the X axis rather than silently becoming
`(2,1,1)`, and a legitimately flattened scale — `m.Vec3{X: 1, Y: 1}` — is
expressible. `WithScale(s)` is the uniform spelling, and `WithScale(0)` is the
identity, as the zero value is.

**A non-uniform scale does not force the inverse-transpose onto every draw.**
That was the argument for a scalar `Scale` with a `Matrix *m.Mat4` escape hatch,
and it never held: the packer flags `SCENE_NONUNIFORM` per instance from the
packed matrix itself (see [The instance record](#the-instance-record)), so only
an instance whose basis does not scale uniformly pays for the cofactors, however
its scale was spelled.

**There is no `Matrix`.** A pointer field made `m.Transform` mutable indirection,
which an ECS Component cannot hold, and everything it spelled that a position, a
rotation and a per-axis scale cannot — a shear — is not something scene draws. A
model primitive's flattened node world is a whole matrix, and scene carries that
on its own draw record, where no caller sees it.

### Layers

```go
type LayerMask uint32 // zero reads as LayersAll
func Layer(i uint) LayerMask
const LayersAll LayerMask = ^LayerMask(0)
```

A camera draws a recorded item iff `layers & CullMask != 0`. **Zero reads as
`LayersAll` on both sides**, so the degenerate frame works with no masks written
anywhere ([Camera model](https://github.com/dvoyni/cog/issues/13)).

### Debug vocabulary

`Box`, `Sphere`, `Plane`, `Line3D` and `WireBox` are the scene twin of canvas's
`FillRect` / `StrokeRect` / `Line`: sugar over scene-owned unit meshes and the
bundled PBR. `Box`, `Sphere` and `Plane` are **lit**; `Line3D` and `WireBox` are
**self-lit** — base colour black, `emissiveFactor` the given colour, through the
same shader — so a debug line stays visible in a frame with no sun, which is
precisely the frame being debugged.

**A line is a long thin box, not a line-list primitive.** WebGPU has no
line-width control at all, so GPU lines rasterise one physical pixel wide and
effectively vanish on a hidpi display. A stretched box has caller-controlled
thickness and keeps scene to one built-in shader and one topology, batching with
everything else. Scene builds the stretch internally, on top of whatever `Scale`
the transform carries. The cost is that thickness is
world-space, so a distant line thins out.

The unit box, sphere and plane are three durable `MeshRef`s baked **lazily on
first use** through `BakeMesh`, because the backend may not be `Ready()` at
startup. The sphere's tessellation is fixed and documented rather than
configurable: a 16 × 12 UV sphere, about 350 triangles
([Buffer-built models: static and dynamic](https://github.com/dvoyni/cog/issues/22)).

### Inspection

Two levels, both always retained, both aliasing flush storage and valid until
the next flush — the same contract as `canvas.Ops`
([Draw sorting, culling, and batching](https://github.com/dvoyni/cog/issues/20)):

- `Ops(dst []Op) []Op` — what a recorder recorded, canvas's shape exactly.
- `Passes(dst []PassView) []PassView` — the flush **result**.

```go
type PassView struct {
    CameraID  CameraID
    Order     gfx.Order
    Tag       PassTag
    Frustum   m.Frustum
    Recorded  int
    Culled    int
    Instances int
    Batches   []BatchView
}

type BatchView struct {
    MeshID, MaterialID           uint32
    FirstInstance, InstanceCount int
}
```

`Frustum` is published because asserting that a specific sphere was rejected by
a specific frustum is the whole point; without it a culling test can only count.
The batch-shaped naming is kept even though `InstanceCount` is 1 for everything
but an explicit instanced draw — that is the shape it takes once the deferred
collapse lands, and renaming later would churn every test. There is no
`Config.Inspect` knob: the lists are built during flush regardless, so retaining
them costs a slice header, and a knob would mean tests exercise a code path
production does not.

---

## Cameras and passes

([Camera model](https://github.com/dvoyni/cog/issues/13),
[Scene recording API sketch](https://github.com/dvoyni/cog/issues/12),
[gfx render passes and render targets](https://github.com/dvoyni/cog/issues/8),
[Canvas layer mapping onto gfx pass order](https://github.com/dvoyni/cog/issues/27))

```go
type CameraID gfx.Order
type ProjectionKind uint8

const (
    Perspective ProjectionKind = iota
    Orthographic
    Oblique
)

type PassTag string
const TagForward PassTag = "forward" // an empty PassTag reads as TagForward

type Pass struct {
    Tag        PassTag
    Target     gfx.TargetDescr // zero is the screen sentinel; NoTarget() for depth-only
    Depth      gfx.DepthDescr  // zero is DepthAuto
    ClearColor m.Maybe[m.Color] // absent preserves
    ClearDepth m.Maybe[float32] // absent preserves; 1.0 is the useful value
    Order      gfx.Order       // offset from the camera id
}

type CameraDescr struct {
    Transform  m.Transform     // the camera as a positioned object; scene inverts it
    Projection ProjectionKind
    FovY       float32         // Perspective: vertical field of view, radians
    Height     float32         // Orthographic and Oblique: world units across the target's height
    Shear      float32         // Oblique: the gain depth rides up the screen by
    Near, Far  float32         // both required; zero is a reported error

    CullMask LayerMask         // zero reads as LayersAll

    SunDirection m.Vec3        // direction of travel; zero means no sun; scene normalises
    SunColor     m.Color       // linear
    SunIntensity float32       // zero means 1

    AmbientSky       m.Color   // linear
    AmbientGround    m.Color   // linear
    AmbientIntensity float32   // zero means 1

    Passes []Pass              // empty means one default pass
}
```

**A camera is a positioned object, not a view matrix.** `m.LookAt(eye, target, up)`
returns an `m.Transform`; scene inverts it with the allocation-free `m.InverseAffine`.
A `View m.Mat4` field would have made the camera the one thing in the API that
is not a `Transform`, would not compose with a follow rig, and scene would
decompose it for culling anyway. **A camera's `Transform.Scale` is ignored** —
scaling the view matrix scales the whole world instead, and the field cannot be
avoided at the call site because zero means 1
([3D-to-screen coordinate helpers](https://github.com/dvoyni/cog/issues/38)).

### The shared ordering space

`gfx.Order` is one flat `int` space that gfx stable-sorts passes by, ties broken
by declaration sequence. Both consumers take a **defined type** over it, because
each carries meaning its order does not:

```go
type gfx.Order int
type canvas.Layer gfx.Order
type scene.CameraID gfx.Order
```

gfx defines no conventions and reserves no ranges. **Equal `Order` between two
recorders is an app-level bug** promising nothing, exactly as two canvas draws
on one `Layer` are ordered by recording and nothing more.

Canvas declares one `PassDescr` per non-empty layer at
`Order = gfx.Order(layerID)`, all screen-targeted, and gfx's merge rule collapses
the contiguous run back to one GPU pass. So a camera interleaves by taking an
order **between** two layer values, with no canvas API for it at all. App layer
constants are already sparse — feuds-26 runs 0..N then 1000/2000/3000/4000/5000 —
so a camera at `Order 1500` lands between the HUD and the tutorial for free.
Scene cameras conventionally take **negative** ids when canvas draws entirely
over them.

`CameraID` orders cameras among themselves and is the **default `Order` for its
passes**; `Pass.Order` is an **offset** from it, not an absolute. An absolute
`int` has no working zero value (0 is a legitimate order), so "pass wins, else
camera" would need a pointer or a companion flag; the offset is equally
expressive — a shadow pass writes `Order: -1000` — and its zero value correctly
means "at the camera".

Recording the same `CameraID` twice in a frame is **reported through
`kernel.ReportError` and the first record wins**. `Camera(id, descr)` is a
registration, not a free parameter, so a repeat means two systems each believe
they own that camera.

**Camera iteration is sorted, never map order** — scene collects ids into a
reused slice and sorts, as canvas does for layers. A Go map range would make
frame output nondeterministic.

### Projection

**`FovY` is the literal vertical field of view**, in radians; horizontal derives
from the target's aspect, so a wider target shows more horizontally and a
narrower one crops the sides. `Height` is the orthographic twin: world units
across the target's height, width derived. There is no `FovAxis` enum and no
canvas-style `ReferenceAspect` — a 3D camera has no reference framing it does
not invent, and a game that wants one computes `FovY` from the target aspect in
one line at the call site.

**`Oblique` is `Orthographic` with a shear**, and the two meet at `Shear: 0`.
`Perspective` and `Orthographic` both project along the camera's forward axis,
so revealing a vertical face always costs ground-plane scale: tilt to elevation
φ and the ground foreshortens by exactly sin φ. **No camera placement shows
vertical faces and leaves the ground unforeshortened.** `Oblique` projects along
a direction that is *not* perpendicular to the image plane, so the plane the
camera sits in renders at true scale while depth is sheared into screen-up by
`Shear`. `1` is cavalier, `0.5` cabinet, and the implied elevation is
`atan(1/Shear)`.

It is a kind rather than something an app composes from outside because it
cannot be faked from outside. **Two cameras do not register**: restricted to the
ground plane, a tilted camera is the top-down one scaled by sin φ along screen-y
*only*, so matching the vertical mismatches the horizontal by 1/sin φ, and the
residual grows linearly toward the screen edges — at a 30-unit view height and
φ = 60°, an object at the top of the screen stands a full 2 units off its own
footprint. **A non-uniform world scale does not work either**: undoing the
ground foreshortening that way stretches every object along one horizontal axis
with it.

Nothing about a shear is degenerate — `0` is the continuum's endpoint, which an
app animating a shear up from rest must pass through; a negative one is a
mirror; a large one is only a useless elevation. So `Oblique` inherits
`Orthographic`'s rules exactly and adds no error of its own, and `Shear` is a
field one kind reads and the others do not, the way `Orthographic` does not read
`FovY`.

**The shear pivots about the camera's own plane**, which is the one trap the
kind carries. For `Orthographic`, distance along the view axis is free: only
`Near` and `Far` care where the camera sits. For `Oblique` that distance *pans
the image* — at `Shear: 1` a camera 50 units above the ground puts that ground
50 units down the screen, and since the camera's height is normally chosen to
bracket the scene in `Near..Far`, the two concerns are coupled through a number
picked for an unrelated reason. Nothing is reported: the frame renders, empty.

The rule is therefore **put the camera in the plane you want held fixed and let
`Near` go negative** — `Near: -100, Far: 100` — rather than standing it off and
re-aiming. `m.Oblique4` only requires `near < far`, the 0..1 depth mapping is
unaffected, and `FrustumFromMat4` extracts correct planes either way. The
rejected alternative was anchoring at world `y = 0` automatically, which would
make `projection()` read `Transform` and assert a ground plane scene has no
concept of anywhere else; anchoring at `(Near+Far)/2` is the same idea and is
wrong for every asymmetric range.

**Depth ordering and culling both hold.** The shear leaves view-space depth
untouched, so along the projection ray depth varies monotonically and the
ordinary depth buffer sorts — no painter's algorithm, no per-draw sort.
`FrustumFromMat4` extracts its planes from the composed view-projection and
`cull.go` reads that and nothing else, so a sheared matrix yields a sheared
frustum with correct planes for free.

**The projection is resolved per pass**, from that pass's target aspect, at
flush. A camera's passes may target different sizes — a 1024×1024 shadow map and
the screen — so there is no single camera aspect. Aspect sources:

| pass colour target | aspect from |
| --- | --- |
| screen sentinel | `gfx.Viewport.WindowWidth` / `WindowHeight` |
| `TextureTarget` / `TemporaryTarget` | the descriptor's declared `Size()` |
| `NoTarget()` | the **depth** attachment's size |

The `NoTarget()` row is load-bearing: a shadow pass has no colour target, and
falling through to the screen sentinel would build its frustum from the window's
aspect and silently drop casters. `DepthAuto` with `NoTarget()` is already a
reported error, so there is no fourth case
([Extension points: shadows, post-processing, IBL](https://github.com/dvoyni/cog/issues/23)).

**A screen-targeted camera renders at framebuffer resolution**, not logical
viewport resolution — all three candidate aspect sources are provably equal, so
this is purely a sharpness choice and sharp is the right default. There is **no
`RenderScale` field**: half-resolution 3D is already expressible as a smaller
`TemporaryTarget` composited by canvas.

**There is no `Viewport` rectangle and no projection escape hatch.** A
projection-baked sub-rect does not clip — a point at NDC x = 1.5, which the
clipper would have discarded, is remapped to 0.25 and rasterises into the
neighbouring camera's half — and gfx exposes no scissor. Split-screen, minimap,
picture-in-picture and render scale all go through **one `TemporaryTarget` per
camera, drawn by canvas as a sprite**, which can additionally be bordered,
rounded, faded and animated. A raw `ProjectionMatrix` input is refused so the
frustum stays derivable from parameters and nobody hands in an OpenGL −1..1
depth matrix; the derived matrix is published as an **output** instead, see
[Coordinate helpers](#coordinate-helpers).

### Depth

Depth is conventional, **not reversed**: near → 0, far → 1, compare `Less`,
clear to **1.0**. Reverse-Z buys large-world precision but would break the
load-bearing property that every `gfx.MaterialState` zero value equals both the
WebGPU default and today's behaviour, and `Depth32F` has precision to spare at
demo scale.

`Near` and `Far` are **both required**; a zero in either is reported through
`kernel.ReportError` and the camera is skipped. Substituting a default would
hide a real caller bug behind a degenerate projection.

**Spec trap worth stating out loud:** under conventional depth the useful
`ClearDepth` is **1.0**. The naive `ClearDepth: m.Some[float32](0)` clears to the *near*
plane and hides the entire scene.

### Passes

```go
func defaultPass(id CameraID) Pass {
    return Pass{Tag: TagForward, ClearDepth: m.Some[float32](1), Order: gfx.Order(id)}
}
```

**A clear is an `m.Maybe`, not a pointer**, and its zero value is absent — so a
zero `Pass` preserves colour and depth exactly as the nil pointers it replaced
did, and a present zero (a clear to transparent black) stays distinct from an
absent one. The pointers made `Pass` mutable indirection, which kept a camera's
pass list out of an ECS Component for no reason a clear value has.

An empty `Passes` means exactly one pass: tag `forward`, screen target, `Order`
at the camera id, **colour preserved and depth cleared to 1.0**.

The asymmetry is deliberate. Defaulting to a **colour** clear would let a second
camera silently erase the first. But a `DepthAuto` pass shares its pooled depth
texture with every other same-size `DepthAuto` pass in the frame, **so it must
clear depth or it inherits garbage** — which would make the simplest possible
scene render against garbage depth. Two cameras compositing into one target
almost always want independent depth; the exception (a weapon view depth-tested
against the world) is exactly the case that should have to write an explicit
`Pass` with no `ClearDepth`.

**Clears live only on passes.** `CameraDescr` carries no clear fields; having
them on both with "the camera's are ignored when `Passes` is non-empty" is a
silent-override rule that produces bug reports.

**Store ops are inferred, not exposed.** `DepthStore = StoreKeep` iff the pass
names an explicit depth texture (you allocated it, you mean to sample it),
otherwise `StoreDiscard`; colour store is always `StoreKeep`. Every forward pass
therefore gets the tiled-GPU depth-discard win for free, a shadow pass gets the
store it needs, and there is no knob to set wrong. `LoadDiscard` on colour is
likewise not exposed: it saves a load only for a pass that provably covers its
whole target, which scene cannot know.

`Pass.Target` takes the gfx target handle **directly** — the screen sentinel, a
durable `ResourceQueue` texture, or a frame-local `q.TemporaryTarget` — passed
through untouched. Scene wraps it in no name registry shared with canvas. A pass
with `NoTarget()` and no explicit depth, or with `NoTarget()` and a present
`ClearColor`, is a reported error.

**A `NoTarget()` pass does not reach the GPU on the desktop backend, and that is
not scene's doing.** gogpu's Vulkan HAL returns a render-pass encoder *without
beginning a render pass* when the descriptor names no colour attachments, and
`End` then calls `vkCmdEndRenderPass` on a pass that was never begun — an access
violation inside the driver, several frames of stack from anything that names a
pass. It is in the pinned version and in the newest published one. `cog/extensions/gogpu`
therefore declines such a pass and reports `ErrDepthOnlyPassUnsupported` once
per run, which turns the segfault into a line a caller can read; a browser's own
WebGPU encodes the pass correctly, so the same build run through `cmd/web` does
execute it. The `cameras` demo is the first frame in the tree that emits one, and
it is deliberately arranged so the skip costs nothing: its prepass writes into a
depth texture **nothing else reads**.

That arrangement is the rule to carry, not an accident of one demo. **A pass
whose depth another pass loads with no `ClearDepth` renders against undefined
depth wherever the depth-only pass is skipped** — the whole minimap, not the one
draw. A depth-only pass may be *written* on any backend but its output may be
*depended on* only where the backend encodes it, which is exactly the constraint
shadow maps will have to negotiate when they land.

**Which backends encode it is a property of the HAL, not of the platform.**
Vulkan does, since `gogpu/wgpu#353` shipped in v0.34.5; a browser always did. The
GLES HAL does not, and it fails more quietly than Vulkan used to: it binds no
framebuffer for a colourless pass and draws into whatever was bound last, with
no fault and nothing reported. `cog/extensions/gogpu` therefore asks the selected backend
rather than the build tag, and **an unrecognised backend is treated as unable** —
a refused pass reports itself, an encoded one that does not work is a wrong
picture on someone else's machine.

Two smaller findings from the same investigation, both in gfx rather than in the
HAL:

- Every pipeline gfx built declared one colour target, whatever pass it was
  going to be set into, and a pipeline's targets are validated against the
  pass's attachments at `setPipeline`. `PipelineDesc.NoColorTarget` now says
  otherwise and is part of the pipeline cache key, so one shader drawn in both
  kinds of pass gets two pipelines. A backend that honours it builds no fragment
  stage, which is also what lets a depth-only shader declare no `fs_main` at all.
- `gfx.PassDesc` could not distinguish `NoTarget()` from a texture target whose
  view does not exist yet, because both resolve to a zero `TextureViewID` — and
  **every temporary target is unresolved on its first frame**, since its
  allocation is a bake the backend replays *after* the pass descriptors were
  built. A backend reading "depth-only" off a zero target therefore misfires on
  the first frame of every app that uses a render target, into the same fault.
  `gfx.PassDesc.NoColor` carries the distinction.

Scene synthesises `PassDescr.Label` from the camera id and tag for debugging; it
is not exposed.

**A pass with zero surviving draws is still emitted.** Skipping it would drop
its clears, making a camera's clear depend on whether anything was visible — an
intermittent bug that only shows when you turn away. gfx's merge rule collapses
an empty no-clear pass into its neighbour at no cost; an empty pass *with* a
clear is exactly the one that must survive.

`PassTag` stays a `string`, interned to an `int32` **once per pass**, so material
shader lookup is a slice index rather than a string-keyed map probe. Interning is
O(passes), not O(draws), so the readable API (`Tag: "forward"`) costs nothing and
needs no registration handshake.

### Depth sharing with canvas

Canvas declares `DepthAuto` on every pass it emits, so a screen-sized camera pass
and canvas share the **same pooled depth texture**. This is harmless today —
canvas's three built-in materials never test depth — but a caller-supplied
depth-testing material on canvas *will* interact with 3D depth. An app wanting
isolation gets it by not ordering the camera adjacent
([Canvas layer mapping onto gfx pass order](https://github.com/dvoyni/cog/issues/27)).

---

## Materials and pass tags

([Extension points: shadows, post-processing, IBL](https://github.com/dvoyni/cog/issues/23),
[Scene recording API sketch](https://github.com/dvoyni/cog/issues/12),
[Pipeline state growth for 3D](https://github.com/dvoyni/cog/issues/10))

```go
// MaterialTag binds one pass tag to the gfx material that serves it.
type MaterialTag struct {
    Tag   PassTag // zero reads as TagForward
    Descr gfx.MaterialDescr
}

// Material is a scene material: the gfx materials it serves, one per pass tag.
// A pass whose tag has no entry skips every draw using this material.
// A nil Material is the bundled PBR.
type Material []MaterialTag
```

`MeshDraw.Material` and `ModelDraw.Material` are `Material`, not
`*gfx.MaterialDescr`. Nil still means the bundled PBR, so every draw literal that
omits the field is untouched, and the hand-written one-entry case is
`Material: scene.Material{{Descr: d}}`.

**Tag participation is purely a material property.** A draw gets no say in which
passes it appears in (the Unity LightMode pattern); layers give per-camera
exclusion and the pass list gives per-pass control. A model lacking an entry for
a pass's tag is **skipped** in that pass.

**A tag entry is a whole `gfx.MaterialDescr`, not a shader.** Two independent
findings force this:

- Pipeline state is strictly per material with no pass or draw override, so
  `Cull` and `DepthCompare` must vary per tag — a shadow pass gets its cull from
  its own tag entry.
- A declared-but-unused WGSL binding is still reflected, must be bound, and
  silently voids **the whole frame's command buffer** if it is not. So the
  *parameter set* is tag-specific too: an `alphaMode: MASK` shadow shader
  declares `baseColorTexture` and `alphaCutoff`; an `OPAQUE` one declares
  neither. Only a whole `MaterialDescr` per tag carries both the state and
  exactly the parameters that entry's shader declares.

A **duplicate tag** in one `Material` is reported through `kernel.ReportError`
and the first entry wins, matching the duplicate-`CameraID` ruling. The check
runs at intern time over a slice of one or two entries, not per draw.

**Lookup is a slice index.** Pass tags intern to an `int32` once per pass;
caller materials intern once per frame; at material-intern time scene resolves
the entry index for each interned tag id into a small fixed array, so the
per-draw per-pass cost is one array read and a negative-means-skip test — no
string compare, no map probe.

In v1 the only tag is `forward`, and the bundled PBR is a `Material` with one
`forward` entry. The shape is paid for now rather than broken later: when
shadows land they add a `shadow` entry to that same value, and every draw that
passed nil gains shadow casting **with no call-site change**.

### Materials are copied at record

**`Mesh` and `Model` copy a draw's `Material` into the frame's arenas when the
call is made** — its tag entries into one arena, each entry's parameters into the
parameter arena `Params` and `OverrideParams` already use — exactly as every other
slice on those calls is copied. The flush reads the copy. It used to read the
caller's slices, which obliged every caller to keep a material alive and
unchanged until the flush, with nothing anywhere saying so; that obligation is
gone, and a caller may reuse or rewrite a material the moment the call returns.
A nil `Material` stays nil, because nil is the bundled PBR.

**The bytes a parameter carries are not copied.** A texture's pixels, a buffer's
contents and a raw parameter's layout are `assets.Blob`s, static by contract, so the
copy is of descriptors and never of megabytes.

**Batching is unchanged.** A caller material is interned per frame by content
(see [The sort key](#the-sort-key)), and the content key never depended on where
the descriptors live, so a copied material batches with its equals exactly as
the original did.

**A material is copied once per frame, not once per draw**
([#314](https://github.com/dvoyni/cog/issues/314)). The recording takes the
material's content key first and looks it up among the frame's copies; a hit
hands the draw the earlier copy and copies nothing. The lookup is by content and
never by the caller's slice, because a caller may rewrite a shared material
between two draws of one frame and the later draw must see the rewrite — keyed
by slice identity it would be handed the earlier draw's copy, which is the very
bug the copy exists to close. Two materials whose keys collide share one copy,
which adds no risk: the flush interns by that same key and would draw both with
the first either way. The copies are forgotten when the recording resets, since
the arenas they point into are rewritten from the next frame on.

The key is not paid twice. The draw record carries it to the flush, which
interns by it without fingerprinting again; only a draw the recording did not
key — a model's own glTF materials — is keyed at the flush.

**What it replaced, and what it costs.** Two alternatives were measured first
and refused, and their per-draw numbers are what the frame benchmark below is
checked against. Both come from a throwaway microbenchmark over a
four-parameter PBR material (one texture, one colour, two floats), 2 000 000
iterations, medians of five, AMD Ryzen 9 7950X3D, Go 1.27.1, windows/amd64:

| per draw | ns | allocs | at 5 000 draws |
| --- | --- | --- | --- |
| `Material.key()`, the content key paid per draw naming a material — at the flush then, at record since [#314](https://github.com/dvoyni/cog/issues/314) | **106** | 0 | 0.53 ms |
| rebuilding a whole material into a frame arena that keeps its backing | **62** | 0 | 0.31 ms |

A durable `MaterialRef` — a handle baked once, whose key is computed at bake —
would have skipped the key. It was refused for one route instead of two: a bake
step and a durable table for something a draw can say by value
([#307](https://github.com/dvoyni/cog/issues/307)); it is reopened only if the
frame benchmark asks ([#311](https://github.com/dvoyni/cog/issues/311)).

### The frame benchmark

`BenchmarkFrame` in `framebench_test.go` records and flushes **5 000 separately
recorded `Mesh` draws** — the shape of an ECS System recording one draw per
Entity — through the kernel, the flush, gfx's translation and a backend that
draws nothing. The three cases differ only in the material each draw names:
**none** (the bundled PBR); **shared** (one caller `Material` built once and named
by every draw: the bundled PBR's own eleven parameters over scene's baked
defaults, plus one float); and **override** (that material plus a one-float
`Params` override, the same value on every draw).

Measured before and after the record-time copy, both test binaries built first
and ten runs of each interleaved, 60 frames a run, medians; AMD Ryzen 9 7950X3D,
Go 1.27.1, windows/amd64:

| per frame | before: ms | after: ms | Δ per draw | B | allocs | batches |
| --- | --- | --- | --- | --- | --- | --- |
| none | 12.75 | 12.39 | −71 ns (noise) | 15.0 MB | 5 028 | 5 000 |
| shared | 13.71 | 15.16 | **+291 ns** | 14.7 MB | 27 | 5 000 |
| override | 14.56 | 16.46 | **+380 ns** | 14.7 MB | 27 | 5 000 |

Four things are worth carrying out of it.

- **The copy is not free, and it costs more than the arena-rebuild row
  predicted.** A `gfx.ParameterDescr` is 328 B, so the shared material's eleven
  parameters are 3.6 KB of descriptors per draw — 18 MB a frame of copying at
  5 000 draws, against the four-parameter material the 62 ns row measured. The
  run ranges do not overlap, so +10.6% and +13.1% of a frame are real rather than
  run order. Copying a shared material once per frame instead of once per draw
  is [#314](https://github.com/dvoyni/cog/issues/314), measured below.
- **The content key is not where a material's frame cost is.** Before the copy,
  naming a shared material cost 192 ns a draw over the bundled PBR, against the
  106 ns the key alone was predicted at — key, intern and a material record per
  batch together.
- **Allocation is flat.** The copy lands in arenas that keep their backing
  across frames, so it adds no allocation in either case. (The bundled-PBR case's
  5 028 allocations a frame predate this work and are not the material path.)
- **Every draw is its own batch** in all three cases, before and after: separately
  recorded draws are not merged, which is the deferred automatic collapse
  ([#49](https://github.com/dvoyni/cog/issues/49)), and the copy changes nothing
  about batching.

**Once per frame** ([#314](https://github.com/dvoyni/cog/issues/314)). Measured
before and after the recording started reusing a frame's copy by content key and
handing that key to the flush, by the same method: both binaries built first, ten
runs of each interleaved, 60 frames a run, medians with the run range; same
machine and Go:

| per frame | before: ms | after: ms | Δ per draw | B | allocs | batches |
| --- | --- | --- | --- | --- | --- | --- |
| none | 13.24 (12.95–13.43) | 13.18 (12.92–13.71) | −12 ns (noise) | 15.0 MB | 5 028 | 5 000 |
| shared | 16.14 (15.74–16.19) | 14.16 (13.96–14.30) | **−396 ns** | 14.7 MB | 27–29 | 5 000 |
| override | 17.63 (17.23–18.21) | 15.26 (15.04–15.39) | **−475 ns** | 14.7 MB | 27–28 | 5 000 |

- **It wins back more than the copy cost.** The "before" column sits about 6%
  above the record-time copy's own "after" run on the same machine, which is the
  run-to-run drift whole-frame numbers carry; within this run the ranges do not
  overlap, and the shared case is 12% of a frame faster, the override case 13%.
  Keying 5 000 draws at record and probing a map is cheaper than 5 000 copies of
  3.6 KB of descriptors, and the flush no longer keys at all.
- **Allocation and batching are unchanged.** The copies map keeps its buckets
  across frames; the allocation count wanders between 27 and 29 in both binaries.
  Every draw is still its own batch.

**One trap in writing such a benchmark.** A material whose textures are inline
bytes measures gfx rather than scene: gfx bakes an inline texture into a
temporary per draw, and deduplicating 25 000 of those per frame took two thirds
of a first attempt's CPU samples and ran the shared case to 80 ms. The shared
material binds baked textures for that reason.

---

## glTF models

([glTF model draw semantics and loading](https://github.com/dvoyni/cog/issues/14),
[glTF 2.0 feature inventory and loader choice](https://github.com/dvoyni/cog/issues/5))

```go
type ModelDraw struct {
    Transform      m.Transform
    Transforms     []m.Transform // non-empty overrides Transform, one instance per entry
    Scene          string      // entry in the file's scenes array; empty is the default scene
    Node           string      // subtree within that scene; empty is the whole scene
    Plays          []model.ClipPlay
    MorphWeights   []float32
    Material       Material    // nil is the bundled PBR
    OverrideParams []gfx.ParameterDescr
}
```

> **Amended by [#539](https://github.com/dvoyni/cog/issues/539).** A model
> draw is scene's; everything a model file is, is `model`'s. The loader,
> addressing by `Scene` and `Node`, flattening, the file's materials and the
> two override knobs, and loading, residency, textures and unloading moved to
> [model.md §glTF models](../../../model/docs/specs/model.md#gltf-models). scene records the draw,
> resolves its selectors through the Lookup's `ModelView` on the load facade,
> and expands the view into one draw record a primitive.

---

## Buffer-built meshes

([Buffer-built models: static and dynamic](https://github.com/dvoyni/cog/issues/22),
[Buffer update strategy: static, per-frame, and dynamic](https://github.com/dvoyni/cog/issues/3))

**The line to remember: `Mesh` covers "the vertices change", `Model` covers "the
vertices are deformed by weights or bones".** A caller wanting deforming
procedural geometry owns its vertices and uses `UpdateMesh`, blending on the CPU.

```go
func (q *OpQueue) TemporaryMesh[TVertex model.VertexLayout](vertices []TVertex, indices []uint32, topology gfx.PrimitiveTopology) model.MeshRef

type MeshDraw struct {
    Transform  m.Transform
    Transforms []m.Transform // non-empty overrides Transform
    Material   Material    // nil is the bundled PBR
    Params     []gfx.ParameterDescr
    Bounds     m.Vec4      // xyz centre, w radius, local space
    NeverCull  bool
}
```

`model.MeshRef` is an opaque value struct with unexported fields and a **discriminated
source** (durable or frame-local), a dense scene id, and a generation counter —
mirroring `gfx.BufferDescr`, the only place in cog that already serves both a
frame-local and a durable path from one type. The zero value means none. `source`
is what makes a temporary ref used in a **later** frame detectable rather than
silently wrong.

The two sources allocate **independent dense ranges**, so `ID()` sets the top bit
for a temporary one. The sort key is a single `uint32` of `meshID`, and without a
bit to tell them apart the first durable and the first temporary mesh of a frame
would batch as though they were the same geometry. The generation of a temporary
ref is the **frame** it was minted in, which is what the later-frame check reads;
a durable ref's is its slot's reissue count.

Both mint functions - scene's `TemporaryMesh` and `model`'s durable `BakeMesh`
on `model.LookupAccess` - feed the single `q.Mesh(layers, ref, draw)` recording call.
An anonymous inline call, canvas's `DrawTriangles` shape, was rejected because
sorting requires a dense `meshID` on every draw and an anonymous call has none.
The inline path costs one extra statement and buys one `MeshDraw`, one sort key
and one culling rule instead of two of each — affordable because scene's
*throwaway geometry* floor is the debug vocabulary, not this API.

> **Amended by [#539](https://github.com/dvoyni/cog/issues/539).** `MeshRef`,
> `BakeMesh`, `UpdateMesh` and `ReleaseMesh` are `model`'s: it owns all mesh
> residency, and scene keeps only the frame-local source `TemporaryMesh` mints
> and the `MeshDraw` recording. Vertices, topology and indices, deferred
> baking and buffer lifetimes moved to
> [model.md §Buffer-built meshes](../../../model/docs/specs/model.md#buffer-built-meshes), and the
> layouts row by row are in [`mesh.md`](../../../model/docs/specs/mesh.md).

---

## Animation

> **Amended by [#539](https://github.com/dvoyni/cog/issues/539).** Baked poses,
> clip plays and morph targets are `model`'s, and moved to
> [model.md §Animation](../../../model/docs/specs/model.md#animation). scene hands a draw's `Plays`
> and `MorphWeights` to `model.ResolvePlays`, `model.BlendMorphWeights` and
> `model.SelectMorphTargets`, and appends the block with `model.AppendAnim`.
> `MeshDraw` has no `Plays`: skinning is model-only.

---

## Lights

([Lighting model and limits](https://github.com/dvoyni/cog/issues/17))

> **Amended by [#539](https://github.com/dvoyni/cog/issues/539).** `LightDescr`,
> the packed light, attenuation, the cone, the sun and the cap of 16 by
> contribution are `model`'s, and moved to
> [model.md §Lights](../../../model/docs/specs/model.md#lights). What scene keeps is which lights a
> pass offers: its layer test and its frustum test, before `model`'s
> `LightSelection` applies the cap.

**Culling: per pass, at flush, before the cap.** Sphere `(Position, Range)`
against that pass's frustum. A camera's 1024×1024 shadow pass and its screen pass
can see different light sets, which is correct rather than surprising. Culling is
what makes the cap survivable: a level with 40 lights of which 6 are on screen
works perfectly.

**A light's `LayerMask` is filtered against the camera's `CullMask` only.** It
decides which cameras' light buffers the light lands in; it does **not** decide
which objects the light illuminates. This is the natural misreading and it must
be stated bluntly, because the other reading requires a per-draw light list,
which contradicts group 0 being invariant for a whole pass.

---

## Bundled PBR material

> **Amended by [#539](https://github.com/dvoyni/cog/issues/539).** The bundled
> PBR is `model`'s: its parameters, slots, defaults, BRDF, pipeline state and
> layouts moved to [model.md §Bundled PBR material](../../../model/docs/specs/model.md#bundled-pbr-material).
> scene wraps its forward descr in a `Material` with one `forward` entry, and
> `MeshDraw.Material` / `ModelDraw.Material` nil selects it.

---

## Sorting, culling, and batching

([Draw sorting, culling, and batching](https://github.com/dvoyni/cog/issues/20))

All of it happens in the update-thread flush. **Within a pass, recording order is
not preserved.**

### Sort classes

Two, not three. `alphaMode: MASK` is fixed-function-identical to `OPAQUE` plus a
shader `discard`, so **alpha-masked sorts with opaque**.

- **Opaque + mask** — sorted by material key only, **no depth term**.
- **Blend** — sorted **back-to-front** by view-space distance to the instance's
  bounding-sphere centre.

Front-to-back for opaque was rejected: with no depth prepass it buys only what
early-z catches unaided, and it directly fights instancing — two copies of the
same crate at different distances land far apart, so a 100-crate floor is one
draw or a hundred. The blend class contributes no state of its own, since
`StateTransparent3D` already sets depth-write off; **sorting is scene's entire
contribution to transparency**.

### The sort key

Scene assigns its own **dense `u32` ids** at load/bake — `meshID` per flattened
primitive, `materialID` per `(material, tag)` — so the common path is a field
read, not a map hit. Every mesh scene can draw comes from a scene-owned handle: a
model primitive, a `MeshRef`, or a built-in unit mesh. The single exception is a
caller-supplied `gfx.MaterialDescr`, which has no `ID()`; those are **interned
per frame** by a fingerprint of shader source-or-path + `MaterialState` +
parameter bytes, costing one map hit only for draws that pass one. The recording
takes that fingerprint when it copies the material and the draw record carries
it, so the flush's hit does not fingerprint again.

`materialID` is per `(material, tag)` rather than per material, because the sort
key must distinguish the pipelines actually bound in that pass.

**Opaque and blend get separate arrays per pass**, emitted in that order, which
removes any class bit from the key. Each pass sorts a reused
`[]sortEntry{key uint64; draw uint32}` — the entries, not the draw structs, so
swaps are 12 bytes — through `slices.SortFunc`, in-place pdqsort allocating
nothing on a reused slice. Opaque key is `materialID<<32 | meshID`; blend key is
the view depth as a monotonically-flipped `uint32`, reversed. **Recording ordinal
is the comparator's final tiebreak**, so an unstable sort is still frame-to-frame
deterministic.

There is deliberately **no `paramsHash` in the key**: with the automatic collapse
deferred nothing reads it, and the collapse will check params over a run the way
canvas's `keyMatches` does — order-preserving, so it changes nothing.

**Scene needs none of gfx's ids.** `ShaderID`, `PipelineID` and `TextureID` are
assigned in `translate` on the render thread; scene never sees them.

### Culling

**Sphere against all six frustum planes**, using `m.Frustum` / `ContainsSphere`.
Far is included, which is why `Far` is required.

**Once per distinct frustum, memoised by `{cameraID, targetAspect}`** — normally
one cull per camera for all its passes; each pass then filters the survivor list
by pass tag.

**At draw granularity**, one sphere test per recorded draw; a surviving model
contributes all its primitives. Per-primitive culling would be tighter for one
large mesh, but `Node` re-rooting is already the answer to "split this file
apart".

**World radius is the local radius times `max(|sx|,|sy|,|sz|)`** of the packed
matrix — exact under a uniform scale, conservative under a non-uniform one,
since a sphere under non-uniform scale is not a sphere.

Bounds resolution, in order:

1. `MeshDraw.NeverCull` short-circuits.
2. An explicit non-zero `MeshDraw.Bounds`.
3. The mesh's baked sphere — **durable buffer-built meshes with the standard
   layout only**, where `POSITION` is known to be `Float32x3` at offset 0. A
   custom layout falls through, because scene cannot locate the positions at all.
   A **temporary mesh never auto-computes a sphere**: it is rebuilt every frame,
   so the O(n) pass would run every frame rather than once.
4. Otherwise **never cull**.

`ModelDraw` has no `Bounds` field: bounds are computed at load and expanded by
the summed max morph delta. **A skinned draw uses its bind-pose sphere and is
marked never-cull** — a bone can swing a vertex anywhere, and the tight version
(a per-joint influence radius unioned per clip across every baked frame) is real
load-time machinery whose demo justification is nil. Never-cull is reported
never, because it is the documented default rather than an error.

The reason a zero `Bounds` means never-cull rather than a zero-radius sphere at
the origin: a large mesh whose origin leaves the frustum would vanish — a silent,
camera-angle-dependent bug, the worst kind. **Drawing too much is a performance
problem you can see and profile.**

### Instancing

Every scene draw is instanced, with `instances = 1` in the degenerate case, and
`firstInstance` is load-bearing: WebGPU's `instance_index` starts *at*
`firstInstance`, so a batch reads its own slice of `sceneInstances` with no
offset plumbing. That machinery is built in full.

v1 does **not** collapse consecutive equal draws automatically. Instead
`Transforms []m.Transform` on `MeshDraw` and `ModelDraw` makes an instanced draw
**explicit** — a forest, a particle field, a tile floor — which is a feature
rather than an optimisation and so earns its place independently, while driving
the exact path the future collapser will drive. Because the sort ships in v1, the
collapse will be **output-identical** when it lands
([scene: collapse consecutive equal draws into instanced batches](https://github.com/dvoyni/cog/issues/49)).

- **The instances share the draw's `Plays` and `MorphWeights`**, so a hundred
  trees sway in lockstep and a hundred independently-animated characters need a
  hundred draws. Per-instance animation is out of scope.
- **Culling is per instance**, not all-or-nothing; survivors pack contiguously.
  The cost is N sphere tests — exactly what N separate draws would have paid.
- **A blend-class instanced draw splits into N single-instance sort entries.**
  Sorting the set by its nearest instance would composite visibly wrong.

The batch is **the call, not the key**. Each record an instanced call expands
into carries the call's group, and the flush packs a run of one group's
survivors as one batch — one draw call, one 256-byte material record, N
contiguous instances. Two separate calls of the same mesh and material stay two
batches, and so do a `WireBox`'s twelve edges; merging those is the deferred
automatic collapse, and doing it early here would make that ticket unfalsifiable.

A group's survivors arrive at the packer contiguous with nothing between them,
which is what lets the run be found with a scan rather than a second grouping
pass: the instances of one call share a mesh and a material and therefore a sort
key, the sort's final tiebreak is the recording ordinal, and every other draw was
recorded wholly before or wholly after the call.

**Batching is per pass**, because the sort is. An instanced crate two cameras
both see is packed twice, once into each pass's instance slice, and a camera with
a shadow pass and a forward pass packs it in both. Sharing one packing across
passes would mean one sort across passes, which is not what a sort is for.

### The instance record

`sceneInstance` is ~64 B: `{world: 3 × vec4, animOffset: u32, flags: u32, …}`,
with 8 spare bytes.

**It carries no normal matrix.** Under non-uniform scale, transforming a normal
by `world` is wrong — and scene generates that case itself, since `Line3D` and
`WireBox` are non-uniformly stretched boxes. The per-skin normal matrix covers
the joint half, not the instance half. So a `flags` bit, **`SCENE_NONUNIFORM`**,
is set at pack time when the packed matrix's scale is not uniform, and the shader
takes a ~30-ALU inverse-transpose only for those instances, branch-uniform across
the whole instance. A second `3 × vec4` in the record was rejected at 128 B —
every debug line paying for a case it does not have.

**The stretched debug boxes are not the case that motivates it** —
`instancing`'s eye criterion is, and the difference is worth writing down
because it is easy to get backwards. Every face normal of an axis-aligned box is
an eigenvector of an axis-aligned scale, so `world · n` and `(world⁻¹)ᵀ · n`
point the same way and differ only in length, which normalising removes. A
stretched `Line3D`, a `WireBox` edge and a slab-shaped `Box` therefore shade
*identically* with the flag and without it, however non-uniform they are — and a
rotation does not change that, since `R·S·n ∝ R·n ∝ R·S⁻¹·n` for an eigenvector
`n`. Setting the flag for them is still right; it is simply not observable
there. What is observable is a **curved** surface under a caller's non-uniform
`Scale`: a squashed bottle loses its specular highlight outright
when the flag does not reach the shader, which is what `instancing` shows
([demo: instancing](https://github.com/dvoyni/cog/issues/96)).

**`SCENE_NOSKIN`** is the second flag, set for every non-model draw. A
buffer-built mesh and the debug vocabulary have no group 2, but a declared
binding must still be bound or the whole frame dies, so scene binds one shared
**null skin** (a single identity pose row plus one-element inverse-bind,
normal-matrix and morph-delta arrays). Riding the free rest-frame path instead
would be correct with no new mechanism, but it charges a procedural terrain mesh
— the highest-vertex-count thing scene can be handed — a full per-vertex pose
fetch and TRS blend for a guaranteed identity. Every buffer-built draw shares
that one bind group, so they batch together instead of fragmenting group 2.

`animOffset == SCENE_NO_ANIM` skips the animation path entirely.

### Buffers

**One `sceneInstances` arena for the whole frame**, uploaded with a single
`BufferWithBytes`, each pass binding its own slice through `BufferRangeParam`.
`firstInstance` then indexes within the pass's bound range, so WGSL's
`instance_index` stays pass-relative and the shader is unchanged. Same arena
discipline for `sceneAnim` and the per-pass `sceneFrame` blocks. One upload per
frame instead of one per pass, and the arenas reuse their backing across frames
the way canvas's batch slices do.

Sorting is per pass, so a crate visible to two cameras is packed twice regardless.

---

## Lookup facade

> **Amended by [#539](https://github.com/dvoyni/cog/issues/539).** The Lookup
> and its facades are `model`'s: `*model.Lookup`, `model.NewLookupAccess`,
> `model.NewLookupDeviceAccess` and the read facade. The queries, their
> `(value, ok)` contract and the failure edges moved to [model.md §Lookup facade](../../../model/docs/specs/model.md#lookup-facade).
> scene's flush holds the Lookup for writing, loads a model on its first draw
> and drains the bake and release queues.

---

## Coordinate helpers

([3D-to-screen coordinate helpers](https://github.com/dvoyni/cog/issues/38))

**Pure package-level functions**, callable on any thread with no plugin instance.
A lookup against last frame's resolved camera state would buy only staleness, a
`LookupAccess` dependency in code that is otherwise arithmetic, and nothing at
all for a camera not recorded this frame. `PassView.Frustum` stays an inspection
and test surface, not a coordinate API.

```go
// viewport is the target's size in pixels: gfx.Viewport.Width/Height for a
// screen camera, the texture size for a TemporaryTarget camera.
func ViewProjection(camera CameraDescr, viewport m.Vec2) m.Mat4
func WorldToScreen(camera CameraDescr, viewport m.Vec2, world m.Vec3) (m.Vec3, bool)
func ScreenToWorld(camera CameraDescr, viewport m.Vec2, screen m.Vec3) (m.Vec3, bool)
func ScreenToRay(camera CameraDescr, viewport m.Vec2, screen m.Vec2) (m.Ray, bool)
```

**Per target, not per camera.** "Where is this point on screen for a camera that
renders both a 1024×1024 shadow map and the window" is an ill-formed question; the
caller names the size it means. The parameter is an `m.Vec2` **size** in all four
signatures rather than an aspect on one and a size on the rest — one spelling of
one concept is worth a wasted division. A `TemporaryTarget` camera returns
**texture pixels**, which canvas then maps to the screen with its own
`WorldToScreen`.

**Screen is logical viewport coordinates, origin top-left, Y down**, which is
what `ui` and pointer handling already use. WebGPU NDC is Y-up and origin-centre,
so the helpers flip:

```
x = (ndc.x + 1) * 0.5 * viewport.X
y = (1 - ndc.y) * 0.5 * viewport.Y
```

**This is the one place scene flips Y**, and it contradicts the render-pipeline
finding that "no Y flip is needed for scene". Both facts are true — one of the
pipeline, one of these helpers — and they are recorded together so nobody deletes
the flip on the strength of the other sentence
([m package 3D math coverage](https://github.com/dvoyni/cog/issues/7)).

`WorldToScreen`'s X/Y are target pixels and its Z is the WebGPU 0..1 NDC depth,
which is exactly what `ScreenToWorld` takes back, so
`ScreenToWorld(c, vp, WorldToScreen(c, vp, p))` round-trips — the one assertion
that catches a sign error, and it needs no GPU.

### The `ok` contract

`ok` means **"this value is real"**, the facade's contract verbatim.
`WorldToScreen` returns `false` when the point is at or behind the eye plane
(`w <= eps`) or the camera or viewport is degenerate; `ScreenToWorld` and
`ScreenToRay` return `false` for the degenerate case only. Orthographic never
fails the `w` test.

- **Off-screen but in front stays `true`.** The coordinate is extrapolated past
  the target edge and is correct there — off-screen indicator arrows are the
  second most common use of this helper. Depth outside `Near`/`Far` likewise
  stays `true`.
- **Behind the camera never returns a coordinate.** Dividing by a negative `w`
  yields a plausible, mirrored, confidently wrong point, and it is the single
  classic bug in this helper. No `NaN` sentinel either: a bool the compiler makes
  you look at beats a value that silently propagates.

**Degenerate input is silent.** A zero-area viewport, a zero or equal
`Near`/`Far`, a zero `FovY` or `Height`: the helper returns the zero value with
`ok = false` and `ViewProjection` returns the identity, exactly as
`canvas.LayerTransform` returns identity for a zero-area window. A pure function
has no kernel handle; the "a zero `Near`/`Far` is a reported error" diagnostic
happens at flush inside the plugin, which is where a caller who forgot `Far` will
actually see it.

### The ray and picking

`m.Ray.Dir` is **unit length**, because every intersect's `t` is a world distance
only if it is. Three intersects, one per geometry type:

- `IntersectPlane` — cursor-to-ground for a strategy game is a ray against
  `y = 0`; nothing to do with picking.
- `IntersectSphere` — pairs with `Bounds(ModelRef)` and `m.Sphere.Transform`:
  transform the sphere into world space, test there.
- `IntersectBox3` — pairs with `AABB(ModelRef)`, which is **local space
  post-re-rooting**. An AABB rotated into world space is no longer axis-aligned,
  so the only correct test is the ray in local space:
  `ray.Transform(m.InverseAffine(modelMatrix))`. `m.Box3.Transform` exists but
  re-fits a looser world box and would report hits on empty space; it is for
  growing bounds, not testing them. `t` survives a rigid transform unchanged and
  does not survive a scaling one, since `Dir` is re-normalised.

Rules: a hit **behind the origin is rejected**; a ray **starting inside** a
sphere or box returns `t = 0, ok = true`; a ray **parallel** to a plane returns
`false`, including when it lies in the plane. There is **no `IntersectTriangle`**
— `m` has no triangle type and the facade cut the mesh queries that would feed
one.

**Object picking stays out of scope, and not only by decree: scene has no list to
raycast against.** Draws are frame-local and consumed at flush, and the retained
`PassView`/`BatchView` carry dense integers, not model refs or world transforms.
A `scene.Raycast(ray)` would have to retain a whole second structure that exists
for nothing else. What a caller writes instead is a loop over its own entities —
which it has and scene does not — calling `Bounds` or `AABB`, transforming, and
keeping the smallest `t`. About ten lines, and the `cameras` demo ships it.

### Cost

One-shot calls only; there is no cached camera-view value. `ViewProjection` is
published precisely so a caller with many points drops to `m.Project` in a loop
over one matrix, so the fast path needs no new API and no staleness question.
Publishing the derived matrix as an **output** forecloses nothing that refusing a
`ProjectionMatrix` **input** protected, and it is the single place the
FovY-is-vertical rule, the `Transform` inversion, the ignored camera scale and
the 0..1 depth convention are encoded.

**A perspective view-projection is not affine**, so `m.InverseAffine` does not
apply and the general `m.Mat4.Inverse` allocates five slices per call —
`ScreenToRay` and `ScreenToWorld` therefore allocate once per call. This is a
note, not a blocker; an implementation may build the inverse in closed form from
the camera parameters instead, a private optimisation with no contract attached.

---

## Shader-side contract

> **Amended by [#539](https://github.com/dvoyni/cog/issues/539).** The shader
> and every record it reads are `model`'s. The group convention, the bindings,
> the `sceneAnim` block and the WGSL functions moved to [model.md §Shader-side contract](../../../model/docs/specs/model.md#shader-side-contract).
> scene fills the view fields of each pass's frame block and appends what
> `model`'s packers return into its own arenas.

---

## Required engine changes

Scene cannot be correct without these. They are grouped by module and each is
traced to the ticket that decided it.

### `m`

([m package 3D math coverage](https://github.com/dvoyni/cog/issues/7),
[3D-to-screen coordinate helpers](https://github.com/dvoyni/cog/issues/38),
[Canvas colour-space migration](https://github.com/dvoyni/cog/issues/33))

Conventions are already correct and match both targets, confirmed by probe rather
than reading: `Mat4` column-major with translation in `m[12..14]`; `A.Mul(B)`
applies `B` first to column vectors and `Quat.Mul` follows the same rule;
rotations right-handed; `LookAt4` yields a −Z-forward, +Y-up view basis;
`Quat{X,Y,Z,W}` matches glTF's `rotation` order; **`Perspective4` already emits
0..1 clip depth**, i.e. WebGPU clip space as-is; gfx uploads `Mat4` in array
order, which is WGSL `mat4x4<f32>` layout, so no transpose.

Additions:

- `Orthographic4` and `Oblique4` — the same volume with view-space depth
  sheared into screen-up by a gain; `Orthographic4` is `Oblique4` at gain 0
- `TRS4` composition and `Mat4.Decompose` — the existing `QuatFromMat4` assumes
  an unscaled rotation and returns a **wrong quaternion for scaled matrices**
- `Mat4.TransformPoint` / `TransformDirection`
- `Mat4.Mat3()` and `Mat4.Translation()` for normal matrices
- `InverseAffine` — allocation-free; the generic `Inverse` allocates five slices
  per call
- `Plane`, `Frustum`, `FrustumFromMat4`, `Frustum.ContainsSphere`
- `Sphere.Transform`, `Box3` → `Sphere` for glTF accessor bounds
- `Project(viewProjection, world) (ndc Vec3, ok bool)` — `ok = false` when
  `w <= eps` — and `Unproject(inverseViewProjection, ndc) Vec3`
- `Ray{Origin, Dir}` with `NewRay` (normalises), `At`, `Transform`,
  `Mat4.TransformRay`, `IntersectSphere`, `IntersectPlane`, `IntersectBox3`
- `Maybe[T]` — an inline optional whose zero value is absent, which is what a
  `Pass` clear is, so a pass holds no pointer
  ([#308](https://github.com/dvoyni/cog/issues/308))
- `Blob` — a `[]byte` static by contract, which is what gfx's descriptors hold
  their pixels, buffer bytes and raw parameter layouts in, so the ECS can admit
  them ([#308](https://github.com/dvoyni/cog/issues/308))

Fixes:

- `LookAt4` falls back to another `up` when the given one is parallel to forward.

Colour, from the linear migration:

- `Color` fields hold **linear** components, documented as such, still exported.
- **Bare `NewColor` and `NewColor8` are deleted**, replaced by `NewColorLinear`,
  `NewColorSrgb` and `NewColorSrgb8`, with `Srgb()` and `Srgb8()` as inverses.
  All ~57 call sites across cog, feuds-26 and cog-examples become compile errors
  that must be classified by hand. This is the same lever that deleting
  `DepthTest` pulls: on a semantic flip the danger lives entirely in sites that
  still compile and now mean something else. A constructor without an inverse
  makes every test assertion a hand-computed magic number, which is how a
  transfer-function bug survives its own test.
- **Alpha never converts, in any constructor.**
- `NewColorHSLA` and `Hsla()` keep HSL as an **sRGB-space** model, converting at
  the boundary: "lightness 0.5" keeps meaning what every colour picker shows.
- `Lerp`, `Mul`, `MulS` and `Add` run in **linear**, with no perceptual variant.
  They are light-transport operations. The cost is real and accepted: a
  black→white fade now passes through 73% grey instead of 50%, and every
  `anim.LerpColor` tween and `ui` crossfade shifts with it. Splitting the op by
  caller would mean two functions differing invisibly.

Deferred and **not** spec gaps: `Quat.LookRotation`, infinite/reverse-Z
perspective.

### `gfx`

Passes and targets
([gfx render passes and render targets](https://github.com/dvoyni/cog/issues/8),
[Camera model](https://github.com/dvoyni/cog/issues/13),
[Extension points](https://github.com/dvoyni/cog/issues/23),
[Canvas layer mapping onto gfx pass order](https://github.com/dvoyni/cog/issues/27)):

```go
type Order int // the frame's shared pass ordering space

type PassDescr struct {
    Order      Order
    Target     TargetDescr  // ScreenTarget() | TextureTarget(tex, mip, layer) | NoTarget()
    Depth      DepthDescr   // DepthAuto() | DepthNone() | DepthTarget(tex)
    Load       LoadOp       // LoadClear | LoadPreserve | LoadDiscard
    Clear      m.Color
    Store      StoreOp      // StoreKeep | StoreDiscard
    DepthLoad  LoadOp
    DepthClear float32
    DepthStore StoreOp
    Label      string
}

ref := q.Pass(desc)  // declare + select
q.SetPass(ref)       // re-select a pass declared earlier this frame
q.TemporaryTarget(w, h int, format TextureFormat) (TargetDescr, TextureDescr)
func (d TextureDescr) Size() (w, h int)
```

**Amended by [gfx: a `TemporaryTarget`'s texture cannot be sampled](https://github.com/dvoyni/cog/issues/111):
`TemporaryTarget` returns both handles, not just the target.** As first written it
returned a `TargetDescr` alone, and a `TargetDescr` is write-only — it names an
attachment and exposes only `Size`, `IsScreen` and `IsNone`, with no accessor to the
texture and no public constructor from an id. So the frame-local render-then-sample
round trip this spec names as the sanctioned mechanism in three places — post-processing,
split-screen, and `cameras`'s composited viewports — was unexpressible: nothing could
sample what the pass had just rendered. Returning the texture beside the target keeps
`TargetDescr` write-only and matches how `TextureTarget` already reads, rather than
making the target a two-way type. Both values are always wanted, because sampling the
result is the only reason the allocation exists.

**A draw still may not sample the attachment its own pass renders into**
(`ErrDrawSamplesAttachment`); that guard is what makes handing the texture back safe.

- Passes are **frame-local state on `OpQueue`**, declared and selected in one
  call; subsequent ops append to the selected pass. `Draw`'s signature is
  untouched.
- **`Order` is explicit and gfx stable-sorts by it**, ties broken by declaration
  sequence — never stream order, because canvas and scene record from separate
  `app.UpdateEvent` subscriptions and defining order between them would need a
  `Before`/`After` edge naming a sibling.
- **Adjacent passes merge** iff they share a colour target, share depth
  attachment identity (two `DepthAuto` at the same size count as the same), the
  successor loads `LoadPreserve` on **both** attachments, and the predecessor
  stores `StoreKeep` on both. A `LoadPreserve`-on-both successor is by definition
  indistinguishable from continuing the previous pass, so this is a pure
  optimisation that provably cannot change results — and it is what makes
  canvas's per-layer pass run cost one GPU pass.
- **`ScreenTarget()` is a sentinel** the recorder cannot resolve: the swapchain
  view is per-frame and known only on the render thread. This is forced.
- Depth: `DepthAuto` (backend-owned, now a **size-keyed map** because several
  target sizes coexist per frame — **so a `DepthAuto` pass shares its texture
  with every other same-size `DepthAuto` pass and must clear depth or inherit
  garbage**), `DepthNone`, `DepthTarget(tex)`.
- **A pass executes iff it has an effect**: any attachment loading `LoadClear` or
  `LoadDiscard` makes it observable. "Clear this target and nothing else" is a
  legitimate frame, and so is a camera that culled everything.
- **Ordering is the only intra-frame read-after-write guarantee**; gfx builds no
  dependency graph. One debug validation: reject a draw that samples a texture
  currently bound as its own pass's attachment.
- **Corrected by [gfx/wgpu: no barrier between a pass that renders a texture and
  a later pass that samples it](https://github.com/dvoyni/cog/issues/112): the
  runtime inserts no barriers, so gfx places them.** This spec said "the whole
  frame stays one command encoder and one submit, so WebGPU inserts the
  barriers". That is false of this backend. `gogpu/wgpu` tracks resources for
  lifetime and for submit-time validation only and derives *no* barriers from
  that tracking — not within one command encoder, and not across a submit
  boundary either. A texture written as a colour or depth attachment and then
  sampled goes straight from `RenderAttachment` to `TextureBinding` with nothing
  ordering the read against the writes, and on Vulkan the sample reads the image
  mid-write. Being one encoder is exactly what makes it *look* safe.
  - The hazard is the worst kind to ship undetected: the symptom is a flicker
    that **looks like a random CPU/GPU race and is not one**, and it is
    invisible to any capture taken while the app is redrawing at speed. So the
    barrier lands *with* render-to-texture, never after it.
  - gfx is the layer that can see it. By translation time the frame's passes are
    sorted and merged, so the write-then-read pairs are computable, and
    `translatePasses` emits a `TextureTransition` for each. The rule is not
    "every render target gets a barrier" but **every write-then-read pair, in
    either direction**: a camera rendering into a temporary target a later pass
    composites is write-then-read, and a post-processing chain ping-ponging two
    targets is also read-then-write. The usage a texture is currently in is
    tracked per frame rather than assumed, because **a layout transition naming
    the wrong old layout is undefined behaviour, not a wasted instruction** —
    which is also why a texture that was uploaded rather than rendered into is
    never transitioned at all.
  - Barriers sit *outside* the pass, because that is the only place one can be
    recorded, and a texture already in the usage it needs pays nothing.
  - The frame buffer is the exception gfx never names: `ScreenTarget()` carries
    no texture id, so the present pass's own transition stays the backend's
    ([#103](https://github.com/dvoyni/cog/issues/103)).
- **The implicit default pass is deleted**, and `OpQueue.Clear` / `ClearDepth`
  with it — canvas was the only recorder and now declares its own passes. Every
  gfx draw names a pass.
- **No per-pass viewport or scissor, no MRT, no MSAA in v1.**

Backend contract:

```go
Execute(queue *Queue)                        // was Execute(TextureViewID, *GpuQueue)
TextureView(TextureID, mip, layer int) TextureViewID

type PassSink interface {
    BeginPass(PassDesc) RenderPass
    EndPass(RenderPass)
    TransitionTextures([]TextureTransition)  // #112; never called with an empty slice
}
func (q *Queue) ReplayPasses(sink PassSink)           // replaces ReplayRenderPass
```

Sink-driven, matching the one existing convention. Because `BeginPass` *returns*
the `RenderPass`, the backend still owns encoder and pass lifetime entirely.
`gfx.Queue` grows a pass list; bakes stay hoisted ahead of all passes.

Pipeline state ([Pipeline state growth for 3D](https://github.com/dvoyni/cog/issues/10)):

```go
type MaterialState struct {
    Blend        BlendMode
    DepthCompare CompareFunc // zero: CompareAlways — no depth test
    DepthWrite   bool
    Cull         CullMode    // zero: CullNone
    FrontFace    FrontFace   // zero: FrontCCW
}
```

- **`DepthTest bool` is deleted, not kept alongside.** It coupled write to
  compare, so the two states 3D needs most were inexpressible: *test but do not
  write*, which is the entire transparent pass, and *test with another compare*,
  which a skybox at depth 1 and reverse-Z need. Deleting it turns all four
  existing `DepthTest: false` literals into **compile errors**, all four in
  canvas — a silently reinterpreted bool is the failure mode worth avoiding.
- `CompareFunc` takes the full WebGPU set. `FrontFace` is not speculative: glTF
  requires reversed winding on negative-determinant node transforms.
- **Every zero value equals both the WebGPU default and today's hardcoded backend
  behaviour**, so `MaterialState{}` renders identically before and after.
- Named states join `Material()`: `StateOpaque3D` (`BlendOpaque`, `Less`, write,
  `CullBack`), `StateTransparent3D` (`BlendAlpha`, `Less`, no write, `CullNone`),
  `StateOverlay2D` (`BlendAlpha`, `Always`, no write, `CullNone`) — which is
  exactly what canvas's three materials spell out by hand today.
- **`pipelineKey` embeds `MaterialState` whole**, plus a colour-target format
  component (`FormatScreen` sentinel or a concrete format) and a depth-format
  component. The mistake to avoid is adding a state field and forgetting the key,
  which silently returns the wrong pipeline.
- **A live bug falls out:** `SetIndexBuffer` hardcodes `IndexFormatUint32` and
  `PrimitiveState` never sets `StripIndexFormat`, so an **indexed**
  `TopologyTriangleStrip` draw is invalid under WebGPU today. gfx sets it for
  strip topologies; the glTF loader converts strips to lists anyway.

Formats and samplers ([Texture formats and sampler growth](https://github.com/dvoyni/cog/issues/11)):

```go
const (
    FormatRGBA8     TextureFormat = iota // linear: normal, metallic-roughness, occlusion
    FormatRGBA8Srgb                      // sRGB: base colour, emissive, canvas atlas, frame buffer
    FormatDepth32F                       // renderable and sampleable
    FormatScreen                         // sentinel; resolves to FormatRGBA8Srgb
)

type AddressMode uint8
const (
    AddressClamp AddressMode = iota // 0 — unchanged zero value
    AddressRepeat
    AddressMirror
)

type SamplerDesc struct {
    AddressU, AddressV AddressMode
    Mag, Min, Mip      FilterMode  // zero = FilterLinear, unchanged
    Anisotropy         uint8       // 0 and 1 both mean off; clamped to 16
    Comparison         bool
    Compare            CompareFunc // ignored unless Comparison
    Label              string
}
```

- **The engine goes linear**: hardware sRGB decode/encode, linear blending
  everywhere. gogpu hardcodes `BGRA8Unorm` for the surface and exposes no
  `ViewFormats` on either path, and on web `bgra8unorm-srgb` is not a legal
  canvas-context format — so a hardware sRGB swapchain is unreachable.
  **`ScreenTarget()` therefore stops meaning the swapchain**: gfx allocates a
  frame-sized `FormatRGBA8Srgb` frame buffer, both recorders render into it, and
  gfx appends one implicit full-screen **present pass** applying the OETF into
  the real swapchain. Allocated lazily on first `ScreenTarget()` use and the
  present pass emitted iff it was allocated, which preserves the laziness rule.
  Cost is ~8 MiB at 1080p and one full-screen pass — which is also precisely the
  hook post-processing needs.
- **One depth format everywhere**, `FormatDepth32F`, including `DepthAuto`. This
  supersedes keeping `Depth24PlusStencil8`. **Stencil disappears entirely**,
  which also resolves the web `StencilReadOnly` trap for depth-only attachments.
- **Depth sampling and comparison samplers ship as capability**, no shadow
  implementation. Reflection learns `Depth` texture and `Comparison` sampler
  binding types instead of typing every texture `Float` and every sampler
  `Filtering`.
- `SamplerDesc` is **restructured, not extended** — the current shape has no room
  for `MIRRORED_REPEAT` and drives mag, min and mipmap from one field while glTF
  specifies them separately. `SamplerParam(name, SamplerDesc)`. `SamplerDesc{}`
  stays byte-identical to today and stays **comparable**, so it remains the dedup
  map key in `translate.go`. Encoding mirror as a clamp-bit-plus-repeat-bit pair
  was rejected for the same reason `DepthTest` was deleted.
- **Anisotropy is in v1** — ~6 lines in the backend, and the first large textured
  ground plane wants it. WebGPU requires mag/min/mip all linear when
  `maxAnisotropy > 1`; that is a debug validation, not a silent clamp.
- **`TextureWithResource` takes a format.** Scene needs base colour and emissive
  as sRGB but normal, metallic-roughness and occlusion as linear, from the same
  call. A silent sRGB default with a `…Linear` variant was rejected: a default
  that is wrong half the time is worse than an argument.
- **Mip generation becomes colour-space aware.** `uploadMipChain` box-filters raw
  bytes today, which for an sRGB texture averages *encoded* values — mathematically
  wrong, and it shows as mips that are too dark. Decode → filter → re-encode for
  `FormatRGBA8Srgb`; plain average for `FormatRGBA8`; **refused** for depth. The
  hardcoded `* 4` bytes-per-texel becomes a per-format size.

Bind groups and parameters
([Bind-group frequency convention](https://github.com/dvoyni/cog/issues/9)):

- `ShaderLayout` gains reflected member layout for **storage** structs — a
  one-level walk, which must handle an **array-of-struct member with an element
  stride** for `lights: array<SceneLight, 16>`.
- Reflection **errors on a second uniform block** instead of silently
  overwriting the first, which today misbehaves in a near-undiagnosable way.
  Scene declares none, so the rule costs nothing and removes a landmine.
- `parameterPlan.sampler`/`samplerBinding` become **lists**; each reflected
  sampler binds independently by name, and `RenderPass.SetTexture` splits the
  bundled sampler binding out. A glTF PBR material has up to five textures whose
  samplers may differ, and a comparison sampler cannot be the same object as a
  colour sampler.
- `BufferRangeParam(name, buf, offset, size)` joins `BufferParam`. `SetBuffer`
  already carries offset and size into the bind-group entry and the cache key
  already includes both — only the translator's hardcoded `0, 0` is in the way.
- **`firstInstance` is plumbed** through `OpQueue.Draw` → `gfx.Queue.Draw` → the
  backend; `gpuOp.arg4` is free on the draw path.

Housekeeping:

- **Drop `BufferDesc.Dynamic`** — dead code, never read, a pure function of
  `Kind`.
- `TextureDesc` grows a **renderable bit**.
- Add `gfx.DefaultLimits()` — a hardcoded table of the **browser spec floor** — for
  the debug-build web-limits check. It is the comparison target precisely because
  the device's own limits are not: a desktop adapter reports hardware limits, so
  checking against them passes a build that cannot run in a browser. The check
  covers **every** shader gfx reflects, not only scene's, and reports the
  declared count, the device's limit (available today through the backend's
  existing device handle) and the web floor, naming the offending shader. Note
  the browser's limit extraction leaves `maxBindGroupsPlusVertexBuffers`,
  `maxInterStageShaderVariables`, `maxPushConstantSize` and
  `maxNonSamplerBindings` at zero, so a field-by-field comparison must skip them.

### `gogpu`

- `resolveTarget` grows a texture-view path cached by `{texture, mip, layer}`; it
  knows only the screen ID today.
- `newTexture` maps the format enum instead of hardcoding `RGBA8Unorm`, and adds
  `RenderAttachment` usage for the renderable bit.
- `ensureDepth` becomes a size-keyed map allocating `Depth32Float`; all stencil
  state disappears.
- `Execute` drops its `target` parameter and implements `BeginPass`/`EndPass`
  over one encoder.
- `NewPipeline` sets `DepthCompare` and `DepthWriteEnabled` independently;
  `PrimitiveState` gains `CullMode`, `FrontFace` and `StripIndexFormat`.
- `NewSampler` maps per-axis address modes including `Mirror`, separate mag/min/
  mip filters, `MaxAnisotropy`, and `Compare`.
- `uploadTexture` derives `BytesPerRow` from the format; `uploadMipChain`
  decodes and re-encodes around the box filter for sRGB.
- `Draw`/`DrawIndexed` pass the real `firstInstance` instead of the hardcoded
  `0` — the argument is already in both signatures.
- `SetTexture` takes per-texture sampler bindings.
- A **redundant-bind filter** in `flushBinds`, reset on shader change and on
  `BeginPass`.
- A **present pipeline**: full-screen triangle, samples the frame buffer, applies
  the sRGB OETF, targets the swapchain's `BGRA8Unorm`. The only place the true
  swapchain format is named.
- `gfxreflect.go` stops typing every texture `Float` and every sampler
  `Filtering`.

**Not changed:** the uniform path, the 256-byte block, and
`canvasTexture`/`canvasSampler`.

### `canvas`

Canvas is in scope only where **scene correctness** requires it: canvas and scene
share one frame buffer and one `m.Color`, so a canvas that writes gamma bytes
into scene's target is this map's problem. A canvas refactor scene merely
benefits from stays out.

Pass declaration
([Canvas layer mapping onto gfx pass order](https://github.com/dvoyni/cog/issues/27)):

- **`canvas.Layer` is `gfx.Order`**, widened from `int32`. Canvas declares one
  `PassDescr` per non-empty layer at `Order = gfx.Order(layerID)`, all
  screen-targeted, `DepthAuto`.
- Depth clears to 1.0 on the **lowest** pass canvas emits and preserves above it;
  depth stores `StoreDiscard` on the **highest** and `StoreKeep` below. Both are
  forced by the merge rule, which needs `StoreKeep` on both attachments in the
  predecessor: discarding everywhere would stop canvas passes merging with each
  other, storing everywhere would cost a merged camera+canvas run the tiled-GPU
  writeback saving. Colour always stores `StoreKeep`.
- **`Clear` becomes positioned**: `Clear(layer Layer, color m.Color)`.
  Frame-global clear cannot survive anything rendering below canvas — a camera at
  `Order -1` followed by canvas clearing at `Order 0` wipes the 3D. The obvious
  repair, "clear at the lowest non-empty layer", is worse: *which* layer that is
  varies per frame, and a frame where the board layer draws nothing moves the
  clear up and wipes a camera that was correct the frame before; and `Clear` with
  no draws recorded would silently stop clearing. One call site in the tree.
- **`canvas.ClearDepth` is deleted** — zero callers, canvas's own materials never
  test depth, and the depth rule above is now unconditional.
- Canvas's three hand-written state literals become `gfx.StateOverlay2D()`.
- **A foreign pass interrupting a layer run needs no rule**: it fails the merge
  predicate and costs one more GPU pass, the honest price of the state change the
  app asked for.

Colour ([Canvas colour-space migration](https://github.com/dvoyni/cog/issues/33)):

- The shared atlas flips to `FormatRGBA8Srgb` wholesale — sprites, glyphs and the
  generated white pixel are one texture and cannot take different colour spaces.
  **Glyph texels are unaffected mechanically**: they are stored `RGB=255,
  A=coverage`, sRGB touches RGB only, and 1.0 is a fixed point. The *edges* still
  shift ([canvas: text AA midpoint shift for linear blending](https://github.com/dvoyni/cog/issues/36)).
- Decoded images are sRGB **by construction** — the atlas array,
  `resolveStandalone`, and the separate resource path all go `RGBA8Srgb`
  unconditionally, because a decoded PNG is sRGB by definition and letting a
  caller say otherwise only invites the bug. `TextureWithBytes` keeps an explicit
  format, meaning data.
- **The key-colour block in all three shaders must be re-derived.** This is the
  hazard the format decision did not name. All three shaders classify texels in
  *texel-value* space (`abs(sampled.r - sampled.b) < 0.2 && sampled.g < 0.2`,
  then a ramp on `intensity <= 0.5`), and feuds-26 depends on it for every unit
  sprite and army flag. Under an sRGB atlas **both halves break**: the `0.2`
  green cutoff means `0.0331` linear, so mid-tones the detector used to exclude
  are now recoloured; and a mid-magenta key texel authored at sRGB `0.5` arrives
  as linear `0.214`, yielding `keyColor × 0.43` where it used to yield `keyColor`
  exactly — **every keyed sprite loses over half its player colour, silently.**
  The fix: keep `intensity` as the **sRGB-encoded scalar** (one `pow` on a float,
  not a `vec3` round trip), so the detector and the ramp *position* stay
  bit-for-bit what the artist tuned, while the ramp's *output* interpolates
  `keyColor`→white in linear. The constants become named WGSL `const`s carrying a
  `// 0.2 in sRGB` provenance comment. The seven non-fixed-point production
  literals are all one constant, `keyColor{0.5, 0.5, 0.5, 1}`, hand-audited as
  part of this work.
- **Canvas never generates mipmaps**, so the colour-aware mip filter has no
  canvas trigger; scene owns its first real use.
- **Compositing a camera's render target cannot go through canvas's built-in
  triangle material**, and this is a consequence of the sRGB atlas rather than a
  separate decision. All three canvas shaders run every sampled texel through
  the key-colour ramp, which is not a no-op on a rendered image: a texel is
  keyed when its red and blue agree within 0.2 in sRGB and its green is below
  0.2 in sRGB, and a keyed texel leaves the shader as neutral grey at its own
  red intensity. A dark warm shadow at linear `(0.10, 0.02, 0.02)` is keyed —
  sRGB-encoding its red and blue puts them 0.19 apart — and comes out grey. **No
  key colour makes the ramp an identity**, because the ramp's output is a
  function of red alone. So split-screen composites through a caller-supplied
  material that samples and returns; `cmd/scene/cameras/composite.go` is the
  minimal one. It declares `canvasViewport` and `canvasLayer` and neither
  `canvasClip` nor `keyColor`, which gfx drops by name against the reflected
  layout.
- **Canvas has no 8-bit colour packing anywhere** — vertex colour is
  `Float32x4`, tints and `keyColor` are float `vec4`s — so a whole class of
  migration bug does not exist here. Only three sites outside `m` read
  `.R/.G/.B/.A`, all packing code.

**Migration order.** Both consumers `replace` to the working tree with no version
pin, so the `m` sweep is atomic across all three trees by construction. The
choice is the gfx/gogpu half, and it lands **first, as a separate step**: the
`RGBA8Srgb` member, `ScreenTarget()` becoming a frame buffer plus present pass,
and `TextureWithResource` taking a format are all additive, used by nothing, and
must produce a **pixel-identical frame**. That is the strongest falsifiable claim
available in this migration, and it buys a bisect point between "the present pass
is wrong" and "the migration looks different" before step two flips `m`, canvas
and both consumers together.

**Verification**, three legs, because golden images and CI are out of scope:

1. **`m` tests pin the transfer function**: the 0 and 1 fixed points, the
   `0.5 sRGB ↔ 0.2140 linear` pair, an 8-bit round trip across all 256 values,
   and the existing HSLA round trip still passing.
2. **One canvas test pins the format choice**, asserting the atlas array is
   allocated `RGBA8Srgb` through the `testBackend.AllocateTexture` hook that
   already exists. This is the only structural fact a device-free test can catch.
3. **A named, captured before/after checklist on feuds-26**, run through the wasm
   build: five shots from a fixed game state — a keyed unit sprite, an army flag,
   light-on-dark text, dark-on-light text, and an alpha crossfade mid-transition.

Leg 3 is not ceremony. Every judgement deferred here — the key-ramp re-tune, the
glyph midpoint, whether a `ui` fade reads wrong — is triggered by *looking*, and
without captured "before" frames those triggers fire against a memory of how it
used to look. Note explicitly that **the existing canvas suite is colour-blind**:
every colour literal in it is fixed-point, so it neither breaks nor verifies, and
it will not force an audit of anything.

The **expected and accepted** visual shifts: flat fills are unchanged (an
sRGB-constructed palette converted to linear and written to an sRGB target
round-trips to the same bytes), a 50% alpha crossfade midpoint moves from ~128 to
~188 in 8-bit terms, and every antialiased glyph edge changes.

---

## Extension points

Three capabilities are deliberately **not** in v1: shadow maps, post-processing,
and image-based lighting. Each is documented here with the seam it attaches to,
what a later effort adds, and what it does not have to undo. **None of them needs
a decision in this spec reversed**
([Extension points: shadows, post-processing, IBL](https://github.com/dvoyni/cog/issues/23)).

### Shadow maps

**A shadow camera is a camera.** It is not a second pass on the lit camera: a
shadow map is rendered from the light, so the sun's shadow map is a second
`Camera` whose `Transform` is `m.LookAt` along `SunDirection`, whose `Projection`
is `Orthographic` sized to cover the region that must cast, and whose one pass is
depth-only.

```go
q.Camera(shadowCameraID, scene.CameraDescr{
    Transform:  m.LookAt(sunEye, sceneCentre, up),
    Projection: scene.Orthographic,
    Height:     40, Near: 1, Far: 200,
    CullMask:   layerCasters,
    Passes: []scene.Pass{{
        Tag:        "shadow",
        Target:     gfx.NoTarget(),
        Depth:      gfx.DepthTarget(shadowTex),
        ClearDepth: m.Some[float32](1),
        Order:      -1000, // ahead of every camera that samples it
    }},
})
```

**Everything in that literal exists in v1.** `NoTarget()` and `DepthTarget` were
added for exactly this; store ops are inferred, and naming an explicit depth
texture is what makes the pass store rather than discard; the pass runs because a
clearing attachment makes it observable; and its frustum comes from the depth
texture's aspect, not the screen's.

`Order` is the only correctness requirement: **the producing pass must sort ahead
of every pass that samples it.** gfx builds no dependency graph, so ordering is
the entire contract. A mis-ordered shadow pass reads the previous frame's texture
and is **not diagnosed** — gfx's one validation catches only a draw sampling its
own pass's attachment.

Two levers already decide what casts, and neither is a new field: **per object**,
the shadow camera's `CullMask` against the draw's layers; **per material**,
whether the material has an entry for the `shadow` tag. A per-draw flag was
deliberately not reserved; if a later effort wants one it is additive, and its
zero value must mean *casts* — `NoShadow bool`, not `CastsShadow bool`.

What a later effort adds, all additive:

1. A link from the lit camera to its shadow source, so scene knows whose
   view-projection to pack.
2. `sceneFrame` grows the light-space matrix and bias parameters, for free: the
   prelude is published by inclusion (`model.FramePath`), so an includer picks
   the grown struct up with the source and no external copy constrains it.
3. Group 0 grows `texture_depth_2d` and `sampler_comparison` bindings. Because a
   declared binding must be bound on every draw or the frame silently vanishes,
   scene owns a **1×1 default shadow map** and binds it when there is no shadow —
   the same pattern as the PBR's white texel and the null skin. It is produced by
   a `NoTarget()` pass with `ClearDepth: 1.0` and no draws, since depth formats
   are not writable through a texture upload.
4. `sceneShadeSurface` multiplies a `sceneShadow(P, N)` term into the **sun's**
   contribution only. The sun is a named member rather than an array entry, so
   the term has somewhere to go without an index-0 convention. Shadows for
   punctual lights need a cube or atlas per light and belong with clustered
   lighting.
5. The bundled PBR gains a `shadow` entry, and every draw that passed a nil
   `Material` gains casting with no call-site change.

**gfx needs nothing new.** Depth-only passes, sampleable `Depth32F`, comparison
samplers and `Depth`/`Comparison` reflection all ship in v1 as capability. Depth
bias is [gfx: depth bias for shadow maps](https://github.com/dvoyni/cog/issues/32);
until it lands, bias is a shader constant, which is where a normal-offset bias
would live anyway.

**Cascades** work today as N depth textures and N shadow cameras. The
atlas-packed form is exactly the trigger `PassDescr.Viewport` was fogged with; a
`depth_2d_array` with one cascade per layer would instead want `DepthTarget` to
take a layer, as `TextureTarget(tex, mip, layer)` already does. Both are additive
and solve the same problem two ways, so neither is chosen now — picking early is
how the wrong one gets reserved.

Tracked as [scene: sun shadow maps](https://github.com/dvoyni/cog/issues/52).

### Post-processing

The mechanism already exists, because v1 pays for it for a different reason:
`ScreenTarget()` renders into an `RGBA8Srgb` frame buffer with one implicit
full-screen present pass. A post-process pass is a pass between a camera and that
one.

1. The camera's forward pass targets a
   `q.TemporaryTarget(w, h, FormatRGBA8Srgb)` instead of the screen.
2. A pass at a higher `Order` draws one full-screen triangle sampling that
   texture and targets `ScreenTarget()` — or the next temporary, for a chain,
   ping-ponging two targets.
3. Canvas then draws the HUD over the result, unchanged.

Ordering and targets are all this needs, and the merge rule cannot accidentally
collapse the chain: adjacent passes merge only when they share a colour target.
The barrier each step of the chain needs is placed by gfx, in both directions, so
a ping-pong costs the author nothing to get right
([#112](https://github.com/dvoyni/cog/issues/112)).

What a later effort adds is the **fullscreen draw**, not the passes: either a
bundled blit material — a full-screen textured pass with an exposed source
texture — or the general custom-shader contract. The bundled blit is much the
cheaper first step, and it does not inherit the reason shader variants were
rejected: that argument was about every variant carrying its own copy of the
BRDF, and a blit shares no code with the PBR at all.

Two honest limits:

- **A post-process pass can only read a target it was given.** gfx's frame buffer
  is not nameable by a recorder, so a chain can grade a camera's own output but
  not the composited frame including canvas. Whole-frame post-processing needs
  new gfx surface, and is in **direct tension** with
  [gfx: delete the present blit via upstream viewFormats](https://github.com/dvoyni/cog/issues/35),
  which removes the frame buffer that would be read. Neither forecloses the
  other; they just cannot both pay off.
- **Effects wanting range beyond 0..1** — bloom, exposure, filmic tonemapping —
  want [gfx: HDR camera targets and tonemapping](https://github.com/dvoyni/cog/issues/34)
  first.

Tracked as
[scene: post-processing chains and a bundled blit material](https://github.com/dvoyni/cog/issues/53).

### Image-based lighting

IBL substitutes into **exactly two terms and nothing else**: the ambient diffuse
`sceneAmbient(N) * occlusion * diffuseColor`, and the ambient specular
`sceneAmbient(reflect(-V, N)) * occlusion * EnvBRDFApprox(F0, roughness, NdotV)`.
Everything else about shading is unaffected.

v1's hemispheric ambient is the degenerate case of the first term, and the
analytic `EnvBRDFApprox` is precisely the LUT-free half of the second — so **v1
is not a dead end that IBL replaces**, it is the same two terms with a constant
environment.

What a later effort adds:

1. An irradiance cubemap for the diffuse term and a roughness-prefiltered
   radiance cubemap for the specular one, plus either a BRDF LUT or the analytic
   approximation already in v1.
2. `sceneAmbient(dir)` keeps its signature; specular sampling needs roughness for
   mip selection, so it gains a *sibling*, `sceneSpecularAmbient(dir, roughness)`.
3. A **skybox**, which the pipeline state model already expresses: a unit box
   drawn with `Cull: CullFront`, `DepthCompare: CompareLessEqual` and
   `DepthWrite: false`, at depth 1 — one of the two reasons the full
   `CompareFunc` set is exposed and `DepthTest` was deleted. It must sit in the
   **opaque** draw class so the back-to-front sort does not apply; either
   position within the opaque bucket is correct, since the depth test decides.

**What gfx must add is small: `TextureViewCube`.** `TextureViewDimension` is
`2D | 2DArray` today, reflection already derives it from the WGSL binding type,
and `TextureDesc.Layers` already allocates six. One enum member and three switch
arms.

Prefiltering is where IBL's real cost sits, and it is the one place an
out-of-scope decision constrains this extension point.
`TextureTarget(tex, mip, layer)` carries mip and layer specifically so a prefilter
chain is expressible as one pass per `(roughness level, face)` — but generating it
needs either compute, out of scope map-wide, or the render-pass mip chain that was
declined. The alternatives are prefiltering on the CPU at load, which is slow, or
shipping pre-baked cubemaps, which wants texture compression. This is **"not
built", not "must be undone"**.

Tracked as
[scene: image-based lighting and environment cubemaps](https://github.com/dvoyni/cog/issues/44).

---

## Demos and acceptance

([Demo set and acceptance criteria](https://github.com/dvoyni/cog/issues/24))

Seven demos in `cog-examples/cmd/scene/`, chosen so every closed contract is
exercised by at least one and none is duplicated on purpose. `cog-examples` is
`github.com/dvoyni/cog-examples` at `c:\Repos\cog-games\cog-examples`, a sibling
of `cog`, referencing it by `replace ../cog`; one self-contained `main.go` per
demo under `cmd/scene/<demo>/`, run with `go run ./cmd/scene/<demo>`. The clone
location is load-bearing
([Found the cog-examples module](https://github.com/dvoyni/cog/issues/2)).
`hello` stays as the cited wiring baseline and `api-sketch` as a superseded
artifact; neither is in the acceptance set.

**The coverage rule is contract coverage.** A demo counts for every contract it
exercises; the table below maps many contracts onto one demo rather than growing
a demo per contract. Build order is advisory; the acceptance bar is the whole
set.

### Contract → demo

| Demo | Assets | Contracts |
| --- | --- | --- |
| `box` | **none** | debug vocabulary; `Transform` TRS and `WithScale`; `LookAt`; empty `Passes` → implicit forward pass at the camera id; sun and hemispheric ambient; the linear pipeline and present pass; every-zero-value-is-the-default; the `m` additions |
| `pbr` | WaterBottle, AlphaBlendModeTest, BoxVertexColors, CompareBaseColor, EmissiveStrengthTest, PointLightIntensityTest | the whole material contract (glTF names, five slots, two 1×1 defaults, Khronos BRDF, `EnvBRDFApprox`, `COLOR_0`, flat `KHR_texture_transform` members); `alphaMode`→state and `Cull`; the back-to-front blend bucket; a rotated non-uniform basis through the lit path, via a per-axis `Transform.Scale`; point and spot lights, `Range` zero-means-infinite, the 16 cap and its silent drop; `emissive_strength`, `lights_punctual` as data |
| `cameras` | reuses `box` + `pbr` | multi-camera, orthographic, `CullMask`, no `Viewport` → `TemporaryTarget` composited by canvas, negative ids, duplicate-id error; targets, `DepthAuto`/`DepthTarget`, `Order` sorting and pass merging; layer masks; multi-tag material and a `NoTarget()` depth-only pass; `WorldToScreen`/`ScreenToRay`, per-target `viewport m.Vec2`, behind-camera `ok`, `m.Ray.IntersectSphere` |
| `animated` | Fox, AnimatedMorphCube **and its glTF-Quantized twin**, MorphStressTest, InterpolationTest | baked poses, `ClipPlay` crossfade, the 4-play cap, the rest frame, `PoseBytes`; sparse morph weights, `MorphWeights` override, the attribute mask; degenerate single-joint skins; u8 index widening. **The web canary.** The quantized twin is shared with `loading` and is not optional here: the attribute mask is *intersected with what the base primitive authored*, and the plain cube's authored `TANGENT` against the quantized one's absence is the only pair in the vendored set that can tell that rule from a mask read off the targets alone. |
| `procedural` | none (custom WGSL) | `TemporaryMesh` vs `BakeMesh`, the generic `VertexLayout`, `UpdateMesh`, `ReleaseMesh` generations, `NeverCull`, the null skin and `SCENE_NOSKIN`; `BakeBuffer`/`ReBakeBuffer`/`BufferWithBytes`; a caller-supplied material with a custom layout |
| `instancing` | reuses `box` + `pbr` | explicit `Transforms`, per-instance culling, the `materialID`/`meshID` sort key, `SCENE_NONUNIFORM` via a per-axis `Scale` per instance, `Passes(dst)`; `firstInstance`, the per-batch material record, one instance arena bound by range |
| `loading` | CesiumMilkTruck, MultipleScenes, TextureSettingsTest, MeshPrimitiveModes, AnimatedMorphCube glTF-Quantized, InterpolationTest, `broken/truncated.glb`, `broken/does-not-exist.glb` | `Node` views and re-rooting; `Scene` naming a file's only scene (six vendored assets name theirs `Scene`, `CesiumMilkTruck` among them) and its unmatched report — but **no asset has a nameable *non-default* scene**: `MultipleScenes`, the set's only multi-scene file, leaves both of its unnamed, so a matched `Scene` here always resolves to the same draw the default would, async skip-never-substitute, `Preload`, `Material` replace vs `OverrideParams` merge, explicit unload with no texture cascade; `(value, ok)`, `State`, `Nodes`/`Bounds`/`AABB`, unmatched-node-reports-once; the four WebGPU papering-over gaps — **`InterpolationTest` is here for the u8-index one**, which none of the other five files in this set carries, and `does-not-exist.glb` for the absent-file failure, which is a *third* mode beside the truncated file and the invalid path. Sixteen stations on a grid, each drawn whether or not it can be, so **a bare pad is what skip-never-substitute looks like**. |

`custom-shader` is **merged into `procedural`**, not dropped: a custom vertex
layout *requires* a custom material, so the two cannot be demonstrated apart.
`box` is kept beside `pbr` despite covering nothing `pbr` misses, because it is
the only demo that runs with **zero assets** — the floor of the API and the smoke
test that still works when the asset story breaks. `loading` is kept despite
being nearly all assertion and almost no picture, because loading and the lookup
facade are the two contracts whose failures are *invisible*: a model that
silently substitutes, a node that silently falls back to the whole scene.

**`FrontFace` has no demo behind it, and cannot get one from this asset set.**
It flips to `FrontCW` only for a primitive under a node transform with a
negative determinant, which is decided at load from the file's own nodes and
not from the transform a draw is given — so no call a demo can make provokes
it, and no vendored asset carries a mirrored node. `pbr` asserts instead that
every material it loads stays `FrontCCW`, which is the whole of what is
reachable. A file with a mirrored node is what would close it.

### Acceptance: two mechanisms, split by failure class

**One falsifiable sentence per demo** for what only eyes can judge, plus
**assertions** for everything that is a number or an ordering. A wrong BRDF and a
wrong sort are the two failure classes this set must catch, and they divide
cleanly between the two.

The assertions live in **`_test.go` files beside each demo and run with no GPU**.
This is available because culling, sorting and packing happen entirely in the
update-thread flush and the result is published as `Passes(dst []PassView)`
including the frustum; `extensions/gfx/plugin_test.go` already has a `fakeBackend`
implementing the full `Backend` interface; `gfx.Backend` is provided to gfx as an Adapter
without the `gogpu` plugin at all; and a headless engine is already a named
concept. **`go test ./cmd/scene/...` is the one command the implementation effort
runs.** Each demo additionally prints its own key numbers on screen through
canvas, so a human running it sees them without a second command.

Two things the shared harness owes a demo beyond the counts. It reports a
reflected shader layout only for the shaders it was told about, so a demo
carrying its own WGSL states its own layout to the fake backend; and it records
every storage-buffer binding, which is the only way a caller material can assert
which of scene's three per-draw parameters it actually bound. Both are inert for
a demo that wants neither.

A demo that provokes a report on purpose must install its own
`kernel.ErrorHandler`. The default one terminates the engine, which is right -
most reports are bugs - but it means `procedural`, whose whole point is that a
released ref is reported and skipped, would otherwise shut its window on the
first swap. It survives exactly that report, matched by mesh id, and terminates
on everything else.

**Demo time is accumulated fixed steps, never wall clock**, so frame N is
reproducible and a test drives N steps directly. Each demo starts at a documented
fixed camera pose, where its reference screenshot is taken; input may then orbit
and pause freely, and touching it voids nothing, because the assertions live in
the test rather than in the running app.

Two visible criteria worth quoting, both on `cameras`, both chosen over
assertions deliberately — a sign error in the Y flip is exactly the bug an
assertion written by the author of the flip will happily confirm:

- **Nameplate**: *the label stays glued to the cube in both viewports, and
  disappears rather than mirroring when the cube passes behind the camera.* One
  sentence covering the Y flip, the per-target size rule and the `ok` contract at
  once.
- **Click-to-highlight**: *clicking a cube tints that cube and no other,
  including through the composited texture camera.* The nameplate proves
  world-to-screen and the click proves screen-to-world; getting both right with
  one wrong sign is impossible. It is also the ten-line picking loop, so "picking
  is yours to write" ships with a worked example rather than an assurance.

### No new engine infrastructure

Every criterion must be satisfiable by a human running
`go run ./cmd/scene/<demo>` today. The repo has **no CI at all**, **no
`testdata/`**, no golden or snapshot tests, no headless cog run, and **no frame
readback**. Golden-image acceptance is therefore a follow-up rather than fog —
its shape is fully known, since `gogpu` one layer down already ships
`golden_test.go`, `testdata/golden/*.png`, `-update-golden`, a headless renderer
and `Surface.ReadPixels` — and what is missing is cog-side plumbing, which is
engine surface this spec does not cover
([scene: golden-image acceptance harness and the first CI workflow](https://github.com/dvoyni/cog/issues/54)).

Two pieces of work are **handed to the implementation effort** rather than
decided here: porting the web build recipe (`build.sh`, `index.html`, the tar
asset unpacker) from `feuds-26/cmd/web/` into `cog-examples`, since the web
canary needs it; and writing `cmd/prepare-assets`.

### Platform

**Desktop is the acceptance bar**, with `animated` designated the **web canary**
— it touches the most storage-buffer bindings, the budget has no spare, and a
single unbound binding silently kills the whole frame. Paired with the check
against `gfx.DefaultLimits()`, so a desktop run fails loudly on a web violation
rather than deferring the discovery to the browser — and that check is **not
debug-gated**, as an earlier draft of this section said: `gfx` measures every
shader it reflects, from `ensureShader`, on every build, and surfaces the
result as a non-fatal diagnostic through the error handler. Every demo passing
on both would double verification for no proportionate gain; desktop-only would
defer the one class of failure desktop provably cannot catch, since a native
device reports hardware limits.

### Assets

**CC0-1.0 and CC-BY-4.0 only.** No NonCommercial, no NoDerivatives, no custom
`LicenseRef` agreements. The `cog-examples` README calls these examples
"collected here for later publication alongside the engine", which makes them a
distribution, and a committed reference screenshot is itself a derivative — so a
permissive-for-vendored, anything-for-fetched split is a trap that would surface
at publication with the spec long frozen. **Baked-in trademarks are allowed** and
recorded per asset: a `LicenseRef-LegalMark-*` entry reserves the right to
withdraw a *mark*, not the licence.

Three earlier candidates are **out**: **DamagedHelmet** is disqualified —
`CC-BY-4.0` **and** `CC-BY-NC-4.0` both apply to its files, the NC term riding in
from the original the rebuild derives from — replaced by **WaterBottle**
(CC0-1.0, no extensions, TANGENT present, the complete core PBR set in one
material). **FlightHelmet** is dropped: no `.glb` variant at all, 48 MB in 18
files, and it needs `KHR_materials_transmission`, which is not in the v1
extension list, so scene would silently render it wrong. **CesiumMan** is dropped:
exactly one animation clip, so it cannot demonstrate a crossfade, while Fox has
three over 24 joints.

Assets are **vendored** under `cog-examples/assets/<ModelName>/`, reachable
through the storage read mount `internal/assets` adds and tarred by the ported web
build. `assets/ATTRIBUTION.md` lists every asset with its exact licence string,
its required attribution line, and any legal-mark entry. `cmd/prepare-assets`
carries the **source commit SHA per asset** — the Khronos repo has no releases
and no tags, so a SHA is the only real pin — and does two jobs: **re-encode** (no
sub-5 MiB PBR showpiece exists; 90–95% of the candidates is four 2048×2048 PNGs,
and CC0 permits re-encoding with zero obligations, landing WaterBottle at 1–2 MiB
with no visible change) and **pack** the three `.gltf`-only assets the set needs
(`MeshPrimitiveModes`, the quantized `AnimatedMorphCube`, `MultipleScenes`), all
three CC0.

**Deliberately broken assets are committed**, because otherwise the most
dangerous contract here — a model that could not be loaded is skipped, never
substituted — has no demo that ever sees a failure. Two cases, which
`State(path)` tells apart by what each error wraps: a path that does not exist,
whose read failure is the asset library's; and `assets/broken/truncated.glb`, a
valid header with the binary chunk cut short, generated by `cmd/prepare-assets`
so it is not a mystery blob either, whose parse failure is scene's own. Both are
terminal, both happen in the call that asked, and unload is the only retry
lever.

### Findings the demo set carries

- **No model in the Khronos repository mixes a skin with independently animated
  non-skeletal nodes** — all seven models with both `skins` and `animations`
  animate a strict subset of their joints. So the degenerate-single-joint-skin
  rule needs a **second asset**: `InterpolationTest` (CC0, no `skins` key at
  all), whose nine animations are exactly `Step`/`Linear`/`CubicSpline` over
  `translation`/`rotation`/`scale` — which is the precise justification for
  baking at 60 Hz rather than 30. It also carries u8 indices, so one 8 KB file
  covers three contracts.
- **A path-keyed texture cache cannot be exercised across two models**: every
  model owns a private directory, so no two files ever resolve to the same path.
  The two honest exercises, both in `loading`, are **drawing the same model
  twice** and `TextureSettingsTest`, whose 3 images back 9 textures through
  different samplers — which also exercises the restructured `SamplerDesc` across
  mirror, repeat and clamp on both axes.
- **No `.glb` in the repository contains a fan, strip, loop, line or point
  primitive** — the histogram is TRIANGLES ×14,192 against 21 of everything else,
  all inside `.gltf`-only assets. The `StripIndexFormat` bug is therefore only
  reachable through a packed `MeshPrimitiveModes` — whose seven nodes are all
  **unnamed**, so it exercises the topology conversion and nothing about
  addressing. Six of its seven primitives survive; the `POINTS` one is skipped
  and reported, and the frame's scene pipelines carry only `TopologyTriangleList`
  and `TopologyLineList`, which is the invariant the batching, index and skinning
  paths rely on.
- **`CesiumMilkTruck`** is the re-rooting asset: `Wheels` and `Wheels.001` sit at
  ±1.43 on X beneath a `Yup2Zup` root, so re-rooting has a real authored world
  transform to discard, at hierarchy depth 4. What it cannot carry is a
  *positional* assertion of the re-root through pass results alone: the wheel
  mesh's bounding sphere has radius 1.22 against an authored offset of 1.50, so a
  frustum tight enough to reject the un-re-rooted sphere rejects the re-rooted one
  too. Selection, subtree extent and the reports are asserted against the real
  bytes; where the re-rooted subtree *lands* is now asserted through `Bounds` and
  `AABB`, which give it a public answer — and finding that answer wrong for this
  very asset is what the rest-placement rule above came out of. The mesh the two
  `Wheels` nodes share is an **axle pair**, not one wheel, and both nodes are
  animated, which is why they are the sharpest re-rooting assertion in the set:
  one mesh, one local rotation, two different parent offsets, and one correct
  box.
- **`MultipleScenes`** is the **only** file in the repository with more than one
  `scenes` entry, and therefore the only possible exercise of the `Scene`
  selector — except that **both of its scenes are unnamed, and so are both of its
  nodes**. A name-keyed selector reaches neither, so what the file exercises is
  the declared default (`"scene": 1`) and the report an unmatched name fires.
  There is no asset anywhere that a non-empty `Scene` can select.
- **No chosen model is both skinned and morphed**, so **morph-then-skin ordering
  — the one thing glTF is emphatic about — is untested by the demo set.** Stated
  here rather than assumed away.
- **A demo shows a `NoTarget()` depth-only pass executing only where the
  backend encodes it**, for the HAL reason recorded under [Passes](#passes) —
  Vulkan and the browser yes, GLES no. `cameras` emits one, and what it
  demonstrates on a GLES desktop is the tag filter deciding that exactly one
  material participates; what it demonstrates on Vulkan or in the browser is the
  pass executing. Both halves are needed, and the browser half is the only place the
  claim is more than bookkeeping — which makes this the second contract, after
  the storage-buffer budget, that **only a wasm run can check**.
- **A camera's own frustum is the one thing a `CullMask` is unarguably for.**
  `cameras` draws the perspective camera's frustum outline on an overlay layer
  that the perspective camera masks out: recorded once, seen by the minimap,
  declined by the camera it describes. Layers give per-camera exclusion and
  nothing else in scene can express that, since the draw is one draw and the
  exclusion is one camera's.
- **`m.Sphere` is the right pick bound and `m.Box3` is not, for the reason the
  ray-and-picking section gives and one more**: a sphere is invariant under
  rotation about its own centre, so a yawed prop needs no rotated bound at all.
  `cameras` yaws its obelisk and picks it with an unrotated sphere.

---

## Out of scope

Ruled beyond this spec's destination. Each is either purely additive or tracked
with a named trigger, and none is foreclosed. Implementation follow-ups hang
under [Scene follow-ups](https://github.com/dvoyni/cog/issues/29).

| Item | Why | Tracked |
| --- | --- | --- |
| Implementing this spec — README, `scene.instructions.md`, plugin code | belongs to the implementation effort that follows | — |
| Shadow maps, post-processing, IBL **implementation** | documented as extension points only; the multi-tag material shape they need **does** ship | [52](https://github.com/dvoyni/cog/issues/52), [53](https://github.com/dvoyni/cog/issues/53), [44](https://github.com/dvoyni/cog/issues/44) |
| Custom shader contract and prelude | no longer out of scope: the preprocessor shipped and model publishes `FramePath` and `PbrPath` beside `VertexDecodePath` ([model.md](../../../model/docs/specs/model.md#custom-shaders)) | [48](https://github.com/dvoyni/cog/issues/48) |
| Compute shaders and any no-compute fallback | not required by this scope | — |
| Offline asset baking / an engine-native model format | glTF at runtime through `storage` is the whole story | — |
| Object picking / id passes | scene retains no draw list to raycast; the caller's own entity loop is ten lines | — |
| Draw hierarchy and **bone sockets** | the socket is the narrow half of "do draws form a hierarchy at all"; answering it first would fix a recording surface the general question must then live inside. The cost is **draw identity**: ops are frame-local and anonymous and the sort destroys recording order, so naming a parent means a new id space and a resolution order surviving that sort — scene's recording model, not a feature on top of it. The enabling work stays: poses hold `globalJoint` unpremultiplied and the flush already walks baked rows CPU-side at instance-pack time | [57](https://github.com/dvoyni/cog/issues/57) |
| Animation state machines / blend graphs | a gameplay concern for the `anim` plugin | [26](https://github.com/dvoyni/cog/issues/26) |
| HDR camera targets and tonemapping | HDR without a tonemap is half a feature, and post-processing is documented-only | [34](https://github.com/dvoyni/cog/issues/34) |
| Deleting the present blit via upstream `viewFormats` | the faster design, but it needs a merge in a module we do not fork, and a spec must not ship blocked on someone else's repo | [35](https://github.com/dvoyni/cog/issues/35) |
| Colour write mask, MSAA + alpha-to-coverage, depth bias | each has a known shape and a known trigger; none is triggered here | [30](https://github.com/dvoyni/cog/issues/30), [31](https://github.com/dvoyni/cog/issues/31), [32](https://github.com/dvoyni/cog/issues/32) |
| Canvas on pooled storage records | a *performance* refactor scene merely benefits from, with its own regression surface — unlike the colour migration, which scene correctness requires | [28](https://github.com/dvoyni/cog/issues/28) |
| Canvas per-layer render targets | scene needs canvas to render nothing to a texture; the traffic runs the other way | [55](https://github.com/dvoyni/cog/issues/55) |
| Canvas async loading and the `(value, ok)` contract | canvas loads synchronously, so its `ok` would be a different predicate wearing the same shape; adopting it honestly means canvas going async and `ui`'s measure path tolerating an unmeasurable element | [50](https://github.com/dvoyni/cog/issues/50) |
| The canvas key-colour **mechanism** | it classifies a texel purely by value, so it cannot tell a sprite's genuine dark neutrals from an authored key region — but scene is correct with the heuristic exactly as it stands, once the constants are re-derived | [56](https://github.com/dvoyni/cog/issues/56) |
| A `ui` viewport element hosting a camera's output texture | scene's side is closed: `Pass.Target` takes the gfx handle and scene hands back no handle of its own | [37](https://github.com/dvoyni/cog/issues/37) |
| Refcounting textures so `UnloadModel` is the only lever | the no-cascade wart | [39](https://github.com/dvoyni/cog/issues/39) |
| Residency budget and automatic eviction | v1 unloading is entirely explicit with nothing evicting on its own, so a budget is the alternative to that design, not a refinement of it | [40](https://github.com/dvoyni/cog/issues/40) |
| Baking poses at 30 Hz with higher-order shader interpolation | the knob exists; what needs designing is the interpolant that makes 30 Hz not a regression, and what to do about `STEP` discontinuities no interpolant reconstructs | [42](https://github.com/dvoyni/cog/issues/42) |
| Morph targets and skinning on buffer-built meshes | both need a group-2 binding a `MeshRef` has no equivalent of, and skinning additionally needs joint-data ownership — most of a model format invented at the call site | [43](https://github.com/dvoyni/cog/issues/43), [51](https://github.com/dvoyni/cog/issues/51) |
| Shader preprocessing and vertex variants; multi-buffer vertex binding | the cheap first step is an entry-point field, not preprocessing: the fragment stage does not vary with vertex layout at all | [45](https://github.com/dvoyni/cog/issues/45), [46](https://github.com/dvoyni/cog/issues/46) |
| gogpu's unvalidated `arrayStride` | upstream; filed so the trap is written down | [47](https://github.com/dvoyni/cog/issues/47) |
| Automatic collapse of consecutive equal draws | shape fully known (canvas's `keyMatches` run-loop over the sorted array), and **output-identical** because the sort ships in v1 | [49](https://github.com/dvoyni/cog/issues/49) |
| Golden-image acceptance and the first CI workflow | engine surface, not plugin spec; the assertion half needs none of it | [54](https://github.com/dvoyni/cog/issues/54) |
| Text AA midpoint shift for linear blending | only if linear-blended text reads wrong | [36](https://github.com/dvoyni/cog/issues/36) |
| Moving `sceneFrame` to a uniform block | the better long-term shape and the named next lever for an eighth storage-buffer slot, but gfx's uniform path is per-draw only, capped at 256 with silent truncation, with no range binding and a `BufferUniform` usage never produced anywhere — a gfx feature, not a budget fix | [100](https://github.com/dvoyni/cog/issues/100) |
| A uniform block over 256 bytes silently truncating | scene declares no uniform block at all, so it is immune; reachable only through canvas's uniform path | [101](https://github.com/dvoyni/cog/issues/101) |

### Known-unspecified, with triggers

These are deliberately **not** decided: the question is not sharp enough to
answer, or the answer needs a measurement nothing in this scope produces. Each
carries the trigger that would make it a real question.

| Item | Trigger |
| --- | --- |
| Per-clip joint subsets | a file holding many independently-animated props *and* per-prop clips, where `(joints the clip does not animate) × frames` dominates |
| Reverse-Z depth | visible z-fighting at the far end of a large scene |
| A framing policy for off-reference aspects (`FovAxis`, or a reference aspect) | a demo is framed wrong on an ultrawide or portrait window |
| A projection escape hatch (likely `ObliqueNearPlaneClip`, not a raw matrix) — oblique **near-plane clipping**, unrelated to the `Oblique` projection kind | portal or water-reflection cameras; nothing in scope needs it |
| `Shear` widened to `m.Vec2` | a caller wants a diagonal shear with the ground upright — not reachable by rolling the camera, which rotates the ground with it |
| A view direction for `selectLights` | the cap ranks lights by falloff at the eye (`contributionAt`), which for `Orthographic` and `Oblique` is a point the viewer is not at; shares `sceneViewDirection`'s root cause |
| Per-pass viewport and scissor in gfx | shadow cascades, atlas-packed targets, or N on-screen cameras where a target each proves too expensive |
| A pose-resolve pass | per-vertex pose fetch cost proves too high in the skinned demo |
| Vertex attribute packing (`Unorm1010102` normals, `Unorm16x4` weights) | vertex memory is a measured problem |
| MikkTSpace tangent generation | a baked normal map shows a seam the UV-gradient tangent causes |
| Texture compression (KTX2 / Basis) | wanting a transcoder; `basisu` is already rejected as a glTF extension |
| GPU mip blit chain | texture volume makes load times unacceptable |
| Level of detail and mesh simplification | a consumer needs it |
| Clustered or tiled light assignment (and a cone bound for spot culling) | a demo wants more local lights than the 16 cap and fragment cost shows it |
| Skinned cull bounds (per-joint influence radius unioned per clip) | enough skinned characters off-screen that their draw cost is measured |
| A set-level early-out sphere for instanced draws | an instanced draw large enough that the per-instance tests show up |
| Per-primitive culling within a model | one recorded model large enough that most of its primitives are off-screen |
| Front-to-back ordering within an opaque bucket | overdraw is measured to dominate |
| Partial and growing mesh updates (`UpdateBuffer(offset)`, `AllocateBuffer(size)`) | `CreateBuffer` plus bind-group invalidation churn measured in a demo |
| Auto-computed bounds for a custom vertex layout | a large custom-layout mesh whose uncullable draw cost is measured |
| A cached camera-view value for bulk projection | a HUD with enough labels that the per-call matrix rebuild is measured |
