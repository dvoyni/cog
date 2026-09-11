# scene

`github.com/dvoyni/cog/scene` records declarative 3D — cameras, glTF models,
buffer-built meshes, punctual lights and a debug shape vocabulary — and
translates it into `gfx` passes and draws at the end of each simulation update.

It is canvas's sibling: the same frame-local `OpQueue` that gameplay writes and
the plugin resets every tick, and the same single persistent `Lookup` behind a
handler-scoped access facade. What differs is that scene *decides* things —
which draws a camera sees, in what order, packed into which batches — and
publishes those decisions back as `Passes`.

This README is the API. `scene/docs/specs/scene.md` is the design record — what each rule is
for and what was rejected to get there — and
[`.github/instructions/scene.instructions.md`](../.github/instructions/scene.instructions.md)
is the traps a caller hits that neither the compiler nor a plausible-looking zero
value warns about.

[`docs/specs/mesh.md`](docs/specs/mesh.md) specifies **what a mesh stores** — the
vertex layout and the precision of each attribute, which attributes a mesh may
omit, how wide its indices are, how morph deltas are packed, and what the bundled
PBR requires of a mesh handed to it. Its **Index width** section is implemented:
a durable mesh of 65535 vertices or fewer stores `uint16` indices, derived from
the vertex count with no pass over the indices, and a temporary mesh keeps
`uint32`. Its **authoring API** section is implemented: scene *packs* the
standard vertex at bake rather than reinterpreting the caller's slice, in the
same traversal that bounds it, so what a mesh stores is scene's to change one
attribute at a time. Six attributes have moved: the **normal** stores as
`oct32` in a `Unorm16x2`, the **tangent** as one `Uint32` of 15/15 octahedral
plus handedness plus a reserved bit, **both UV sets** as a `Unorm16x2`
against a scale and bias derived per mesh, the four **joints** as a `Uint8x4`
and the four **weights** as a `Unorm8x4` — four bytes each against twelve,
sixteen, eight, eight, eight and sixteen. Its **per-mesh
record** section is implemented with it: a 32-byte record per mesh in a storage
buffer at `@group(0) @binding(3)`, named by the instance record's last spare
word. Its **attribute presence** section is implemented too, and it is the one
presence trim the axis earns: there are **two named layouts**, the standard 32
bytes at six locations and the skinned 40 at eight, and a static primitive no
longer carries joints and weights it never reads. Morph deltas are unchanged,
and that spec is the plan for them.

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

`Config` is the exported configuration type, and `PoseSampleRate` — the global
animation bake rate in Hz, default 60 — is the only number in it. `Plugin`
implements `Name`, `Dependencies`, `Register` and `Start` for the kernel
lifecycle; `Start` mounts scene's embedded shader filesystem through
`storage.SetMountCmd`.

**Register `storage` before `scene`.** The order the demos use is `storage`,
`input`, `gfx`, `canvas`, `scene`, then the driver (`wgpu`), with the app's own
recording plugin last, because it records into the queues those plugins declare.

**Scene needs a WebGPU core adapter.** It reads storage buffers from the vertex
stage, which compatibility mode does not guarantee — `maxStorageBuffersInVertexStage`
defaults to 0 there. The binding cog runs on cannot request compatibility mode at
all, so a compat-only device fails `requestAdapter()` outright and gets no WebGPU
rather than a degraded scene. There is no fallback path and no partial mode.

## The Two-Statement Floor

A camera and a box. Everything else is optional.

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

`Near` and `Far` are the only required fields: a zero in either is reported and
the camera skipped. `Passes` empty means one forward pass into the screen; the
zero `LayerMask` means every layer; a nil `Material` means the bundled PBR.

## Resources

- `*OpQueue` — frame-local recording surface. Scene consumes and republishes it
  on `app.UpdateEvent`.
- `*Lookup` — the single persistent resource: resident models, baked pose and
  morph buffers, the path-keyed texture cache, buffer-built meshes and scene's
  own unit meshes, plus the deferred bakes and unloads the flush applies at the
  frame boundary. Query and mutate it only through a scoped `LookupAccess`.

Gameplay normally writes only `*OpQueue`. Residency, mesh baking, bounds queries
and unloading go through `*Lookup` via a `LookupAccess`; scene's own flush
handler also writes `*Lookup` to drain bakes and apply unloads.

## Recording API

Bind `access.GetWrite[*scene.OpQueue]()` in the recording subscription's `Lock`
and call:

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

func (q *OpQueue) TemporaryMesh[TVertex VertexLayout](
	vertices []TVertex, indices []uint32, topology gfx.PrimitiveTopology) MeshRef

