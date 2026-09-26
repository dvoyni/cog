# gfx draw params — specification

`github.com/dvoyni/cog/slots/gfx` stops having materials. A draw today names a
`MaterialDescr` — a shader descriptor, fixed pipeline state and a slice of named
parameters — and the queue copies that slice into its frame arena on every
draw, the translator hashes its names on every draw to find a plan, and the
translator looks every binding up by name in two lists, the material's and the
draw's, at every step it takes. `OpQueue.FrameMaterial` and the
`FrameRecording` it stamps onto a public value exist only to claw that cost
back, and canvas, scene and model each build a batching key of their own on
`Fingerprint` over the same bytes. None of that work is needed for a material
that has not changed since the last frame, which is nearly every material.

This document specifies what replaces it: **explicit shaders**, compiled on the
CPU and created once; **draw params**, a durable opaque set of whole-binding
values created once per "material" and resolved against its shader's layout;
and a **per-frame version** of a set, which is how a draw gets values that
differ from the set's own. `Draw` loses its material and its trailing params.

The design is bound by four requirements, in this order:

1. **Nothing that did not change is copied, hashed or resolved again.** A set
   is resolved once; a frame pays only for the bindings it actually changes.
2. **GC-free storage on the hot path.** What a set and a frame hold is
   pointer-free — packed bytes and binding records — so the collector has
   nothing in them to scan, and reset truncates rather than frees.
3. **No System parallelism is lost.** The expensive step, compiling a shader,
   holds no gfx lock; the queue methods stay as cheap as a record.
4. **One mechanism, extensible by adding bindings.** gfx binds whatever the
   shader reflects, by the name the shader gives it; an extension adds a binding
   and never reaches into another's.

Where a claim rests on something unverified it is marked **Gap** and says what
would settle it; where assembling decisions next to each other settled
something no conversation did, it is marked **Settled here**.

**This specification is not yet implemented.** It is built on the branch
`draw-params` in cog, feuds-26, nox and cog-examples, merged together.

---

## Contents

