---
name: "Scene Rendering"
description: "Use when creating or changing Go code that draws 3D through the cog scene plugin - Camera, Model, Mesh, Light, Material, Params, Animation or debug-shape Components, the Systems that write them, or custom shaders over model's published sources. Covers wiring, System ordering, the zero values that are load-bearing, residency, and the traps that are decisions rather than bugs."
applyTo: "**/*.go"
---

# Scene Rendering

`bundles/scene/docs/README.md` documents the API and `bundles/scene/docs/specs/scene.md` records why each rule is
what it is. scene is cog's one 3D renderer: a game spawns Entities carrying an
`m.Transform` and scene's Components, and scene's Systems draw them into gfx
every tick. There is no recording queue to call; the Components are the frame.
These are the traps a caller hits that neither the compiler nor a
plausible-looking zero value warns about. Follow them in new and changed code
without expanding a focused task into unrelated cleanup.

The house rule, and the one that explains most of what follows: **skip, never
substitute**. A model that could not be loaded, a selector that matches nothing,
a stale mesh ref, a camera with no clip planes, a light with no cone — each costs its own draw, is
reported once, and is stood in for by nothing. A frame with a hole in it is the
correct picture.

The one stated exception is a **texture the file named and model could not
get**. There is no draw to skip — the model is sound and the rest of it is
worth seeing — so the slot binds a placeholder instead: **magenta** for a colour
slot, because a missing base colour rendering white looks deliberate, and the
slot's own 1×1 default for a data slot, because magenta as a normal map is a
surface lit from nowhere. An **empty** slot is not this case and still binds the
plain default: the file said nothing, and nothing is the right picture.

## Wiring

Compose scene with what it depends on — storage and its `PermanentFS` Adapter,
gfx and its `Backend` Adapter, app, whose `MainLoop` Adapter gogpu provides with
gfx's `Backend`, model, which registers the `*model.Lookup` scene draws from,
and ecs, whose world holds the Components — and the game's own plugin. The
kernel orders them by their dependencies:

```go
plugins := []kernel.Plugin{
	storageplugin.New(), diskstorageplugin.New(), // diskstorage.Config{AppId: "demo"} under diskstorage.Name
	inputplugin.New(), appplugin.New(), gfxplugin.New(), modelplugin.New(), gogpuplugin.New(),
	ecsplugin.New(), sceneplugin.New(),
	game, // spawns Entities and runs the Systems that write them
}
```

Only the composition root imports `sceneplugin` and `modelplugin`. A game's
Systems import the roots `scene` and `model`. `scene` holds the Components, the
camera, layer and pass vocabulary, the errors, and the ordering identities.
`model` holds everything a model file can contain: `*model.Lookup`,
`model.NewLookupAccess`, `model.NewLookupDeviceAccess`, `model.ModelRef`,
`model.MeshRef`, `model.ClipPlay`, `model.LightDescr`, `model.Vertex` and the
`ErrModel…` and `ErrMesh…` reports. scene takes no configuration: the pose
sample rate is model's, `model.Config` keyed by `model.Name`, and a zero field
takes its default. A configuration keyed by `scene.Name` is ignored.

**Every Component Store is owned by `scene`.** A game System that names
`scene.Model`, `scene.Camera` or any other scene Component in its signature
declares `scene.Name` among its plugin's `Dependencies`, or composition refuses
it. Likewise **a plugin that locks `*model.Lookup` depends on `model.Name`**.

**Order a System by what it writes, or the change lands a tick late:**

| A game System that… | orders itself |
| --- | --- |
| spawns or edits debug shapes | `Before[scene.DebugOnUpdate]()` |
| spawns drawables, or writes `Model`, `Mesh`, `Material` or `Params` | `Before[scene.LoadOnUpdate]()` |
| moves `m.Transform`s, writes `Camera`, `Light` or `Animation` | `Before[scene.RecordOnUpdate]()` |

