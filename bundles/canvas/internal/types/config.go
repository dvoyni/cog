package types

// Config sizes canvas's atlases; see canvas.Config. It is declared here because
// the atlases and the Lookup resource hold it, and the root aliases it.
type Config struct {
	// AtlasSize is the width and height of one atlas page, in texels.
	AtlasSize int
	// LayersPerArray is how many pages one atlas texture array holds. It must be
	// at least two.
	LayersPerArray int
	// MaxAtlasBytes bounds the GPU memory one atlas may allocate; one whole
	// array must fit within it.
	MaxAtlasBytes int
}

// WithDefaults fills every zero field of config with the value canvas runs with
// when it is given none: 4096-texel pages, two pages per array and 256 MiB.
func WithDefaults(config Config) Config {
	if config.AtlasSize == 0 {
		config.AtlasSize = 4096
	}
	if config.LayersPerArray == 0 {
		config.LayersPerArray = 2
	}
	if config.MaxAtlasBytes == 0 {
		config.MaxAtlasBytes = 256 << 20
	}
	return config
}
