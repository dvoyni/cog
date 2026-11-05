package gfx

// Buffer names a GPU buffer: a pointer-free, comparable handle whose zero value
// names none. Released, it names nothing, and using it answers ErrReleased.
type Buffer struct{ index, generation uint32 }

// Index answers the slot buffer names in gfx's table, where a backend keeps
// its native object; it is for backends, not for users.
func (handle Buffer) Index() uint32 { return handle.index }

// Texture names a texture: a pointer-free, comparable handle whose zero value
// names none. Released, it names nothing, and using it answers ErrReleased.
type Texture struct{ index, generation uint32 }

// Index answers the slot texture names in gfx's table, where a backend keeps
// its native object; it is for backends, not for users.
func (handle Texture) Index() uint32 { return handle.index }

// TextureView names a view of a texture, or of a window's framebuffer: a pointer-free, comparable handle whose zero value
// names none. Released, it names nothing, and using it answers ErrReleased.
type TextureView struct{ index, generation uint32 }

// Index answers the slot textureView names in gfx's table, where a backend keeps
// its native object; it is for backends, not for users.
func (handle TextureView) Index() uint32 { return handle.index }

// Sampler names a sampler: a pointer-free, comparable handle whose zero value
// names none. Released, it names nothing, and using it answers ErrReleased.
type Sampler struct{ index, generation uint32 }

// Index answers the slot sampler names in gfx's table, where a backend keeps
// its native object; it is for backends, not for users.
func (handle Sampler) Index() uint32 { return handle.index }

// ShaderModule names a WGSL shader module, kept with its naga IR for reflection: a pointer-free, comparable handle whose zero value
// names none. Released, it names nothing, and using it answers ErrReleased.
type ShaderModule struct{ index, generation uint32 }

// Index answers the slot shaderModule names in gfx's table, where a backend keeps
// its native object; it is for backends, not for users.
func (handle ShaderModule) Index() uint32 { return handle.index }

// BindGroupLayout names a bind-group layout, explicit or built by reflection: a pointer-free, comparable handle whose zero value
// names none. Released, it names nothing, and using it answers ErrReleased.
type BindGroupLayout struct{ index, generation uint32 }

// Index answers the slot bindGroupLayout names in gfx's table, where a backend keeps
// its native object; it is for backends, not for users.
func (handle BindGroupLayout) Index() uint32 { return handle.index }

// BindGroup names a bind group: a pointer-free, comparable handle whose zero value
// names none. Released, it names nothing, and using it answers ErrReleased.
type BindGroup struct{ index, generation uint32 }

// Index answers the slot bindGroup names in gfx's table, where a backend keeps
// its native object; it is for backends, not for users.
func (handle BindGroup) Index() uint32 { return handle.index }

// PipelineLayout names a pipeline layout: a pointer-free, comparable handle whose zero value
// names none. Released, it names nothing, and using it answers ErrReleased.
type PipelineLayout struct{ index, generation uint32 }

// Index answers the slot pipelineLayout names in gfx's table, where a backend keeps
// its native object; it is for backends, not for users.
func (handle PipelineLayout) Index() uint32 { return handle.index }

// RenderPipeline names a render pipeline: a pointer-free, comparable handle whose zero value
// names none. Released, it names nothing, and using it answers ErrReleased.
type RenderPipeline struct{ index, generation uint32 }

// Index answers the slot renderPipeline names in gfx's table, where a backend keeps
// its native object; it is for backends, not for users.
func (handle RenderPipeline) Index() uint32 { return handle.index }

// ComputePipeline names a compute pipeline: a pointer-free, comparable handle whose zero value
// names none. Released, it names nothing, and using it answers ErrReleased.
type ComputePipeline struct{ index, generation uint32 }

// Index answers the slot computePipeline names in gfx's table, where a backend keeps
// its native object; it is for backends, not for users.
func (handle ComputePipeline) Index() uint32 { return handle.index }
