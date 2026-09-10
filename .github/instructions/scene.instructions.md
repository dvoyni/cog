---
name: "Scene Recording"
description: "Use when creating or changing Go code that records a 3D frame through the cog scene plugin - cameras, passes, glTF models, buffer-built meshes, lights, or debug shapes. Covers wiring, the zero values that are load-bearing, residency and the frame boundary, and the traps that are decisions rather than bugs."
applyTo: "**/*.go"
---

# Scene Recording

`scene/README.md` documents the API and `scene/docs/specs/scene.md` records why each rule is
what it is. These are the traps a caller hits that neither the compiler nor a
plausible-looking zero value warns about. Follow them in new and changed code
without expanding a focused task into unrelated cleanup.

Scene's house rule, and the one that explains most of what follows: **skip,
never substitute**. A model that is not resident, a selector that matches
nothing, a stale mesh ref, a light with no cone — each costs its own draw, is
reported once, and is stood in for by nothing. A frame with a hole in it is the
correct picture.

## Wiring

Register `storage` before `scene`, and put the app's own recording plugin last:

```go
plugins := []kernel.Plugin{
	storage.New(), input.New(), gfx.New(), canvas.New(), scene.New(), wgpu.New(),
	demo, // records into the queues the plugins above declare
}
```

Scene reads storage buffers from the vertex stage, so it needs a **WebGPU core
adapter**. Compatibility mode defaults that limit to zero and the binding cannot
request compat at all, so such a device fails `requestAdapter()` and gets no
WebGPU. There is no degraded mode to fall back to.

The kernel's default error handler logs and **returns true, which terminates the
engine**. Any code that provokes a report on purpose — a deliberately broken
asset path, a cap it means to exceed — installs its own handler with a named
allow-list and returns false for the entries on it:

```go
kernel.New(config).Handler(demo.report).WithPlugins(plugins...).Run(ctx)
```

## The Zero Values Are The API

Writing a field that already means what you want is how a caller finds the wrong
default the hard way.

| Field | Zero means | The trap |
| --- | --- | --- |
| `Pass.ClearDepth` | preserve | Depth is conventional: near → 0, far → 1. `&zero` clears to the **near plane and hides the whole scene**. `1.0` is the useful value. |
| `Pass.ClearColor` | preserve | A defaulted colour clear would let a second camera erase the first. Clear colour deliberately, once, on the lowest pass. |
| `CameraDescr.Passes` | one default pass | Writing any pass replaces the default outright, including its `ClearDepth: 1.0`. Carry the clear into the first pass you write. |
| `CameraDescr.CullMask` | `LayersAll` | So does a recorded item's own zero `LayerMask`. Zero reads as *all* on **both sides**, so a mask only ever excludes once both ends write one. |
| `LightDescr.Range` | infinite | glTF's own default. A forgotten `Range` is a light that reaches too far — visible immediately — rather than a light silently dropped. |
| `LightDescr.OuterCone` | π/4 | `InnerCone` zero is a **real value**, not a default: falloff straight from the axis. |
| `CameraDescr.Shear` | `0`, i.e. plain `Orthographic` | Only `Oblique` reads it. Setting it on a `Perspective` or `Orthographic` camera does nothing, the way `FovY` does nothing under `Orthographic`. |
| `Transform.Scale` | 1 | Scalar. Non-uniform scale goes through `Matrix`, which replaces the whole transform. |
| `Material` (nil) | the bundled PBR | Every draw literal that omits the field gets lit PBR and needs no shader. |
| `ModelDraw.Scene` / `.Node` | the default scene / the whole scene | A **non-empty** selector that matches nothing skips the draw and never falls back. |

`CameraDescr.Near` and `.Far` are the exception: both are required, and a zero in
either skips the camera and reports. Nothing plausible is substituted.

