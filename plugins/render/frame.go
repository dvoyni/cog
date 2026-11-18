package render

import (
	"errors"
	"fmt"
	"image"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/platform"
)

// Frame is the task parameter a recording task reaches the frame through,
// declared in its signature as kernel.Read is:
//
//	func drawScene(k *kernel.Kernel, event render.FrameEvent, frame render.Frame) kernel.NoResult
//
// It locks render's frame state, its asset table, gfx's Device and
// platform's FS, the last for the mip shader CommandList.GenerateMips reads,
// all for read, so tasks holding it run together, and its user names
// neither gfx nor its locks. It is valid only while the task's job runs.
type Frame struct {
	state  kernel.Read[*frameState]
	table  kernel.Read[*assetTable]
	device kernel.Read[gfx.Device]
	files  kernel.Read[platform.FS]
}

// Declare declares Frame's locks and answers it bound to what they guard.
func (Frame) Declare(access *kernel.Access) Frame {
	return Frame{state: access.Read[*frameState](), table: access.Read[*assetTable](), device: access.Read[gfx.Device](), files: access.Read[platform.FS]()}
}

// BeginCommandList begins a command list of the frame being recorded, at
// order among the frame's other lists, for one task to fill. A list at an
// order another list of the frame has, or at one of render's own,
// math.MinInt32 and math.MaxInt32, is refused and reported once; so is a
// list begun while no frame is recording. A refused list records into
// nothing.
func (f Frame) BeginCommandList(label string, order Order) CommandList {
	state := f.state.Get()
	list := CommandList{frame: f}
	if order == ownOrder || order == closingOrder {
		state.report(ReportKindOrderReserved, fmt.Errorf("render: command list %q is at order %d, one of render's own", label, order))
		return list
	}
	gfxFrame := state.recordingFrame()
	encoder, err := f.device.Get().Encoder(gfxFrame, gfx.SubmitOrder(order), label)
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
		list.listState = state.listStates.acquire(&state.arena, state.temporaryChunkSize, order)
		list.listState.inspectedList = state.inspector.addList(order, label)
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
		return texture
	}
	state.inspector.add(func(inspection *Inspection) {
		inspection.TemporaryTextures = append(inspection.TemporaryTextures, texture)
	})
	return texture
}

// RequestCapture asks for window's picture as the first frame acquired after
// the call draws it: the next frame when one is recording, this call
// included. CapturedEvent delivers it a frame after that frame. A window
// already being captured answers ErrCaptureBusy until its CapturedEvent is
// published, one that cannot be captured ErrCaptureUnsupported, and any
// window ErrDeviceLost while the device is lost.
func (f Frame) RequestCapture(window platform.Window) error {
	err := f.device.Get().RequestCapture(window)
	switch {
	case errors.Is(err, gfx.ErrCaptureBusy):
		return ErrCaptureBusy
	case errors.Is(err, gfx.ErrCaptureUnsupported):
		return ErrCaptureUnsupported
	case errors.Is(err, gfx.ErrDeviceLost):
		return ErrDeviceLost
	case err != nil:
		return fmt.Errorf("render: requesting a capture of %v: %w", window, err)
	}
	return nil
}

// ReadCapture answers event's picture as straight-alpha pixels, the window's
// texels as it displayed them, with no colour conversion, in RGBA order and
// alpha opaque. It answers it only while the jobs of event's publication
// run, so a handler of CapturedEvent calls it in a task it joins into the
// out dependency it returns; later, and for an event whose Status is not
// CaptureStatusCaptured, it answers an error, ErrDeviceLost while the device
// is lost.
func (f Frame) ReadCapture(event CapturedEvent) (*image.NRGBA, error) {
	picture, err := gfx.ReadCapture(f.device.Get(), event.captured)
	switch {
	case errors.Is(err, gfx.ErrDeviceLost):
		return nil, ErrDeviceLost
	case err != nil:
		return nil, fmt.Errorf("render: reading a capture of %v: %w", event.Window, err)
	}
	return picture, nil
}

