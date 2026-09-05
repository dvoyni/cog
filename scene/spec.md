# scene — specification

`github.com/dvoyni/cog/scene` records declarative 3D draws — cameras, glTF
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

The plugin does not exist yet. Neither do the `gfx`, `wgpu`, `m` and `canvas`
changes it depends on; those are specified here as checklists
([Required engine changes](#required-engine-changes)), because scene cannot be
correct without them.

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
- Constructor: `scene.New() *scene.Plugin`
- Plugin dependencies: `gfx`, `storage`
- Go package dependencies: `app`, `gfx`, `kernel`, `m`, `storage`,
  `github.com/qmuntal/gltf`
- Events declared or published: none

```go
cfg := scene.DefaultConfig()
cfg.PoseSampleRate = 60
```

`Config` is the exported configuration type. `Plugin` implements `Name`,
`Dependencies`, and `Init` for the kernel lifecycle. During `Init` scene
executes `storage.SetReadFSCmd` to mount its embedded shaders, as canvas does.
Register `storage` before `scene`. A typical order is `storage`, `input`, `gfx`,
`canvas`, `scene`, then the system driver.

`PoseSampleRate` is the global animation bake rate in Hz, default 60. It is the
only configurable number in the plugin; see
[Animation](#animation)
([Baked pose buffer and skinning contract](https://github.com/dvoyni/cog/issues/15)).

### Adapter requirement

Scene reads storage buffers from the **vertex stage**, which WebGPU guarantees
only on a **core** adapter — compatibility mode defaults
`maxStorageBuffersInVertexStage` to 0. cog never requests
`featureLevel: "compatibility"`, so a compat-only device already fails
`requestAdapter()` and gets no WebGPU at all rather than a degraded scene. This
is consistent with the map's no-fallback rule, but the failure is total
([wgpu backend capabilities inventory](https://github.com/dvoyni/cog/issues/4),
[GPU skinning and morph techniques survey](https://github.com/dvoyni/cog/issues/6)).

Design to **browser spec defaults**, not to desktop's reported hardware limits:
8 storage buffers per shader stage, 128 MiB per binding, 256 MiB per buffer, 4
bind groups, 64 KiB uniform. A native device reports hardware limits, so a
desktop run will **not** catch a web limit violation; the implementation adds a
debug-build check against `gfx.DefaultLimits` so it fails loudly on desktop
instead ([wgpu backend capabilities inventory](https://github.com/dvoyni/cog/issues/4)).

---

## Resources

- `*OpQueue` — frame-local recording surface. Scene consumes and resets it on
  `app.UpdateEvent`.
- `*Lookup` — the single persistent resource holding resident models, baked pose
  and morph buffers, the path-keyed texture cache, buffer-built meshes, and the
  built-in unit meshes. It also owns deferred unloads and deferred bakes. Query
  and mutate it only through a scoped `LookupAccess`.

Gameplay normally writes only `*OpQueue`. Sizing, naming, residency, mesh baking
and unloading go through `*Lookup` via a `LookupAccess`; the flush handler also
writes `*Lookup` to apply deferred bakes and unloads.

### Event subscribed

`UpdateEventHandler` subscribes to `app.UpdateEvent`. It writes the scene
`*OpQueue` and `*Lookup`, reads `app.Viewport`, and writes `gfx.OpQueue` and
`gfx.ResourceQueue`. It is ordered `Last()` but explicitly before
`gfx.UpdateEventHandler`, exactly as canvas is: gameplay records first, canvas
and scene emit graphics draws second, gfx presents last.

**Everything scene decides happens in that flush, on the update thread** —
projection resolve, frustum culling, light culling, sorting, instance packing,
buffer uploads. Scene never runs on the render thread: gfx renders on
`app.RenderEvent` from a latest-wins snapshot taken by `present` on
`app.UpdateEvent`, so there is no mechanism for it, and no need for one, because
a frustum needs **aspect**, not pixel size, and `app.Viewport` already carries
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
func (q *OpQueue) Mesh(layers LayerMask, mesh MeshRef, draw MeshDraw)
func (q *OpQueue) PointLight(layers LayerMask, light LightDescr)
func (q *OpQueue) SpotLight(layers LayerMask, light LightDescr)

func (q *OpQueue) Box(layers LayerMask, transform Transform, color m.Color)
func (q *OpQueue) Sphere(layers LayerMask, center m.Vec3, radius float32, color m.Color)
func (q *OpQueue) Plane(layers LayerMask, center m.Vec3, size m.Vec2, color m.Color)
func (q *OpQueue) Line3D(layers LayerMask, start, end m.Vec3, thickness float32, color m.Color)
func (q *OpQueue) WireBox(layers LayerMask, center, size m.Vec3, thickness float32, color m.Color)

func (q *OpQueue) TemporaryTarget(w, h int, format gfx.TextureFormat) gfx.TargetDescr
func (q *OpQueue) TemporaryMesh[TVertex VertexLayout](vertices []TVertex, indices []uint32, topology gfx.PrimitiveTopology) MeshRef

func (q *OpQueue) Reset()
func (q *OpQueue) OpCount() int
func (q *OpQueue) Ops(dst []Op) []Op
func (q *OpQueue) Passes(dst []PassView) []PassView
```

The floor of the API is two statements:

```go
q.Camera(cameraMain, scene.CameraDescr{
    Transform: scene.LookAt(m.Vec3{X: 3, Y: 2, Z: 4}, m.Vec3{}, m.Vec3{Y: 1}),
    FovY:      1.0472,
    Near:      0.1, Far: 100,
    SunDirection: m.Vec3{X: -0.3, Y: -1, Z: -0.2},
    SunColor:     m.NewColorSrgb(1, 1, 1, 1),
})
q.Box(0, scene.At(0, 0, 0), m.NewColorSrgb(0.42, 0.71, 0.94, 1))
```

**Every slice field on every descriptor is borrowed for the duration of the
call.** Scene copies into its frame arena before returning, so a hot-loop caller
reuses one backing array.

### Transform

```go
type Transform struct {
    Position m.Vec3
    Rotation m.Quat
    Scale    float32 // zero means 1
    Matrix   *m.Mat4 // non-nil replaces the whole transform
}

func At(x, y, z float32) Transform
func (t Transform) WithScale(s float32) Transform
func (t Transform) WithRotation(q m.Quat) Transform
func LookAt(eye, target, up m.Vec3) Transform
```

The zero value is the identity. `Scale` is **scalar**, following
`canvas.SpriteTransform.Scale`: an `m.Vec3` scale reads as "twice as wide" for
`m.Vec3{X: 2}` but must silently become `(2,1,1)`, a legitimately flattened
scale is inexpressible, and it forces the inverse-transpose normal path on every
draw. Non-uniform scale goes through `Matrix`, which replaces the transform
whole and is what sets `SCENE_NONUNIFORM` at pack time
(see [Sorting, culling, and batching](#sorting-culling-and-batching)).

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
everything else. Scene builds the non-uniform matrix internally, so the scalar
`Scale` decision is untouched at the API surface. The cost is that thickness is
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
)

type PassTag string
const TagForward PassTag = "forward" // an empty PassTag reads as TagForward

type Pass struct {
    Tag        PassTag
    Target     gfx.TargetDescr // zero is the screen sentinel; NoTarget() for depth-only
    Depth      gfx.DepthDescr  // zero is DepthAuto
    ClearColor *m.Color        // nil preserves
    ClearDepth *float32        // nil preserves; 1.0 is the useful value
    Order      gfx.Order       // offset from the camera id
}

type CameraDescr struct {
    Transform  Transform       // the camera as a positioned object; scene inverts it
    Projection ProjectionKind
    FovY       float32         // Perspective: vertical field of view, radians
    Height     float32         // Orthographic: world units across the target's height
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

**A camera is a positioned object, not a view matrix.** `LookAt(eye, target, up)`
returns a `Transform`; scene inverts it with the allocation-free `m.InverseAffine`.
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

**The projection is resolved per pass**, from that pass's target aspect, at
flush. A camera's passes may target different sizes — a 1024×1024 shadow map and
the screen — so there is no single camera aspect. Aspect sources:

| pass colour target | aspect from |
| --- | --- |
| screen sentinel | `app.Viewport.WindowWidth` / `WindowHeight` |
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
`ClearDepth` is **1.0**. The naive `ClearDepth: &zero` clears to the *near*
plane and hides the entire scene.

### Passes

```go
func defaultPass(id CameraID) Pass {
    return Pass{Tag: TagForward, ClearDepth: &one, Order: gfx.Order(id)}
}
```

An empty `Passes` means exactly one pass: tag `forward`, screen target, `Order`
at the camera id, **colour preserved and depth cleared to 1.0**.

The asymmetry is deliberate. Defaulting to a **colour** clear would let a second
camera silently erase the first. But a `DepthAuto` pass shares its pooled depth
texture with every other same-size `DepthAuto` pass in the frame, **so it must
clear depth or it inherits garbage** — which would make the simplest possible
scene render against garbage depth. Two cameras compositing into one target
almost always want independent depth; the exception (a weapon view depth-tested
against the world) is exactly the case that should have to write an explicit
`Pass` with `ClearDepth: nil`.

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
with `NoTarget()` and no explicit depth, or with `NoTarget()` and a non-nil
`ClearColor`, is a reported error.

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

---

## glTF models

([glTF model draw semantics and loading](https://github.com/dvoyni/cog/issues/14),
[glTF 2.0 feature inventory and loader choice](https://github.com/dvoyni/cog/issues/5))

```go
type ModelDraw struct {
    Transform      Transform
    Transforms     []Transform // non-empty overrides Transform, one instance per entry
    Scene          string      // entry in the file's scenes array; empty is the default scene
    Node           string      // subtree within that scene; empty is the whole scene
    Plays          []ClipPlay
    MorphWeights   []float32
    Material       Material    // nil is the bundled PBR
    OverrideParams []gfx.ParameterDescr
}
```

### Loader

Parse with [`github.com/qmuntal/gltf`](https://github.com/qmuntal/gltf)
(v0.29.0, BSD-2-Clause) as a **parse layer only**: zero runtime dependencies, a
`Decoder` taking an `fs.FS` that maps straight onto `storage.FileSystem`, and a
clean `GOOS=js GOARCH=wasm` build. Scene converts the `gltf.Document` into its
own mesh, material and baked-pose types **in one pass at load and drops it**; the
document never appears in scene's API. Decode allocates about 2× file size once.

**Supported:** GLB and `.gltf` with external buffers and images, interleaved and
sparse accessors, 8/16/32-bit indices, per-primitive materials, generated flat
normals and tangents when missing, node tree with default scene, skins with
`inverseBindMatrices`, morph POSITION/NORMAL/TANGENT deltas with default weights,
animations on TRS and `weights` channels with STEP/LINEAR/CUBICSPLINE (all
resolved at load, since clips are baked), the full metallic-roughness material
set with two UV sets, alpha modes, double-sided, and samplers.

**Extensions in v1:** `KHR_texture_transform`, `KHR_materials_emissive_strength`,
`KHR_mesh_quantization` (dequantised at load, including morph deltas),
`KHR_lights_punctual` parsed and exposed **as data** for the app to declare —
nothing in scene converts a glTF light automatically, and plain directional
lights beyond the camera's sun are dropped. Rejected in `extensionsRequired`,
failing the model wholesale: Draco, meshopt, basisu, webp. Other `extensionsUsed`
are ignored.

**WebGPU gaps the loader papers over:** no `uint8` indices (widen to 16),
no 3-component 8/16-bit vertex formats (dequantise), no fan or loop topology and
no strips (converted to triangle lists at load, so every model mesh is one
topology and batching, index buffers and the skinning path never branch on it),
and no mipmap generation API (CPU box filter).

### Addressing

`path` names the file and is the **only cache key**. `Scene` names an entry in
the file's `scenes` array (empty is the default scene). `Node` names a node
within that scene (empty is the whole scene) — a **plain name, first depth-first
match**, not a slash path; a duplicate name reports once and keeps the first.

**Nodes only.** glTF `mesh` names are optional, non-unique and carry no place in
space, so a mesh is not addressable by name. Selectors are not encoded in the
path (`"props.glb#crate"`), because that makes the cache key a parsed string.

**A `Node` draw re-roots.** The node's authored world transform inside the file
is discarded and the draw's `Transform` replaces it, descendants keeping their
relative transforms. So `props.glb` + `Node: "crate"` behaves as an independent
asset however the artist laid the file out. `Node: ""` keeps the scene's root
transforms, because a scene *is* authored as one unit.

**A draw with an unmatched `Node` skips**, and never falls back to the whole
scene. One typo'd node name rendering an entire building at the origin is the
worse failure ([Model lookup facade](https://github.com/dvoyni/cog/issues/21)).

### Flattening

Load walks the selected scene depth-first into a flat, **subtree-contiguous**
list of `{primitive, localMatrix, material, joint}`, each `localMatrix`
accumulated relative to the scene root. Depth-first order is exactly what makes
a subtree a slice rather than a filter, so a `Node` draw takes that node's
contiguous slice.

Three consequences:

- **A skinned node's own transform is ignored** per the glTF spec, so skinned
  primitives get an identity `localMatrix`.
- **A baked `localMatrix` with negative determinant reverses winding**, so load
  creates a `FrontFace: FrontCW` material variant for those primitives. Pipeline
  state is per material and the draw gets no say.
- **A node animated by TRS channels becomes a degenerate single-joint skin** —
  see [Animation](#animation).

**The re-root inverse is resolved per instance per frame, not precomputed at
load.** If an ancestor of the named node is animated, the node's true world
transform is time-varying and a load-time inverse is the wrong matrix — the crate
would inherit its ancestor's motion, contradicting the point of re-rooting. At
load scene records each named node's chain of *animated* ancestors, usually
empty; at instance-pack time, if the chain is non-empty, it walks the chain
against the same baked pose rows, inverts, and folds the result into the
instance world matrix. The packer already knows the clip and time, the chain is
a handful of joints, and the empty case costs nothing
([Baked pose buffer and skinning contract](https://github.com/dvoyni/cog/issues/15)).

### Materials and overrides

Each glTF material converts at load into one bundled-PBR record plus its texture
bindings, owned by the model entry and shared by every draw of it. The two
override knobs do not overlap:

| knob | behaviour | case |
| --- | --- | --- |
| `Material != nil` | **replaces wholesale**; the file's PBR records are not bound and their parameters do not survive | dissolve, silhouette, depth-only |
| `OverrideParams` | **merges by name** over each primitive's own record into a per-draw copy in the frame arena, keeping the file's textures | team colour, hit flash, fade |

A nil `Material` with no overrides binds the file's records directly, with no
copy. `OverrideParams` **broadcasts** to every material the draw binds — all six
of a multi-material model's — which is what the common per-draw override wants.
It is matched against the *resolved tag entry*, and a name that entry's shader
does not declare is **ignored rather than reported**: that is what keeps the
broadcast safe across tags.

### Loading

**Loading is asynchronous and a non-resident model is skipped, never
substituted.** A draw of a non-resident path enqueues a load through
`kernel.ExecuteCommandAsync` and draws nothing this frame — no placeholder. The
command parses, decodes and bakes CPU-side holding **no locks**, then acquires
`*gfx.ResourceQueue` write only for the upload.

Synchronous loading, canvas's model, was rejected: applied to a 47 MiB model or
a 24-joint three-clip rig it is a multi-hundred-millisecond hitch mid-frame.
`Preload(path)` is the same command fired without a draw, so an app moves the
hitch into a loading screen it controls. The honest limit: on `js/wasm` there is
no parallelism, so a decode still occupies the one thread; the win is desktop,
plus a draw call that never blocks anywhere.

**Residency is atomic and per path**, in `loading | resident | failed`. A model
becomes resident only when geometry, baked poses, material records **and every
one of its textures** are uploaded in that one command — there is no half-drawn
model. An in-flight path is a state, not an absence, so a model drawn every
frame while loading enqueues exactly one command. **`failed` never retries**: a
typo'd path must not spawn a load command every frame forever, and the state
clears only on unload.

**Partial failure binds a fallback.** A model that parses but is missing a
texture still becomes resident; the missing texture binds a 1×1 white texel and
reports once. A **rejected required extension** is different — there is no
geometry to fall back to, so the model fails wholesale.

**Textures** live in a scene-owned cache, never canvas's atlas (wrap modes, mips
and per-texture samplers rule the atlas out). Keyed by **resolved storage path**
and shared across models; GLB-embedded images key on `(modelPath, imageIndex)`.
**No refcount**: nothing unloads automatically, so there is nothing for a count
to drive.

**Unload is explicit and lands at the frame boundary**, so a same-frame unload
never dangles a live draw. `UnloadModel` frees geometry, baked poses and
material records **only — it does not cascade to textures**, because with no
refcount it cannot know whether another resident model shares them by path.
Unloading an absent path is a no-op. Each entry carries a **generation counter**,
so an unload while a load is in flight makes the completing load discard its
result rather than become resident as a ghost. A later draw of an unloaded path
reloads it.

**Failures report once** through `kernel.ReportError`, keyed `"model:"+path` and
`"texture:"+path`, cleared on a successful load and on unload — canvas's
precedent. Because the report fires from the load command's goroutine it lands a
frame or more after the draw that triggered it: **an error can outlive the draw
call that caused it**, and a caller who draws a bad path once and never again
still gets exactly one report.

**Bounds** come from the POSITION accessor `min`/`max`, which glTF requires,
computed per primitive in the flattened local space and expanded by the summed
maximum morph position delta. A primitive whose accessor carries no `min`/`max`
makes the **whole model never-cull, reported once**.

---

## Buffer-built meshes

([Buffer-built models: static and dynamic](https://github.com/dvoyni/cog/issues/22),
[Buffer update strategy: static, per-frame, and dynamic](https://github.com/dvoyni/cog/issues/3))

**The line to remember: `Mesh` covers "the vertices change", `Model` covers "the
vertices are deformed by weights or bones".** A caller wanting deforming
procedural geometry owns its vertices and uses `UpdateMesh`, blending on the CPU.

```go
type MeshRef struct{ /* source, id, generation — all unexported */ }
func (r MeshRef) ID() uint32 // 0 when none

func (q *OpQueue) TemporaryMesh[TVertex VertexLayout](vertices []TVertex, indices []uint32, topology gfx.PrimitiveTopology) MeshRef
func (la LookupAccess) BakeMesh[TVertex VertexLayout](vertices []TVertex, indices []uint32, topology gfx.PrimitiveTopology) MeshRef
func (la LookupAccess) UpdateMesh[TVertex VertexLayout](ref MeshRef, vertices []TVertex, indices []uint32) bool
func (la LookupAccess) ReleaseMesh(ref MeshRef)

type MeshDraw struct {
    Transform  Transform
    Transforms []Transform // non-empty overrides Transform
    Material   Material    // nil is the bundled PBR
    Params     []gfx.ParameterDescr
    Bounds     m.Vec4      // xyz centre, w radius, local space
    NeverCull  bool
}
```

`MeshRef` is an opaque value struct with unexported fields and a **discriminated
source** (durable or frame-local), a dense scene id, and a generation counter —
mirroring `gfx.BufferDescr`, the only place in cog that already serves both a
frame-local and a durable path from one type. The zero value means none. `source`
is what makes a temporary ref used in a **later** frame detectable rather than
silently wrong.

Both mint functions feed the single `q.Mesh(layers, ref, draw)` recording call.
An anonymous inline call, canvas's `DrawTriangles` shape, was rejected because
sorting requires a dense `meshID` on every draw and an anonymous call has none.
The inline path costs one extra statement and buys one `MeshDraw`, one sort key
and one culling rule instead of two of each — affordable because scene's
*throwaway geometry* floor is the debug vocabulary, not this API.

### Vertices

The generic mechanism is canvas's verbatim: a plain-data struct implementing
`VertexLayout() []gfx.VertexAttr`, memcpy'd through `unsafe.Slice` into an
arena, its layout id cached by `reflect.Type`. Go 1.27 permits type parameters
on **methods**, so both mint functions carry `[TVertex]` directly.

Typed over raw `[]byte` + `[]gfx.VertexAttr` because the type system ties data
to layout, and because it lets scene recognise the standard layout **by type**
rather than by comparing attribute slices — which the custom-material check and
the bounds computation both need.

`scene.Vertex` carries **all eight** attributes of the bundled PBR layout, 84 B.
A buffer-built mesh never skins, so the last 24 bytes are dead — but the Go zero
value of `Joints`/`Weights` is **correct**, because the no-skin flag means the
shader never reads them. There is no trap to document, there is one struct rather
than two, and both paths stay a pure `unsafe.Slice` append with no per-vertex
conversion. A 60-byte static subset expanded at pack time was rejected because it
charges the **per-frame** inline path an expansion loop over every vertex, every
frame, on the largest data this API produces.

**A custom vertex layout requires a custom material.** The bundled PBR is one
shader module with one vertex stage and no entry-point selection, so its inputs
are locations 0..7 at those exact types. The reverse is fine. A violation is
**reported once and the draw skipped** — checked at bake time for a durable ref
and first draw for a temporary, so the report fires once per ref.

### Topology and indices

One `topology` argument, zero value `TriangleList`, every gfx topology passed
straight through. Indices are `[]uint32` only, matching `gfx.MeshDescr` and the
finding that WebGPU has no `uint8` indices. The bundled PBR is documented as
meaningful for triangles only; a custom shader doing point sprites or a
wireframe overlay is legitimate and costs scene nothing to allow.

### Baking is deferred, and holds no gfx lock

`BakeMesh` mints the scene id and returns the ref **immediately**, queueing the
upload onto the `Lookup` for scene's own flush to drain — the flush already
write-locks `*gfx.ResourceQueue`, so a mesh baked and drawn in the same update
handler uploads in that same frame.

This keeps `LookupAccess` **GPU-free**, canvas's deliberate design. The house
style of write-locking `*gfx.ResourceQueue` in the app handler and threading it
down was rejected because `BakeBuffer` dereferences its backend with **no nil
guard**, so every caller would have to gate on `Ready()` and a mesh baked at
startup before the backend is installed would either panic or silently not exist.
Deferring gates it in one place.

`BakeMesh` **copies** the caller's bytes into a scene staging arena at call time
and hands `BakeBuffer` `copyData: false` at flush — one copy total. Consequently
the ref is a **scene** id and the `gfx.BufferDescr` lives inside the `Lookup`.

`UpdateMesh` is a **wholesale re-bake at any size**, deferred the same way,
recomputing the baked sphere in the pass that copies. There is **no capacity
concept in the API**: `ReBakeBuffer` already re-bakes at any length while
preserving the id, so growth is free at the gfx level. Sizing to a maximum vertex
count is a *performance* note about `CreateBuffer` plus bind-group invalidation,
not a constraint the API encodes. `UpdateMesh` rejects, reported once, a change
of vertex **layout** or **topology** — the pipeline key and the `meshID` both
assume they are fixed for the ref's life — and calling it on a temporary ref.

`ReleaseMesh` is explicit, frame-boundary and generation-counted. Drawing a
**released or stale** ref skips the draw and **reports once**, keyed by the ref's
id: a mesh that quietly stops appearing is the same failure class the load rules
guard against, and the generation counter is what makes a recycled id detectable
rather than drawing whatever now occupies that slot.

Invalid input — zero vertices, an index out of range, or an index count that is
not a multiple of 3 under `TriangleList` — is **reported once and yields a zero
`MeshRef`**, which then skips at draw time under the stale-ref rule. This departs
from `canvas.DrawTriangles`, which silently returns on bad input, because that is
a per-frame recording call where a report would spam every frame whereas a bake
happens once.

### Buffer lifetimes

There is no static/dynamic split at the API level: in WebGPU memory type is
chosen only by `MAP_*` flags, and every GPU-readable buffer is updated by a copy.
`queue.writeBuffer` is the recommended default on both paths — gogpu's native
path runs a 256 KiB-chunk staging belt flushed ahead of the user command buffer
in the same submit, and the browser path is `CopyBytesToJS` + `writeBuffer`. The
split that matters is **lifetime**, which gfx already encodes:

| lifetime | surface |
| --- | --- |
| durable geometry, baked poses, morph deltas | `ResourceQueue.BakeBuffer`, `ReleaseBuffer` on unload |
| per-frame instances, animation, lights, per-pass camera blocks | `BufferWithBytes` in the `OpQueue` temporary arena |
| caller-owned dynamic meshes | `BakeBuffer` + `ReBakeBuffer` (or the arena if regenerated every frame anyway) |

gfx needs nothing new for v1 and must not grow a mapped staging ring.
`gfx.BufferDesc.Dynamic` is dead code — never read, and a pure function of
`Kind` — and is deleted.

---

## Animation

Animation is **stateless**: each draw passes clip plays `{clip, time, weight}`,
and gameplay or the `anim` plugin owns time. Everything is baked at load;
runtime skinning and morphing are entirely vertex-shader work. **There are no
compute shaders in this scope.**

### Baked poses

([Baked pose buffer and skinning contract](https://github.com/dvoyni/cog/issues/15),
[GPU skinning and morph techniques survey](https://github.com/dvoyni/cog/issues/6))

**A pose record is 48 B — `[rot.xyzw][trans.xyz, _][scale.xyz, _]`** — three
aligned `vec4` loads, f32, holding **`globalJoint` alone**. Scale is a `vec3`.

Two rejected alternatives, both worth recording because each looks cheaper:

- **Premultiplying `globalJoint * inverseBind`** (the original research
  recommendation) is overturned. Inverse bind matrices routinely carry
  non-uniform scale from the bind pose, and premultiplying injects it into a
  record that is then decomposed to TRS, which cannot represent shear at all;
  degenerate single-joint skins have no inverse bind to premultiply; and the
  unpremultiplied buffer literally contains bone world transforms, which is what
  the fogged bone-socket work needs. `inverseBind` is instead a small per-skin
  `array<mat4x3>` — one entry per joint, not per joint-frame — applied *after*
  the cross-play blend. Cost is one extra 48 B fetch and one 4×3 concat per
  *influence*, from an array small enough to stay L1-resident.
- **A 32 B record** with scalar scale packed into translation's `w` would cut
  pose memory and per-vertex loads by a third — the single largest cost in this
  design. It is rejected because **squash-and-stretch is animated non-uniform
  scale**, a mainstream idiom, and unlike `Transform` there is no `Matrix`
  escape hatch to fix it at, so a stretched bone would be silently averaged away
  with no call site to correct.

**Normals and tangents use a precomputed normal matrix, never a shader inverse.**
Because the inverse bind can be non-orthonormal, so can the composed skinning
matrix. Load computes `transpose(inverse(inverseBind))` per joint into a second
per-skin `array<mat3x3>`. The shader transforms the normal by the blended TRS
rotation (orthonormal, so direct), then by that matrix, then renormalises; the
tangent takes the same path plus Gram-Schmidt against the skinned normal, and its
`w` handedness flips from the inverse bind's determinant sign, also precomputed.

**Sample rate is 60 Hz, global**, from `Config.PoseSampleRate`; linear
interpolation, clip duration rounded up to whole frames. No per-clip override:
glTF has no field to express one. 60 rather than the researched 30 because
`STEP` channels and `CUBICSPLINE` curves are exactly what 30 Hz degrades visibly,
and glTF has both. **Per-vertex cost is unaffected by the rate** — always two
frames per play — so only storage doubles, and at demo scale that is noise
(a 24-joint three-clip rig is ~350 KiB).

**One joint index space per model.** Every skin's joints and every degenerate
node joint share a single numbering, remapped from each primitive's `JOINTS_0`
at load. Rows lay out per model path as `[rest frame][clip 0][clip 1]…`, so

```
row = clipBase + frame * jointCount + joint
```

is one MAD in the shader and the instance record needs only `clipBase`. The waste
is `(joints this clip does not animate) × frames`; the pathological file — many
independently-animated props sharing one file *and* per-prop clips — is precisely
what `Node` re-rooting exists to split apart.

**Row 0 of every model is an implicit rest frame**, the authored node hierarchy
resolved once. Without it a draw with `Plays: nil` has nothing to place its
geometry, because skinned nodes get an identity `localMatrix` and a degenerate
node's transform lives in the pose buffer — the model would collapse to the
origin. One extra frame per model (~1 KiB) makes `Preload` plus draw-with-no-plays
legal, and defines the zero-total-weight case.

**Any node targeted by a clip's TRS channels becomes a degenerate single-joint
skin** with a weight-1.0 binding; nodes no clip touches bake flat into
`localMatrix`. Rigid node animation — wheels, propellers, doors — is ordinary
glTF, so the alternative was a second animation mechanism with the per-frame CPU
hierarchy walk this design exists to eliminate. **The rule keys on TRS channels
only**: a node whose clip touches only its `weights` channel creates **no joint**
and keeps its authored `localMatrix`, so a morph-only model loads with zero
joints and an empty pose buffer
([Morph target contract](https://github.com/dvoyni/cog/issues/16)).

### Clip plays

```go
type ClipPlay struct {
    Clip   string
    Time   float32
    Weight float32
    Loop   bool
}
```

**Clips are addressed by name**, first match; an unknown name is reported once
and the play dropped.

**`Loop` is on the play, not the caller's time.** Gameplay owns time and `Time`
arrives already advanced, but the frame *pair* at the seam — last frame to frame
1, row 0 being the rest pose — can only be built by whoever knows the clip wraps,
and that is not derivable from a raw time value. `Loop` false clamps to
`[0, duration]`; true takes `Time` modulo duration, which makes negative time
legal and a reversed animation free.

**Caps: 4 influences, 4 plays, no joint ceiling.** Influences are `JOINTS_0`
only. A 5th play is **dropped by lowest weight and reported once per model**.
There is deliberately no joints-per-model cap: poses live in a storage buffer
indexed by row against a 128 MiB binding, so the only real limit is glTF's own
u16 joint index.

**Play weights are normalised on the CPU** at pack time. The blend is a weighted
*mean* of TRS, not an additive layer: weights summing to 0.5 do not half-apply
the animation, they shrink every bone's translation toward the origin and mangle
the character. There is no legitimate non-unit sum, so "as given" would preserve
only the ability to express a bug. A total of ~0 falls back to the rest frame.

Per play the CPU folds `weight * (1 - frac)` and `weight * frac` into one scalar
each and emits `{baseRow0, baseRow1, w0, w1}` (16 B), so the shader does no
clip-length, wrap or normalisation arithmetic. It accumulates weighted TRS per
influence with quaternions sign-fixed against the running accumulator,
normalises, builds a 4×3, applies the inverse bind, and runs standard linear
blend skinning over 4 influences. Quaternion hemisphere continuity is fixed
within a clip **at bake**, so the frame lerp needs no runtime check.

**Unrepresentable data is best-effort plus one report.** Compose along the
hierarchy, decompose, recompose, and if the residual exceeds an epsilon report
once keyed `"model:"+path` and bake the decomposition anyway: a slightly wrong
elbow beats a missing character, and shear is invisible on virtually every real
rig. A single-keyframe clip and a zero-duration clip each bake to one frame;
neither is an error.

**Skinning is model-only.** `MeshDraw` has no `Plays` field and no joint concept.

### Morph targets

([Morph target contract](https://github.com/dvoyni/cog/issues/16))

**Morphing is linear in the weights**, so blending N plays' weight vectors on the
CPU and applying the deltas once is *exactly* equal to morphing per play and
blending the results. Unlike the pose case there is no approximation to trade
away, so the entire morph blend is CPU-side and **the shader never sees a play**.

glTF `weights` channels bake onto the same 60 Hz grid as a plain CPU-side
`[]float32` per clip (`frames × targetCount`) that **never reaches the GPU**. At
pack time the CPU does the two-frame lerp per play and the weighted mean across
plays using the already-normalised play weights, and emits one final weight
vector.

**`MorphWeights` is positional** — the one index-addressed thing in the plugin.
Name addressing would put ~52 map hits per face per frame on the recording path
to re-derive a mapping the caller computed at startup; naming lives on the lookup
facade as `MorphTargets(path)` instead, so the per-frame path is a memcpy.
A non-nil `MorphWeights` **overrides the animated result wholesale**; nil falls
back to animated weights, then `node.weights`, then `mesh.weights`, then zero. A
**short slice leaves the remaining targets at 0**; a long slice ignores the tail
and reports once per model. Neither is an error.

**A slot belongs to a node, not a mesh.** glTF requires every primitive of a mesh
to carry the same targets in the same order, so a 3-primitive 8-target mesh
contributes **8** slots, not 24 — and `node.weights` overrides `mesh.weights`, so
two nodes referencing the same mesh have independent weights. The flattened list
is one entry per target of every morphed node in depth-first node order, so a
duplicated head appears as two runs of the same names. The *deltas* stay shared
per mesh, byte-identical between those two nodes: `morphBase` points at the
mesh's block while the weights come from the node's slots.

**One `sceneMorphDeltas` buffer per model.** One buffer per *primitive* is
overturned: it would mean a bind group per primitive, collapsing group 2's whole
reason for existing. Every morphed primitive's targets concatenate into the one
buffer and are reached by a base offset.

**Records are vec4-aligned and masked, stride 16 / 32 / 48 B**, slot order fixed
as position, normal, tangent, `stride = 16 * popcount(attrMask)`. Most targets in
real assets carry POSITION only, and a fixed 48 B record stores explicit zeros
for the rest — a 10k-vertex face with 52 shapes is 25 MiB at 48 B and ~8 MiB when
position-only targets cost 16 B. A tightly packed 12/24/36 B layout is smaller
still but forces scalar `array<f32>` indexing, 9 loads per target per vertex
instead of 3: memory bought with per-vertex bandwidth, the wrong direction.

**The mask is per primitive**: the union across that primitive's targets,
intersected with the base primitive's **authored** attributes. Per-target masks
would make the stride vary *within* a block, so the shader could no longer compute
an address with one multiply. Intersecting against authored attributes matters
because scene generates flat normals for a primitive that has none — those are
scene's reconstruction, not the asset's, so a NORMAL delta on a primitive with no
authored NORMAL is dropped at load.

Addressing needs **no base-vertex correction**: `gfx.MeshDescr` owns its own
buffers and always binds them at offset 0, so `@builtin(vertex_index)` is 0-based
within a primitive and

```
i = morphBase + target * morphTargetStride + vertexIndex * morphStride + slot
```

indexes `sceneMorphDeltas` directly, with `morphTargetStride = vertexCount *
morphStride` folded on the CPU.

**Caps: 64 active targets**, culled first by `|w| < 1e-5` — absolute value,
because glTF does not clamp weights to `[0,1]` and a negative weight is
meaningful — then, if still over, dropped by lowest absolute weight and reported
once per model. Stored targets are unlimited. With sparse packing the cap no
longer constrains memory or layout at all and is purely a guard against runaway
per-vertex ALU.

**The morph half adds deltas and normalises nothing.** The naive reading of glTF
renormalises the normal after morphing *and* after skinning; the first is dead
work, because skinning is linear. Tangent deltas add to `xyz` and leave `w`
untouched.

**Bounds** expand at load by the maximum position-delta magnitude summed over the
targets: conservative (it assumes every target at weight 1 at once), computed in
one pass over delta data already being read, and it over-draws rather than
under-draws.

**Instancing morphed geometry saves draw calls, not vertex work.** 100 morphed
heads is one draw and 100× the morph ALU.

Buffer-built meshes have **no morph targets** in v1.

---

## Lights

([Lighting model and limits](https://github.com/dvoyni/cog/issues/17))

Naive forward: one light list per pass, **every shaded fragment loops all of
it**. The honest consequence, stated rather than hidden: adding a light costs
every shaded pixel in the pass, which is what makes the cap load-bearing rather
than decorative.

The sun and hemispheric ambient are per-camera fields
(see [Cameras and passes](#cameras-and-passes)); the array holds point and spot
lights only, which is what makes the record branchless and 48 bytes with **no
`kind` field**.

```go
type LightDescr struct {
    Position   m.Vec3
    Direction  m.Vec3  // direction of travel; zero vector for a point light
    Color      m.Color // linear
    Intensity  float32 // zero means 1
    Range      float32 // zero means infinite
    InnerCone  float32 // radians; zero is a real value (falloff from the axis)
    OuterCone  float32 // radians; zero means pi/4, glTF's default
    Kind       LightKind
}
```

`PointLight` and `SpotLight` set `Kind` themselves over the one struct, so call
sites stay explicit, a hand-written point light leaves the cone fields zero, and
the per-camera light buffer is homogeneous without scene converting between two
structs.

```wgsl
struct SceneLight {
    position:   vec3<f32>,  // world
    invRange4:  f32,        // 1/range^4; 0 is infinite
    direction:  vec3<f32>,  // direction of travel; zero vector for point
    spotScale:  f32,        // 0 for point
    color:      vec3<f32>,  // linear, Intensity premultiplied
    spotOffset: f32,        // 1 for point
}

let toLight  = light.position - P;
let d2       = dot(toLight, toLight);
let L        = toLight * inverseSqrt(max(d2, 1e-12));
let window   = saturate(1.0 - d2 * d2 * light.invRange4);
let cone     = saturate(dot(-L, light.direction) * light.spotScale + light.spotOffset);
let radiance = light.color * window * cone / max(d2, 1e-6);
```

**Attenuation keeps glTF's falloff geometry but not its photometry.**
`Intensity` is **unitless**, documented as "radiance at one world unit".
Photometric units were rejected on a hard constraint, not taste: targets are
`RGBA8Srgb`, tonemapping is out of scope, so shading lands directly in 0..1 with
**no exposure control anywhere** — a 60 W bulb is ~64 cd and every frame would be
pure white. When HDR lands this becomes photometric by redefining the unit and
changing nothing else. `max(d2, 1e-6)` is a robustness guard for a light sitting
on a surface, not a falloff parameter.

**`Range` zero means infinite**, glTF's own default. Storing `invRange4 =
1/range^4` rather than `range` makes `saturate(1 - d^4 * invRange4)` evaluate to
exactly 1 when it is 0 — no branch, no `select`, no special case. Bug visibility
runs the right way too: a forgotten `Range` yields a light that reaches too far,
which you see immediately, rather than a silently skipped light. The residual
cost is that **an infinite-range light is unculled by construction and always
survives to the cap.**

**Spot cone** is `KHR_lights_punctual`'s smoothing, linear in cosine, with the
CPU precomputing:

```
spotScale  = 1 / max(cos(InnerCone) - cos(OuterCone), 1e-4)
spotOffset = -cos(OuterCone) * spotScale
```

so the shader is one `dot`, one MAD, one `saturate`. `InnerCone >= OuterCone` is
reported and the light skipped. **Trap worth a line:** a point light's
`direction` must be packed as an **actual zero vector**, not left uninitialised —
the `spotScale = 0` trick relies on `x * 0 == 0`, which is false for `NaN`.

**The sun stays out of the array.** Packing it as a directional entry would cost
an explicit `kind` field (with infinite range taken, no free discriminator is
left) and waste position, range and cone on that entry. The tiebreaker is that
the unification is unachievable anyway: **hemispheric ambient is normal-dependent,
not a direction**, so it can never join the loop.

**Culling: per pass, at flush, before the cap.** Sphere `(Position, Range)`
against that pass's frustum. A camera's 1024×1024 shadow pass and its screen pass
can see different light sets, which is correct rather than surprising. Culling is
what makes the cap survivable: a level with 40 lights of which 6 are on screen
works perfectly.

**Cap: 16, silently, by contribution.** Past 16, scene keeps the 16 with the
highest contribution at the camera position — each light's own falloff evaluated
at the eye, times its colour's luminance — and drops the rest **with no error
reported**. The silence departs from the "reported once" rule used for plays and
morph targets, and the difference is principled: those caps are static and
asset-shaped, so "once at load, per model" is a well-defined moment and the
excess is an authoring mistake. A 17th light is **dynamic and camera-shaped** —
it appears when you turn around — so there is no natural "once", a per-frame
report is pure noise, and the degradation is continuous by construction, since
the lights dropped are exactly the ones contributing least. Record order was
rejected as the drop policy because it silently punishes recording order, which
a caller has no reason to believe is significant.

16 is a **fixed constant, not a `Config` knob**: a knob needs documented
interaction rules, and the answer to "I need 40 lights" is clustered lighting,
not a number that makes the naive loop slower. Being fixed is also what lets the
member be declared `lights: array<SceneLight, 16>` — 768 bytes inside
`sceneFrame` — rather than a runtime-sized array, which is a strictly smaller ask
on gfx's reflection work. `lightCount` still bounds the loop. The over-16
selection is a fixed `[16]` insertion by contribution — no sort, no allocation.

**A light's `LayerMask` is filtered against the camera's `CullMask` only.** It
decides which cameras' light buffers the light lands in; it does **not** decide
which objects the light illuminates. This is the natural misreading and it must
be stated bluntly, because the other reading requires a per-draw light list,
which contradicts group 0 being invariant for a whole pass.

`Intensity` is premultiplied into `color` at pack time for the punctual array,
the sun and both ambient colours: it removes a per-fragment multiply and costs
nothing. All colours are **linear**, so an sRGB-picked literal must go through
`m.NewColorSrgb`.

**Nothing is reserved for shadows** — no sun shadow matrix, no comparison sampler
slot. An unused matrix and an unbound sampler are dead weight, and reflection
would type the sampler wrongly anyway, so the reservation would not even be
usable as reserved.

---

## Bundled PBR material

([Bundled glTF PBR material contract](https://github.com/dvoyni/cog/issues/18))

The bundled PBR is a `gfx.MaterialDescr` like any other, wrapped in a `Material`
with **one `forward` entry and nothing else**. `MeshDraw.Material` /
`ModelDraw.Material` nil selects it.

### Parameters are glTF's names, verbatim

`baseColorFactor`, `baseColorTexture`, `metallicFactor`, `roughnessFactor`,
`metallicRoughnessTexture`, `normalTexture`, `normalScale`, `occlusionTexture`,
`occlusionStrength`, `emissiveFactor`, `emissiveTexture`, `alphaCutoff`.

These are **user-facing**: `OverrideParams` merges by name, so
`gfx.ColorParam("baseColorFactor", c)` is what a caller writes to tint a model.
Verbatim naming means the loader maps 1:1 with no translation table to drift, and
**the glTF specification becomes the parameter documentation** — including the
exact semantics of `occlusionStrength` and `normalScale`, which are easy to get
subtly wrong from memory. The storage binding itself is `scenePbrMaterial`,
reserved-prefixed, because no caller ever addresses the whole record.

**Five samplers**, one per slot (`baseColorSampler`, `metallicRoughnessSampler`,
`normalSampler`, `occlusionSampler`, `emissiveSampler`). glTF references a
sampler per texture and two slots of one material can legitimately differ — a
tiling ground beside a clamped decal — so a single shared sampler would silently
mis-sample a legal file. `SamplerDesc` stays comparable precisely so
`gfx/translate.go` dedupes identical descriptors to one object, making the GPU
cost near zero. Groups 0 and 2 use no samplers at all.

### Absent slots bind 1×1 defaults, and there are only two

WGSL requires every declared binding bound and gfx has no preprocessing, so
omitting a texture is not available without shader variants. Scene owns the
defaults and always binds all five; the factor parameters multiply through
unchanged.

Only **two** textures are needed: **a single white texel serves baseColor,
metallic-roughness, occlusion and emissive**, because 1.0 is a fixed point of the
sRGB transfer curve, so the sRGB-format slots and the linear-format slots both
read 1.0 from it. The second is the flat normal `(0.5, 0.5, 1)`. 1×1 rather than
larger because uploads go through `queue.WriteTexture`, which carries no 256-byte
row alignment, and for a constant texel every mip level is identical.

### Per-slot texture metadata is flattened, not arrayed

Each slot carries a UV-set selector and a `KHR_texture_transform`, declared as
**flat named members**:

```wgsl
baseColorTransform: vec4<f32>,   // offset.xy, scale.xy
baseColorRotation:  f32,
// ... x5 slots, plus a packed 5-bit uvSets selector
```

not `transforms: array<TexTransform, 5>`. gfx packs the record from name-matched
parameters and array members are not name-addressable, so flattening keeps every
member reachable through `OverrideParams` — and that buys a real capability
rather than symmetry: **animating `baseColorTransform` per frame *is* UV
scrolling** (water, lava, conveyor belts, scan lines), which the array form
forecloses permanently. It costs nothing to carry, since the record is a
256-byte-aligned bound range and the actual fields come to roughly 64 bytes, and
it widens no reflection ask.

**UV sets are capped at two** (TEXCOORD_0/1, glTF core's minimum), selected per
slot by one `select`. A slot naming `texCoord >= 2` is reported once and falls
back to set 0 — ignoring `texCoord: 1` would be a silent wrong-output failure on
a core feature. The transform is applied unconditionally, about 30 ALU across
five slots, less than one iteration of the 16-light loop. A single shared
transform per material was rejected as the classic trap: right for the common
atlas case, silently wrong the moment two slots differ.

### The BRDF is the Khronos reference, exactly

GGX/Trowbridge-Reitz distribution, Smith height-correlated visibility, Schlick
Fresnel, Lambert diffuse, `F0 = 0.04` for dielectrics lerped to `baseColor` by
`metallic`, `diffuseColor = baseColor * (1 - metallic)`.

Every demo model is a Khronos sample authored and screenshot-verified against
this exact BRDF, which makes it the only choice where "the model looks wrong" is
a **bug** rather than an open question about which approximation was picked.

**Ambient reaches metals through an analytic specular term.** The trap:
`diffuseColor = baseColor * (1 - metallic)`, so a pure metal has zero diffuse and
would render **black** everywhere the sun and punctual lights do not reach. So
ambient splits two ways, both scaled by occlusion:

- diffuse: `sceneAmbient(N) * occlusion * diffuseColor`
- specular: `sceneAmbient(reflect(-V, N)) * occlusion * EnvBRDFApprox(F0, roughness, NdotV)`

about six ALU and one extra `mix`. Lerping toward `baseColor` by metallic keeps
metals visible but makes them behave like diffuse paint — roughness stops
affecting them, which is precisely the property a metallic-roughness workflow
exists to express. **Honest limitation:** this approximates an environment that
does not exist, so a mirror-smooth metal reflects a smooth gradient rather than
the scene. Image-based lighting substitutes into exactly these two terms.

`KHR_materials_emissive_strength` **folds into `emissiveFactor` at load** and
clamps above 1 until HDR lands. Keeping it separate would only buy animatability,
and `emissiveFactor` is itself overridable.

### Pipeline state mapping

([Pipeline state growth for 3D](https://github.com/dvoyni/cog/issues/10))

| glTF | state |
| --- | --- |
| `doubleSided: true` | `Cull: CullNone` |
| `doubleSided: false` | `Cull: CullBack` |
| `alphaMode: OPAQUE` | `gfx.StateOpaque3D` |
| `alphaMode: MASK` | `gfx.StateOpaque3D` plus a shader `discard` against `alphaCutoff` |
| `alphaMode: BLEND` | `gfx.StateTransparent3D` |
| node transform determinant < 0 | the same material with `FrontFace: FrontCW` |

`MASK` is **fixed-function-identical to `OPAQUE`** — it writes depth and batches
with the opaque geometry — and the cutoff is entirely a fragment-shader concern.
It cannot be alpha-to-coverage, which needs MSAA.

**The double-sided normal flip is unconditional**: `N = select(-N, N,
frontFacing)`. glTF requires the shading normal be flipped on back faces of a
double-sided material; making it conditional would cost a record flag and a
branch to save nothing, since for single-sided materials the select is a proven
no-op.

### One vertex layout, 84 bytes

`POSITION` Float32x3 · `NORMAL` Float32x3 · `TANGENT` Float32x4 · `TEXCOORD_0`
Float32x2 · `TEXCOORD_1` Float32x2 · `COLOR_0` Unorm8x4 · `JOINTS_0` Uint16x4 ·
`WEIGHTS_0` Float32x4. Eight of gfx's 16 attribute slots, at positional
`@location(0..7)`, well inside its 2048 stride cap.

**Nothing is truly optional.** Row 0 makes every scene mesh effectively skinned
and any animated node becomes a degenerate joint, so joints and weights are never
absent; the PBR needs tangents and both UV sets. **`COLOR_0` is included on
failure mode**, not on evidence: it is glTF core, costs 4 bytes as `Unorm8x4`,
and omitting it renders a vertex-coloured model **silently white** rather than
erroring.

**Variants were rejected for a mechanical reason.** `gfx.ShaderDescr` is
source-or-path and the backend hardcodes `vs_main`/`fs_main`, so one module is
exactly one vertex plus one fragment stage — a variant is a whole separate module
carrying **its own copy of the entire BRDF**, the failure where a shading fix
lands in some copies and not others.

**Missing attributes are generated and repacked in place at load**, not bound
from a second buffer. gfx binds exactly one vertex buffer, and multi-buffer was
rejected on the finding that **total memory is identical either way** — repacking
gives one buffer at full stride, multi-buffer the original plus exactly the
remainder — so the only saving is a copy the one-pass conversion and
dequantisation largely already spend.

**The `arrayStride: 0` broadcast trick is unusable on cog's backends.** It is
spec-legal and gogpu's browser path forwards it untouched, but the pure-Go native
path validates it not at all and its backends diverge silently: the software
rasteriser drops the whole draw, GLES reads 0 as "tightly packed", Metal sets
`stepRate: 1`. A trick that works in a browser and silently corrupts the desktop
HAL is exactly the divergence the map forbids
([gogpu/wgpu: arrayStride 0 is unvalidated and backends diverge](https://github.com/dvoyni/cog/issues/47)).

**Nothing is packed in v1.** `Unorm1010102` normals and `Unorm16x4` weights would
save 20 of the 84 bytes and gfx has both formats, but that is a load-time
encoding behind unchanged attribute names, so it waits on a measured trigger.

### Tangents

Tangents are **UV-gradient generated per triangle** when the primitive's material
has a normal map and `TANGENT` is absent, and an arbitrary orthonormal basis
otherwise. This is explicitly **not MikkTSpace**: a normal map baked against
MikkTSpace can show seams. No asset in the demo set exercises the generation path
— WaterBottle ships real tangents — which weakens the trigger rather than
strengthening it.

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
parameter bytes, costing one map hit only for draws that pass one.

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
matrix — exact under scalar `Scale`, conservative under the `Matrix` escape
hatch, since a sphere under non-uniform scale is not a sphere.

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
`Transforms []Transform` on `MeshDraw` and `ModelDraw` makes an instanced draw
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

([Model lookup facade](https://github.com/dvoyni/cog/issues/21))

```go
la := scene.NewLookupAccess(kernel, lookup) // no FileSystem

type ModelRef struct{ Path, Scene, Node string } // mirrors ModelDraw's selectors

type ModelState uint8
const (
    ModelMissing ModelState = iota
    ModelLoading
    ModelResident
    ModelFailed
)

type ClipInfo struct {
    Name     string
    Duration float32 // seconds
}
```

Bind `access.GetWrite[*scene.Lookup]()` in the handler's `Lock`, then use:

| Method | Result | Notes |
| --- | --- | --- |
| `State(path) ModelState` | residency | the only way to tell *wait* from *never coming* |
| `Preload(path)` | — | the load command fired without a draw; no return |
| `Nodes(ref, dst) ([]string, bool)` | node names | a non-empty `Node` lists that subtree |
| `Bounds(ref) (m.Vec4, bool)` | xyz centre, w radius | local space post-re-rooting; rest pose when skinned |
| `AABB(ref) (min, max m.Vec3, ok bool)` | axis-aligned box | same space and pose rules |
| `Joints(path, dst) ([]string, bool)` | joint names | names only; count is `len` |
| `Clips(path, dst) ([]ClipInfo, bool)` | clip names and durations | |
| `MorphTargets(path, dst) ([]string, bool)` | target names | one flattened list per path, depth-first node order |
| `PoseBytes(path) (int, bool)` | GPU pose memory | |
| `MorphBytes(path) (int, bool)` | GPU delta memory | |
| `TotalPoseBytes() int` | — | no bool: a sum over residents is always real |
| `TotalMorphBytes() int` | — | |
| `BakeMesh` / `UpdateMesh` / `ReleaseMesh` | see [Buffer-built meshes](#buffer-built-meshes) | |
| `UnloadModel(path)` / `UnloadTexture(path)` / `UnloadAll()` | — | queued, applied at the frame boundary |

### Every query returns `(value, ok)`, and every query triggers the load

This is the facade's real contract. A query on a `missing` path fires the **same
idempotent load command a draw fires**, so a path enters residency exactly one
way and `Preload` is an optimisation for callers who cannot tolerate a first
frame without the answer, **not a step you can forget**. Inert queries have a
silent and *permanent* failure mode: a caller who forgets `Preload` polls an
empty list forever with nothing to observe.

`ok` means **"this value is real"**, and nothing finer. It is false for an
invalid path, a missing path just queued, a loading path, a failed path, and a
resident path whose `Scene`/`Node` matched nothing. Inferring the same from a
zero return does not work: `PoseBytes` returning 0 is indistinguishable between a
still-loading model and a resident model with no skeleton, which is the whole
point of a memory report. When `ok` is false the `dst`-append accessors return
`dst` **untouched**, not a zeroed slice.

`ok` deliberately conflates *wait* with *never coming*, which is what keeps
**`State(path)` load-bearing**: `failed` is terminal, so a loading screen
watching only `ok` hangs forever on a typo'd path. There is no `Pending()`
aggregate — a caller polling a preload list it already holds can count residents
itself.

### Selectors, and what is not here

`Clips`, `MorphTargets`, `PoseBytes`, `MorphBytes`, `State`, `Preload` and the
unloads are **per path**: `path` is the whole cache key, a model has one joint
index space, and `MorphTargets` is one flattened list per path that `Node`
re-rooting does not renumber. Only `Nodes`, `Bounds` and `AABB` are
scene/node-scoped, and they take **`ModelRef{Path, Scene, Node}`** mirroring
`ModelDraw`'s own fields — three bare strings were rejected on a transposition
bug that compiles (`Bounds(p, "crate", "")` and `Bounds(p, "", "crate")` are both
valid and mean different things).

**Both bound geometries are published** because scene already has both: the tight
sphere is baked per node anyway, and the AABB is the load-time by-product it is
computed from. Publishing only the AABB would be a regression, since a sphere
derived from one is the circumsphere, up to √3 loose. `Bounds` uses the same
`m.Vec4` convention as `MeshDraw.Bounds`, so the name means one thing across the
plugin.

**Mesh names are cut** — a draw addresses nodes only, so a mesh name is a string
a caller cannot act on. **No texture queries** beyond `UnloadTexture`. `Joints`
is names only, **no hierarchy** — parents and rest transforms are what bone
sockets need and they are purely additive when that lands. `Clips` returns name
and duration together because a caller needs the duration to know when a one-shot
play has ended and to normalise `Time`.

Every list accessor appends into a caller `dst`. Returning the internal slice
read-only would be free but dangles past the handler's lock scope, which is the
one thing `LookupAccess` forbids. These are cold paths — naming lives here
precisely so a caller resolves names to indices **once at startup**.

### Failure edges

- **An invalid path never reaches a load command**, so the report-from-the-goroutine
  rule cannot see it and every query would return a silent `false` forever. The
  facade therefore validates **synchronously** (canvas's `validateResourcePath`
  rules) and records the path as `ModelFailed` — one state machine rather than a
  `reported` set beside it.
- **An unmatched `Scene`/`Node` on a resident model** returns `ok = false` and
  reports once, keyed `"model:" + path + "#" + node`.
- **Unload is the only retry lever.** `failed` clears only on unload, so
  `UnloadModel(p)` on a failed path lets the next draw or query retry, including
  recovery from an invalid path once the string is fixed. There is no
  `Retry`/`Reload`: it is `UnloadModel` + `Preload`.

### Two dependencies, not three

`NewLookupAccess(k kernel.Kernel, lookup *Lookup)` takes **no `storage.FileSystem`**,
unlike canvas's equivalent, because scene's load command opens, parses and bakes
the file itself holding no locks. A consumer system declares one fewer resource
than canvas's. This is stated because the asymmetry reads as an oversight
otherwise.

**Canvas is left inconsistent on purpose.** Canvas loads *synchronously*, so a
canvas `ok` would mean "valid and decodable" — the same shape over a different
predicate, which is worse than no convention
([canvas: async loading and the (value, ok) query contract](https://github.com/dvoyni/cog/issues/50)).

---

## Coordinate helpers

([3D-to-screen coordinate helpers](https://github.com/dvoyni/cog/issues/38))

**Pure package-level functions**, callable on any thread with no plugin instance.
A lookup against last frame's resolved camera state would buy only staleness, a
`LookupAccess` dependency in code that is otherwise arithmetic, and nothing at
all for a camera not recorded this frame. `PassView.Frustum` stays an inspection
and test surface, not a coordinate API.

```go
// viewport is the target's size in pixels: app.Viewport.Width/Height for a
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

([Bind-group frequency convention](https://github.com/dvoyni/cog/issues/9))

The frequency convention is a **WGSL group-numbering contract**, not new gfx
machinery. gfx binds whatever reflection reports and never renumbers, so scene
expresses its three frequencies purely in shader source. **This binds scene
shaders only**; canvas keeps its own numbering untouched.

| group | frequency | contents |
| --- | --- | --- |
| 0 | per pass | `sceneFrame`, `sceneInstances`, `sceneAnim` — bound once per pass |
| 1 | per material | the material record as a bound range, plus material textures and samplers |
| 2 | per model | baked poses, inverse binds, normal matrices, morph deltas |
| 3 | — | unassigned, reserved |

Ascending frequency, lowest group changing least. Web's floor is 4 bind groups,
so three fit with one spare for shadows or post-processing to claim without
renumbering.

### Scene declares no uniform block

All numeric data lives in **storage buffers** from the per-frame arena. gfx's
uniform path gives every draw its own pooled 256-byte buffer and its own
`WriteBuffer` — a thousand draws is a thousand buffers, a thousand uploads, and a
256-byte cap. Scene abandons it entirely: there is no cap and one upload instead
of a thousand. The honest cost is that a uniform read is scalar-uniform across a
wave while a storage read indexed by `instance_index` is not — small, and not
worth two shader paths. `uniformMax` stays at 256 for the uniform path canvas
uses; scene never reaches it.

### Material records are bound ranges, not indices

```wgsl
@group(1) @binding(0) var<storage, read> material: PbrMaterial;
```

read directly, with no subscript. A `u32` material index in the instance record
**does not work**: gfx packs at translate time on the render thread, because
offsets come from `Backend.ShaderLayout`, while scene writes instance records at
record time on the update thread — scene would have to write an index for a
record gfx has not laid out yet. Two writers, one field, opposite sides of the
thread boundary. **The binding *is* the addressing**, so nothing has to agree
across it, reflection needs only a one-level walk, and group 1 rebinds per
material exactly as intended.

The cost, named plainly: storage binding offsets must be 256-aligned, so each
record pads to a 256 multiple. That is per **batch**, and it is a pad rather than
a cap — a 400-byte PBR record pads to 512 instead of being truncated.

**One record per batch, no dedupe.** Two meshes sharing a material produce two
byte-identical records; collapsing them would cost a hash of every record every
frame on the render thread to save an upload nobody has measured. While the
automatic collapse is deferred this degenerates to one record per draw.

**Scene bindings do not bypass name matching.** Scene injects
`BufferParam("sceneFrame", …)` and friends as ordinary per-draw parameters and
the existing matcher binds them exactly like a material texture. The plan cache
is keyed by parameter *shape*, so injecting the same names on every draw keeps
shapes identical and hits the cache every time. A separate "system bindings"
channel would be a second path to keep in sync for no capability.

The `scene`-prefixed name space is **reserved** for engine-supplied bindings,
mirroring canvas's `canvasTexture`/`canvasSampler`. A material parameter named
`scene*` is an app bug; gfx does not police it, the material simply loses.

### Declared bindings

| binding | group | contents |
| --- | --- | --- |
| `sceneFrame` | 0 | view, projection, viewProj, camera position, sun direction and colour, ambient sky/ground, `lightCount`, `lights: array<SceneLight, 16>` |
| `sceneInstances` | 0 | `array<SceneInstance>`, bound by range per pass |
| `sceneAnim` | 0 | `array<vec4<f32>>` arena, indexed by `sceneInstance.animOffset` |
| `scenePbrMaterial` | 1 | the bundled PBR record, a bound range |
| `scenePoses` | 2 | baked 48 B pose records |
| `sceneInverseBinds` | 2 | per-skin `array<mat4x3>` |
| `sceneNormalMatrices` | 2 | per-skin `array<mat3x3>` |
| `sceneMorphDeltas` | 2 | per-model morph delta records |

Plus the PBR's five textures and five samplers in group 1
(see [Bundled PBR material](#bundled-pbr-material)).

**Gap — storage-buffer budget.** Those are **eight** storage buffers. Every
reflected binding is emitted with visibility `Vertex|Fragment` unconditionally,
because reflection walks module globals without consulting entry points, so a
buffer only the vertex stage reads still consumes a fragment-stage slot — which
means eight in **each** stage, against the browser core-adapter floor of 8. That
is legal and leaves **zero** headroom, where the closed tickets record the count
as six with two spare: that figure counted `scenePoses` and `sceneMorphDeltas`
but not the separate per-skin inverse-bind and normal-matrix arrays. Filed as
[scene: the storage-buffer budget is eight of eight, not six](https://github.com/dvoyni/cog/issues/58);
nothing here changes until it is resolved, but an implementation must not add a
ninth.

### The `sceneAnim` block

Per-instance animation parameters are **indirect**. `sceneInstances` is bound
once per pass and shared by every draw in it, so a fixed record would have to be
sized for the worst case — 64 B of skinning plus 256 B of morph weights, a ~320 B
tax on every debug line against ~48 B of content. Instead `sceneInstance` stays
~64 B and `animOffset` indexes a second per-frame buffer that only animating
draws write to.

```
vec4 0: { playCount: u32, targetCount: u32, morphBase: u32, morphStride: u32 }
vec4 1: { morphTargetStride: u32, _, _, _ }          // 3 words reserved
then  :  playCount   x { baseRow0: u32, baseRow1: u32, w0: f32, w1: f32 }  // 16 B
then  :  targetCount x { targetIndex: u32, weight: f32 }                   // 8 B,
                                                                           // padded to a vec4 boundary
```

Morph weights are a **count-prefixed sparse list**, not a dense 64-float block:
the CPU knows which entries are non-zero before it writes anything, so a 52-shape
face with 5 active shapes costs 40 B instead of 256 B and the shader loops 5
times over real work instead of 64 times with a `continue`. **The zero-skip
branch disappears entirely**, because zeros never reach the GPU.

**Four animation states, two independent counts.** A draw is skinned only,
morphed only, both, or neither; `playCount` and `targetCount` are each
independently zero-checkable and `animOffset == SCENE_NO_ANIM` covers neither. No
flags bitfield — two counts the shader reads anyway already carry the
information.

The four morph words are per-*primitive* constants duplicated per instance, 20 B
of the 32 B header. Putting them in the per-batch material record would remove
the duplication exactly, and was rejected: it would put scene geometry constants
into a record gfx packs on the render thread while scene records on the update
thread — the two-writers-across-a-boundary problem the bound-range design exists
to avoid.

### WGSL functions

These are the signatures the bundled shaders implement. **They are not published
in v1** — see the next section — but they are fixed here so that publishing them
later changes nothing.

```wgsl
struct SceneVertex { position: vec3f, normal: vec3f, tangent: vec4f }

// The 99% call: morph, then skin, per glTF order.
// Handles animOffset == SCENE_NO_ANIM and the SCENE_NOSKIN flag.
fn sceneDeformVertex(inst: u32, vertexIndex: u32,
                     joints: vec4u, weights: vec4f,
                     v: SceneVertex) -> SceneVertex

// The escape hatch: fully blended joint transform, inverse bind applied.
fn sceneJointMatrix(inst: u32, joint: u32) -> mat4x3f
fn sceneSkinMatrix(inst: u32, joints: vec4u, weights: vec4f) -> mat4x3f

struct SceneSurface {
    position:  vec3<f32>,   // world
    normal:    vec3<f32>,   // world, normalised
    baseColor: vec3<f32>,   // linear
    metallic:  f32,
    roughness: f32,
    occlusion: f32,
}

// Sun + every punctual light + hemispheric ambient scaled by s.occlusion.
fn sceneShadeSurface(s: SceneSurface) -> vec3<f32>

struct SceneLightSample { direction: vec3<f32>, radiance: vec3<f32> } // surface -> light

fn sceneLightCount() -> u32
fn sceneLightSample(i: u32, position: vec3<f32>) -> SceneLightSample
fn sceneSun() -> SceneLightSample                 // radiance zero when SunDirection is zero
fn sceneAmbient(normal: vec3<f32>) -> vec3<f32>   // mix(ground, sky, normal.y*0.5+0.5)
fn sceneCameraPosition() -> vec3<f32>

struct ScenePbrSurface {
    surface:  SceneSurface,
    emissive: vec3<f32>,
    alpha:    f32,
}

fn scenePbrSurface(uv0: vec2<f32>, uv1: vec2<f32>, normal: vec3<f32>,
                   tangent: vec4<f32>, color: vec4<f32>,
                   worldPos: vec3<f32>, frontFacing: bool) -> ScenePbrSurface
```

Two levels for lighting because the common custom material wants a shaded
surface, while the main reason to write one — a toon ramp — needs per-light
`NdotL` *before* shading; without the low level such a shader must reimplement
attenuation and the cone, reintroducing exactly the inconsistency the contract
exists to prevent. `SceneSurface` carries **no view vector** (derived from
`sceneCameraPosition()`, one normalise, one less field to get wrong) and **no
emissive** (emissive is the material's own output, not lighting, and debug lines
are self-lit through `emissiveFactor`, so a lighting function owning it would
read as a contradiction). A shader writes `sceneShadeSurface(s) + emissive`.

`scenePbrSurface` returns everything one set of texture fetches produces in one
call, rather than separate emissive/alpha helpers that invite the same texture to
be fetched two or three times — the compiler *may* common those up, but "may" is
not a contract, and this is the hot path.

The bundled fragment shader then reads as what it is:

```wgsl
let r = scenePbrSurface(uv0, uv1, normal, tangent, color, worldPos, frontFacing);
// alphaMode MASK: if (r.alpha < alphaCutoff) { discard; }
return vec4(sceneShadeSurface(r.surface) + r.emissive, r.alpha);
```

### Custom shaders are not supported in v1

([Custom shader contract and prelude](https://github.com/dvoyni/cog/issues/19),
closed out of scope)

**gfx does no shader preprocessing of any kind** — `ShaderDescr` is inline text
or a storage path handed straight to the backend, with no include, macro or
injection point. So publishing the functions above means either **copy-paste**,
with every consumer carrying its own copy of the BRDF — the exact failure that
shader variants were rejected over — or new gfx surface. v1 ships the bundled PBR
and publishes no contract; the function bodies live inside the two bundled
shaders. Publication is purely additive, so nothing is foreclosed
([scene: custom shader contract and prelude](https://github.com/dvoyni/cog/issues/48),
blocked by
[gfx: shader preprocessing and vertex variants](https://github.com/dvoyni/cog/issues/45)).

A caller may still supply a whole `gfx.MaterialDescr` with its own WGSL, as the
`procedural` demo does — it simply gets no scene helper functions and must
declare only bindings scene binds on every draw that uses it.

**Two findings that bind the bundled shaders themselves:**

- **A declared-but-unused binding is frame-fatal.** Reflection is naga, which
  deliberately does not compact unused globals, so every declared
  `@group/@binding` lands in the explicit `BindGroupLayout` and must be bound at
  draw time. Miss one and `CreateBindGroup` fails the entry-count rule, the error
  is swallowed, `encoder.Finish()`'s error is dropped, and **the whole frame's
  command buffer vanishes silently**. This is why the null skin and the 1×1
  default textures exist.
- **Every reflected binding is emitted `Vertex|Fragment`**, so a vertex-only
  buffer consumes a fragment-stage slot too — see the budget gap above.

### Binding cost

`resetAcc()` runs on every `SetPipeline` and after every `Draw`, so gfx re-emits
every binding per draw. That is left exactly as it is: the bind-group cache
already returns the *same object* for an unchanged group, so the only real cost
is the redundant `SetBindGroup` call, killed in the backend by comparing against
the last group bound — a pure optimisation with **zero API change**. Two
correctness rules on that filter: reset it on **shader change**, since bind-group
layout compatibility across shaders cannot be inferred from object identity, and
reset it at every **`BeginPass`**, since bind-group state does not survive a
render pass boundary. Draws are sorted by material, so runs are long and the
filter earns its keep; group 0, invariant for a whole pass, is bound once.

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

- `Orthographic4`
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
q.TemporaryTarget(w, h int, format TextureFormat) TargetDescr
func (d TextureDescr) Size() (w, h int)
```

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
  dependency graph. The whole frame stays one command encoder and one submit, so
  WebGPU inserts the barriers. One debug validation: reject a draw that samples a
  texture currently bound as its own pass's attachment.
- **The implicit default pass is deleted**, and `OpQueue.Clear` / `ClearDepth`
  with it — canvas was the only recorder and now declares its own passes. Every
  gfx draw names a pass.
- **No per-pass viewport or scissor, no MRT, no MSAA in v1.**

Backend contract:

```go
Execute(queue *GpuQueue)                    // was Execute(TextureViewID, *GpuQueue)
TextureView(TextureID, mip, layer int) TextureViewID

type GpuPassSink interface {
    BeginPass(GpuPassDesc) RenderPass
    EndPass(RenderPass)
}
func (q *GpuQueue) ReplayPasses(sink GpuPassSink)   // replaces ReplayRenderPass
```

Sink-driven, matching the one existing convention. Because `BeginPass` *returns*
the `RenderPass`, the backend still owns encoder and pass lifetime entirely.
`GpuQueue` grows a pass list; bakes stay hoisted ahead of all passes.

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
- **`firstInstance` is plumbed** through `OpQueue.Draw` → `GpuQueue.Draw` → the
  backend; `gpuOp.arg4` is free on the draw path.

Housekeeping:

- **Drop `BufferDesc.Dynamic`** — dead code, never read, a pure function of
  `Kind`.
- `TextureDesc` grows a **renderable bit**.
- Add `gfx.DefaultLimits` for the debug-build web-limits check.

### `wgpu`

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
- Canvas's three hand-written state literals become `gfx.StateOverlay2D`.
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
- **Canvas has no 8-bit colour packing anywhere** — vertex colour is
  `Float32x4`, tints and `keyColor` are float `vec4`s — so a whole class of
  migration bug does not exist here. Only three sites outside `m` read
  `.R/.G/.B/.A`, all packing code.

**Migration order.** Both consumers `replace` to the working tree with no version
pin, so the `m` sweep is atomic across all three trees by construction. The
choice is the gfx/wgpu half, and it lands **first, as a separate step**: the
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
`Camera` whose `Transform` is `LookAt` along `SunDirection`, whose `Projection`
is `Orthographic` sized to cover the region that must cast, and whose one pass is
depth-only.

```go
q.Camera(shadowCameraID, scene.CameraDescr{
    Transform:  scene.LookAt(sunEye, sceneCentre, up),
    Projection: scene.Orthographic,
    Height:     40, Near: 1, Far: 200,
    CullMask:   layerCasters,
    Passes: []scene.Pass{{
        Tag:        "shadow",
        Target:     gfx.NoTarget(),
        Depth:      gfx.DepthTarget(shadowTex),
        ClearDepth: &one,
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
2. `sceneFrame` grows the light-space matrix and bias parameters — free, since no
   prelude is published, so no external contract constrains the struct.
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
| `box` | **none** | debug vocabulary; `Transform` TRS and scalar `Scale`; `LookAt`; empty `Passes` → implicit forward pass at the camera id; sun and hemispheric ambient; the linear pipeline and present pass; every-zero-value-is-the-default; the `m` additions |
| `pbr` | WaterBottle, AlphaBlendModeTest, BoxVertexColors, CompareBaseColor, EmissiveStrengthTest, PointLightIntensityTest | the whole material contract (glTF names, five slots, two 1×1 defaults, Khronos BRDF, `EnvBRDFApprox`, `COLOR_0`, flat `KHR_texture_transform` members); `alphaMode`→state, `Cull`/`FrontFace`; the back-to-front blend bucket; point and spot lights, `Range` zero-means-infinite, the 16 cap and its silent drop; `emissive_strength`, `lights_punctual` as data |
| `cameras` | reuses `box` + `pbr` | multi-camera, orthographic, `CullMask`, no `Viewport` → `TemporaryTarget` composited by canvas, negative ids, duplicate-id error; targets, `DepthAuto`/`DepthTarget`, `Order` sorting and pass merging; layer masks; multi-tag material and a `NoTarget()` depth-only pass; `WorldToScreen`/`ScreenToRay`, per-target `viewport m.Vec2`, behind-camera `ok`, `m.Ray.IntersectSphere` |
| `animated` | Fox, AnimatedMorphCube, MorphStressTest, InterpolationTest | baked poses, `ClipPlay` crossfade, the 4-play cap, the rest frame, `PoseBytes`; sparse morph weights, `MorphWeights` override, the attribute mask; degenerate single-joint skins; u8 index widening. **The web canary.** |
| `procedural` | none (custom WGSL) | `TemporaryMesh` vs `BakeMesh`, the generic `VertexLayout`, `UpdateMesh`, `ReleaseMesh` generations, `NeverCull`, the null skin and `SCENE_NOSKIN`; `BakeBuffer`/`ReBakeBuffer`/`BufferWithBytes`; a caller-supplied material with a custom layout |
| `instancing` | reuses `box` + `pbr` | explicit `Transforms`, per-instance culling, the `materialID`/`meshID` sort key, `SCENE_NONUNIFORM` via the `Matrix` escape hatch, `Passes(dst)`; `firstInstance`, the per-batch material record, one instance arena bound by range |
| `loading` | CesiumMilkTruck, MultipleScenes, TextureSettingsTest, MeshPrimitiveModes, AnimatedMorphCube glTF-Quantized, `broken/truncated.glb` | `Scene`/`Node` views, re-rooting, async skip-never-substitute, `Preload`, `Material` replace vs `OverrideParams` merge, explicit unload with no texture cascade; `(value, ok)`, `State`, `Nodes`/`Bounds`/`AABB`, unmatched-node-reports-once; the four WebGPU papering-over gaps |

`custom-shader` is **merged into `procedural`**, not dropped: a custom vertex
layout *requires* a custom material, so the two cannot be demonstrated apart.
`box` is kept beside `pbr` despite covering nothing `pbr` misses, because it is
the only demo that runs with **zero assets** — the floor of the API and the smoke
test that still works when the asset story breaks. `loading` is kept despite
being nearly all assertion and almost no picture, because loading and the lookup
facade are the two contracts whose failures are *invisible*: a model that
silently substitutes, a node that silently falls back to the whole scene.

### Acceptance: two mechanisms, split by failure class

**One falsifiable sentence per demo** for what only eyes can judge, plus
**assertions** for everything that is a number or an ordering. A wrong BRDF and a
wrong sort are the two failure classes this set must catch, and they divide
cleanly between the two.

The assertions live in **`_test.go` files beside each demo and run with no GPU**.
This is available because culling, sorting and packing happen entirely in the
update-thread flush and the result is published as `Passes(dst []PassView)`
including the frustum; `gfx/plugin_test.go` already has a `fakeBackend`
implementing the full `Backend` interface; `gfx.SetBackendCmd` installs one
without the `wgpu` plugin at all; and a headless engine is already a named
concept. **`go test ./cmd/scene/...` is the one command the implementation effort
runs.** Each demo additionally prints its own key numbers on screen through
canvas, so a human running it sees them without a second command.

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
single unbound binding silently kills the whole frame. Paired with the debug
check against `gfx.DefaultLimits`, so a desktop run fails loudly on a web
violation rather than deferring the discovery to the browser. Every demo passing
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
through `storage.DefaultConfig("cog-examples")` and tarred by the ported web
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
dangerous contract here — a non-resident model is skipped, never substituted —
has no demo that ever sees a failure. Two cases, which `State(path)` must tell
apart: a path that does not exist, failing **synchronously**; and
`assets/broken/truncated.glb`, a valid header with the binary chunk cut short,
generated by `cmd/prepare-assets` so it is not a mystery blob either, failing
**asynchronously** and terminally, with unload the only retry lever.

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
  reachable through a packed `MeshPrimitiveModes`.
- **`CesiumMilkTruck`** is the re-rooting asset: `Wheels` and `Wheels.001` sit at
  ±1.43 on X beneath a `Yup2Zup` root, so re-rooting has a real authored world
  transform to discard, at hierarchy depth 4.
- **`MultipleScenes`** is the **only** file in the repository with more than one
  `scenes` entry, and therefore the only possible exercise of the `Scene`
  selector.
- **No chosen model is both skinned and morphed**, so **morph-then-skin ordering
  — the one thing glTF is emphatic about — is untested by the demo set.** Stated
  here rather than assumed away.

---

## Out of scope

Ruled beyond this spec's destination. Each is either purely additive or tracked
with a named trigger, and none is foreclosed. Implementation follow-ups hang
under [Scene follow-ups](https://github.com/dvoyni/cog/issues/29).

| Item | Why | Tracked |
| --- | --- | --- |
| Implementing this spec — README, `scene.instructions.md`, plugin code | belongs to the implementation effort that follows | — |
| Shadow maps, post-processing, IBL **implementation** | documented as extension points only; the multi-tag material shape they need **does** ship | [52](https://github.com/dvoyni/cog/issues/52), [53](https://github.com/dvoyni/cog/issues/53), [44](https://github.com/dvoyni/cog/issues/44) |
| Custom shader contract and prelude | needs a preprocessor; publishing by copy-paste is the failure variants were rejected over | [48](https://github.com/dvoyni/cog/issues/48) (blocked by [45](https://github.com/dvoyni/cog/issues/45)) |
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

### Known-unspecified, with triggers

These are deliberately **not** decided: the question is not sharp enough to
answer, or the answer needs a measurement nothing in this scope produces. Each
carries the trigger that would make it a real question.

| Item | Trigger |
| --- | --- |
| Per-clip joint subsets | a file holding many independently-animated props *and* per-prop clips, where `(joints the clip does not animate) × frames` dominates |
| Reverse-Z depth | visible z-fighting at the far end of a large scene |
| A framing policy for off-reference aspects (`FovAxis`, or a reference aspect) | a demo is framed wrong on an ultrawide or portrait window |
| A projection escape hatch (likely `ObliqueNearPlane`, not a raw matrix) | portal or water-reflection cameras; nothing in scope needs it |
| Per-pass viewport and scissor in gfx | shadow cascades, atlas-packed targets, or N on-screen cameras where a target each proves too expensive |
| A pose-resolve pass | per-vertex pose fetch cost proves too high in the skinned demo |
| f16 morph delta packing | `MorphBytes` shows delta memory is a measured problem |
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
