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

// Frame names one frame in flight: a slot in gfx's pool of frames, which
// grows only while more render beats are in flight than it holds, so a
// render track needs a MaxInFlight. It is valid from its DrawEvent until the
// frame is presented, when its slot returns to the pool.
type Frame struct{ slot, generation uint32 }

// SubmitOrder is the number an encoder is handed under: a frame's encoders
// are submitted sorted by it. The number space is shared by every plugin that
// records; gfx defines no numbers of its own.
type SubmitOrder int32