func (q *OpQueue) Reset()
func (q *OpQueue) OpCount() int
func (q *OpQueue) Ops(dst []Op) []Op
func (q *OpQueue) Passes(dst []PassView) []PassView
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
func LookAt(eye, target, up m.Vec3) Transform
func (t Transform) WithScale(s float32) Transform
func (t Transform) WithRotation(q m.Quat) Transform
func (t Transform) Mat4() m.Mat4
```

The zero value is the identity. `Scale` is **scalar**; non-uniform scale goes
through `Matrix`, which replaces the transform whole and is what puts the draw
on the inverse-transpose normal path.

### Layers

```go
type LayerMask uint32 // zero reads as LayersAll
const LayersAll LayerMask = ^LayerMask(0)
func Layer(i uint) LayerMask
```

A camera draws a recorded item iff `layers & camera.CullMask != 0`. There are 32
layers, and **zero reads as `LayersAll` on both sides**, so the degenerate frame
works with no masks written anywhere. A light's own mask selects **cameras**,
not the objects it lights.

### Debug Vocabulary

`Box`, `Sphere`, `Plane`, `Line3D` and `WireBox` are the scene twin of canvas's
`FillRect` / `StrokeRect` / `Line`: sugar over scene-owned unit meshes and the
bundled PBR, with no file on disk anywhere in the frame. `Box`, `Sphere` and
`Plane` are lit; `Line3D` and `WireBox` are **self-lit** — black base colour,
the given colour as `emissiveFactor` — so a debug line stays visible in a frame
with no sun, which is precisely the frame being debugged.

A line is a long thin box, not a line-list primitive: WebGPU has no line-width
control, so a GPU line is one physical pixel and vanishes on a hidpi display.
Thickness is therefore world-space, and a distant line thins out. The unit box,
sphere (a 16 × 12 UV sphere, about 350 triangles) and plane are baked lazily on
first use.

## Cameras And Passes

```go
type CameraDescr struct {
	Transform  Transform // the camera as a positioned object; scene inverts it
	Projection ProjectionKind
	FovY       float32 // Perspective: the literal vertical field of view, radians
	Height     float32 // Orthographic and Oblique: world units across the target's height
	Shear      float32 // Oblique: the gain depth rides up the screen by
	Near, Far  float32 // both required

	CullMask LayerMask // zero reads as LayersAll

	SunDirection m.Vec3 // direction of travel; zero means no sun
	SunColor     m.Color
	SunIntensity float32 // zero means 1

	AmbientSky       m.Color
	AmbientGround    m.Color
	AmbientIntensity float32 // zero means 1

	Passes []Pass // empty means one default pass
}
```

`CameraID` is a defined type over `gfx.Order`: it is both the camera's identity
— recording one twice is reported and the second dropped — and the default order
of the passes it emits. gfx reserves no ranges, so a camera interleaves with
canvas by taking an order between two canvas layer values; a scene camera
conventionally takes a negative id when canvas draws entirely over it.

A camera's `Transform.Scale` is **ignored**. Only its position and rotation are
inverted into the view matrix.

`ProjectionKind` is `Perspective`, `Orthographic` or `Oblique`. Both of the
first two project along the camera's forward axis, so revealing a vertical face
always costs ground-plane scale: tilt to elevation φ and the ground foreshortens
by exactly sin φ. **`Oblique` separates the two** — it projects along a direction
that is not perpendicular to the image plane, so the plane the camera sits in
renders at true scale while depth is sheared into screen-up by `Shear` instead.
`Shear: 1` is cavalier, `0.5` cabinet, `0` exactly `Orthographic`, and the
implied elevation is `atan(1/Shear)`. It is the family behind most 2.5D looks,
and it cannot be faked from outside scene: two cameras do not register, and a
non-uniform world scale stretches every object along one horizontal axis.

**An `Oblique` camera's distance is not free the way an `Orthographic` one's
is.** The shear pivots about the camera's own plane, so standing the camera off
pans the image: at `Shear: 1` a camera 50 units above the ground puts that ground
50 units down the screen, with nothing reported. Put the camera **in** the plane
you want held fixed and let `Near` go negative:

```go
q.Camera(-1, scene.CameraDescr{
	// Straight down from inside the ground plane, screen-up towards -Z.
	Transform:  scene.LookAt(m.Vec3{}, m.Vec3{Y: -1}, m.Vec3{Z: -1}),
	Projection: scene.Oblique,
	Height:     30,
	Shear:      0.5,
	Near:       -100, Far: 100, // the camera sits inside its own depth range
})
```

Depth is conventional: near → 0, far → 1, compare `Less`, **clear to 1.0**.
Oblique does not change that: the shear leaves view-space depth untouched, so
the ordinary depth buffer still sorts and frustum culling still holds.

```go
type Pass struct {
	Tag        PassTag         // zero reads as TagForward
	Target     gfx.TargetDescr // zero is the screen; gfx.NoTarget() for depth-only
	Depth      gfx.DepthDescr  // zero is gfx.DepthAuto(), pooled by size and shared
	ClearColor *m.Color        // nil preserves
	ClearDepth *float32        // nil preserves; 1.0 is the useful value
	Order      gfx.Order       // offset from the camera id, not an absolute
}
```

The zero `Pass` is the default pass: forward tag, screen target, pooled depth,
**colour preserved and depth cleared to 1.0**. The asymmetry is deliberate — a
defaulted colour clear would let a second camera silently erase the first, while
a pooled depth texture shared with every other same-size pass in the frame must
be cleared or it inherits garbage.

Clears live only on passes; `CameraDescr` carries none. Store ops are inferred
and not exposed: depth is kept iff the pass names an explicit depth texture,
colour is always kept.

`Target` is the gfx handle passed through untouched. Scene mints no textures of
its own — that takes the gfx queue, which a scene recorder does not hold — so a
render-to-texture camera calls `gfx.OpQueue.TemporaryTarget(w, h, format)`,
which returns the target to render into and the texture to sample back, and
hands the target across. A pass with `NoTarget()` takes its size from an
explicit depth texture; one with neither, or with `NoTarget()` and a
`ClearColor`, is reported.

A pass with zero surviving draws is still emitted, so a camera's clear does not
depend on whether anything was visible.

## Materials And Pass Tags

```go
type PassTag string
const TagForward PassTag = "forward"

