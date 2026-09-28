package types

// Opaque GPU handles minted by a Backend. The zero value means "none".
type (
	ResourceID uint32
	// TextureID identifies a logical texture. The backend creates its native GPU
	// object lazily when it executes the first bake op for this ID.
	TextureID ResourceID
	// BufferID identifies a logical buffer. The backend creates its native GPU
	// object lazily when it executes the first bake op for this ID.
	BufferID ResourceID
	// SamplerID references a texture sampler.
	SamplerID ResourceID
	// ShaderID references a compiled shader module.
	ShaderID ResourceID
	// PipelineID references a render pipeline (shader + vertex layout + state).
	PipelineID ResourceID
	// TextureViewID references a renderable view (a render target). The screen
	// framebuffer and offscreen render targets are both TextureViewIDs.
	TextureViewID ResourceID

	// DrawStateID names one durable set of draw params: a shader, a fixed
	// DrawState and a value for some or all of the shader's bindings, created by
	// ResourceQueue.NewDrawParams. Its identity is the set's: two draws naming one
	// DrawStateID share shader, state and values, so the id alone is the complete
	// batch key a recorder needs.
	//
	// It is minted by the ResourceQueue rather than a Backend, and like the handles
	// above it is comparable and pointer-free, so a Component may hold one. The
	// zero value names no set.
	DrawStateID uint32

	// PassID names a pass OpQueue.NewPass declared this frame, for Draw to record
	// into. Its zero value names no pass.
	PassID int
)
