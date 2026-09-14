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
)
