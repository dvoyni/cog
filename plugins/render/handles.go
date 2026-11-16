package render

import "github.com/dvoyni/cog/plugins/render/internal/handle"

// Each of render's handles is a type of render/internal/handle, named here
// with a public alias, so render and rendertest alone can make one.

// Texture names one of render's textures, a loaded, made or temporary one: a
// pointer-free, comparable handle an ECS component can hold, its zero value
// naming none. It lives only while the program runs.
type Texture = handle.Texture

// Buffer names one of render's buffers, a loaded or a made one: a
// pointer-free, comparable handle an ECS component can hold, its zero value
// naming none. It lives only while the program runs.
type Buffer = handle.Buffer

// Shader names one of render's shaders, a variant loaded with its defines: a
// pointer-free, comparable handle an ECS component can hold, its zero value
// naming none. It lives only while the program runs.
type Shader = handle.Shader

// Mesh names one of render's meshes, made with Inventory.MakeMesh: a
// pointer-free, comparable handle an ECS component can hold, its zero value
// naming none. It lives only while the program runs.
type Mesh = handle.Mesh

// Material names one of render's materials, made with Inventory.MakeMaterial:
// a pointer-free, comparable handle an ECS component can hold, its zero value
// naming none. It lives only while the program runs.
type Material = handle.Material

// Readback names one of render's readbacks, recorded with
// CommandList.ReadBuffer or ReadTexture: a pointer-free, comparable handle,
// its zero value naming none, that ReadbackEvent delivers. It names the
// readback until the out dependency of that publication; a later readback
// may then be handed the same handle.
type Readback = handle.Readback
