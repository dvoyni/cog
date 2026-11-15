package render

import (
	"time"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/metronome"
	"github.com/dvoyni/cog/platform"
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

// CapturedEvent delivers a capture Frame.RequestCapture asked for, published
// a frame after the frame it was taken from, once its picture can be read.
// A handler reads it with Frame.ReadCapture in tasks it joins into the out
// dependency it returns; after that, the picture is gone.
type CapturedEvent struct {
	Window platform.Window
	// Beat is the captured frame's: Tick and InterpolatedTick are the moment
	// it shows.
	Beat   metronome.Beat
	Status CaptureStatus
	// captured is gfx's event, naming the buffer the picture is in.
	captured gfx.CapturedEvent
}

// ReadbackEvent delivers a readback CommandList.ReadBuffer or ReadTexture
// recorded, published once the frame it was recorded in has run on the GPU
// and its bytes can be read: a frame or more after that frame. A handler
// reads them with Frame.ReadbackData in tasks it joins into the out
// dependency it returns; after that, they are gone.
type ReadbackEvent struct {
	Readback Readback
	Status   ReadbackStatus
}

// readbackDeliveryEvent carries a readback whose map has ended, or failed to
// start, from the job that found it to render's handler, which publishes it
// as ReadbackEvent in its own batch, to release the readback's staging
// buffer after.
type readbackDeliveryEvent struct {
	delivered ReadbackEvent
}