// RequestInspection asks for an inspection of the first frame whose
// FrameEvent is published after the call, one at a time: a request while
// one is pending, asked for and not yet delivered, joins it. While that
// frame records, render's calls also write it down, and InspectedEvent
// delivers it once the frame is presented, or, while pass timings are on,
// once its pass timings are read back; every other frame pays one
// check a call, and allocates nothing for it.
func (f Frame) RequestInspection() {
	f.state.Get().inspector.request()
}

// ReadInspection answers event's inspection. It answers it only while the
// jobs of event's publication run, so a handler of InspectedEvent calls it
// in a task it joins into the out dependency it returns; later, it answers
// an error.
func (f Frame) ReadInspection(event InspectedEvent) (Inspection, error) {
	inspection, ok := f.state.Get().inspector.read(event.Beat)
	if !ok {
		return Inspection{}, fmt.Errorf("render: reading the inspection of beat %d, which is read only while InspectedEvent's jobs run", event.Beat.Tick)
	}
	return inspection, nil
}

// SetPassTimings turns pass timings on or off from the next frame to
// begin. While they are on, the GPU writes when each pass and compute pass
// of a frame, render's own included, begins and ends, and PassTimingsEvent
// delivers the frame's timings once the GPU has run it, a frame or more
// later, for each frame with a pass; an inspection of a timed frame waits
// for them, and holds them. A frame's passes after its 2048th get no
// timings, and are reported with ReportKindPassUntimed. Timings need
// timestamp queries: gfx's Config requiring FeatureFlagTimestampQuery, on a
// backend that has them, which none but render/rendertest's fake GPU has
// until the wgpu fork exposes them. Turning them on without is reported
// once, with ReportKindPassTimingsUnavailable, and leaves them off.
func (f Frame) SetPassTimings(on bool) {
	state := f.state.Get()
	state.passTimer.setEnabled(on, f.device.Get(), state.reporter)
}

// ReadPassTimings answers event's pass timings: each pass and compute pass
// of the frame of event's beat, by the order of its command list, those of
// one list in the order they began, with that order, its label and how long
// the GPU took to run it. They are render's own, so a task copies what it
// keeps. It answers them only while the jobs of event's publication run, so
// a handler of PassTimingsEvent calls it in a task it joins into the out
// dependency it returns; later, it answers an error.
func (f Frame) ReadPassTimings(event PassTimingsEvent) ([]PassTiming, error) {
	timings, ok := f.state.Get().passTimer.read(event.Beat)
	if !ok {
		return nil, fmt.Errorf("render: reading the pass timings of beat %d, which are read only while PassTimingsEvent's jobs run", event.Beat.Tick)
	}
	return timings, nil
}

// ReadbackData answers event's bytes: a buffer's range as it was where
// CommandList.ReadBuffer recorded it, or the rows of a texture's region as
// they were where ReadTexture recorded it, laid out as
// CommandList.CopyBufferToTexture lays them, each row starting a multiple of
// 256 bytes after the one before. They are the staging buffer's own, so a
// task copies what it keeps. It answers them only while the jobs of event's
// publication run, so a handler of ReadbackEvent calls it in a task it joins
// into the out dependency it returns; later, the staging buffer goes back to
// render's pool, and it answers an error, as it does for an event whose
// Status is not ReadbackStatusReady, ErrDeviceLost while the device is lost.
func (f Frame) ReadbackData(event ReadbackEvent) ([]byte, error) {
	data, err := f.state.Get().staging.read(event.Readback, f.device.Get())
	switch {
	case errors.Is(err, gfx.ErrDeviceLost):
		return nil, ErrDeviceLost
	case err != nil:
		return nil, fmt.Errorf("render: reading readback %v: %w", event.Readback, err)
	}
	return data, nil
}