A camera's `Transform.Scale` is **ignored**. Only position and rotation are
inverted into the view matrix, so a rig that scales its camera node changes
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
q.Camera(cameraMain, scene.CameraDescr{
	Transform:  scene.LookAt(m.Vec3{}, m.Vec3{Y: -1}, m.Vec3{Z: -1}), // in the ground plane
	Projection: scene.Oblique,
	Height:     30,
	Shear:      0.5,
	Near:       -100, Far: 100, // the camera sits inside its own depth range
})
```

In a shader, **do not difference against `sceneCameraPosition()` for a view
vector.** Use `sceneViewDirection(worldPos)`. The camera position is a real
viewer only under `Perspective`: an orthographic camera has no eye point, and an
oblique one looks one way while its viewer sees another, so a hand-rolled
`normalize(sceneCameraPosition() - p)` lights vertical faces as if edge-on and
floors as if head-on. `sceneCameraPosition()` remains correct for what it is —
the transform's translation — and so for fog and detail fades.

## Layers Select Cameras

A camera draws an item iff `item.layers & camera.CullMask != 0`.

A **light's** `LayerMask` selects which cameras see the light, not which objects
it lights. Both ends of the test are camera-side. Per-camera exclusion is the
whole of what layers do, and it is the one thing nothing else in scene can
express — a frustum outline drawn once, seen by the minimap and declined by the
camera it describes, is one draw and one exclusion.

## Residency Lands At The Frame Boundary

A draw of a path that is not resident **draws nothing** and enqueues exactly one
load however many frames name it. Wire the empty case into the picture — a bare
pad under every model slot — so a frame in progress reads as loading rather than
as broken.

Every residency change and every unload lands at the next frame boundary, not at
the call. Three consequences worth writing down:

- `UnloadModel` followed by `Preload` in one handler is a **no-op**: the unload
  has not landed yet, so the preload sees the old entry. A retry straddles the
  boundary.
- Residency flips one frame earlier than the bakes reach the backend, so a
  texture count read the instant `State` says resident is the count from the
  frame before.
- A residency snapshot read inside an update handler is on the **near side of
  scene's flush**, which runs after it. Judging a flush result against it
  compares two different frames.

Move the decode into a loading screen the app controls with `Preload`, which is
the same idempotent load a draw fires, fired without one.

## Queries Answer About Now

Every lookup query returns `(value, ok)` and every query **triggers the load**,
exactly as a draw does.

`ok` means only **"this value is real"**. It is false for a path still loading
and false for a path that will never arrive, so a loading screen watching `ok`
alone hangs forever on a typo. `State(path)` is the only call that tells the two
apart, and it is what a wait loop watches.

`Bounds` and `AABB` answer in local space after re-rooting, and about the **rest
pose** for anything drawn through the pose buffer — a glTF skin, and equally a
node carrying a mesh that has an animation channel of its own. Where an animated
model is *this* frame is a question scene does not answer, which is also why such
a draw is never culled. A model whose file declares no bounds is never culled
either.

Unloads are the caller's lever and cascade to nothing. `UnloadModel` releases
geometry, poses and material records but **not textures** — with no refcount the
lookup cannot know whether another resident model binds the same image by path.
`UnloadTexture` is the separate lever and checks no resident model, so it is for
a texture whose models are already gone. `UnloadModel` is also the only retry
there is: a failed path is terminal and clears there and nowhere else.

## Animation Is Stateless And Positional

Nothing in scene advances a clock. Gameplay owns the time, and no play survives
the frame that recorded it.

`MorphWeights` is **positional** over the model's whole flattened target list —
one entry per target of every morphed node, in depth-first node order, which is
exactly what `MorphTargets(path)` returns. Resolve names to indices once at
startup and index from there; the recording path is a memcpy on purpose. A short
slice leaves the rest at zero and is not an error, and a long one drops its tail
and reports once.

`Plays` blends up to four clips with weights **normalised across them**, so they
are proportions rather than intensities: two plays at 1.0 and two at 0.1 are the
same even blend. An empty `Plays` draws the rest pose, which is a real pose.

Instances share the draw's animation. A hundred crates is one call; a hundred
independently animated characters is a hundred calls.

## Materials, Shaders And The Binding Budget

Two ways to change a model's appearance, and picking the wrong one is a silent
wrong picture:

- `OverrideParams` **merges** by glTF's own parameter names over each
  primitive's material and keeps the file's textures. That is the team colour,
  the hit flash and the fade.
- `Material` **replaces** wholesale. The file's PBR records are not bound at
  all, so its base colours, factors and texture transforms go with it. That is
  the dissolve, the silhouette and the depth-only case.

A replacement `Material` is **always the caller's own shader**. It sets the draw
off the bundled PBR path, and the bundled shader declares a material record plus
five texture/sampler pairs that nothing would then bind — which fails
`CreateBindGroup`, and a failed bind group takes the **whole frame's command
buffer** with it, silently. Apply one only to a subtree that declares whatever
group 2 needs.

That failure mode is general: **a declared-but-unused WGSL binding is
frame-fatal**. Reflection keeps every module global, so every `@group/@binding`
a shader declares must be bound at draw time. Declare exactly what the shader
reads.

A caller-supplied material declares **no storage buffers of its own**. Use the
bindings scene binds on every draw — `sceneFrame`, `sceneInstances`,
`scenePbrMaterial`, any subset — because those are scene's and already counted
against a budget that stands at seven of eight with the eighth reserved. Put
per-object data in the vertices.

A **custom vertex layout requires a custom `Material`**: the bundled PBR is one
module with one vertex stage and no entry-point selection, so its inputs are
`scene.Vertex`'s eight attributes and nothing else. The reverse — the standard
layout with a custom material — is fine.

## Passes And Targets

`Pass.Target` takes the gfx handle untouched. Scene mints no textures, so a
render-to-texture camera calls `gfx.OpQueue.TemporaryTarget(w, h, format)`,
which hands back the target to render into and the texture to sample, and locks
both queues to pass the target across.

A `DepthAuto` pass shares a pooled depth texture with every same-size
`DepthAuto` pass in the frame, canvas's included. Give a pass its own depth
texture when it needs isolation, or keep the camera away from canvas's orders.
Naming an explicit depth texture is also what makes scene keep the depth
attachment, so two adjacent auto-depth passes into one target never merge.

**A depth-only pass may be written, but its output may not be depended on in the
same frame unless the backend is known to encode it.** Vulkan and a browser do;
GLES does not, and neither does anything `cog/wgpu` does not recognise — it
declines a `NoTarget()` pass and reports `wgpu.ErrDepthOnlyPassUnsupported`. It
is the backend that decides, not the platform. A later pass loading that depth
with `ClearDepth: nil` therefore renders against undefined depth wherever the
pass was skipped — the whole target, not the one draw.

**Canvas cannot composite a rendered texture through its built-in triangle
material.** All three canvas shaders run every texel through the key-colour
ramp, whose output is a function of red alone, and no key colour makes it an
identity — so a 3D render composited that way loses every dark warm shadow to
neutral grey. Supply a passthrough material; `cmd/scene/cameras/composite.go` is
a minimal one.

## Reading The Frame Back

`Passes(dst)` publishes the frame the **last flush** consumed, so a reader inside
an update handler is looking at the previous frame. Pair anything compared
against it with a snapshot read at the same moment.

Within a pass, **recording order is not preserved**: draws are sorted by
material then mesh, and blended draws sort back to front. A `Model` call expands
into the draw list at flush time, so every directly recorded debug shape reaches
the culler ahead of every model draw whatever the recording order was.

Every slice on every descriptor is **borrowed for the duration of the call** —
scene copies into its frame arena before returning — so one backing array serves
a whole hot loop.

## Verification

`go test ./cmd/scene/...` in `cog-examples` runs the whole acceptance suite with
no GPU, because culling, sorting and packing all happen in the update-thread
flush and the result is published as `Passes`. Assert on those numbers.

One contract a desktop run passes while saying nothing about, needing
`bash cmd/web/build.sh <demo>` and a browser: the **storage-buffer budget** (a
native adapter reports hardware limits, where 200 storage buffers is ordinary).
Treat a desktop green as silent on it.

The **depth-only pass** used to be the second, and is no longer: it executes on a
Vulkan desktop as well as in a browser. It is still declined on GLES, so a
desktop green says nothing about it on a machine where GLES won the adapter
selection — the startup log's `adapter selected ... backend=` line is what tells
you which run you had.