Nothing orders against gfx: its present is subscribed `Last`, so the recording
System already runs before it.

scene reads storage buffers from the vertex stage, so it needs a **WebGPU core
adapter**. Compatibility mode defaults that limit to zero and the binding cannot
request compat at all, so such a device fails `requestAdapter()` and gets no
WebGPU. There is no degraded mode to fall back to.

The kernel's default error handler logs and **returns true, which terminates the
engine**. Any code that provokes a report on purpose — a deliberately broken
asset path, a cap it means to exceed — installs its own handler with a named
allow-list and returns false for the entries on it:

```go
kernel.New(config).Handler(demo.report).WithPlugins(plugins...).Run()
```

## An Entity Is Drawn Only With A Transform

**`m.Transform` is required**, and it is the ecs plugin's Component, not
scene's. An Entity carrying a `Model`, `Mesh`, `Light` or `Camera` and no
Transform is not recorded, and nothing reports it. Do not declare a placement
Component of your own beside it: two Components describing one position are
unrelated to the scheduler, and nothing reports that they disagree. Copy one
way, in one System.

A `Light`'s `Descr.Position` and `Descr.Direction` are **ignored**: the light
stands at its Transform's position, and a spot points along the Transform's
rotation applied to −Z, the way `m.LookAt` faces. Writing the descriptor's
direction and leaving the Transform unrotated gives a spot pointing down −Z.

## The Zero Values Are The API

Writing a field that already means what you want is how a caller finds the wrong
default the hard way.

| Field | Zero means | The trap |
| --- | --- | --- |
| `Pass.ClearDepth` | preserve | Depth is conventional: near → 0, far → 1. `m.Some[float32](0)` clears to the **near plane and hides the whole scene**. `m.Some[float32](1)` is the useful value. |
| `Pass.ClearColor` | preserve | A defaulted colour clear would let a second camera erase the first. Clear colour deliberately, once, on the lowest pass. |
| `Camera.Passes` | one default pass | Writing any pass replaces the default outright, including its `ClearDepth: 1.0`. Carry the clear into the first pass you write. |
| `Camera.CullMask` | `LayersAll` | So does a drawable's own zero `Layers`. Zero reads as *all* on **both sides**, so a mask only ever excludes once both ends write one. |
| `LightDescr.Range` | infinite | glTF's own default. A forgotten `Range` is a light that reaches too far — visible immediately — rather than a light silently dropped. |
| `LightDescr.OuterCone` | π/4 | `InnerCone` zero is a **real value**, not a default: falloff straight from the axis. |
| `Camera.Shear` | `0`, i.e. plain `Orthographic` | Only `Oblique` reads it. Setting it on a `Perspective` or `Orthographic` camera does nothing, the way `FovY` does nothing under `Orthographic`. |
| `m.Transform.Scale` | 1 on every axis | Only an **all-zero** scale reads as the identity. A partly zero scale is taken literally: `m.Vec3{X: 2}` collapses the draw onto the X axis. `WithScale` is the uniform spelling. |
| `Mesh.Bounds` | the mesh's own baked sphere | A non-zero sphere replaces it outright. `NeverCull` beats both. |
| `ModelRef.Scene` / `.Node` | the default scene / the whole scene | A **non-empty** selector that matches nothing skips the draw and never falls back. |
| `MaterialTag.Shader` / `.State` | unset: the default scene shader / the file's state | A zero is not a value, so a tag cannot ask for the zero state, `gfx.StateOverlay2D`. |

`Camera.Near` and `.Far` are the exception: both are required, and a zero in
either skips the camera and reports `ErrCameraClipPlanesMissing`. Nothing
plausible is substituted. Two Cameras with one `ID` draw the first walked and
report `ErrCameraAlreadyRecorded` for the rest.

