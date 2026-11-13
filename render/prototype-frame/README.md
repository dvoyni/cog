# PROTOTYPE: the calls a user makes to draw one frame

Throwaway, for [render prototype: the calls a user makes to draw one frame](https://github.com/dvoyni/cog/issues/815). It is not render. Remove it once the ticket's verdict is recorded and the prototype is kept on its own branch.

```
cd render/prototype-frame
GOWORK=off go run .      # variant A: a setter per parameter kind
GOWORK=off go run . b    # variant B: parameters as values
```

- `render/` holds stubs of render's API. Each body only records what render would do.
- `main.go` is a game's plugin drawing two frames:
  - a loading task;
  - a task that acquires the frame's temporary textures;
  - four drawing tasks filling command lists concurrently: a compute cull, the scene, the present into the window, and a 2D interface over it.
- `variantb.go` is the scene task again, in variant B.
- The run prints each frame as render would submit it: command lists by order, passes, the parameters each draw reads, and each material's values at the submit.

The kernel is left out. Where a task would reach a render resource through `kernel.Read` or `kernel.Write`, it gets a pointer. The resources' shape is for [render on the kernel](https://github.com/dvoyni/cog/issues/811).

## Verdict

The calls read well. The stubs are kept as they were judged; where the verdict changes a shape, the change is listed here, not made in the code.

1. **Parameters** are set with a setter per kind, as in variant A: `SetTexture`, `SetBuffer`, and `SetUniform[T]` as a generic method. Variant B, parameters as values in a list, is dropped.
2. **Asset verbs** follow the Go rules' pairs. Loaded assets are `AcquireShader`, `AcquireTexture` and `AcquireBuffer`, given back with `Release…`. Made assets are `MakeMesh` and `MakeMaterial`, given back with `Release…`. Temporaries are `AcquireTemporaryTexture` and `AcquireTemporaryBuffer`, given back when the frame ends.
3. **Pass parameters** are set with setters right after `BeginPass`, not in the `PassDescriptor`.
4. **A draw without a mesh** is `pass.DrawVertices(topology, vertexCount, DrawArgs{…})`, not a zero mesh. Its topology's zero is a triangle list. It binds no per-vertex buffers; the shader places vertices from `vertex_index` and `instance_index`, and reads the rest from storage buffers and instance buffers. A shader whose vertex stage declares a per-vertex input is refused and reported once, as `DrawVertices`' doc comment says.
5. **Instance buffers** are set with `pass.SetInstanceBuffer(slot, buffer)`, which lasts as a draw parameter does, not given in `DrawArgs`. `DrawArgs` is `{InstanceCount, FirstInstance}`.
6. **A temporary texture shared between tasks** is held by its user, acquired in a task the kernel orders before the drawing tasks. Temporaries are not named.
7. **A pass that clears a target an earlier pass of the frame drew into** is reported once at the submit. The first run of this prototype placed the interface below the present, and the present's clear wiped it.
8. **A draw covers its whole mesh.** A part that shares its buffers with others is a mesh of its own, over ranges of those buffers.
9. **How render sits on the kernel**, the `Assets` resource and the `Frame`, is left to [render on the kernel](https://github.com/dvoyni/cog/issues/811).