type MaterialTag struct {
	Tag   PassTag // zero reads as TagForward
	Descr gfx.MaterialDescr
}

type Material []MaterialTag
```

A `Material` is the gfx materials a draw serves, one per pass tag. **Tag
participation is purely a material property**: a pass whose tag the material has
no entry for skips every draw using it, and a draw gets no say in which passes it
appears in. Layers give per-camera exclusion; the pass list gives per-pass
control.

A nil `Material` is the bundled PBR, so every draw literal that omits the field
is untouched. The hand-written one-entry case is `scene.Material{{Descr: descr}}`.
A duplicate tag in one `Material` is reported and the first entry wins.

An entry is a whole `gfx.MaterialDescr` rather than a shader, because pipeline
state and the parameter set both vary per tag — and a declared-but-unused WGSL
binding is still reflected and must be bound.

In v1 the only tag is `forward`. The shape is paid for now so that shadows can
add a `shadow` entry to the same value and every draw that passed nil gains
shadow casting with no call-site change.

## glTF Models

```go
q.Model(layers, "models/truck.glb", scene.ModelDraw{
	Transform: scene.At(0, 0, 0),
	Scene:     "",      // empty is the file's declared default
	Node:      "crate", // empty is the whole scene; a plain name, not a path
})
```

`Scene` names an entry in the file's `scenes` array and `Node` a node within it,
matched against the first depth-first node carrying that name. **A `Node` draw
re-roots**: the node's authored world transform is discarded and `Transform`
replaces it, descendants keeping their relative transforms, so one node of a
props file behaves as an independent asset. An empty `Node` keeps the scene's
root transforms, because a scene is authored as one unit.

**Neither selector falls back.** A `Scene` or `Node` that matches nothing skips
the draw and reports once — one typo'd node name rendering an entire building at
the origin is the worse failure of the two.

Two ways to change what a model looks like, and they do not overlap:

- `OverrideParams` **merges** by name over each primitive's own material,
  keeping the file's textures. glTF's parameter names are the contract, so
  `gfx.ColorParam("baseColorFactor", c)` is what tints a model. It broadcasts to
  every material the draw binds, and a name the resolved tag entry's shader does
  not declare is ignored rather than reported.
- `Material` **replaces** the file's materials wholesale. The file's PBR records
  are not bound at all, so its base colours, factors and texture transforms do
  not survive. That is the dissolve, the silhouette and the depth-only case.

A draw may use both: the replacement takes glTF's own defaults and the overrides
merge over those.

`Transforms []Transform` instances the draw. Instancing is per primitive, so a
six-primitive model at a hundred transforms is six batches of a hundred, not six
hundred draw calls — and the instances share the draw's animation.

### Residency

Loading is asynchronous and idempotent. A draw of a path that is not resident
**draws nothing** — no placeholder, no substitute — and enqueues exactly one load
however many frames name it.

```go
const (
	ModelMissing ModelState = iota // no entry; the zero value
	ModelLoading
	ModelResident
	ModelFailed // terminal; clears only on UnloadModel
)
```

Every residency change lands at a **frame boundary**, not at the call. So does
every unload. `LookupAccess.Preload(path)` is the same idempotent load a draw
fires, fired without one, which is how a decode moves into a loading screen the
app controls.

## Buffer-Built Meshes

```go
ref := la.BakeMesh(vertices, indices, gfx.TopologyTriangleList) // durable
ref := q.TemporaryMesh(vertices, indices, gfx.TopologyTriangleList) // this frame only
q.Mesh(layers, ref, scene.MeshDraw{Transform: scene.At(0, 1, 0)})
```

`BakeMesh` mints the ref immediately and queues the upload onto the `Lookup` for
scene's own flush to drain, so a mesh baked and drawn in the same handler still
uploads in that frame. `UpdateMesh` replaces a durable mesh's geometry wholesale
at any size, keeping the ref and its id, and refuses a change of vertex layout or
topology. `ReleaseMesh` stales the ref at once and frees at the frame boundary.

Scene blesses **two named layouts and no others**. `scene.Vertex` is the
**standard** layout: six attributes at locations 0..5, 72 bytes authored and 32
stored, interleaved. The **skinned** layout is the same six plus `JOINTS_0` and
`WEIGHTS_0` at locations 6..7, 40 bytes, and it belongs to the glTF loader — a
converted geometry takes it iff some placement draws it under `SCENE_SKIN`,
decided once per geometry. **No exported type reports it and no app can author
one**: nothing but a loaded model's animation ever set a skin binding, so the
public vertex carries no joints and no weights at all.

A skinned-layout mesh drawn by a variant that declares only the first six is
ordinary and happens on every model that shares a mesh between an animated node
and a static one: extra attributes a shader never declares are permitted
(measured on a conformant D3D12 adapter; the direction that fails is a shader
input no attribute supplies). Any other `VertexLayout` is a custom layout, and
**a custom layout requires a custom `Material`**; the reverse — a named layout
with a custom material — is fine.

`scene.Vertex` is what an app *writes*, not what scene *stores*: scene packs it
into the storage layout its `VertexLayout()` reports, and the two differ. The
`Normal` and `Tangent` an app writes as `m.Vec3` and `m.Vec4` store octahedrally
encoded in four bytes each — so they are **directions**, their length is divided
out and unrecoverable after bake, and `Tangent.W` keeps only its sign. An
unwritten one stores as +Z with positive handedness, which falls out of the
encoding rather than being a special case.

`UV0` and `UV1` store in four bytes each too, as two 16-bit unorms against a
**scale and bias derived per mesh** from the spread of that mesh's own UVs. The
range is never a parameter: `BakeMesh`, `UpdateMesh` and `TemporaryMesh` take
none, and every one of them re-derives it, so an update that moves a mesh's UVs
moves its precision with them. A half float would cost the same four bytes and
carry no record, but an island touching exactly 1.0 crosses a binade and doubles
its step for the whole primitive — over the vendored corpus, at a 4096-texel
texture, the derived range's worst error is **0.48 texels where a half float at
the same coordinate is 32**.

The record lives in a per-frame storage buffer at `@group(0) @binding(3)`, and
`sceneInstance`'s last spare word indexes it. **Slot 0 is a reserved identity
record** — scale 1, bias 0 — which a custom-layout mesh and a standard mesh with
no UVs both name, so their dequantisation is a branchless no-op with no validity
flag anywhere. A UV set collapsed to one point stores scale 0 and bias equal to
that point, which the same multiply-add decodes exactly.

A **custom material drawing a standard-layout mesh must decode them**: include
`scene.VertexDecodePath` (`builtin/scene/vertexdecode.wgsl`) and declare
`@location(1) normal: vec2<f32>` and `@location(2) tangent: u32`, then call
`sceneDecodeNormal` and `sceneDecodeTangent` at the top of the vertex stage,
before any morph or skin. Declaring the old `vec3<f32>`/`vec4<f32>` is refused at
pipeline time by gfx's vertex-interface check rather than shading from garbage.
A material that *samples* a texture must also call `sceneDecodeUV` with that
set's scale and bias from the mesh record — `@location(3)` and `@location(4)`
are still `vec2<f32>`, so nothing refuses a shader that reads them raw; it
samples the wrong place instead. `instance.wgsl` declares the buffer and
`sceneMeshOf(instance)` reaches it.

`Color` is an `m.Color` and stores as a `Unorm8x4`. It is the one attribute
whose authored and stored forms would otherwise have coincided, and a colour
type also says which space a component is in where four raw bytes cannot —
glTF's `COLOR_0` is linear, so a loaded model's byte colour round-trips through
the float form exactly.

The **skinned layout's** `JOINTS_0` and `WEIGHTS_0` store in four bytes each — a
`Uint8x4` and a `Unorm8x4` — and neither needs a decode source, because the
fetch unit hands a shader the same `vec4<u32>` and `vec4<f32>` the wide forms
did. Only a loaded model has them, and two consequences are contract:

- **A skin is capped at 256 joints**, because a joint index is one byte. It is a
  per-model cap: every model has its own joint numbering, so a level full of
  rigged characters does not share one budget. Exceeding it **fails the whole
  model at load**, naming it, rather than truncating an index to a different
  bone and welding a prop to the wrong limb with nothing reported.
- **The shader renormalises.** `sceneDeformVertex` divides the deformed position
  by the weight total it accumulates, because eight bits cannot hold four
  weights that sum to exactly one. This is a **behaviour change on existing
  content, in the direction of correctness**: glTF only says a producer *should*
  normalise `WEIGHTS_0`, and a file that does not used to be skinned silently
  shrunk or inflated. A custom material that skins a model's mesh itself owes
  the same divide.

A custom layout has no such split — scene cannot pack a struct it does not know,
so its buffer is the caller's Go memory reinterpreted, and its declared offsets
must be the struct's own.

Buffer-built meshes never skin and never morph. A `MeshRef` has no equivalent of
the group-2 bindings those need, and their draws take the bundled variant that
declares no group 2 at all — fourteen bindings and four storage buffers, against
the eighteen and eight a fully animated draw declares.

## Animation

Animation is **stateless**. Nothing in scene advances a clock, and no play
survives the frame that recorded it: gameplay owns the time and hands the result
to the draw, which is what makes scrubbing, reversing and pausing the caller's
business.

```go
draw.Plays = []scene.ClipPlay{{Clip: "Walk", Time: t, Loop: true, Weight: 1}}
draw.MorphWeights = []float32{0.4, 0, 0.9}
```

Up to **four plays** blend per draw, weights normalised across them so they
express proportions rather than intensities. An empty `Plays` draws the rest
pose, which is a real pose: row 0 of every model is the authored hierarchy
resolved once. Poses are baked at `PoseSampleRate` Hz per clip at load.

`MorphWeights` is **positional** over the model's whole flattened target list,
which `LookupAccess.MorphTargets(path)` names in the same order — one entry per
target of every morphed node, in depth-first node order. It is the one
index-addressed thing in the plugin; naming lives on the lookup facade so the
recording path stays a memcpy. A short slice leaves the rest at 0 and is not an
error; a long one ignores the tail and reports once. Up to 64 targets blend per
draw.

## Lights

```go
q.PointLight(layers, scene.LightDescr{Position: p, Color: c, Range: 12})
q.SpotLight(layers, scene.LightDescr{Position: p, Direction: d, Color: c,
	InnerCone: 0.2, OuterCone: 0.5})