**Presence is meaning.** An absent `Material` is no material — the default scene
shader over the file's own materials, or the bundled PBR for a `Mesh` — but a
**present `Material` whose `Tags` is empty serves no pass, so its Entity draws
nowhere**. Remove the Component to go back to the default; do not empty it.

A camera's `Transform.Scale` is **ignored**. Only position and rotation are
inverted into the view matrix, so a rig that scales its camera Entity changes
nothing about what is seen.

**An `Oblique` camera's distance is not free, and getting it wrong reports
nothing.** For `Perspective` and `Orthographic`, where the camera sits along its
own view axis affects only what falls inside `Near..Far`. `Oblique` shears about
the camera's own plane, so that distance *pans the image*: at `Shear: 1` a
camera 50 units above the ground puts that ground 50 units down the screen. The
usual instinct — stand the camera well back so nothing clips the near plane —
is exactly what renders an empty frame, with no error anywhere, because the
camera is working correctly and pointed at nothing.

Put the camera **in** the plane you want held fixed and let `Near` go negative,
which is legal and means what it says:

```go
type CameraRig struct {
	Place  m.Transform
	Camera scene.Camera
}

sp.New(CameraRig{
	Place: m.LookAt(m.Vec3{}, m.Vec3{Y: -1}, m.Vec3{Z: -1}), // in the ground plane
	Camera: scene.Camera{
		ID:         cameraMain,
		Projection: scene.Oblique,
		Height:     30,
		Shear:      0.5,
		Near:       -100, Far: 100, // the camera sits inside its own depth range
	},
})
```

In a shader, **do not difference against `sceneCameraPosition()` for a view
vector.** Use `sceneViewDirection(worldPos)`. The camera position is a real
viewer only under `Perspective`: an orthographic camera has no eye point, and an
oblique one looks one way while its viewer sees another, so a hand-rolled
`normalize(sceneCameraPosition() - p)` lights vertical faces as if edge-on and
floors as if head-on. `sceneCameraPosition()` remains correct for what it is —
the transform's translation — and so for fog and detail fades.

## Picking Goes Through The Camera's Own Matrix

`scene.ViewProjection(camera, at, viewport)` returns the matrix a Camera placed
at `at` draws its screen-targeted passes through, `Oblique` shear and `Near`/`Far`
included. Pass it to `m.WorldToScreen`, `m.ScreenToWorld` or `m.ScreenToRay`
from `libs/m`; do not rebuild the projection from the Camera's fields by hand,
which drifts the first time a projection kind changes.

- Screen passes take their aspect from `gfx.Viewport`'s `WindowWidth` and
  `WindowHeight`, so pass that size as the viewport, and screen coordinates in
  the same units.
- It refuses what the renderer would skip, with the error the renderer reports:
  `ErrCameraClipPlanesMissing`, `ErrCameraProjectionDegenerate`, and
  `ErrViewportUnsized` for a side of zero or less — which is what a minimised
  window gives. Check the error; a zero matrix picks nothing.

## Layers Select Cameras

A camera draws an Entity iff `entity.Layers & camera.CullMask != 0`.

A **light's** `Layers` selects which cameras see the light, not which objects
it lights. Both ends of the test are camera-side. Per-camera exclusion is the
whole of what layers do, and it is the one thing nothing else in scene can
express — a frustum outline drawn once, seen by the minimap and declined by the
camera it describes, is one Entity and one exclusion.

## Loading Happens In The Tick That Names It

**A model loads in the load System of the tick its `Model` Component is added or
changed.** The load is synchronous: the file is read, parsed, decoded and
uploaded inside `LoadOnUpdate`, so a spawn ordered before it is drawn that same
tick — and a large file **hitches** it, holding the Lookup and gfx's resource
queue while it does. That is a stated property of the design, not a bug to
profile.

`Preload(path)` on the load facade is the lever: the same load, fired without an
Entity, so the hitch lands in a loading screen the app chose. A path that could
not be loaded draws nothing, so wire the empty case into the picture — a bare
pad under every model slot — and it reads as a hole rather than as broken.

