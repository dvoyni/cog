package canvas

import (
	"time"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/metronome"
)

// FrameEvent is the frame canvas's users record, published inside
// render.FrameEvent every beat. A handler schedules its recording tasks,
// each declaring a DrawList, after Ready and returns ex.Join(in, own), with
// no ordering among the handlers; canvas draws the frame after the last of
// them. An entry recorded by a task not joined so, which runs once canvas's
// drawing has begun, is refused.
type FrameEvent struct {
	metronome.Beat
	// Ready is the job opening the frame for entries, after render's Ready;
	// recording starts after it.
	Ready kernel.JobHandle
	// AcquireWait and GPULatency are render's FrameEvent's: how long the
	// previous frame's acquire blocked, and the newest finished frame's time
	// on the GPU from its submit.
	AcquireWait time.Duration
	GPULatency  time.Duration
}

// InspectedEvent delivers the inspection Assets.RequestInspection asked for,
// published once render has delivered its own inspection of the frame of
// Beat, the frame inspected. A handler reads it with Assets.ReadInspection
// in tasks it joins into the out dependency it returns; after that, it is
// gone, and a request asks for another.
type InspectedEvent struct {
	Beat metronome.Beat
}

// startEvent is canvas's own event, published once as it starts, whose
// handler makes the default font set of Config.DefaultFontSet, once every
// mount its fonts are read through is bound.
type startEvent struct{}

// fontReloadEvent is canvas's own event, published by the drawing job
// once Assets.ReloadChanged has asked for the font files' reload, whose
// handler reloads them under FontLibrary's write lock.
type fontReloadEvent struct{}

// inspectionDeliveryEvent carries the inspection of the frame of beat, once
// render's inspection of it is beside it, from the job that matched them to
// canvas's handler, which publishes it as InspectedEvent in its own batch,
// to drop the inspection after.
type inspectionDeliveryEvent struct {
	beat metronome.Beat
}