- [Vocabulary](#vocabulary) · [Facts this rests on](#facts-this-rests-on)
- [Shaders](#shaders) · [Draw params](#draw-params) · [A param is a whole binding](#a-param-is-a-whole-binding)
- [The frame's version of a set](#the-frames-version-of-a-set) · [Draw](#draw)
- [What the translator does](#what-the-translator-does) · [Unsupplied bindings](#unsupplied-bindings)
- [Errors](#errors) · [Released ids](#released-ids) · [Inspection](#inspection)
- [The bundles, for now](#the-bundles-for-now)
- [What is not foreclosed](#what-is-not-foreclosed)
- [Shapes that were rejected](#shapes-that-were-rejected)
- [Required work](#required-work) · [Out of scope](#out-of-scope)

---

## Vocabulary

`CONTEXT.md` is the glossary of record; Shader source, Shader module, Batch,
Pass and Vertex layout keep their meaning there. This document adds:

- **Shader program** — the output of `CompileShaderCmd`: the flattened WGSL of
  one shader descriptor, its source map, its reflected layout and the binding
  table built from it. Pure, immutable data; no GPU object stands behind it yet.
- **Shader** — a durable GPU shader module, named by a `ShaderID` that
  `NewShader` reserves, created from the Shader program `UploadProgram` gives
  it, freed by `ReleaseShader`.
- **Binding** — one global resource a shader declares at a `@group`/`@binding`:
  a uniform, a storage buffer, a texture or a sampler. It is named by its WGSL
  global variable name, which a module cannot declare twice.
- **Draw params** (a *set*) — a durable, opaque handle created by
  `NewDrawParams`: one shader, one Draw state, and a value for some or all of
  that shader's bindings.
- **Draw state** — the fixed pipeline state a set draws with: blend, depth
  compare, depth write, cull and front face. It is today's `MaterialState`
  renamed, and it is fixed on the set.
- **Version** — the frame-local state of a set after `SetDrawParams`: a copy of
  the set's binding table in which the changed bindings point at new values.

**Material** is retired from gfx. It stays a word the bundles may use for their
own concepts — scene's Scene material, model's Model material, canvas's
material set — because there it means one.

---

## Facts this rests on

- **Reflection needs no GPU.** gogpu reflects a layout by parsing and lowering
  WGSL with naga, which its own code calls pure, and only then creates the GPU
  module. Reflection is a function of the source alone.
- **Nothing reloads shaders today.** There is no file watching; a shader
  reloads only when `ReleaseCachedResourceCmd` names a path it read, and nothing
  in cog, feuds-26, nox or cog-examples sends one. Explicit shader lifetime
  loses no working behaviour.
- **Every shader already has one struct per uniform binding.** What Go does
  today is set that struct's *members* by name, which the translator packs one
  member at a time. Textures, samplers and storage buffers are already set
  whole.
- **Three extension shaders reach into another's struct.** feuds' lens adds
  members to canvas's `CanvasUniforms`, its three fade shaders add a `fade`
  member (and triangles a `keyColor`), and nox's sight shader adds members to
  model's `ScenePbrMaterial`.
- **Resource ids are never reused.** Backend ids come from monotonic counters,
  so a released id cannot alias a new resource; an "is it still there" check is
  exact without generations.
- **A re-rendered frame can name released ids.** When no new frame is pending,
  render translates the previous read queue again. A mesh released in between
  reaches the backend with its vertex and index buffers skipped but its draw
  issued — it draws with whatever was bound earlier in the pass, or fails
  validation. A texture falls back to white and a storage buffer drops its
  bind group; only the mesh case is unguarded.

---

## Shaders

```go
type CompileShaderCmd kernel.Command[CompileShaderRequest, CompileShaderResponse]

type CompileShaderRequest struct {
	FS    fs.FS
	Descr ShaderDescr
}

type CompileShaderResponse struct {
	Program ShaderProgram
	Err     error
}

func (q *ResourceQueue) NewShader() ShaderID
func (q *ResourceQueue) UploadProgram(k kernel.Kernel, shader ShaderID, program ShaderProgram)
func (q *ResourceQueue) ReleaseShader(k kernel.Kernel, id ShaderID)
```

`CompileShaderCmd` flattens the descriptor through the preprocessor, reflects
the result, and builds the binding table by name. It is a kernel Command with an
**empty lock**: the kernel runs an empty-lock task on the caller's goroutine
with no coordinator round-trip, so a System declaring `Uses[CompileShaderCmd]`
widens its lock set by nothing, and the command's cost — file reads, the
preprocessor and a naga parse — never serialises another System. It reads only
the filesystem the request carries, which the caller holds its own read of.
It is the one call whose failure is a normal outcome (a missing file, an
include that does not resolve, WGSL that does not parse), so its failure is the
response's `Err`, never also reported, beside the zero program; the caller
decides what a missing shader means.

Reflection moves behind a **port** on the Backend the driver's Adapter fills:

```go
ReserveShader() ShaderID                          // CPU-only, any thread
CreateShader(id ShaderID, desc ShaderDesc) error  // render thread, at replay
ReflectShader(code []byte) (ShaderLayout, error)  // pure, any thread
```

`ReflectShader` needs no device and is safe from any thread; the command reaches
it through the Backend adapter it captured at registration. `slots/gfx` cannot
import naga, and a backend-neutral slot should not; gogpu fills the port from
the naga reflection it already runs.

`NewShader` and `UploadProgram` mirror `NewTexture` and `UploadTexture`.
`NewShader` reserves an id at once, CPU-side, from `ReserveShader`'s own atomic
counter. `UploadProgram` records the program: the CPU side keeps its binding
table from that call on, so a set built on the shader resolves against it at
creation, and the backend creates the module through `CreateShader` when the
upload is replayed, measuring it against the web floor then, once. A set can
therefore be built on a shader only once its program is uploaded.

**An id takes one upload.** A second `UploadProgram` to the same id is reported
through `k`, once, and ignored; so is the zero program a failed compile returns,
which leaves the shader reserved for a program that compiled. An id `NewShader`
never reserved, or one already released, is reported by both calls.
`ReleaseShader` frees the module and every pipeline keyed on it; a shader
released before its upload never reached the GPU and frees nothing there.

Each variant — a different Define or Const — is a separate program and a
separate shader, which the caller compiles when it loads, not when it draws.

**Reloading is the caller's.** To reload, the app releases the shader and
creates it again, with every set built on it. There is no replacement in place.

---

## Draw params

```go
func (q *ResourceQueue) NewDrawParams(k kernel.Kernel, shader ShaderID, state DrawState, params ...ParameterDescr) DrawParams
func (q *ResourceQueue) UpdateDrawParams(k kernel.Kernel, set DrawParams, params ...ParameterDescr)
func (q *ResourceQueue) ReleaseDrawParams(k kernel.Kernel, set DrawParams)
```

A set is created once per whatever the caller calls a material and is named by
an opaque handle. Its handle is its identity, so it is also the complete batch
key a bundle needs: two draws naming the same set share shader, Draw state and
values, and nothing has to be fingerprinted to know it.

At creation the set is resolved against its shader's binding table, which the
Shader program already carries, so it happens on the caller's thread and the
translator never resolves a name. The set holds:

- a pointer-free **binding table**, one slot per `@group`/`@binding`;
- for each uniform binding, its **packed bytes** in a byte arena;
- for each texture, sampler and buffer binding, its **resource record** — id,
  offset and size — in the slot itself, with no bytes behind it;
- the binding names, in a side table only diagnostics read.

**Draw state is fixed on the set.** The same surface under a different state —
a depth prepass, an overlay — is a second set, and sets sharing their resources
cost almost nothing.

**A durable set names durable ids only.** A temporary buffer, texture or target
from an OpQueue lives one frame; a set outliving it would bind a stale id.
Inline bytes a param carries — `TextureWithBytes`, an inline buffer — are baked
into durable ids that the set owns and that `ReleaseDrawParams` frees.

**Settled here:** a durable id is one the ResourceQueue minted — `NewBuffer`,
`NewTexture`, `NewRenderTarget`, or a bake of a set's own — and the queue keeps
one bit an id to say so; any other id is refused as a temporary. A texture named
by path is neither, and is kept: the set holds the path, interned, and the
render thread's texture cache resolves it as it resolves every path. Canvas's
materials name their sprites by path, and a set that refused one would have to
load the file itself.

`UpdateDrawParams` changes the set's own values and persists. Because the
ResourceQueue is consumed by whichever render comes next, a durable update is
for material-rate changes — a swapped texture, a tint, a value an editor
tweaks. It may be seen one frame early by a re-rendered frame, as every durable
resource change already may. A value that must match its frame goes in the
frame's version.

---

## A param is a whole binding

A param addresses one binding by its global name and supplies all of it:

| Binding | Param |
|---|---|
| `var<uniform>` of any type | bytes of a pointer-free Go value with the same size and WGSL layout |
| `var<storage>` | a buffer, or a range of one |
| texture | a texture |
| sampler | a sampler |

`ParameterDescr` keeps four kinds — bytes, texture, sampler, buffer — and
`FloatParam`, `VecParam`, `MatParam` and `ColorParam` stay as typed
constructors that produce bytes: `var<uniform> tint: vec4f` is set with a
`VecParam`, `var<uniform> mvp: mat4x4f` with a `MatParam` or a `[16]float32`,
`var<uniform> material: Material` with the Go struct that mirrors it. The check
is the same for all of them: the value's size equals the binding's reflected
size, and its layout passes WGSL's alignment rules once per Go type, as
`RawParameter` already checks.

**There are no member-level writes.** To change one field of a struct, the
caller resubmits the struct, which it holds a Go copy of. gfx reflects no member
offsets and packs no members.

**Shaders group uniforms by rate of change**: a material struct set once,
a per-draw struct set per frame, a per-pass struct if one is needed. That is
ordinary WGSL practice and it is what keeps a frame's version small.

**Settled here:** names are unique for free. A param names a WGSL global, and a
module cannot declare two, so the only check a name needs is that the binding
exists.

**Settled here:** a bytes param of up to 64 bytes — every typed constructor's,
and a small `RawParameter` — carries its value inside the descriptor, so a param
built per draw allocates nothing. The bytes remember which constructor built
them, which binding never reads: it is what keeps `ColorValue` and its siblings
answering, and a snapshot naming a param a color rather than bytes.

**Settled here:** reflection names every buffer binding by its address space,
whatever type it is declared at. A bare `var<uniform> tint: vec4f` is a uniform
with its size, and a bare `var<storage> models: array<mat4x4f>` a storage buffer;
reading only the struct-typed ones left them out of the pipeline layout and out
of every set's binding table.

---

## The frame's version of a set

```go
func (q *OpQueue) SetDrawParams(k kernel.Kernel, set DrawParams, params ...ParameterDescr)
```

`SetDrawParams` changes a set **for the rest of this frame, from this point
on**. The next frame starts from the set's durable values again. It is
frame-local because the OpQueue is: a queue may be dropped, and a durable value
must not depend on which frames survived.

**A draw sees the values the set had when the draw was recorded.** Passes run
in `Order`, not in the order they were recorded, so a set drawn into two passes
with a change between must not let the later value leak into the earlier draw:

```go
q.SetDrawParams(k, set, mvp(cameraA))
q.Draw(passA, mesh, set, 1, 0)
q.SetDrawParams(k, set, mvp(cameraB))
q.Draw(passB, mesh, set, 1, 0) // passB may run before passA
```

So a change makes a **version**, copy-on-write, per binding:

1. the version's binding table is made in the frame's arena — a few dozen
   bytes per binding, pointer-free — copied from the version the draws so far
   captured, or empty for the set's first version this frame;
2. each uniform binding the call names gets its new bytes in the frame's byte
   arena, and its slot points there; bindings it does not name keep pointing at
   the bytes they had, shared rather than copied;
3. each resource binding the call names has its slot's record replaced;
4. `Draw` records the set and its current version, which is an offset.

A version no draw has captured yet is patched in place rather than copied
again, so several Systems each setting a few bindings before the draws pay one
copy. Draws with no change between them share a version.

**Settled here:** a version holds only what the frame changed. An empty slot is
the set's own, and the translator reads it from the set as the render finds it,
which is what "shared rather than copied" comes to once the set's values live
where only the render reads them. `SetDrawParams` is called from Systems that
hold the OpQueue and not the ResourceQueue, so it never reads a set's values:
it resolves names against the set's program, which is immutable, through a
table the ResourceQueue publishes each set to — the program written first, the
state after, atomically — and reads without a lock. What that costs is that a
durable update made later in the same tick shows through a version's untouched
bindings, which the durable update's one-frame-early rule already allows.

Temporaries belong here. A per-frame instance arena is
`SetDrawParams(k, set, BufferParam("instances", arena))`, and `firstInstance`
still picks a Batch's slice of it.

The frame's arenas are byte and record slices, truncated by reset like the
queue's other arenas: nothing in them is a pointer.

---

## Draw

```go
func (q *OpQueue) Draw(pass PassRef, mesh MeshDescr, set DrawParams, instances, firstInstance int)
```

Every draw names a set; there is no draw without one. A one-off draw — a debug
overlay, a fullscreen composite — creates its set once, at setup, like any other
resource. The draw carries no params of its own: whatever differs per draw is a
version.

Per-instance data is combined outside gfx, as it already is: the caller packs
an instance arena and binds it as a storage buffer. gfx has no per-instance
parameter concept.

---

## What the translator does

For each draw it reads the set's version, and:

- **Pipeline.** It keys the pipeline on shader, Draw state, the mesh's Vertex
  layout and the pass's attachments, lazily, as today. The set supplies the
  first two whole.
- **Uniforms.** Each distinct uniform version is written into the frame's
  uniform arena once, keyed by its arena offset; every draw sharing it binds the
  same offset. Nothing is packed per draw.
- **Resources.** It binds each slot's record.

The name-keyed parameter plans, the two-list lookup, the shape hash and every
per-draw kind check go: everything a draw names was resolved at creation.
The checks that depend on the frame stay where they are — a texture the pass is
drawing into, a texture of the wrong view dimension, a vertex interface the
mesh does not satisfy.

---

## Unsupplied bindings

A binding the shader declares and neither the set nor its version supplies is
checked when the draw is resolved, not when the set is created, because the
frame may supply it through `SetDrawParams`:

| Binding | Unsupplied |
|---|---|
| uniform | zero bytes |
| sampler | the default, clamp and linear |
| texture | white, 2D or 2D-array as declared |
| storage buffer | the draw is dropped and reported once |

The first three are legitimate "not used here" values, and model relies on
white for a missing slot. A missing storage buffer means the draw has no data.
This is today's behaviour, unchanged.

---

## Errors

Every error except `CompileShaderCmd`'s is a programmer mistake —
deterministic, seen the first time the code runs:

- a param naming no binding of the shader;
- a param of the wrong kind for its binding;
- uniform bytes of the wrong size or layout;
- a temporary id in a durable set;
- a released or unknown shader or set;
- a second upload to one shader, or an upload of the zero program.

They are **reported through `kernel.Kernel`**, which each method that can find
one takes as its first argument: `UploadProgram`, `ReleaseShader`,
`NewDrawParams`, `UpdateDrawParams`, `ReleaseDrawParams` and `SetDrawParams`.
The report comes from the System that made the mistake, in its own tick. The
bad param is ignored; a set whose creation failed still exists and draws
nothing, silently — naming it is no mistake, so `UpdateDrawParams` and
`SetDrawParams` ignore it without a report and `ReleaseDrawParams` releases
it. `SetDrawParams` is on the hot path and reports each condition once, under a
key naming the set and the binding.

`Draw` keeps no kernel. Its failures are the frame's — a pass not declared, a
pipeline that does not build, a storage buffer unsupplied — and the translator
reports them as it does today.

---

## Released ids

The translator checks that every id a draw names is live — its set, and its
mesh's vertex and index buffers — and drops a draw naming one that is not,
silently: releasing something a frame still names is not a mistake, it is the
re-rendered frame. Ids are never reused, so liveness is a lookup in a dense
table indexed by id.

This closes the unguarded mesh case too.

**Settled here:** a set's liveness is its own record's state. The ResourceQueue
keeps its sets in a table indexed by id, which the translator already reads to
draw the set, so the check costs no load of its own. A set built on a shader
released since is dropped the same way: releasing the shader is the app
reloading it.

**Gap:** the check is a few indexed loads a draw. It ships if an interleaved
A/B of `TranslateSteadyState` against the pre-change commit shows it within the
±3% noise, and is reconsidered if not.

---

## Inspection

The frame snapshot and the MCP views lose `MaterialView` and
`MaterialStateView`. A draw's set is rendered as a **draw params view**: the
shader's label, the Draw state with its enums named, and each binding's name,
kind, group, binding and either its size or its resource id, naming whether the
value came from the set or the frame's version. Draw and instance counts are
unchanged. The spec of record is `mcp.md`, which is amended with it.

---

## The bundles, for now

This refactor keeps every bundle's public API where it can. Scene's proper
materials, and then canvas rebuilt on scene, are the next two refactors and are
out of scope here.

- **Model** compiles its four shader variants and creates its durable sets
  when it loads a file, where it already builds its materials once. The PBR
  values are packed into a Go struct mirroring `ScenePbrMaterial` and set as
  one binding.
- **Scene** keeps its Scene material and `Material` Component. It caches a set
  per material key, using the keys it already computes, and sets its per-Batch
  bindings — frame, instances, animation, poses — through `SetDrawParams`.
  `FrameMaterial` is gone, and scene's recording no longer calls it.
- **Canvas** keeps its API by moving the old descriptor into canvas as
  `canvas.Material` — shader descriptor, Draw state and params, with the same
  constructors — and caches a shader per descriptor and a set per material
  fingerprint. It fills `CanvasUniforms` as one struct per Batch through
  `SetDrawParams`. Halo's members and triangles' `keyColor` move to bindings of
  their own.
- **UI** draws through canvas and changes with it.

**Settled here:** canvas compiles in its flush, on a material's first batch.
The flush already holds the ResourceQueue and the storage filesystem, and
declares `Uses[CompileShaderCmd]`, whose lock is empty, so no System's lock set
grows; a steady-state frame finds every material in its cache and compiles
nothing. A shader that fails to compile is reported once, under canvas's own
key, and cached as failed, so its draws draw nothing rather than retry a frame.
`canvas.NewMaterial` keeps `gfx.Material`'s state and `MaterialWithState` takes
one, as the constructors they replace did.

**Settled here:** canvas's sets are one per material fingerprint *and binding
shape* — the names a batch sets, in order — rather than one per fingerprint. A
version carries forward what the next `SetDrawParams` does not name, and canvas
sets optional bindings per batch:

```go
q.DrawTriangles(layer, verts, nil, gfx.TextureParam(canvas.TextureSlot, grass)) // version: canvasTexture = grass
q.DrawTriangles(layer, verts, nil)                                             // copies it: grass, not white
```

Under one shape a set, every batch of it names the same bindings and overwrites
everything the frame changed. Shapes do not vary with values, so the sets stay
bounded. Canvas also drops a param naming a binding the shader never declared
before `SetDrawParams` sees it: one scope's list serves every family, and gfx
used to drop such a name silently.

**Settled here:** `CanvasUniforms` is 96 bytes, past the 64 a param carries
inline, so `RawParameter` would allocate once a batch. `RawParameterRef[T](name,
*T)` is `RawParameter` over a value the caller keeps: validated the same way,
inline when it fits, and borrowed rather than copied when it does not. Every
draw-params call copies a param's bytes before it returns, so canvas refills one
`canvasUniforms` in its flush scratch per batch and allocates nothing. It is
also what an app with a large per-draw record reaches for.

- **Extension shaders** each declare their own uniform struct at their own
  binding: feuds' lens and three fade shaders, and nox's sight shader. The
  struct they used to extend stays its owner's, unmixed.

`FingerprintParams` stays in gfx through this transition, as a hash over
params with no material concept, because scene's and canvas's keys are built on
it. It is reconsidered with scene's materials.

---

## What is not foreclosed

- **Bind groups by rate of change.** A set's bindings could become a bind group
  built once, and a version only the groups it changed. Nothing in the API
  prevents it.
- **Reporting without a Kernel argument.** Error reporting may be refactored
  later — a retainable reporter handed out at Register was prototyped and set
  aside; taking `k` now keeps that open.
- **Generation-checked PassRefs.** A PassRef from an earlier frame still
  silently names a same-index pass. That is a separate question, and nothing
  here depends on it.
- **Durable per-draw values.** If per-draw versions ever dominate a frame, a
  second set per draw bound at another group is the remedy, not a new concept.

---

## Shapes that were rejected

- **Material plus draw params, the draw's overriding the material's by name.**
  The current shape. Two lists looked up on every step, a copy per draw, and a
  recording stamp on a public value to avoid it.
- **`FrameRecording` on the descriptor.** Queue-private cache state riding on a
  public value, with four friend accessors and an identity check through `any`.
  A durable handle makes it unnecessary.
- **A flat per-draw `perInstanceParams` list,** `len % instances == 0`. At
  roughly 300 bytes a `ParameterDescr`, 5000 instances of three fields is about
  4.5 MB copied a frame, with every name repeated per instance and nothing
  enforcing that the groups agree. Instance data is packed outside and bound as
  a buffer.
- **Draw params on the OpQueue.** A handle made there dies with the frame,
  which is `FrameMaterial` again. Sets are durable and live on the
  ResourceQueue.
- **Resolving group, binding and offset in the translator.** The render thread
  was where reflection lived, but an explicit compile step puts the layout in
  the caller's hand at creation, so there is nothing left to resolve later.
- **Member-level params,** a param naming a field of a uniform struct.
  Rejected for whole bindings: no member offsets to reflect, no packing, no
  ambiguous names across structs, and copy-on-write that never copies an old
  value it is about to overwrite.
- **Dropping the scalar uniform constructors.** A `vec4f` binding is 16 bytes
  like any struct; the typed constructors are kept as sugar for the one bytes
  kind.
- **Pipeline state on each draw, or changeable per frame.** It is part of what
  the set is, and fixing it keeps the set a complete batch key.
- **A lazy `ShaderDescr` path beside shader ids.** It would bring back the
  per-frame descriptor, the render-side cache and the hidden load this removes.
- **A draw with no set,** shader and state given per draw. A second path with
  per-draw resolution.
- **Returning errors from the set methods.** They are programmer mistakes;
  only `CompileShaderCmd` fails for a reason the caller must handle.
- **`CompileShader` as a free function,** `CompileShader(fsys, descr)`. An
  Adapter is reachable only through the plugin that requires it, so a free
  function could reach the reflection port only through process-global state
  the backend installs — shared between engines and swapped by every test. A
  Command with an empty lock reaches the adapter it captured at registration and
  costs its caller exactly what the free function would have: no lock and no
  coordinator round-trip.
- **`NewShader(k, program)`, reserving and creating in one call.** The backend
  minted a shader's id only when it created the module, on the render thread,
  from a counter shared with pipelines and samplers. Reserving the id CPU-side
  and uploading the program after mirrors `NewTexture` and `UploadTexture`,
  which is the settled shape for a durable id handed out at once.
- **`ReplaceShader(id, program)`, or a second `UploadProgram` replacing the
  first.** Replacement in place would re-resolve every set built on the shader
  at replay, while the CPU side has already resolved them against the old table
  — two tables for one id, and a set valid against one and not the other. Hot
  reload is out of scope, and release-and-recreate is exact because ids are
  never reused: an id takes one upload for its life.
- **A retained `kernel.Reporter`, or a queue holding a `Kernel`.** `Kernel` is
  documented as scoped to a dispatch, and no Kernel exists at Register. The
  reporter was prototyped; passing `k` was chosen, keeping error reporting free
  to be refactored as a whole later.
- **Extensions adding members to another bundle's struct.** With whole-binding
  params the owner fills its struct whole; each extension has its own.
- **`SetDrawParams` copying the set's current values.** The values live on the
  ResourceQueue and `SetDrawParams` is called holding the OpQueue; reading them
  would add the ResourceQueue to the lock set of every System that versions a
  set, which serialises those Systems against every durable writer. A version
  holds what the frame changed and nothing else.
- **A mutex around the table `SetDrawParams` reads sets from.** Every System
  versioning a set in parallel would contend one lock a call. The program is
  immutable and the state one atomic word, so the table is read with none.
- **Refusing a texture path in a durable set.** Canvas names its sprites by path,
  and the render thread's cache is the one thing that can read the file; a set
  keeping the path costs a string, and a set refusing it moves the load into
  every recorder.
- **Canvas resetting what an earlier batch set, per batch.** A version cannot
  hand a binding back to the set, so a reset would re-supply the material's
  value or the binding's default — and a zero uniform of a reflected size has no
  param to say it in. A set per binding shape needs no reset at all.
- **A reset call on the OpQueue, restoring a set's own value to a version.** It
  would do what canvas needs, but it is a second verb on the hot path for a
  problem the one bundle with optional bindings solves by keying its cache.
- **Raising the inline size to fit `CanvasUniforms`.** Every param would grow
  by the difference, in every arena that copies params, for one struct's sake;
  and the next record past it would allocate again.
- **Compiling canvas's shaders at load, outside the flush.** Canvas's materials
  are values an app builds anywhere and names at a draw; there is no load step
  that sees them before the flush does.

---

## Required work

On the `draw-params` branch in each repo, in this order. Every step builds for
desktop and wasm and passes its tests before the next; nothing is merged or
pushed until all four branches are ready.

**gfx**
- [ ] The reflection port, `Backend.ReflectShader`, beside `ReserveShader` and
      `CreateShader`; gogpu fills all three from its naga reflection.
- [ ] `CompileShaderCmd` and `ShaderProgram`; `ResourceQueue.NewShader`,
      `UploadProgram` and `ReleaseShader`, with their resource ops.
- [ ] `DrawParams`, `NewDrawParams`, `UpdateDrawParams`, `ReleaseDrawParams`:
      the pointer-free binding table, byte arena, baked inline resources.
- [ ] `ParameterDescr` reduced to four kinds, the typed uniform constructors
      producing bytes.
- [ ] `OpQueue.SetDrawParams`: versions, copy-on-write per binding, in-place
      patch of an uncaptured version.
- [ ] `Draw(pass, mesh, set, instances, firstInstance)` - built as `DrawSet`
      beside the old `Draw` so every step builds, and renamed by the deletion.
- [ ] The translator over versions: pipeline keying, uniform versions uploaded
      once, resource records bound; the name-keyed plans deleted.
- [ ] Unsupplied bindings as tabled; the liveness check, A/B-measured.
- [ ] Draw params views in the snapshot and MCP; `mcp.md` and the README
      amended.
- [ ] `MaterialState` renamed `DrawState`.

**model** — shaders and sets created at load; `ScenePbrMaterial` packed whole.

**scene** — sets cached per material key; per-Batch bindings through
`SetDrawParams`; `FrameMaterial` gone.

**canvas and ui** — `canvas.Material`, cached shaders and sets;
`CanvasUniforms` filled whole; halo and `keyColor` as own bindings.

**feuds-26, nox, cog-examples** — lens, fade and sight shaders with their own
uniform structs; call sites moved.

**deletion** — `MaterialDescr`, `Material`, `MaterialWithState`,
`FrameMaterial`, `FrameRecording`, `Clone`, `CloneTo`, `Fingerprint`,
`ParameterShapeState`, `ContinueParameterShape` and their friend accessors.

The work is tracked as sub-issues #596-#604 of #595, with blocking edges in
this order.

**Testing.** Tests assert external behaviour at the highest seam:

- gfx's own seam is **queue calls in, the backend's op stream out**: tests call
  the ResourceQueue and OpQueue methods, translate against the recording fake
  Backend, and assert what each draw binds, the uniform bytes and offsets it
  binds, which draws are dropped, and what was reported. Reflection goes
  through a fake port returning a hand-written layout; one gogpu test runs real
  WGSL through the real port. Prior art: the plugin, pass, materialstate and
  storagebinding tests.
- Errors are asserted through a test engine that captures reports, as the
  kernel's report-once tests do.
- The bundles keep their existing seams — canvas's flush tests, model's
  ingredient tests, scene's batch and recording tests — and change only in what
  they build and assert.
- Benchmarks: `TranslateSteadyState`, `OpQueueDrawSteadyState` and scene's
  `BenchmarkFrameMaterial5000`, interleaved A/B over pre-built binaries against
  the commit before the branch. Every hot path stays at zero allocations.
- End to end: feuds-26 desktop driven over MCP, draw counts and screenshots
  compared before and after; wasm builds of feuds-26, nox and cog-examples.

**Gap:** `CompileShaderCmd`'s cost — a naga parse on the caller's goroutine — is
unmeasured. It runs at load, outside any gfx lock, so it costs no parallelism,
but a loader compiling many variants in one tick should know the figure.

---

## Out of scope

- Scene's proper materials — the next refactor.
- Canvas drawing through scene rather than gfx — the one after.
- Hot reload of any kind, and `ReplaceShader`.
- Bind groups by rate of change.
- A retainable error reporter, and any refactor of error reporting.
- Generation checks on PassRef.
- The temporary pools never releasing their buffers and textures.