**A failed model stays unkeyed until its Component changes again.** Every load
failure but a missing backend is cached, and the load System only looks at what
changed. Fixing a file on disk does nothing by itself: `UnloadModel` clears the
failure, and then the `Model` Component has to be written again for the load
System to look.

Entities touched before the backend is up wait for the first frame that has one.

**Unloading is cleanup between scenes.** Unload a model only once no Entity
draws it — despawn or re-point every Entity naming the path first. Drawing an
unloaded model is undefined behaviour: there are no generations, no invalidation
and no report.

## Queries Answer About Now

Every lookup query returns `(value, ok)` and every query **triggers the load**,
exactly as naming the path in a `Model` does.

`ok` means only **"this value is real"**. It is false for a file that could not
be read and false for a selector that matched nothing, so a loading screen
watching `ok` alone can never say why. `State(path)` returns an `error` — `nil`
when the model loaded, and the load's own failure otherwise — and it is the only
call that can print a reason.

**The facade is two facades.** `model.NewLookupAccess(k, lookup)` carries
`BakeMesh`, `UpdateMesh`, `ReleaseMesh`, `UnloadModel` and the two memory
totals, and costs its caller one resource. Everything that loads — `Preload`,
`State` and every query — plus `UnloadTexture` and `UnloadAll` is on
`model.NewLookupDeviceAccess(k, lookup, fsys, resources)`, which needs
`storage.FileSystem` read and `*gfx.ResourceQueue` write beside the Lookup. **A
System that only bakes a mesh must use the first**: declaring a gfx write to
bake a cube serialises that System against canvas's flush and gfx as well.

`Bounds` and `AABB` answer in local space after re-rooting, and about the **rest
pose** for anything drawn through the pose buffer — a glTF skin, and equally a
node carrying a mesh that has an animation channel of its own. Where an animated
model is *this* frame is a question scene does not answer, which is also why a
skinned primitive is never culled. A model whose file declares no bounds is
never culled either.

Unloads are the caller's lever and cascade to nothing. `UnloadModel` releases
geometry, poses and material records but **not textures** — with no refcount the
lookup cannot know whether another loaded model binds the same image by path.
`UnloadTexture` is the separate lever and checks no loaded model, so it is for a
texture whose models are already gone; it frees every colour-space variant and
every embedded image the path baked, because the path is the whole of what a
caller can name. `UnloadModel` is the only retry there is for a model, and
`UnloadTexture` is the only one for a picture: a failed load is terminal and
clears there and nowhere else, so a broken image that has been fixed on disk
needs both.

## Animation Is Stateless

Nothing in scene advances a clock. Gameplay owns the time: a game System writes
each play's `Time` into the Entity's `Animation` every tick, and
`model.ClipMachine.PlaysInto(&animation.Plays)` is the stepped-machine spelling.

`Animation.Plays` holds up to `model.MaxClipPlays` (four) clips, blended with
weights **normalised across them**, so they are proportions rather than
intensities: two plays at 1.0 and two at 0.1 are the same even blend. A play
with an empty `Clip` is an unused slot, wherever it sits. No `Animation`, or one
with every slot empty, draws the rest pose, which is a real pose. `Animation`
has no effect on a `Mesh`.

Every Entity carries its own animation block, so animated Entities that share a
model, material and `Params` still share a Batch.

## Batches, Params And Materials

Entities batch automatically into one instanced draw per pass when their key is
equal: the same model primitive or `MeshRef`, the same `Material`, and **equal
`Params` values**. Layers, culling and the camera are not in the key.

- **A distinct `Params` value is a Batch of its own.** Five thousand crates
  tinted red are one draw; five thousand crates tinted five thousand ways are
  five thousand. That is the price of per-Entity tint until per-instance
  properties land.
- **Blended materials are one draw per Entity**, sorted back to front across
  Batches. An alpha-tested cutout is opaque, so it batches.
