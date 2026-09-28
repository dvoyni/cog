package types

import "strconv"

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

// String spells the usage for a debug document.
func (usage TextureUsage) String() string {
	switch usage {
	case TextureUsageRenderAttachment:
		return "renderAttachment"
	case TextureUsageTextureBinding:
		return "textureBinding"
	case TextureUsageCopySrc:
		return "copySrc"
	}
	return "unknown(" + strconv.Itoa(int(usage)) + ")"
}
