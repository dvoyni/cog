# declarebindings plugin

`cmd/declarebindings`' own plugin, listed after render. On `DeclareEvent` it
acquires each shader its `Config` names through render's `Inventory`, with no
defines, unless `Config.VariantsOnly` is set for a shader that compiles only
with defines, and with each of `Config.DefineSets`, so render reports a shader
it fails to compile or refuses. It then compiles the same variants through gfx's
`Device` and hands each shader's binding declaration, what gfx reflects of its
variants, to `Config.TakeDeclaration`. The command writes the declarations
once the kernel has quit with no error logged.

```go
configs := map[kernel.PluginName]any{
	plugin.PluginName: &plugin.Config{
		ShaderPaths:     []string{"lit.wgsl"},
		DefineSets:      [][]render.ShaderDefine{{{Name: "SKINNED"}}},
		TakeDeclaration: func(shaderPath string, declaration []byte) { declarations[shaderPath] = declaration },
	},
}
```

## Kernel surface

- **Resources it installs:**
  - `Handlers`: `DeclareBindings`, to order handlers around.
  - a private declaration request, `Config`'s shaders, define sets,
    `VariantsOnly` and `TakeDeclaration`.
- **Resources it reads from other plugins:** `gfx.Device` and
  `platform.FS`, for read, and render's `Inventory` parameter.
- **Events it publishes:** none.
- **Events it subscribes to:** `DeclareEvent` (`Handlers.DeclareBindings`),
  whose job declares the shaders' bindings.
- **Task parameters it offers:** none.
- **Sockets it declares:** none.
- **Sockets it binds:** `platform.ReadMountSocket`, `Config.Directory` on
  disk, or the folder the process runs in when it is empty.