- **Draw order within a pass is not recording order**: opaque draws sort by
  material and Batch, blended ones back to front. Do not rely on spawn order.

`Params` is laid by name over every tag's params, so
`gfx.ColorParam("baseColorFactor", c)` tints a `Model` and a `Mesh` alike.

A `Material` **overlays** the file rather than replacing it. For each primitive
and each tag: the tag's `Shader`, or the default scene shader; the primitive's
own textures, samplers and numbers, overlaid by name with the default shader's
`Params` and then the tag's; and the tag's `State`, or the file's. Two
consequences:

- **Replacing is not a mode.** gfx binds params by name and ignores one no
  binding declares, so a shader that reads none of the file's bindings has
  replaced it — the dissolve, the silhouette and the depth-only case.
- **The variant is cog's.** Whatever shader is in effect is compiled under the
  primitive's `SCENE_SKIN` / `SCENE_MORPH`, so a custom shader over a skinned
  model gets the pose buffers and must handle the define — build it on
  `model.VertexStagePath` rather than a vertex stage of your own.

The **default scene shader** is model's:
`model.LookupAccess.SetDefaultSceneShader(model.SceneShaderDescr{Source, Params})`,
unset by the zero descriptor. It feeds every Entity with no `Material` and every
tag that names no shader, and takes effect on the next frame.

**A custom shader has group 3 and one storage buffer of its own.** Groups 0 to 2
are what cog binds. The material's numbers are the one uniform block gfx allows
a shader, so per-draw numbers of the app's own are members it adds to that
block, composing it from `model.MaterialProloguePath`, its own fields over
`model.MaterialFieldsPath`, and `model.MaterialEpiloguePath`, before it includes
the stages. It may also declare the bindings scene binds on every draw —
`sceneFrame`, `sceneInstances`, `sceneAnim`, `sceneMeshes`, any subset — because
those are scene's and already counted against a budget of eight that the
bundled shader holds seven of. Declare exactly what the shader reads: **a
declared storage buffer nothing supplies drops that draw** and reports
`gfx.ErrStorageBufferUnsupplied` once per shader and binding.

A **custom vertex layout requires a `Material`**: a `Mesh` with one and no
`Material` is skipped with `ErrMeshCustomLayoutNeedsMaterial`, because the
bundled PBR knows two layouts and no others — the **standard** one
`model.Vertex` reports, six attributes at 32 bytes, and the **skinned** one the
glTF loader gives a geometry some placement skins, the same six plus `JOINTS_0`
and `WEIGHTS_0` at 40. The skinned layout is unreachable from the public API;
nothing an app builds can supply locations 6 and 7, and nothing needs to. The
reverse — a named layout with a custom material — is fine.

A custom material over the **standard** layout must **decode the normal and the
tangent**. They are stored octahedrally, four bytes each, so `@location(1)` is a
`vec2<f32>` and `@location(2)` is a `u32`; include `model.VertexDecodePath` and
call `sceneDecodeNormal` and `sceneDecodeTangent` at the top of the vertex
stage, before any morph or skin. Declaring the `vec3<f32>` and `vec4<f32>` those
used to be is refused at pipeline time — which is the only reason it is not a
trap: WebGPU itself would have filled the missing components with `(0, 0, 0, 1)`
and shaded from a plausible direction lying in the XY plane.

**The UVs are the trap that nothing refuses.** They are stored as 16-bit unorms
against a per-mesh scale and bias, but `@location(3)` and `@location(4)` are
`vec2<f32>` either way, so a custom material that samples a texture from them
raw builds, runs, and samples the wrong place. Call `sceneDecodeUV(vertex.uv0,
mesh.uv0Scale, mesh.uv0Bias)` with the record `sceneMeshOf(instance)` returns —
which means declaring `SceneMesh` and `@group(0) @binding(3)` as
`instance.wgsl` does, because scene binds `sceneMeshes` on every draw.

