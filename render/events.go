package render

import (
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/metronome"
)

// FrameEvent is the frame render's users record, published inside gfx's
// draw event every beat, even when no window has a framebuffer. A handler
// schedules its recording tasks after Ready and returns ex.Join(in, own),
// with no ordering among the handlers; gfx flushes the frame and presents it
// after the last of them. Frames record one at a time: a frame's Ready waits
// for the previous frame's present.
type FrameEvent struct {
	metronome.Beat
	// Ready is the job readying the frame for recording, after gfx has
	// acquired its framebuffers; recording starts after it.
	Ready kernel.JobHandle
	// AcquireWait is how long the previous frame's acquire blocked, by
	// metronome's clock: under Fifo, a long wait is headroom and a short one
	// a frame near its budget. Zero for the first frame.
	AcquireWait time.Duration
	// GPULatency is the time from the newest finished frame's submit to when
	// it was first seen complete, accurate to one check a frame, by
	// metronome's clock: a signal for adaptive quality. Zero until a frame
	// completes.
	GPULatency time.Duration
}
