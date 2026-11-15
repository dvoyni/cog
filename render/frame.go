package render

import (
	"errors"
	"fmt"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/kernel"
)

// Frame is the task parameter a recording task reaches the frame through,
// declared in its signature as kernel.Read is:
//
//	func drawScene(k *kernel.Kernel, event render.FrameEvent, frame render.Frame) kernel.NoResult
//
// It locks render's frame state, its asset table and gfx's Device, all for
// read, so tasks holding it run together, and its user names neither gfx nor
// its locks. It is valid only while the task's job runs.
type Frame struct {
	state  kernel.Read[*frameState]
	table  kernel.Read[*assetTable]
	device kernel.Read[gfx.Device]
}

// Declare declares Frame's locks and answers it bound to what they guard.
func (Frame) Declare(access *kernel.Access) Frame {
	return Frame{state: access.Read[*frameState](), table: access.Read[*assetTable](), device: access.Read[gfx.Device]()}
}

// BeginCommandList begins a command list of the frame being recorded, at
// order among the frame's other lists, for one task to fill. A list at an
// order another list of the frame has, or at render's own, math.MinInt32, is
// refused and reported once; so is a list begun while no frame is recording.
// A refused list records into nothing.
func (f Frame) BeginCommandList(label string, order Order) CommandList {
	state := f.state.Get()
	list := CommandList{frame: f}
	if order == ownOrder {
		state.report(ReportKindOrderReserved, fmt.Errorf("render: command list %q is at order %d, render's own", label, order))
		return list
	}
	gfxFrame := state.recordingFrame()
	encoder, err := f.device.Get().Encoder(gfxFrame, gfx.SubmitOrder(order))
	switch {
	case errors.Is(err, gfx.ErrOrderTaken):
		state.report(ReportKindOrderTaken, fmt.Errorf("render: command list %q is at order %d, which another list of the frame has", label, order))
	case errors.Is(err, gfx.ErrFrameFlushed):
		state.report(ReportKindListOutsideFrame, fmt.Errorf("render: command list %q is begun while no frame is recording", label))
	case errors.Is(err, gfx.ErrDeviceLost):
		// gfx logged the loss.
	case err != nil:
		state.logger.LogErr(fmt.Errorf("render: beginning command list %q: %w", label, err))
	default:
		list.gfxFrame = gfxFrame
		list.encoder = encoder
		list.listState = state.listStates.acquire(&state.arena, state.temporaryChunkSize)
	}
	return list
}

// AcquireTemporaryTexture hands out a temporary texture of descriptor from
// render's pool, made the first time the pool has none of descriptor free,
// its label included, with every usage its format allows: a pass target and
// a texture to sample. The texture is held until the frame ends, with no
// reuse within the frame, and its contents are undefined when taken. Its
// handle names it only until then: a later frame may hand the same texture,
// under the same handle, to another user, so a handle kept past its frame
// silently draws into or samples theirs. The pool frees the texture at the
// end of the frame that leaves it unused for Config's TemporaryIdleFrames
// frames. A user hands it between its tasks through
// state of its own, acquired in a task the kernel orders before those that
// draw into or sample it. A texture gfx refuses to make is logged, and the
// zero Texture answered.
func (f Frame) AcquireTemporaryTexture(descriptor TextureDescriptor) Texture {
	state := f.state.Get()
	texture, err := state.temporaryTextures.acquire(descriptor.fillDefaults(), f.table.Get(), f.device.Get())
	if err != nil {
		state.logUnlessLost(fmt.Errorf("render: acquiring temporary texture %q: %w", descriptor.Label, err))
	}
	return texture
}