## Debug Shapes Own Their Mesh

`DebugBox`, `DebugSphere`, `DebugPlane`, `DebugLine` and `DebugWireBox` are
drawn as ordinary `Mesh` Entities. The shape's Systems add a `Mesh`, a `Params`
holding `Color` as `baseColorFactor`, and a `Material` to the Entity, and **the
shape owns all three**: do not add, write or remove them yourself — edit the
shape and its Systems rebake, repaint or release. Removing the shape takes the
three away.

- Every shape is **self-lit**: no light, sun or ambient reaches it. An alpha
  below 1 draws blended, one draw per shape; at 1 it is opaque and batches.
- A `DebugLine`'s `Width` is a size in the Entity's space, so a distant line
  thins out on screen; there is no fixed-pixel line.
- **A shape with nothing to draw draws nothing and reports nothing**: a size or
  radius not above zero, a zero `Width`, a line of no length, a plane with no
  normal.

## Passes And Targets

`Pass.Target` takes the gfx handle untouched: the screen sentinel (zero), a
durable texture through `gfx.TextureTarget`, or a frame-local one from
`gfx.OpQueue.NewTemporaryTarget(w, h, format)`, which hands back the target to
render into and the texture to sample. **A `Camera` Component outlives the
frame and a temporary target does not**: a pass holding one is valid for the
frame it was allocated in only, so a System that uses one allocates it and
rewrites the pass every tick, before `RecordOnUpdate`. A durable texture is the
simpler fit for a Component.

Passes reach gfx labelled `scene.camera<ID>.<tag>`, which is how a HUD or a test
reading gfx's `ArmFrameCmd` snapshot finds one.

A `DepthAuto` pass shares a pooled depth texture with every same-size
`DepthAuto` pass in the frame, canvas's included. Give a pass its own depth
texture when it needs isolation, or keep the camera away from canvas's orders.
Naming an explicit depth texture is also what makes scene keep the depth
attachment, so two adjacent auto-depth passes into one target never merge.

A depth-only pass (`gfx.NoTarget()`) needs an explicit depth texture to take its
size from, and may not clear a colour; either mistake skips the pass and reports
`ErrColourlessPassWithoutDepth` or `ErrColourlessPassClearsColour`.

**A depth-only pass may be written, but its output may not be depended on in the
same frame unless the backend is known to encode it.** Vulkan and a browser do;
GLES does not, and neither does anything `cog/extensions/gogpu` does not recognise — it
declines a `NoTarget()` pass and reports `gogpu.ErrDepthOnlyPassUnsupported`. It
is the backend that decides, not the platform. A later pass loading that depth
with `ClearDepth` absent therefore renders against undefined depth wherever the
pass was skipped — the whole target, not the one draw.

**Canvas cannot composite a rendered texture through its built-in triangle
material.** All three canvas shaders run every texel through the key-colour
ramp, whose output is a function of red alone, and no key colour makes it an
identity — so a 3D render composited that way loses every dark warm shadow to
neutral grey. Supply a passthrough material.

## Verification

scene publishes no inspection API. Assert on what reached gfx: scene's own
tests in `bundles/scene/internal` run a real kernel against a recording
`gfx.Backend` with no GPU, and a game's tests read gfx's `ArmFrameCmd` snapshot
or run under cog-examples' `headless.NewECS`, which composes ecs and scene over
a headless backend.

One contract a desktop run passes while saying nothing about, needing
`bash cmd/web/build.sh <demo>` and a browser: the **storage-buffer budget** (a
native adapter reports hardware limits, where 200 storage buffers is ordinary).
Treat a desktop green as silent on it.

The **depth-only pass** executes on a Vulkan desktop as well as in a browser,
but is still declined on GLES, so a desktop green says nothing about it on a
machine where GLES won the adapter selection — the startup log's `adapter
selected ... backend=` line is what tells you which run you had.
