package render

import (
	"sync"

	"github.com/dvoyni/cog/gfx"
)

// frameState is the private resource of the frames in flight: what render
// keeps from one frame to the next. Every job reads it.
type frameState struct {
	// temporaryIdleFrames is how many frames something pooled may go unused
	// before render frees it, and temporaryChunkSize the size in bytes of an
	// arena chunk: Config's, or their defaults.
	temporaryIdleFrames int
	temporaryChunkSize  uint64
	reporter
	// temporaryTextures is the pool AcquireTemporaryTexture hands out from.
	temporaryTextures texturePool
	// mips generates the mip levels of textures in render's own command
	// list.
	mips mipGenerator
	// mutex guards frame, gfx's frame being recorded, or the last one
	// recorded once gfx has flushed it, which gfx then refuses encoders for.
	mutex sync.Mutex
	frame gfx.Frame
}

// setRecordingFrame keeps frame as the frame being recorded.
func (s *frameState) setRecordingFrame(frame gfx.Frame) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.frame = frame
}

// recordingFrame answers the frame being recorded, or the last one recorded.
func (s *frameState) recordingFrame() gfx.Frame {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.frame
}
