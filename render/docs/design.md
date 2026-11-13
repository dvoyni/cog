# render: design

The design of `github.com/dvoyni/cog/render`, the immediate-mode rendering
API between gfx and the plugins that draw. It was mapped in
[render: an immediate-mode rendering API over gfx](https://github.com/dvoyni/cog/issues/800);
each section links the ticket that holds its detail. The vocabulary is in
[`CONTEXT.md`](../CONTEXT.md).

Status: designed, not yet built.

## Problem Statement

gfx is a thin mirror of wgpu. It hands out handles to buffers, textures,
shader modules, bind groups and pipelines, and an encoder per submit order, and
it leaves everything above that to its users: loading files into GPU objects,
sharing them, binding a shader's values by name, caching pipelines, per-frame
memory for uniforms and transient targets, ordering several plugins' passes,
and a profiler. Every plugin that draws (drawbridge's render paths, 2D's
sprites and text, a debug renderer, an immediate-mode UI) would build the
same layer on gfx, each with its own bugs, and each would reach into gfx's
locks and handles from ECS code that should only hold opaque values.

v1 had that layer (its draw params, translator and Library cache), but spread
over the gfx slot itself, with draw params resolved by name across every
group, a bind group per offset and a cache that never evicted.

## Solution

render is to gfx what Unity's core graphics layer (Mesh, Texture, Shader,
Material, RenderTexture, CommandBuffer) is to the GPU. A plugin that draws
uses render alone and never reaches gfx:

- it **acquires** shaders, textures and buffers from paths in platform's
  FileSystem, ready in the frame that asks, shared by asset key and counted;
- it **makes** meshes, materials, and buffers and textures from memory;
- it holds them through **opaque, pointer-free handles**, which an ECS
  component can hold;
- it records each **frame** as **command lists**, one per task, filled
  concurrently and placed by an **order**, each a run of **passes**, **compute
  passes** and copies;
- in a pass it sets a **material** and its **parameters** and **draws** a
  mesh, instanced or not, or a count of vertices with no mesh;
- render binds every value by its WGSL name, builds and caches layouts,
  pipelines and bind groups, and puts per-draw values in a per-frame arena
  bound at dynamic offsets;
- capture, readback, an **inspection** of a frame, **pass timings**, device
  recovery and **reloads** of changed files come with it.

render does no culling, sorting, batching, lighting or shadows, and ships no
materials. Those belong to drawbridge, a plugin above render with a map of
its own; 2D is another plugin on render.

## User Stories

The actors are a **plugin author** writing a plugin that draws with render
(drawbridge, 2D, a debug renderer), a **game developer** composing an
application, an **agent** looking into a running game through a tool plugin,
and a **test author** testing a plugin that draws.

### Loading and sharing

1. As a plugin author, I want to acquire a texture from a path in the game's files, so that I don't decode images or create GPU textures myself.
2. As a plugin author, I want an acquired texture's handle to be usable in the same frame, so that I never handle a loading state.
3. As a plugin author, I want to choose a texture's colour space and whether it gets mipmaps when I acquire it, so that colour textures and data textures load correctly.
4. As a plugin author, I want a second acquire of the same path and options to share the first texture, so that two plugins loading one file use one GPU object.
5. As a plugin author, I want each acquire counted and each release uncounted, so that a shared asset is freed only when its last holder lets go.
6. As a plugin author, I want an asset freed at the end of the frame its count reaches zero, so that releasing it mid-frame never breaks a draw already recorded.
7. As a plugin author, I want to acquire a shader with defines as a variant, so that one WGSL file serves several feature sets.
8. As a plugin author, I want two acquires listing the same defines in another order to share one variant, so that define order doesn't duplicate compiles.
9. As a plugin author, I want to acquire a buffer from a file, so that baked vertex or index data loads without a decoder of mine.
10. As a plugin author, I want a texture that fails to load to draw magenta, so that a missing asset is loud on screen without crashing.
11. As a plugin author, I want a draw using a failed buffer or shader to be skipped, so that one broken asset doesn't take the frame down.
12. As a plugin author, I want each failure reported once, so that the log isn't flooded every frame.
13. As a plugin author, I want to load bytes that aren't files, such as a glTF's embedded image, by putting them in a memory mount, so that everything loads through one path-based call.
14. As a plugin author, I want handles that are plain comparable values, so that an ECS component can hold one.

### Making

15. As a plugin author, I want to make a mesh from vertex buffers with their layout, an optional index buffer and a topology, in any vertex layout my shader accepts, so that render imposes no vertex format.
16. As a plugin author, I want a mesh and a material to own nothing they name, so that I control the lifetime of the textures and buffers I acquired.
17. As a plugin author, I want to make buffers and textures filled from memory, so that generated data draws like loaded data.
18. As a plugin author, I want to write into a buffer or texture for the whole frame, so that per-frame data reaches the GPU in one call.

### Materials and parameters

19. As a plugin author, I want a material to be a shader variant with its render state and the values bound to its parameters, so that what a draw looks like is one value.
20. As a plugin author, I want to set a material's textures, samplers, buffers and uniform blocks by their WGSL names, so that I never count binding numbers.
21. As a plugin author, I want a uniform block given as a Go struct checked once against its WGSL struct's size and member offsets, so that a layout mismatch is reported, not drawn as garbage.
22. As a plugin author, I want each parameter's level (pass, material, draw) to follow from its group, so that render rebinds only what changed.
23. As a plugin author, I want pass parameters bound once per pass and read by every material in it, so that a camera's matrices are set once.
24. As a plugin author, I want draw parameters set between draws, lasting until set again, until the material changes or until the pass ends, so that per-draw values don't leak into unrelated draws.
25. As a plugin author, I want a draw to use its material's values as they are when the draw is recorded, so that I can change a material between two of its draws.
26. As a plugin author, I want it documented that a material changed by another task while mine records may not show in my draws, so that I know when a change is guaranteed to show.
27. As a plugin author, I want an unbound binding to draw a fixed default (white, depth 1.0, zeros, linear clamp), so that an optional texture needs no placeholder of mine.
28. As a plugin author, I want a name the shader lacks to be ignored, so that one set of values serves every variant.
29. As a plugin author, I want render state given as one comparable struct whose zero value is WebGPU's defaults, so that a material states only what differs.
30. As a plugin author, I want pipelines built and shared by render from the variant, state, vertex layout, targets and topology, so that I never manage pipelines.
31. As a plugin author, I want the cost of a first draw of a new combination, which builds its pipeline, documented, so that I can warm materials behind a loading screen.

