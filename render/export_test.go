package render

// FrameState is the private frame state, for render's tests to read what Init
// made of Config.
type FrameState = frameState

// TemporaryIdleFrames answers how many frames something pooled may go unused
// before render frees it.
func (s *frameState) TemporaryIdleFrames() int { return s.temporaryIdleFrames }

// TemporaryChunkSize answers the size in bytes of an arena chunk.
func (s *frameState) TemporaryChunkSize() uint64 { return s.temporaryChunkSize }