```

The sun and the hemispheric ambient are per-camera fields; the light array holds
point and spot lights only. Every zero in `LightDescr` is a default: `Intensity`
zero means 1, `Range` zero means infinite, `OuterCone` zero means π/4, and
`InnerCone` zero is a real value.

Shading is naive forward — **every shaded fragment loops the whole list** — so
the cap of **16 lights per pass** is load-bearing rather than decorative. Past
the cap, lights are ranked by falloff at the eye and the rest dropped silently; a
light whose `Range` does not reach the camera scores zero.

A model file's own `KHR_lights_punctual` lights are exposed as data through
`LookupAccess.ModelLights` and converted by nobody: an app reads them and
declares the ones it wants.

## Sorting, Culling And Batching

All of it happens in the update-thread flush. **Within a pass, recording order is
not preserved.**

- **Culling** is the draw's bounding sphere against all six frustum planes. A
  model uses the bounds its file declares; a `MeshDraw` uses the mesh's baked
  sphere unless `Bounds` overrides it, and `NeverCull` exempts it outright. A
  model with no declared bounds, and any draw animated through the pose buffer,
  is never culled.
- **Two sort classes**, not three: `alphaMode: MASK` is opaque plus a shader
  `discard`, so it sorts with opaque. Opaque and mask sort by material then mesh
  with no depth term; blend sorts back-to-front by view-space distance. Sorting
  is scene's entire contribution to transparency.
- **Batching** collapses the surviving instances of one instanced call into one
  draw call. A blended instanced draw is the exception and splits back into one
  batch per instance, so its entries keep their own depths.

The sort is per pass, not per frame: a draw two cameras both see is culled,
sorted and packed once in each of their passes.

## Inspecting A Recording

Two levels, both always retained, both aliasing flush storage and valid until the
next flush — the same contract as `canvas.Ops`:

- `Ops(dst []Op) []Op` is what a recorder recorded, canvas's shape exactly: the
  call as it was made, not the draws scene derived from it, so a `WireBox` is one
  `Op`.
- `Passes(dst []PassView) []PassView` is the flush **result**.

```go
type PassView struct {
	CameraID  CameraID
	Order     gfx.Order
	Tag       PassTag
	Frustum   m.Frustum
	Recorded  int // draws the camera's cull mask selected
	Culled    int // of those, rejected by the frustum
	Instances int // packed after the tag filtered the survivors
	Lights    int // packed after light culling and the cap
	Batches   []BatchView
}

