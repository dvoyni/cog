package gfx

// handle is what every resource handle is: a slot in gfx's table and the
// generation it was handed out under. Each handle type embeds it, so they
// stay distinct types with one Index.
type handle struct{ index, generation uint32 }

// Index answers the slot the handle names in gfx's table, where a backend
// keeps its native object; it is for backends, not for users.
func (h handle) Index() uint32 { return h.index }

// Buffer names a GPU buffer: a pointer-free, comparable handle whose zero
// value names none. Released, it names nothing, and using it answers
// ErrReleased.
type Buffer struct{ handle }

// Texture names a texture: a pointer-free, comparable handle whose zero value
// names none. Released, it names nothing, and using it answers ErrReleased.
type Texture struct{ handle }

// TextureView names a view of a texture, or of a window's framebuffer: a
// pointer-free, comparable handle whose zero value names none. Released, it
// names nothing, and using it answers ErrReleased.
type TextureView struct{ handle }

// Sampler names a sampler: a pointer-free, comparable handle whose zero value
// names none. Released, it names nothing, and using it answers ErrReleased.
type Sampler struct{ handle }

// ShaderModule names a WGSL shader module, kept with its naga IR for
// reflection: a pointer-free, comparable handle whose zero value names none.
// Released, it names nothing, and using it answers ErrReleased.
type ShaderModule struct{ handle }

// BindGroupLayout names a bind-group layout, explicit or built by reflection:
// a pointer-free, comparable handle whose zero value names none. Released, it
// names nothing, and using it answers ErrReleased.
type BindGroupLayout struct{ handle }

// BindGroup names a bind group: a pointer-free, comparable handle whose zero
// value names none. Released, it names nothing, and using it answers
// ErrReleased.
type BindGroup struct{ handle }

// PipelineLayout names a pipeline layout: a pointer-free, comparable handle
// whose zero value names none. Released, it names nothing, and using it
// answers ErrReleased.
type PipelineLayout struct{ handle }

// RenderPipeline names a render pipeline: a pointer-free, comparable handle
// whose zero value names none. Released, it names nothing, and using it
// answers ErrReleased.
type RenderPipeline struct{ handle }

// ComputePipeline names a compute pipeline: a pointer-free, comparable handle
// whose zero value names none. Released, it names nothing, and using it
// answers ErrReleased.
type ComputePipeline struct{ handle }

// QuerySet names a query set: occlusion or timestamp queries, resolved into a
// buffer with CommandEncoder.ResolveQuerySet.
type QuerySet struct{ handle }
