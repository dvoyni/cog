package render

// FrameState is the private frame state, for render's tests to read what Init
// made of Config.
type FrameState = frameState

// TemporaryIdleFrames answers how many frames something pooled may go unused
// before render frees it.
func (s *frameState) TemporaryIdleFrames() int { return s.temporaryIdleFrames }

// TemporaryChunkSize answers the size in bytes of an arena chunk.
func (s *frameState) TemporaryChunkSize() uint64 { return s.temporaryChunkSize }

// AssetTable is the private asset table, for render's tests to lock.
type AssetTable = assetTable

// AcquireMippedTexture makes a texture of descriptor and requests its mips,
// as a load with Mipmaps is once written, for render's tests of
// 2D-array and cube textures, which no file loads yet; it panics when gfx
// refuses it.
func (f Frame) AcquireMippedTexture(descriptor TextureDescriptor) Texture {
	table := f.table.Get()
	texture, err := table.addTexture(f.device.Get(), descriptor.fillDefaults())
	if err != nil {
		panic(err)
	}
	f.state.Get().mips.request(texture)
	return texture
}
