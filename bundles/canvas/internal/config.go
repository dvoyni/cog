package internal

// Config sizes canvas's atlases. It is declared here because the atlases and the
// Lookup resource are, and canvasimpl re-exports it as canvasimpl.Config.
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

// DefaultConfig is the configuration canvas runs with when it is given none, and
// what fills a field a caller left zero.
func DefaultConfig() Config {
	return Config{
		AtlasSize:      4096,
		LayersPerArray: 2,
		MaxAtlasBytes:  256 << 20,
	}
}
