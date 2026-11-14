package render

// frameState is the private resource of the frames in flight: what render
// keeps from one frame to the next. Every job reads it.
type frameState struct {
	// temporaryIdleFrames is how many frames something pooled may go unused
	// before render frees it, and temporaryChunkSize the size in bytes of an
	// arena chunk: Config's, or their defaults.
	temporaryIdleFrames int
	temporaryChunkSize  uint64
}
