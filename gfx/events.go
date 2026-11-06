package gfx

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/metronome"
)

// RenderEvent is the beat of the render track the application declares with
// metronome.NewTrack[gfx.RenderEvent]. gfx has the one handler that matters
// on it, Handlers.Draw, which publishes DrawEvent inside it.
type RenderEvent struct {
	metronome.Beat
}

// DrawEvent is published inside gfx's RenderEvent handler, every beat, even
// when no window has a framebuffer. A handler schedules its recording tasks
// after Ready, with the event as their arg, and returns ex.Join(in, own); gfx
// flushes and presents after the last. Frames record one at a time: a frame's
// acquire waits for the previous frame's present.
type DrawEvent struct {
	metronome.Beat
	// Frame names the frame being drawn, for Device.Encoder and
	// Device.Framebuffer.
	Frame Frame
	// Ready is the acquire job, after the previous frame's present;
	// recording starts after it.
	Ready kernel.JobHandle
}

// DeviceLostEvent is published once, by the job that first finds the device
// lost, after the loss goes to LogErr. Until DeviceRecoveredEvent no frame is
// drawn, and calls answering an error answer ErrDeviceLost.
type DeviceLostEvent struct{}

// DeviceRecoveredEvent is published once gfx has made a new device and
// recreated every object on it under its old handle, empty: owners schedule
// tasks from it to fill their buffers and textures again. A recovery that
// fails is retried on a later beat, at most once a second, with LogErrOnce.
type DeviceRecoveredEvent struct{}

// Frame names one frame in flight: a slot in gfx's pool of frames, which
// grows only while more render beats are in flight than it holds, so a
// render track needs a MaxInFlight. It is valid from its DrawEvent until the
// frame is presented, when its slot returns to the pool.
type Frame struct{ slot, generation uint32 }

// SubmitOrder is the number an encoder is handed under: a frame's encoders
// are submitted sorted by it. The number space is shared by every plugin that
// records; gfx defines no numbers of its own.
type SubmitOrder int32
