package render

import "github.com/dvoyni/cog/gfx"

// parameterBindGroup is a bind group of pass or draw parameters that an
// interned group layout keeps, shared by every pass and draw binding the
// same: the group; the textures and buffers it names, so it is dropped at
// the end of the frame one of them is freed; and how many frames have ended
// since a draw last bound it.
type parameterBindGroup struct {
	bindGroup     gfx.BindGroup
	namedTextures []Texture
	namedBuffers  []Buffer
	idleFrames    int
}

// bindGroupKeyEntry is what one entry of a bind group binds, as the key of
// its group's cache spells it. It is laid out with no padding, so a run of
// them, one for each entry of the group's layout in order, is the key's
// bytes.
type bindGroupKeyEntry struct {
	buffer       gfx.Buffer
	sampler      gfx.Sampler
	textureView  gfx.TextureView
	offset, size uint64
}

// bindGroupDraft is what a command list's draws build a bind group of pass
// or draw parameters in, kept between them so building one allocates
// nothing: its entries, the key they make, and the textures and buffers they
// name.
type bindGroupDraft struct {
	entries       []gfx.BindGroupEntry
	keyEntries    []bindGroupKeyEntry
	namedTextures []Texture
	namedBuffers  []Buffer
}
