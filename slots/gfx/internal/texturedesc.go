package internal

// Region is a rectangular sub-area of a texture in texels. The json tags are
// there because a region reaches an agent inside a frame snapshot, and the
// rest of that document is lowerCamel.
type Region struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// TextureDesc describes a texture to create. Layers <= 1 creates a regular 2D
// texture; larger values create a 2D-array texture. Renderable asks for a
// texture a render pass can draw into as well as sample.
type TextureDesc struct {
	Width, Height int
	Layers        int
	Format        TextureFormat
	Mipmaps       bool
	Renderable    bool
	Label         string
}

// TextureUsage names the role a texture is in as far as the GPU's memory
// pipeline is concerned. It is deliberately just the roles gfx can put a
// texture in, not a mirror of the backend's usage flags.
type TextureUsage uint8

const (
	// TextureUsageRenderAttachment is a texture being written as a pass's
	// colour or depth attachment.
	TextureUsageRenderAttachment TextureUsage = iota
	// TextureUsageTextureBinding is a texture being read by a shader.
	TextureUsageTextureBinding
	// TextureUsageCopySrc is a texture being read back into CPU-visible
	// memory. It is the third role rather than a reuse of the other two
	// because From must name the usage a texture is actually in: a layout
	// transition that names the wrong old layout is undefined behaviour, and
	// a backend silently inserting an unnamed barrier for a capture is
	// precisely the undeclared hazard this type exists to abolish.
	TextureUsageCopySrc
)

// TextureTransition orders one texture's writes against its reads, or the other
// way round. The backend derives none of these itself: it tracks resources for
// lifetime and submit-time validation only, so a texture written as an
// attachment and then sampled is not ordered against those writes - not within
// one command encoder, and not across a submit boundary either. On Vulkan the
// sample then reads the image mid-write, which shows as a flicker that looks
// random, is not a CPU/GPU race, and is invisible to a capture taken while the
// app is redrawing.
//
// gfx is the layer that can see the hazard, because by translation time the
// frame's passes are sorted and merged and the write-then-read pairs are
// computable. From is the usage the texture is actually in, not a guess: a
// layout transition that names the wrong old layout is undefined behaviour.
type TextureTransition struct {
	Texture  TextureID
	From, To TextureUsage
}