type BatchView struct {
	MeshID, MaterialID           uint32
	FirstInstance, InstanceCount int
}
```

`Passes` publishes the frame the **last flush** consumed, so a reader inside an
update handler is looking at the previous frame. `Frustum` is published because
asserting that a specific sphere was rejected by a specific frustum is the whole
point of a culling test; without it such a test can only count.

Everything here is decided with no GPU anywhere, which is what lets a demo's
assertions be a plain `go test` beside its `main.go`.

## Lookup API

Residency, bounds, naming and mesh lifecycle go through `*scene.Lookup`. A
resource must not retain filesystem or GPU handles past its lock scope, so
callers acquire a handler-scoped facade:

```go
la := scene.NewLookupAccess(kernel, lookup) // two dependencies, not three
```

Bind `access.GetWrite[*scene.Lookup]()` in the handler's `Lock`. Unlike canvas's
equivalent it takes **no `storage.FileSystem`**: scene's load command opens,
parses and bakes the file itself holding no locks.

| Method | Result | Notes |
| --- | --- | --- |
| `State(path) ModelState` | residency | The only way to tell "wait" from "never coming". |
| `Preload(path)` | — | The same idempotent load a draw fires. |
| `Nodes(ref, dst) ([]string, bool)` | addressable node names | Depth-first, the hierarchy's own order. Unnamed nodes are absent. |
| `Bounds(ref) (m.Vec4, bool)` | xyz centre, w radius | Local space post-re-rooting; the **rest pose** for anything drawn through the pose buffer. |
| `AABB(ref) (min, max m.Vec3, bool)` | axis-aligned box | Same space and same pose rules as `Bounds`. |
| `Joints(path, dst) ([]string, bool)` | joint names in joint order | Names only; no hierarchy. |
| `Clips(path, dst) ([]ClipInfo, bool)` | name and duration | Duration is what tells a caller a one-shot play has ended. |
| `MorphTargets(path, dst) ([]string, bool)` | target names | The flattened order `MorphWeights` is positional over. |
| `ModelLights(path, dst) ([]ModelLight, bool)` | the file's punctual lights | In the model's own space; nothing converts one automatically. |
| `PoseBytes(path)` / `MorphBytes(path)` | `(int, bool)` | Per-model GPU memory. |
| `TotalPoseBytes()` / `TotalMorphBytes()` | `int` | Lookup-wide sums; no `ok`, and they trigger no load. |
| `BakeMesh` / `UpdateMesh` / `ReleaseMesh` | — | Buffer-built mesh lifecycle. |
| `UnloadModel(path)` | — | Geometry, poses and material records. **Does not cascade to textures.** |
| `UnloadTexture(path)` | — | Every texture that path baked. Checks no resident model. |
| `UnloadAll()` | — | Every resident model and cached texture. |

**Every query returns `(value, ok)` and every query triggers the load.** `ok`
means only "this value is real": it is false for a path still loading and for one
that will never arrive, which is why `State` exists. A `ModelRef` is a struct
rather than three bare strings because the bare form has a transposition bug that
compiles.

Unloads are queued and applied at the top of the next flush, so `UnloadModel`
followed by `Preload` in one handler is a no-op — the preload sees the old entry.
`UnloadModel` is also the only retry lever there is: a failed path clears there
and nowhere else.

## Coordinate Helpers

```go
func ViewProjection(camera CameraDescr, viewport m.Vec2) m.Mat4
func WorldToScreen(camera CameraDescr, viewport m.Vec2, world m.Vec3) (m.Vec3, bool)
func ScreenToWorld(camera CameraDescr, viewport m.Vec2, screen m.Vec3) (m.Vec3, bool)
func ScreenToRay(camera CameraDescr, viewport m.Vec2, screen m.Vec2) (m.Ray, bool)
```

`WorldToScreen` returns X and Y in target pixels from the top-left and Z as the
WebGPU 0..1 NDC depth, which is exactly what `ScreenToWorld` takes back, so the
two round-trip. Off-screen but in front stays `ok` — the coordinate is
extrapolated past the target edge and is correct there, which is what an
off-screen indicator arrow needs. **Behind the eye plane never returns a
coordinate**: dividing by a negative w yields a plausible, mirrored,
confidently wrong point.

Scene retains no draw list to raycast against, so picking is `ScreenToRay` plus a
loop over the caller's own entities, calling `Bounds` or `AABB` and keeping the
smallest `t`.

## Shader-Side Contract

Scene declares no uniform block. Everything it binds is a storage buffer or a
bound range of one.

| binding | group | contents |
| --- | --- | --- |
| `sceneFrame` | 0 | view, projection, viewProj, camera position, sun, ambient, `lightCount`, `lights: array<SceneLight, 16>` |
| `sceneInstances` | 0 | `array<SceneInstance>`, bound by range per pass |
| `sceneAnim` | 0 | `array<vec4<f32>>` arena, indexed by `sceneInstance.animOffset` |
| `sceneMeshes` | 0 | `array<SceneMesh>`, 32-byte UV scale/bias records, indexed by `sceneInstance.mesh`; slot 0 is the identity |
| `scenePbrMaterial` | 1 | the bundled PBR record, a bound range |
| `scenePoses` | 2 | baked 48-byte pose records |
| `sceneSkinJoints` | 2 | per-skin, per-joint 112-byte record |
| `sceneMorphDeltas` | 2 | per-model morph delta records |

Plus the bundled PBR's five textures and five samplers in group 1. The `scene`
name prefix is reserved for engine-supplied bindings.

**The storage-buffer budget is eight of eight, and it is now fully spent.**
Reflection walks module globals without consulting entry points, so every
reflected binding is emitted `Vertex|Fragment` and a vertex-only buffer consumes
a fragment-stage slot too. Three rules follow, and they are contract:

- No scene shader may declare a ninth storage buffer. The eighth, which
  interleaving the two per-skin arrays had recovered and which was reserved,
  went to the per-mesh record at `@group(0) @binding(3)`.
- **A caller-supplied material may declare none of its own.** It may freely use
  the bindings scene binds on every draw — `sceneFrame`, `sceneInstances` and
  `scenePbrMaterial`, any subset — because those are scene's and already counted.
  That is what the `procedural` demo does.
- gfx checks every reflected shader against `gfx.DefaultLimits`, the browser
  floor, never against the device's reported limits, and reports
  `ErrShaderExceedsWebLimits`. A desktop adapter reports hardware limits, so
  checking the real device would pass a build no browser can run.

There is no custom shader contract in v1. gfx now preprocesses, so scene's WGSL
helpers *could* be published as includable sources, but what scene publishes and
how it splits is a decision of its own that has not been taken. A caller may
still supply a whole `gfx.MaterialDescr` with its own WGSL — it simply gets no
scene helper functions.

## Errors

Everything scene refuses is reported through `kernel.ReportError` as a typed
error value, and the frame carries on. The house rules behind them:

- **Skip, never substitute.** A model that is not resident, a selector that
  matches nothing, a mesh ref that has gone stale, a light with no cone — each
  costs its own draw and nothing else. Nothing is stood in for.
- **Report once.** Load failures key on the path, so a model drawn every frame
  reports once, not sixty times a second.
- **Never default a required number.** A zero `Near` or `Far` skips the camera
  rather than substituting a plausible value that would hide the caller's bug
  behind a degenerate projection.

A load report fires from the load command's goroutine, so it lands a frame or
more after the draw that triggered it.

## Event Subscribed

`UpdateEventHandler` subscribes to `app.UpdateEvent`. It writes the scene
`*OpQueue` and `*Lookup`, reads `app.Viewport`, and writes `gfx.OpQueue` and
`gfx.ResourceQueue`. It is ordered `Last()` but explicitly before
`gfx.UpdateEventHandler`, exactly as canvas is: gameplay records first, canvas
and scene emit graphics draws second, gfx presents last. It is exported so a
recorder can order itself before scene.

**Everything scene decides happens in that flush, on the update thread** —
projection resolve, culling, sorting, instance packing and buffer uploads. Scene
never runs on the render thread: a frustum needs aspect, not pixel size, and
`app.Viewport` already carries the exact aspect here. The one cost is that a
screen-targeted camera's aspect is up to one frame stale during a window resize.

## Demos

Every contract above has a runnable demo in
[cog-examples](https://github.com/dvoyni/cog-examples) under `cmd/scene/`, each
with its own doc comment saying what it exercises and what only eyes can judge,
and each with a `_test.go` beside it that asserts the flush result with no GPU.
`go test ./cmd/scene/...` is the whole acceptance suite.

| demo | what it is for |
| --- | --- |
| `tracer` | the narrowest complete path through every layer |
| `box` | the whole debug vocabulary, with no file on disk in the frame |
| `procedural` | caller-owned geometry, and a material the program wrote itself |
| `pbr` | the material and lighting contract, over six Khronos models |
| `animated` | skinning, morph targets, and the browser canary |
| `instancing` | one call for five hundred crates, per-instance culling, the sort key |
| `cameras` | two cameras, a texture target, layers, and screen-space projection |
| `loading` | residency, addressing and the lookup facade |

One contract a desktop run cannot check, because a native adapter reports
hardware limits rather than the spec's floor: **the storage-buffer budget**. It
needs a browser run — `bash cmd/web/build.sh <demo>`. The **depth-only pass** used
to be the second one; it now executes on any Vulkan desktop as well as in a
browser, and is declined only on GLES.

## Deviations From The Specification

`scene/docs/specs/scene.md` is the contract this implementation was judged against, and each
place the build had to depart from it is recorded in the spec itself rather than
absorbed quietly. The ones a caller can observe:

- **There is no `scene.OpQueue.TemporaryTarget`.** The spec's recording API
  listed one. A temporary target's texture id is minted through `gfx.OpQueue`,
  which a scene recorder does not hold, so the passthrough would hand back a
  target with no id in it. `gfx.OpQueue.TemporaryTarget` returns the target and
  the texture together; `Pass.Target` takes the handle untouched.
- **A `NoTarget()` depth-only pass executes on Vulkan and in a browser, and is
  declined on GLES.** `cog/wgpu` asks the selected backend, not the build tag,
  and reports `wgpu.ErrDepthOnlyPassUnsupported` once per run where it declines.
  Vulkan was fixed by `gogpu/wgpu#353` in v0.34.5; the GLES HAL binds no
  framebuffer for a colourless pass and would draw into whatever was bound last,
  so it is refused rather than encoded. An unrecognised backend is refused too.
  The rule this leaves: a depth-only pass may be written, but its output may not
  be depended on in the same frame unless you know the backend. Shadow maps
  inherit it.