### Passes, command lists and draws

32. As a plugin author, I want to begin a pass into up to 8 colour targets and a depth-stencil target, each a window or a texture at one mip level and layer, so that G-buffers, shadow cascades, cube faces and mip chains are all passes.
33. As a plugin author, I want each target to clear or keep, store or discard, and resolve a multisampled target, so that a pass costs no more memory traffic than it needs.
34. As a plugin author, I want a read-only depth target, so that a pass can test depth while sampling it.
35. As a plugin author, I want to change the viewport, scissor rectangle, stencil reference and blend constant between draws, so that UI clipping and split views need no extra passes.
36. As a plugin author, I want to record my frame's passes in a command list of my own at an order I choose, so that several plugins' passes interleave predictably.
37. As a plugin author, I want command lists filled concurrently by different tasks, so that recording uses every worker.
38. As a plugin author, I want a heavy pass split into consecutive passes in consecutive lists, so that one pass's draws can be recorded in parallel.
39. As a plugin author, I want two lists at one order refused and reported, so that an order collision is found, not silently resolved.
40. As a plugin author, I want a pass that clears a target an earlier pass drew into reported, so that a wrong order doesn't silently wipe another plugin's drawing.
41. As a plugin author, I want to draw a mesh with an instance count and a first instance, so that instancing and batch offsets work.
42. As a plugin author, I want per-instance data from a storage buffer read at `instance_index` or from instance buffers set beside the mesh, so that I can pick the cheaper path per shader.
43. As a plugin author, I want to draw a count of vertices in a topology with no mesh, so that full-screen triangles, UI quads and debug lines need no dummy mesh.
44. As a plugin author, I want a shader with per-vertex inputs drawn with no mesh to be refused and reported, so that the mistake is named.
45. As a plugin author, I want to draw into a window by drawing into it, with no implicit blit, so that the last pass's cost is mine to see.
46. As a plugin author, I want a pass into a window with no framebuffer this frame skipped silently, so that a hidden window needs no checks of mine.

### Compute, copies and temporaries

47. As a plugin author, I want compute passes among my passes, with dispatches binding their parameters by name, so that GPU culling or particles sit in the frame where I put them.
48. As a plugin author, I want copies between buffers and textures and buffer clears recorded at a point in a command list, so that data moves where the frame needs it.
49. As a plugin author, I want temporary textures from a pool by descriptor, held until the frame ends, so that transient targets cost no allocation once warm.
50. As a plugin author, I want temporary buffers as ranges of a per-frame arena, so that per-frame data costs no gfx object.
51. As a game developer, I want how long pooled things stay unused before they're freed, and the arena's chunk size, set in render's config with stated defaults, so that I can tune memory without code.

### The frame on the kernel

52. As a plugin author, I want to record on render's frame event, scheduling my tasks after its ready job and joining them, as gfx's guide says for its own event, so that my recording runs in parallel with every other plugin's.
53. As a plugin author, I want to reach render's calls through render's own task parameters, so that my task signature names neither gfx nor platform.
54. As a plugin author, I want the acquire wait and GPU latency of the previous frames on render's frame event, so that I can adapt quality without reaching gfx.
55. As a game developer, I want to change the surface's format and present mode at run time through render, so that a vsync toggle needs no gfx import.

### Capture, readback, device loss and reloads

56. As a plugin author, I want to capture a window and read the picture when render's event delivers it, so that screenshots work on every backend.
57. As a plugin author, I want to read back a buffer range or texture region recorded at a point in a command list, so that GPU results reach the CPU.
58. As a plugin author, I want loaded assets to come back by themselves after a device loss, and an event telling me to refill what I made, so that a lost device costs a hitch, not a crash.
59. As a game developer, I want to reload every loaded asset whose file changed with one call, so that I can iterate on shaders and textures without restarting.
60. As a game developer, I want a shader to reload when a file it includes changes, so that editing a shared include refreshes every shader using it.
61. As a game developer, I want handles to survive a reload, so that nothing holding them needs updating.
62. As a game developer, I want a failed reload to keep drawing the last good version, so that a typo doesn't blank the scene.

### Inspection and pass timings

63. As an agent, I want to ask for an inspection of the next frame and read its command lists, passes, draws and what they bound, so that I can tell why something is or isn't on screen.
64. As an agent, I want an inspection to show the pipelines and bind groups made that frame, so that I can find first-draw hitches.
65. As a game developer, I want inspections to cost other frames nothing, so that the tool can stay attached.
66. As a plugin author, I want continuous per-pass GPU timings when I turn them on, so that I can see which pass costs what.

### Testing and performance

67. As a test author, I want a harness that runs render on a fake GPU in a real kernel, so that my plugin's frames are tested without a GPU.
68. As a test author, I want each recorded frame read back in render's terms (lists, passes, draws, parameters by name), so that I assert what my plugin drew without reading gfx's calls.
69. As a test author, I want a test to fail on any report it didn't expect, so that silent mistakes surface in tests.
70. As a test author, I want my shaders' bindings declared in a file beside each shader, generated by a tool from the shader itself, so that the fake GPU knows them without me writing them by hand.
71. As a test author, I want pixel tests on wgpu's software adapter, so that shader output is checked for real.
72. As a plugin author, I want render to allocate nothing per frame once warm, so that drawing doesn't feed the garbage collector.

## Implementation Decisions

### The module and the plugin

