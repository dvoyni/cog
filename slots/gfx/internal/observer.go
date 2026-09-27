package internal

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// FrameObserver watches gfx's frames from outside it. gfx calls it at the
// points only gfx's own handlers reach - the recorded queue before present
// swaps it away, and the render that reads a target back - and the observer
// keeps whatever state it needs behind its own lock.
//
// It is how the capture and frame-snapshot tools live in internal/mcp without
// gfx importing them: that package depends on this one, gfxplugin hands its
// observer to New, and gfx never names it.
type FrameObserver interface {
	// Register adds the observer's own commands, subscriptions and adapters to
	// gfx's registration.
	Register(registrar *kernel.Registrar)
	// Presenting runs inside the present handler, after every recorder has
	// flushed into the queue and before present swaps it away: the one moment
	// the tick's frame is both complete and still alive.
	Presenting(queue *OpQueue, resources *ResourceQueue, tick int64)
	// Capturing reports what the frame about to be rendered should read back.
	Capturing() (types.CaptureDesc, bool)
	// Captured hands over a readback the backend has completed.
	Captured(capture Capture)
	// Encoded reports that the render just executed carried the capture op
	// Capturing asked for.
	Encoded()
	// Abandon completes whatever is still waiting. It runs from Stop, after
	// the scheduler has stopped and grants no locks.
	Abandon()
}

// noObserver is the plugin's observer when none is given: it registers nothing
// and never asks for a capture.
type noObserver struct{}

func (noObserver) Register(*kernel.Registrar)                 {}
func (noObserver) Presenting(*OpQueue, *ResourceQueue, int64) {}
func (noObserver) Capturing() (types.CaptureDesc, bool)       { return types.CaptureDesc{}, false }
func (noObserver) Captured(Capture)                           {}
func (noObserver) Encoded()                                   {}
func (noObserver) Abandon()                                   {}
