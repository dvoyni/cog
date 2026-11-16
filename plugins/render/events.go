package render

import (
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/platform"
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

// RecoveredEvent is published once gfx has recovered a lost device and
// render has written its own assets again under their handles: every loaded
// texture and buffer read from its file again, the mips of those loaded with
// Mipmaps requested for the next frame, and what render fills itself. Every
// asset made from a slice is back under its handle, holding nothing: its
// maker schedules tasks from this event to fill it again. A loaded asset
// whose file is gone, such as one loaded from memory-mount bytes since
// removed, is reported, as a failed load is.
type RecoveredEvent struct{}

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

// InspectedEvent delivers the inspection Frame.RequestInspection asked for,
// published once the frame of Beat, the frame inspected, is presented, or,
// for a frame timed, once its pass timings are read back. A
// handler reads it with Frame.ReadInspection in tasks it joins into the out
// dependency it returns; after that, it is gone, and a request asks for
// another.
type InspectedEvent struct {
	Beat metronome.Beat
}

// PassTimingsEvent delivers the pass timings of the frame of Beat, timed
// while Frame.SetPassTimings(true) held, published once the GPU has run the
// frame and its timestamps are read back: a frame or more after that
// frame, for each frame with a pass. A handler reads them with
// Frame.ReadPassTimings in tasks it joins into the out dependency it
// returns; after that, they are gone.
type PassTimingsEvent struct {
	Beat metronome.Beat
}

// inspectionDeliveryEvent carries the inspection of the frame of beat, once
// the frame ends, or once its pass timings are read back when the frame was
// timed, from that job to render's handler, which publishes it as
// InspectedEvent in its own batch, to release the inspection after.
type inspectionDeliveryEvent struct {
	beat metronome.Beat
}

// passTimingsDeliveryEvent carries the pass timings of the frame of beat,
// once read back, from the job that read them to render's handler, which
// publishes them as PassTimingsEvent in its own batch, to free them after.
type passTimingsDeliveryEvent struct {
	beat metronome.Beat
}

// readbackDeliveryEvent carries a readback whose map has ended, or failed to
// start, from the job that found it to render's handler, which publishes it
// as ReadbackEvent in its own batch, to release the readback's staging
// buffer after.
type readbackDeliveryEvent struct {
	delivered ReadbackEvent
}
