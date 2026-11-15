# Render

Render is the immediate-mode rendering API between gfx and the plugins that draw: it loads and shares shaders, textures, buffers and meshes, and draws meshes with materials into passes. Its users never reach gfx. It knows nothing of cameras, lights, culling or the ECS: those belong to the layers above it. This glossary records its domain language.

## Language

**Material**:
A shader with its render state and the values bound to its parameters. Each user of render writes its own; render ships none. A draw uses the values its material holds when the draw is recorded, so a material may change between its draws; values that change between most draws are draw parameters.
_Avoid_: Draw params, set, effect

**Material version**:
A material's values as a change after its draw in the frame leaves them: the draws recorded after the change bind them, while those before keep the old. It lasts until the frame ends.
_Avoid_: Material instance, snapshot, material copy

**Render state**:
How a material's draws blend and mask each target, test, write and bias depth, test stencil, and cull faces.
_Avoid_: Draw state, pipeline state

**Variant**:
A shader loaded with one set of defines. Each variant is its own loaded asset, and a material names one.
_Avoid_: Keyword, permutation, specialization

**Entry point**:
A function of a shader that one stage runs: a material names its vertex and fragment ones and a dispatch its compute one, an empty name meaning the shader's only one of that stage.
_Avoid_: Main, kernel, stage function

**Parameter**:
A value bound by name to one of a shader's bindings: a texture, a sampler, a buffer or a block of constants.
_Avoid_: Uniform, property, param, binding

**Texture view**:
Part of a texture, or a texture seen another way: a range of mip levels or layers, another view dimension, or one aspect of a depth-stencil texture, bound wherever a texture is.
_Avoid_: View (mux's), subresource, slice

**Pass**:
A run of draws into the same targets, with a load or clear for each target. Its viewport and scissor rectangle start as the whole target and, with its stencil reference and blend constant, may change between draws.
_Avoid_: Camera, layer, render pass

**Compute pass**:
A run of dispatches, recorded among passes in the order they were recorded. Its parameters are set by name in any group of the shader its dispatches run, and reset when the shader changes.
_Avoid_: Compute list, compute stage

**Frame**:
What one beat draws: the command lists recorded for it and the temporaries it holds, from render's frame event until it is presented. Frames are recorded one at a time.
_Avoid_: Render frame, frame context

**Command list**:
One task's run of passes, compute passes and copies for a frame, kept in the order they were recorded. Command lists fill concurrently, each from one task, and the frame runs them by their order.
_Avoid_: Encoder, recording, sequence, queue

**Order**:
The number that places a command list among the frame's others, in one space every user of render shares. Two command lists of a frame never share an order.
_Avoid_: Priority, layer, submit order

**Pass parameter**:
A parameter bound once for a whole pass, which every material drawn in it reads, such as a camera's matrices.
_Avoid_: Global, view uniform

**Draw parameter**:
A parameter set between draws, which the draws after it read until it is set again, the material changes or the pass ends.
_Avoid_: Property block, per-draw uniform, instance property

**Dispatch**:
A run of a compute shader's function with its parameters given by name, over a count of workgroups. It has no material.
_Avoid_: Compute call, kernel launch, compute material

**Copy**:
A command list's move of bytes from a buffer range or texture region into another, or its clear of a buffer range to zeros, recorded among its passes in the order recorded.
_Avoid_: Blit, transfer

**Draw**:
One mesh drawn whole with the current material and parameters, as many instances as it asks for; or, with no mesh, a count of vertices in a topology, which its shader places from their indices alone.
_Avoid_: Draw call, render item

**Mesh**:
Vertex buffers with their layout, an optional index buffer and a topology, in whatever layout the material's shader accepts. A draw covers all of it; a part that shares its buffers with others is a mesh of its own.
_Avoid_: Geometry, model, primitive, submesh

**Instance buffer**:
A vertex buffer read once per instance, with its layout, set between draws beside the mesh, which the draws after it read until it is set again, the material changes or the pass ends.
_Avoid_: Instance stream, per-instance mesh, instance attributes

**Temporary buffer**:
A range of the temporary arena holding data for one frame, taken from a command list, which the frame's draws and copies read until it ends.
_Avoid_: Dynamic buffer, transient buffer, scratch buffer

**Temporary arena**:
The chunks that temporary buffers, the uniform blocks of pass and draw parameters, and those of material versions are ranges of. Each command list takes chunks of its own and hands them to the GPU when it ends; they go back to the arena when the frame ends.
_Avoid_: Ring buffer, staging buffer, uniform ring

**Temporary mesh**:
A mesh taken from render's pool for one frame, over buffer ranges made or written that frame, which goes back to the pool when that frame ends.
_Avoid_: Dynamic mesh, transient mesh, immediate geometry

**Temporary texture**:
A texture taken from render's pool for one frame, which goes back to the pool when that frame ends. A texture kept across frames, such as a history, is an ordinary texture.
_Avoid_: Dynamic texture, transient texture, render target

**Asset**:
Anything render holds behind a handle: a shader, a texture, a buffer, a mesh or a material. A handle lives only while the program runs; what a user saves is what it loaded the asset from.
_Avoid_: Resource, object

**Loaded asset**:
A shader, texture or buffer read from a path in platform's FileSystem, ready in the frame that loads it. Every load of the same asset key shares it, and it is freed once the last of them is released.
_Avoid_: File asset, imported asset

**Made asset**:
An asset its maker builds from memory or from other assets, such as a mesh, a material or a buffer filled from a slice. It has no asset key, owns nothing it names, and its maker releases it once.
_Avoid_: Dynamic asset, runtime asset, procedural asset

**Write**:
Filling a region of a buffer or texture from memory, which lands before every command list of the next submit, so it fills the asset for the whole frame; two writes to one region in a frame leave the last.
_Avoid_: Upload, update

**Asset key**:
What identifies a loaded asset: its path, and the load options that make one file several assets, such as a shader's defines, a texture's colour space and mipmaps, or a buffer's usage.
_Avoid_: Descriptor, asset ID, path

**Reload**:
Building a loaded asset again from its changed file, behind the handles that already name it, so nothing holding them notices but the new look.
_Avoid_: Hot swap, refresh, reimport

**Inventory**:
Every asset render holds, as a task reaches them to acquire, make, change and release them.
_Avoid_: Library, asset table, asset store, assets

**Capture**:
A window's picture as one frame drew it, asked for at any time and delivered after that frame is presented.
_Avoid_: Screenshot, grab

**Inspection**:
One frame written down in render's terms, asked for ahead of it: its command lists, passes and draws with what each bound, what the frame made and loaded, and its reports.
_Avoid_: Snapshot, frame dump, trace

**Pass timing**:
How long one pass took on the GPU, from timestamps at its begin and end, delivered a few frames after it ran.
_Avoid_: Profile, GPU time, query

**Report**:
A mistake render finds at run time, logged once and then carried past, such as a refused shader, a duplicate order or a draw skipped for a missing binding.
_Avoid_: Warning, error, diagnostic

**Binding declaration**:
A shader's bindings written beside it, generated from the shader itself, for tests whose fake GPU reads no shader.
_Avoid_: Reflection file, shader manifest, metadata

**Readback**:
A copy of part of a buffer or texture, taken at a point in a command list and delivered to the CPU after the frame is submitted.
_Avoid_: Download, map, fetch

**Staging pool**:
The buffers readbacks are copied into, each held by one readback until the jobs its delivery's handlers joined have run, and freed after going unused for as many frames as render waits.
_Avoid_: Readback buffer, download pool, temporary arena