([render on the kernel: its resources and tasks, and how its frame hangs off gfx's DrawEvent](https://github.com/dvoyni/cog/issues/811))

- **A module of its own,** `render`, importing kernel, metronome, platform and gfx. It imports no ECS. Its tests and one tool also require gogpu's module; nothing else does.
- **Dependencies:** gfx and platform. render is listed after gfx, so its `RenderEvent` handler runs after gfx's `Draw`.
- **`Config`:**
  - `TemporaryIdleFrames`, how many frames something pooled may go unused before render frees it; zero means 10.
  - `TemporaryChunkSize`, the arena's chunk size; zero means 1 MiB.
- **Resources:**
  - a public `Handlers`, with `RecordFrame` on `gfx.DrawEvent`, `EndFrame` on `gfx.RenderEvent`, `ReloadAssets` on `gfx.DeviceRecoveredEvent`, `DeliverCapture` on `gfx.CapturedEvent` and `DeliverReadback` on `gfx.BufferMappedEvent`;
  - two private ones: the asset table and the state of the frames in flight.
- **No `Tasks` resource** (render offers no task to schedule or pin) and **no sockets**.
- **Published events:**
  - `FrameEvent{Beat, Ready, AcquireWait, GPULatency}`;
  - `RecoveredEvent`;
  - `CapturedEvent{Window, Beat, Status}`;
  - `ReadbackEvent{Readback, Status}`;
  - `InspectedEvent{Beat}`;
  - `PassTimingsEvent{Beat}`.
- **Custom kernel parameters, which declare gfx's and platform's locks on the user's behalf.** A resource is never kept inside another, and no user names gfx, so render reaches gfx and platform for its users:

  | Parameter | Locks | Carries |
  |---|---|---|
  | `Inventory` | the asset table, `gfx.Device` and `platform.FS`, all read | every asset call, and `ReloadChanged` |
  | `Frame` | the frame state, the asset table and `gfx.Device`, all read | `BeginCommandList`, `AcquireTemporaryTexture`, capture, readback, inspection and pass timings |
  | `Surface` | `gfx.Device`, write | `Request(SurfaceRequest{Format, PresentMode})` |

  A task that records and changes materials declares both `Inventory` and `Frame`; the kernel merges the duplicate read locks.
- **Locks:** every job takes render's resources for read, render's own jobs included. render's state is synchronized internally, as gfx's `Device` is: a mutex for each table and each material, and frame slots as gfx has. A command list needs no lock, because one task fills it.
- **No write lock of render's own.** `Surface.Request` is the only exclusive call, and it takes gfx's write lock, so it waits for every job holding `Inventory` or `Frame`, which is fine for a rare toggle.
- **The surface request at start** stays in gfx's `Config.Surface`, set by the application where it lists gfx. render mirrors the format and present-mode enums for the run-time request.

### Assets: loaded, made, shared and freed

([render: what render loads, how a second load shares the first, and when an asset is freed](https://github.com/dvoyni/cog/issues/806), with the verbs from [render prototype: the calls a user makes to draw one frame](https://github.com/dvoyni/cog/issues/815))

- **Loaded assets** are shaders, textures and buffers read from a path in platform's FileSystem:
  - `AcquireShader(path, defines…)`;
  - `AcquireTexture(path, TextureOptions{ColorSpace, Mipmaps})`;
  - `AcquireBuffer(path)`.

  Each is given back with its `Release…`. Acquire and release are the Go rules' pool pair: a load hands out the shared asset or a fresh one.
- **Made assets** are made from memory or from other assets: `MakeMesh`, `MakeMaterial`, and buffers and textures filled from a slice. Each is given back with its `Release…`. A made asset has no key, and its maker releases it once. A made texture carries every usage its format allows, as a temporary texture does, so it can be a pass target kept across frames, such as a history or a render-to-texture layer.
- **A load is ready in the frame that asks for it.** The GPU object exists when the call returns, and there is no loading state, no ready event and no callback:
  - **Texture:** `image.DecodeConfig` reads only the header, which with the colour space and mipmaps option fixes the descriptor.
  - **Buffer:** `fs.Stat` gives the size, and the buffer is made with every usage, the indirect one included.
  - **Shader:** read, preprocessed and compiled in the caller's job.

  render's jobs decode texture bodies, write contents and generate mips before gfx submits the frame, so a draw recorded right after its acquire reads the data. CPU-side bytes are dropped once uploaded.
- **Sharing is a count per asset key.** Each kind has a comparable key type and its own map:
  - a texture's key is its path, colour space and mipmaps;
  - a shader's key is its path and its defines, sorted by name and joined into one string;
  - a buffer's key is its path.
- **Freeing:** an asset is freed at the end of the frame in which its count reached zero. An acquire of its key before then revives it.
- **Failure:**
  - a failed texture draws magenta (1×1 for a header failure, its own size for a body failure);
  - a failed buffer or shader skips every draw that uses it.

  Each failure is reported once per key and forgotten when the asset is freed.
- **No cascade:** a material or mesh owns nothing it names. A freed texture it names draws as unbound; a freed buffer or shader skips the draw. Each case is reported once.
- **Handles are transient,** valid only while the program runs. A user saves what it loaded from and acquires again after a restore.
- **Bytes that aren't files** go into a memory mount, a platform adapter (see *Delta to platform*), and load from there by path. There are no blobs.
- **Samplers are given by value** and shared when equal.

### Shaders, variants, layouts and pipelines

([render: how a material binds its parameters, its variants, and the pipelines it shares](https://github.com/dvoyni/cog/issues/808))

- **WGSL only,** through gfx's `PreprocessShader`, which is v1's preprocessor in the backend. A variant is a shader acquired with its defines; WGSL `override` constants are not used.
- **Entry points follow WebGPU's rule** ([render: shader entry points](https://github.com/dvoyni/cog/issues/852)):
  - A material names its vertex and fragment functions; a compute pass names its compute function with `SetShader(shader, entryPoint)`.
  - An empty name means the shader's only function of that stage. A file with one of each names nothing; a file with several, such as a depth-only path beside a colour path or a culling file's kernels, names the one it wants.
  - Entry points are not part of a variant's asset key: one module serves every function in it. They are part of the pipeline key.
  - A missing name, or several functions of a stage with none named, is refused and reported once: at `MakeMaterial` for a material, whose draws are then skipped, and at a dispatch's first pipeline for a compute function, the dispatch then skipped.
- **Named reflection from gfx** (see *Delta to gfx*):
  - each binding's name, group, number and kind;
  - a buffer struct's size and members;
  - the entry points that use each binding.
- **A parameter's level is its group:**

  | Group | Holds |
  |---|---|
  | `@group(0)` | pass parameters |
  | `@group(1)` | material parameters |
  | `@group(2)` | draw parameters |
  | `@group(3)` | reserved: a shader declaring it is refused and reported once |

- **render builds every pipeline layout itself,** never WebGPU's "auto". A group's layout holds every binding the shader declares, used or not. Identical layouts are interned, so shaders sharing an include share one `@group(0)` layout and a pass's parameters are one bind group. Unused groups below the highest get an empty layout.
- **Dynamic offsets, by group and kind** ([render: the dynamic storage-buffer budget](https://github.com/dvoyni/cog/issues/853)):
  - Uniform blocks in `@group(0)` and `@group(2)` bind at a dynamic offset, at most 8 per pipeline, WebGPU's default.
  - Storage buffers bind at a dynamic offset in `@group(2)` only, at most 4 per pipeline, which is also all the fork's Metal backend offers.
  - Storage buffers in `@group(0)` bind statically. A pass's parameters are one bind group per pass, cached by their resources, so a made or loaded buffer there costs nothing per frame, and a forward+ shader's lights, clusters, light indices, per-object data and shadow data fit under the limit of 8 storage buffers per stage.
  - A shader over any of these limits is refused by gfx's limit check and reported once.
- **The pipeline cache:**
  - The key is the variant and its vertex and fragment entry points, render state, the mesh's vertex layout and instance buffers' layouts, the pass's target formats, depth format and sample count, the topology and the strip index format. A compute pipeline's key is its variant and its compute entry point.
  - A pipeline is freed with its shader and never evicted otherwise.
  - A failure stays under its key and is reported once, and its draws are skipped.
- **The first draw of a new key** makes its pipeline and bind groups synchronously in the recording job. render's design and the draw calls document that hitch.
- **Render state is the whole pipeline state by value,** one comparable struct whose zero value is WebGPU's defaults:
  - for each colour target, blend and write mask;
  - depth compare, write and bias;
  - stencil for each face, with masks;
  - cull mode, front face and alpha-to-coverage.

  Fields are phrased so zero means the default, because the fork takes zero masks literally. render ships no presets.

### Materials and parameters

([render: how a material binds its parameters, its variants, and the pipelines it shares](https://github.com/dvoyni/cog/issues/808), changed by [render on the kernel](https://github.com/dvoyni/cog/issues/811))

- **A material is made from a descriptor,** `MakeMaterial(MaterialDescriptor{Label, Shader, State, VertexEntryPoint, FragmentEntryPoint})`, as a mesh is.
  - A shader with no `@fragment` makes a material with no fragment stage. It draws only in passes without colour targets, such as shadow maps and depth prepasses. Drawn into a pass with colour targets, it is refused and reported once, and its draws are skipped.
  - A material with a fragment function drawn into a pass with no colour targets keeps its fragment function with no targets, so alpha-tested shadows still discard.
- **A parameter is a whole binding:** a texture, a sampler, a buffer, or a uniform block given as a Go struct. The struct is checked once for each Go type and binding: its size, and each member's offset in order, ignoring names.
- **Setters, one for each kind, by name:**
  - a material's: `SetMaterialTexture`, `SetMaterialSampler`, `SetMaterialBuffer`, and `SetMaterialUniform[T]` as a generic method;
  - pass parameters: `SetPassTexture`, `SetPassSampler`, `SetPassBuffer` and `SetPassUniform[T]`, set right after a pass begins;
  - draw parameters: `SetTexture`, `SetSampler`, `SetBuffer` and `SetUniform[T]`.
- **Texture views** ([render: texture views as parameter values](https://github.com/dvoyni/cog/issues/854)):
  - A texture view is a value, `TextureView{Texture, BaseMipLevel, MipLevelCount, BaseLayer, LayerCount, Dimension, Aspect}`: zero counts mean the rest, and a zero dimension or aspect means the binding's. It is bound with `SetMaterialTextureView`, `SetPassTextureView`, `SetTextureView` for draw parameters and the compute pass's `SetTextureView`, each beside its `…Texture` neighbour; a texture handle alone is the whole texture.
  - A view names a range of mips and layers, a view dimension (a cube seen as a 2D array, one layer of an array seen as 2D) and an aspect. An aspect left zero comes from the binding: a depth binding over a depth-stencil texture gets the depth aspect.
  - A storage-texture binding given a whole texture binds its first mip level; given a view, the view names exactly one.
  - Reading a texture's bytes as another format, such as sRGB as linear, is designed for later: WebGPU needs it listed when the texture is made.
  - render interns gfx views by texture and view, made at first bind and freed with their texture. A pooled temporary keeps its handle across frames, so views over it are reused, allocation-free once warm.
  - A view that doesn't fit its binding (dimension, sample type, aspect, more than one mip for a storage binding) or names mips or layers its texture lacks is refused, reported once per shader and name, and the binding draws its default.
  - A sampled texture or view overlapping a target of the pass it is bound in is caught when bound, reported once, and the binding draws its default, so the list's encoder survives the mistake a bloom chain without views would make.
- **A draw uses the values its material holds when the draw is recorded.**
  - A material changed after it was drawn in the frame gets a new version: its uniform blocks copied into the arena and a new bind group. Each version allocates a fixed count, which the per-frame gate checks.
  - Values that change between most draws belong in draw parameters, which bind at dynamic offsets and make no bind group.
  - A change made by one task while another records draws of the material may not show in those draws. It shows for certain only in draws recorded by the task that made it, or after a task the kernel orders before the frame's recording.
  - The setters' doc comments and this design state the rule.
- **A draw parameter lasts** until it is set again, until the material changes, or until the pass ends. An instance buffer set with `SetInstanceBuffer` lasts the same way.
- **Unbound defaults:**

  | Binding | Draws with |
  |---|---|
  | float texture | 1×1 white, in every view dimension |
  | depth texture | 1×1 at depth 1.0 |
  | integer texture | zeros |
  | sampler | linear, clamp to edge |
  | comparison sampler | less-equal |
  | uniform block | zeros of its size |
  | storage buffer or texture | a shared placeholder when no running entry point reads it; otherwise the draw is skipped, reported once |

- **Mismatches:**
  - A name the shader lacks is ignored silently.
  - A value that can't fill its binding (the wrong kind, a struct of the wrong layout, the wrong view dimension or sample type, a storage buffer that's too small) is refused, reported once per shader and name, and the binding draws its default.
  - A material's values are checked when set; pass and draw parameters are checked at the first draw under each shader, and the result is cached.
  - Nothing panics.
- **Bind groups:**
  - A bind group naming an asset is dropped at the end of the frame the asset is freed.
  - Draw-parameter bind groups are dropped after `TemporaryIdleFrames` unused.
  - render never keeps a handle it has released.

### Per-draw and per-instance data

([render: per-draw and per-instance data without push constants](https://github.com/dvoyni/cog/issues/810))

- **Uniform blocks of pass and draw parameters, and storage buffers given as temporaries,** live in the temporary-buffer arena.
  - Setting a block copies its bytes there, and a draw rebinds the same bind group at a new offset.
  - A temporary range given as a `@group(0)` storage parameter binds statically, so it costs a new bind group for its pass every frame, which allocates a fixed count the per-frame gate checks. Data a whole pass reads goes in a made buffer written with `WriteBuffer` each frame, which allocates nothing; data that varies per draw goes in a draw parameter.
  - Each block takes at least 256 bytes. A user drawing thousands of small values puts them in one storage array read at `instance_index`.
- **Each command list takes arena chunks of its own,** so concurrent lists never contend.
  - A list's chunks go to the GPU in one `WriteBuffer` each when the list ends.
  - A list that outgrows its chunk takes another, and a block larger than a chunk gets a chunk of its own size.
- **Per-instance data comes two ways:**
  - **A storage buffer read at `instance_index`:** a draw's first instance selects a batch's rows, on every backend for direct draws.
  - **Instance buffers:** vertex buffers stepped per instance, each a buffer range with its layout, set with `SetInstanceBuffer(slot, InstanceBuffer{Range, Layout})`. Their layouts join the pipeline key.
- **Room for indirect draws:** every buffer render makes carries the indirect usage. render's first version offers no indirect call.

### Passes, command lists and draws

([render: what a pass declares, temporary buffers and textures, and how passes from several users are ordered](https://github.com/dvoyni/cog/issues/809), [render prototype: the calls a user makes to draw one frame](https://github.com/dvoyni/cog/issues/815))

- **A frame is command lists, not a graph.**
  - A command list is one task's run of passes, compute passes, copies and readbacks, kept in the order it was recorded.
  - `frame.BeginCommandList(label, order)` takes the list's gfx frame encoder at its order, and the list's `End` writes its arena chunks.
  - Two lists at one order are refused and reported once.
  - render's own list sits at the lowest order, `math.MinInt32`, and a user's list there is refused.
  - Called while no frame is recording, `BeginCommandList` answers a list that records into nothing, reported once.
- **Recording is immediate:** each call on a pass goes straight into the list's gfx encoder in the task that records it.
  - Recording waits for gfx's acquire, because a pass into a window needs its framebuffer when it begins.
  - The deferred alternative, which would have kept one set of material values per frame and overlapped the acquire, was dropped for the cost of a second pass over every command.
- **Command lists fill concurrently,** each from one task. A heavy pass is recorded in parallel as split passes: consecutive passes into the same targets in lists at consecutive orders, each after the first keeping its targets. Render bundles are not used.
- **`BeginPass(PassDescriptor)` declares:**
  - up to 8 colour targets, each a window or a texture at one mip level and one layer or cube face, with clear or keep, store or discard, and an optional resolve target;
  - an optional depth-stencil target, which may be read-only.

  The sample count and formats follow from the targets. A pass whose targets disagree, or that has none, is refused and reported once, and its draws are skipped.
- **Pass state:** the viewport and scissor rectangle start as the whole target. They, the stencil reference and the blend constant may change between draws and last until set again or until the pass ends.
- **The window:**
  - It is drawn into, never sampled or copied from; what reaches it is drawn into it, with no implicit blit.
  - A pass into a window with no framebuffer this frame is skipped silently.
  - The framebuffer is `BGRA8Unorm` on gogpu's surfaces, so the last pass into a window encodes sRGB in its shader.
- **Draws:**
  - `SetMaterial(material)` selects the material for the draws after it and resets every draw parameter and instance buffer.
  - `Draw(mesh, DrawArgs{InstanceCount, FirstInstance})` draws the whole mesh. A part of a mesh that shares its buffers with others is a mesh of its own, over ranges of those buffers.
  - `DrawVertices(topology, vertexCount, DrawArgs{…})` draws with no mesh. It binds no per-vertex buffer, and its topology's zero is a triangle list. A shader whose vertex stage declares a per-vertex input is refused and reported once, as the call's doc comment says.
- **Writes land before the whole frame:** a write fills a buffer or texture for the whole frame, and two writes to one region leave the last. `WriteTexture` writes a region of one mip level and one layer, so an atlas page lands in a layer of a 2D-array texture. To change data between passes, a user writes a temporary buffer and copies it at that point in a command list.
- **The clear-after-draw check:** after the users' recording, render reports once each pass that clears a target an earlier pass of the frame drew into, in submit order.

How a plugin's recording task reads, from the prototype with its verdicts applied:

```go
func drawScene(k *kernel.Kernel, e render.FrameEvent, frame render.Frame, scene kernel.Read[Scene]) kernel.NoResult {
	list := frame.BeginCommandList("scene", sceneOrder)
	pass := list.BeginPass(render.PassDescriptor{
		Label:        "scene",
		ColorTargets: []render.ColorTarget{{Texture: scene.Get().Color, Clear: true}},
		DepthStencil: render.DepthStencilTarget{Texture: scene.Get().Depth, Clear: true, ClearDepth: 1},
	})
	pass.SetPassUniform("camera", scene.Get().Camera)
	pass.SetMaterial(scene.Get().Crate)
	pass.SetBuffer("objects", list.AcquireTemporaryBuffer(scene.Get().Rows))
	pass.Draw(scene.Get().Cube, render.DrawArgs{InstanceCount: 500})
	pass.End()
	list.End()
	return kernel.NoResult{}
}
```

### Compute, copies and temporaries

([render: what a pass declares, temporary buffers and textures, and how passes from several users are ordered](https://github.com/dvoyni/cog/issues/809))

- **A compute pass is a run of dispatches** recorded in a command list among its passes. `SetShader(shader, entryPoint)` names the compute shader and its function, an empty name meaning its only one, and resets its parameters, which are set by name in any group, with no levels. `Dispatch(x, y, z)` runs it. There is no compute material.
- **Copies** (buffer to buffer, texture to texture, and between buffers and textures) **and buffer clears** are recorded between passes. gfx's backend inserts the barrier when a later pass samples what an earlier one wrote.
- **Temporary textures:**
  - `frame.AcquireTemporaryTexture(descriptor)` pools them by descriptor, with every usage the format allows.
  - Each is held until the frame ends, with no reuse within the frame, and its contents are undefined when taken.
  - Its user hands it between its tasks through state of its own, acquired in a task the kernel orders before the tasks that draw into or sample it. Temporaries are not named.
- **Temporary buffers:**
  - `list.AcquireTemporaryBuffer[T](data []T)` answers a 256-byte-aligned range of the list's arena chunk holding `data`, valid until the frame ends. It is generic, as `SetUniform[T]` is, so a vertex slice or an index slice goes in as it is.
  - Taking one makes no gfx object.
- **Temporary meshes** ([render: per-frame geometry in a vertex layout of its own](https://github.com/dvoyni/cog/issues/855)):
  - `list.AcquireTemporaryMesh(MeshDescriptor{...})` answers a mesh valid until the frame ends, over buffer ranges: usually temporary buffers of the same list, or ranges of made buffers. It draws with `Draw`, as any mesh does.
  - It is how per-frame geometry in a vertex layout of its own is drawn, such as canvas's triangles or a Dear ImGui-style UI. Each of ImGui's commands is one temporary mesh over sub-ranges of the frame's vertex and index buffers: the index range's offset is its first index, and the vertex range's offset is its base vertex. A draw still covers its whole mesh.
  - Its vertex layout joins the pipeline key, compared by value, as a made mesh's does.
  - Entries are pooled with their own arrays of vertex buffers, copied from the descriptor, so acquiring one allocates nothing once warm. It returns to the pool when the frame ends, and a draw of its handle after that is skipped and reported once.
- **Arena chunks, temporary textures and draw-parameter bind groups** are freed after `TemporaryIdleFrames` unused.

### The frame on the kernel

([render on the kernel: its resources and tasks, and how its frame hangs off gfx's DrawEvent](https://github.com/dvoyni/cog/issues/811))

```
gfx.RenderEvent ─ gfx's Draw: configure, acquire ─ gfx.DrawEvent
  render's RecordFrame:
    decode-and-write jobs for loads pending since the last frame
    reloads swapped in
    publishes render.FrameEvent{Beat, Ready, AcquireWait, GPULatency}
      users' recording tasks, after e.Ready, joined into the out dependency
    after that out dependency:
      decode-and-write jobs for loads requested during recording
      the mips of every texture loaded this frame, at math.MinInt32
      the clear-after-draw check
    returns them all, so gfx's flush waits
  gfx: flush (submit by order), present
render's EndFrame, on gfx.RenderEvent, after the present:
  frees, material versions, temporaries, the arena
```

- **Users subscribe to `FrameEvent`** with no ordering, schedule their recording tasks after `e.Ready` (gfx's acquire), and join them into the out dependency, as gfx's guide says for its own event. `AcquireWait` and `GPULatency` are copied from gfx's event. Users never subscribe to gfx's events.
- **Loads are picked up at two points:**
  1. in `RecordFrame`, for loads pending since the last frame, decoded in parallel with the recording;
  2. after the recording, for loads requested during it.

  Each loaded texture and buffer gets one decode-and-write job, and one job then generates their mips in render's encoder.
- **The frame's end:** nothing a frame used is freed before gfx's flush, because the fork fails a submit whose bind group names a freed buffer.
  - `EndFrame` runs after the present: it frees the assets whose count reached zero, the made assets released and the material versions made that frame; it returns temporaries to their pools, freeing those idle for `TemporaryIdleFrames`; and it resets the arena.
  - It is joined into the beat's out dependency, so the next beat starts after it.

### Device loss, capture and readback

([render on the kernel](https://github.com/dvoyni/cog/issues/811))

render republishes gfx's events as its own, each nested inside gfx's publication, so gfx frees what an event names only after render's handlers' jobs.
- **Recovery:**
  1. On `gfx.DeviceRecoveredEvent`, render reloads every loaded texture and buffer (bodies read and written again, mips regenerated) and refills its default textures.
  2. Then it publishes `RecoveredEvent`, on which makers refill their made buffers and textures.

  Shaders, pipelines and bind groups come back from gfx under their old handles. An asset loaded from memory-mount bytes since removed can't come back, and that is reported.
- **Capture:**
  1. `frame.RequestCapture(window)` can be called at any time, and is taken from the first frame acquired after it.
  2. render publishes `CapturedEvent`.
  3. `frame.ReadCapture(event)` answers the picture while that publication's jobs run.
- **Readback:**
  1. `list.ReadBuffer(range)` and `list.ReadTexture(texture, mipLevel, region)` record a copy at that point in the list, into a staging buffer from render's pool, and answer a `Readback` handle.
  2. render maps the buffer after the submit and publishes `ReadbackEvent`.
  3. `frame.ReadbackData(event)` answers the bytes until that publication's out dependency, after which the buffer goes back to the pool.

### Inspection and pass timings

([render: a frame inspection for agents, and per-pass GPU timings](https://github.com/dvoyni/cog/issues/820))

- **An inspection** is one frame in render's terms:
  - the beat;
  - command lists by order, with labels;
  - each list's passes with their targets and pass parameters, its compute passes with their dispatches, and its copies and readbacks;
  - every draw with its material and version (variant, render state with enums named, each parameter's binding name, kind, group, number and value), its draw parameters, instance buffers and counts;
  - what the frame loaded, wrote, took from the pools and versioned;
  - the pipelines and bind groups made that frame;
  - the reports;
  - the pass timings when they are on.

  A uniform block's value is its bytes.
- **Shared types:** the inspection's types are exported and are the same ones `rendertest`'s reader produces from gfx's recording.
- **Asking and delivering:**
  - `frame.RequestInspection()` inspects the first frame whose `FrameEvent` is published after it, one at a time.
  - While that frame records, render's calls also append to the inspection; every other frame pays one flag check per call and allocates nothing.
  - After the present, render publishes `InspectedEvent{Beat}`, and `frame.ReadInspection(event)` answers it while the publication's jobs run.
- **Pass timings are opt-in and continuous.**
  - While `frame.SetPassTimings(true)` holds, every pass and compute pass gets begin and end timestamps through gfx's `TimestampWrites`, resolved and mapped through render's staging pool, allocation-free once warm.
  - A few frames later, render publishes `PassTimingsEvent{Beat}`, and `frame.ReadPassTimings(event)` answers each pass's order, label and GPU duration.
  - Until the fork exposes query sets, gfx answers `ErrQuerySetsUnavailable`, so turning timings on is reported once and no events come.
- **Not render's:** the agent-facing tool, which summarizes, filters, steps a paused game and writes JSON, is a plugin of its own on these events.

### Reloads

([render: hot reload of shaders and loaded assets](https://github.com/dvoyni/cog/issues/821))

- **`inventory.ReloadChanged()`** checks every loaded asset's file with `fs.Stat` (modification time and size) and reloads those that changed. A game binds it to a dev key, or a dev plugin calls it every second; it allocates only when it runs.
- **A shader's includes count.** render wraps the filesystem it hands `PreprocessShader` so that it notes every file the preprocessor opens, as each variant's include list.
- **Where changes come from:**
  - embedded files report no modification time and never change, so a game in development mounts its asset directory from disk above them;
  - a memory mount stamps a new modification time at each put, so generated assets reload too.
- **Handles survive.** Reloaded objects are built in the caller's job, as an acquire's are, and swapped in at the start of the next frame, so a frame draws all old or all new. The old objects are freed after the last frame that used them.
- **What changes, by kind:**
  - **A shader:** every variant of its path gets a new module. Its pipelines are rebuilt at their next draw, and materials rebind by name under the mismatch rules.
  - **A texture:** with an unchanged descriptor, it is written in place and its mips regenerated. With a new size or format, a new gfx texture sits behind the same handle, and the bind groups naming the old one are rebuilt on their next use.
  - **A buffer:** with an unchanged size, it is written in place. With a new size, it is replaced, and a mesh whose range no longer fits skips its draws and is reported once.
- **Not reloaded:** made assets.
- **A failed reload** keeps drawing the last good version and is reported once; the next change to the file retries.

### Reports

- **A report** is a mistake render finds at run time, logged once through the kernel's `LogErr` and carried past.
- **It carries a kind** that tests name.
- **It goes into the inspection** of the frame that raised it.

### Performance

([render: the per-frame bar](https://github.com/dvoyni/cog/issues/819))

- **Nothing render does per frame allocates once its pools have grown.** This is a rule in render's `AGENTS.md`, enforced by an allocation gate (see *Testing Decisions*).
- **Allowed to allocate:**
  - each material version, a fixed count that the gate checks;
  - each pass given a temporary range as a `@group(0)` storage parameter, a fixed count that the gate checks;
  - outside the frame: loads, first pipelines and bind groups, `MakeMaterial`, and the first temporary of a new descriptor.
- **Gated at zero:**
  - a second acquire of a held key;
  - any release;
  - `MakeMesh` once render's tables have grown.
- **Time per draw** is recorded in a *Performance* section of this design and in `render/docs/benchmarks-baseline.txt`.

### Delta to gfx

- **Named reflection** ([render: how a material binds its parameters](https://github.com/dvoyni/cog/issues/808)):
  - `Device` answers a shader module's bindings: each one's name, group, number and kind (view dimension, sample type, storage access and format), a buffer struct's size and members (name, offset, size), and the entry points that use it, declared bindings included.
  - The backend seam's reflection carries the same.
  - gogpu fills it from naga's IR, which it drops today.
  - The recording adapter answers it from queued answers, as it answers gfx's other reflection calls.
- **The recording adapter gains a mode that records nothing,** answering and checking as before, for benchmarks and allocation gates ([render: the per-frame bar](https://github.com/dvoyni/cog/issues/819)).
- **What gfx left to scene is render's:**
  - sharing pipelines of one descriptor;
  - the uniform ring and the pool of transient targets;
  - the per-pass GPU profiler;
  - materials.

  The render path and post effects are drawbridge's, and there is no frame graph.

### Delta to platform

([render: what render loads](https://github.com/dvoyni/cog/issues/806), [render: hot reload](https://github.com/dvoyni/cog/issues/821))

- **The memory mount,** `filesystems.NewMemoryFS()`, is an adapter its owner binds as a read mount with a priority and keeps as a resource of its own.
- **The owner puts files in and removes them** through the adapter, under the adapter's own lock. Every plugin reads them through platform's `FS`.
- **Its `Stat` answers** each file's size and a modification time stamped when the owner put it, strictly later at every put, even within one clock tick.
- **Platform's overlay and its `FS` API stay as they are.**

## Testing Decisions

([render: how render and the plugins that draw with it are tested](https://github.com/dvoyni/cog/issues/818), [render: the per-frame bar](https://github.com/dvoyni/cog/issues/819))

**A good test asserts what reached the GPU, in render's words.** It drives render the way a plugin does, through the kernel and render's parameters. It never calls render's internals to stand in for a lifecycle step, as the kernel's test rules say.

**`render/rendertest`**, exported, serves render's own tests and its users' alike:
- **A harness.** Its test plugin binds platform's headless windows and gfx's recording adapter, the device wrapped to answer shader bindings from binding declarations. It paces metronome on a `ManualClock`, through the `Supervisor`. The test lists its own plugin after render and steps one beat at a time.
- **A reader.** It turns each frame gfx recorded into the inspection's types:
  - command lists by order;
  - passes with label and targets as render's textures and windows;
  - draws with material and counts;
  - each parameter by binding name with its value, the dynamic offsets resolved from the arena bytes written.
- **Strict reports.** A test fails on any report it didn't name with `ExpectReport(kind)`.

**Binding declarations.** The recording adapter parses no WGSL, and gfx gains no WGSL parsing.
- **Where:** each test shader has a JSON file beside it, `<shader>.wgsl.json`.
- **What:** each binding's group, number, name and kind (a texture with its sample type and view dimension, a sampler, a uniform block with its size and member offsets, a storage buffer) and the entry points that use it, and a vertex entry point's inputs. A variant whose defines change its bindings has a section of its own.
- **Missing:** a shader compiled with no declaration fails the test, naming its path.
- **Generated by `render/cmd/declarebindings`,** a small kernel app:
  - **Its plugins:** platform with a disk read mount, metronome, gfx, gogpu on the software adapter, render, and its own plugin.
  - **What it does:** it acquires each shader named on the command line, with its defines, through render's real path, and writes what render reflected.
  - **How it runs:** from `//go:generate` beside the test shaders. It needs no window and no GPU.
- **Stale:** the pixel tests compare each declaration with the real reflection and fail on a difference.

**Pixel tests** are part of render's own tests, on wgpu's software adapter:
- **Setup:** the test lists gogpu's plugin with `ForceFallbackAdapter` in place of the recording adapter, under the same harness.
- **Method:** each test draws into textures and reads them back through render's readback, so it needs no window. It is a small fixed scene with exact expected texels, skipped under `-short`.
- **What they cover:**
  - the mip shader;
  - the unbound defaults;
  - the magenta of a failed load;
  - sRGB encoding in the last pass;
  - a Go struct matching its WGSL block on a real driver;
  - readback contents.

**What render's own tests cover, through the recording adapter:**
- **Loads:** keys, counts, frees at the frame's end, and failure behaviour.
- **Binding:** by name, an ignored unknown name, a refused mismatch, the unbound defaults, and the dynamic offsets with their arena bytes.
- **Material versions:** a change between two draws shows only in the second.
- **Command lists and passes:** order, a duplicate order refused, render's own order first, and `DrawVertices`' topology with no vertex buffers.
- **Pipelines:** keyed, shared, and freed with their shader.
- **The frame:** the clear-after-draw check, temporaries pooled and freed after `TemporaryIdleFrames`, readback, capture and inspection events, the recovery reload with `RecoveredEvent`, and reloads.
- **Concurrency, under `go test -race`,** each test checked by breaking the code it guards:
  - parallel acquires of one key;
  - a material changed while two lists record it;
  - lists filled concurrently.

**The per-frame gate and benchmarks**, the method of ecs's and mux's bars:
- **The gate:** zero allocations per frame once warm, checked with `testing.AllocsPerRun(...) == 0` over a whole beat in plain `go test`, on the recording adapter in its mode that records nothing. It covers render, gfx, the kernel and metronome, so gfx's "allocation-free once warm" is enforced too.
- **Operations:**
  - list begin, and end with its chunk write;
  - pass begin and end, with pass parameters;
  - `SetMaterial`, the draw-parameter setters and `SetInstanceBuffer`;
  - `Draw` and `DrawVertices`;
  - both kinds of temporaries;
  - the clear-after-draw check;
  - the end-of-frame job.
- **Counts:**
  - 16, 256 and 4096 draws per list;
  - 1, 4 and 16 lists per frame;
  - a material switched every draw, every 16 draws, or once per list;
  - 1 and 8 passes per list.
- **Draw shapes:** an indexed mesh with one uniform draw parameter; `DrawVertices` reading a storage buffer at `instance_index`; a texture draw parameter.
- **Time per draw** is benchmarked, not gated, against the same frame written by hand on gfx. Ten runs go in the baseline, compared with `benchstat`.

**Prior art:**
- gfx's tests in a real kernel with the recording adapter;
- gogpu's software-adapter tests;
- ecs's and mux's allocation gates and baselines.

## Out of Scope

- **drawbridge's:**
  - the render path, cameras, lights, shadows, culling, sorting, batching, post effects, FXAA and TAA, its materials and debug views;
  - [drawbridge: forward+, deferred, or both](https://github.com/dvoyni/cog/issues/807).
- **scene's:** the hierarchy, animation, skinning and morph targets.
- **2D's:** sprites, text, fonts, atlases and logical viewport modes.
- **Loading formats and content:** glTF loading, mesh files and their compression, unit shapes, particles, terrain, water and decals.
- **Designed for, built later:** KTX2 with BC compression, HDR images, 3D textures, indirect draws, occlusion queries, and recording that starts before gfx's acquire.
- **Absent from wgpu:** ray tracing, mesh shaders, bindless and push constants.
- **Deferred by gfx:** GLES, the web and HDR presentation.
- **Not render's:** the agent-facing tool plugin.

## Further Notes

- **Research behind the design**, on its research branches:
  - [v1's renderers](https://github.com/dvoyni/cog/blob/research/draw-v1/draw/docs/research/v1-renderers.md);
  - [asset lifetime](https://github.com/dvoyni/cog/blob/research/draw-asset-lifetime/draw/docs/research/asset-lifetime.md);
  - [render paths](https://github.com/dvoyni/cog/blob/research/draw-render-paths/draw/docs/research/render-paths.md);
  - [frame structure](https://github.com/dvoyni/cog/blob/research/draw-frame-structure/draw/docs/research/frame-structure.md).
- **The call shapes** were judged on a [prototype](https://github.com/dvoyni/cog/tree/prototype/render-frame/render/prototype-frame), whose README holds the verdict.
- **Names avoid other contexts' terms:**
  - ecs owns *Shadow* and *Transient*;
  - mux owns *View*, *Depth*, *Sample*, *Weight* and *Model*;
  - gfx owns *draw event*, *Frame* (its handle) and *SubmitOrder*.

  render's glossary uses qualified terms instead: shadow map, depth buffer, sample count.
