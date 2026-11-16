package render

// TextureDescriptor describes a two-dimensional texture, with layers: its
// label, for reports, inspections and GPU debuggers; its size in texels; how
// many layers, mip levels and samples it has, zero meaning one; and its
// format.
type TextureDescriptor struct {
	Label                                  string
	Width, Height                          uint32
	LayerCount, MipLevelCount, SampleCount uint32
	Format                                 TextureFormat
}

// fillDefaults answers d with each count that is zero set to one.
func (d TextureDescriptor) fillDefaults() TextureDescriptor {
	d.LayerCount = max(d.LayerCount, 1)
	d.MipLevelCount = max(d.MipLevelCount, 1)
	d.SampleCount = max(d.SampleCount, 1)
	return d
}