- **gfx places every texture barrier itself.** The spec claimed the frame being
  one command encoder and one submit meant WebGPU inserted them. It does not —
  `gogpu/wgpu` derives no barriers at all — so gfx transitions each texture in
  both directions around the passes that use it.
- **The morph attribute mask is a contiguous prefix** of position, normal,
  tangent, not an arbitrary set. The record header carries a stride and no mask,
  so which slots a record holds must be recoverable from the stride alone. A
  target that deforms the normal and not the position stores an explicit zero
  position slot.
- **The storage-buffer budget is eight of eight**, not the "six with two spare"
  the early tickets record. Interleaving the two per-skin arrays into one
  `sceneSkinJoints` buffer recovered the eighth slot; the per-mesh UV record
  then spent it, so the fully animated variant now sits exactly on the browser
  floor and there is no ninth.
- **`Bounds` and `AABB` answer about a primitive's rest placement**, not the
  local matrix the load first used. That matrix is the identity for anything
  drawn through the pose buffer — a glTF skin, and equally a node with an
  animation channel of its own carrying a mesh.
- **Canvas cannot composite a rendered texture through its built-in triangle
  material.** All three canvas shaders run every texel through the key-colour
  ramp, and no key colour makes it an identity, so a 3D render composited that
  way loses its dark warm shadows to neutral grey. A caller-supplied passthrough
  material is the fix; `cmd/scene/cameras/composite.go` is a minimal one.
- **`SCENE_NONUNIFORM` is invisible on a box.** Every face normal of an
  axis-aligned box is an eigenvector of an axis-aligned scale, rotation
  included, so the spec's motivating examples — `Line3D` and `WireBox` — are
  exactly the cases where the flag cannot be seen. Only a curved surface under a
  caller's `Matrix` shows it.
