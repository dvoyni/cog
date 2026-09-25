package internal

import (
	"fmt"
)

// ErrVertexStrideAlignment reports a vertex stride that is not a multiple of 4.
// WebGPU requires that unconditionally, and the platforms disagree about it:
// a 30-byte stride succeeds on Vulkan, Apple-silicon Metal and D3D12 and fails
// on js/wasm, on GLES and on older Apple GPUs. The draw is dropped here so that
// a Windows dev machine and a browser give the same answer.
type ErrVertexStrideAlignment struct {
	Shader string
	Stride int
}

func (e ErrVertexStrideAlignment) Error() string {
	return fmt.Sprintf("gfx: the vertex layout bound to shader %q has a %d-byte stride; WebGPU requires a multiple of 4",
		e.Shader, e.Stride)
}

// ErrDrawWithoutPass is reported when a frame records draws before declaring a
// pass. There is no implicit pass to absorb them, so they are dropped: a draw
// with no pass has no target, no depth attachment and no place in the frame's
// order.
type ErrDrawWithoutPass struct{ Count int }

func (e ErrDrawWithoutPass) Error() string {
	return fmt.Sprintf("gfx: %d draws recorded outside any pass were dropped; declare a pass with OpQueue.Pass", e.Count)
}

// ErrDrawSamplesAttachment is reported when a draw samples a texture its own
// pass renders into. The draw is dropped: reading a live attachment is
// undefined, and the frame is more useful with the mistake named than with one
// silently wrong draw in it.
type ErrDrawSamplesAttachment struct{ Pass, Parameter string }

func (e ErrDrawSamplesAttachment) Error() string {
	return fmt.Sprintf("gfx: draw in pass %q samples %q, which that pass renders into", e.Pass, e.Parameter)
}

// ErrParameterKindMismatch reports a parameter whose name matched a binding its
// kind cannot fill - a buffer supplied where the shader declared a uniform
// member, say, or a value where it declared a storage buffer.
//
// The draw is dropped. Parameters resolve by name and ignore kind, so without
// this the wrong descriptor is bound into the slot and the draw renders garbage
// with nothing reported anywhere; a missing storage binding is worse still,
// because the bind group is rejected and the draw encodes with no bindings at
// all. One name has one frequency, and naming it at two is an authoring error.
type ErrParameterKindMismatch struct {
	Shader    string
	Parameter string
	Supplied  string // the kind the caller passed
	Declared  string // the kind the shader declared
}

func (e ErrParameterKindMismatch) Error() string {
	return fmt.Sprintf("gfx: shader %q declares %s named %q, but the draw supplied a %s parameter",
		e.Shader, e.Declared, e.Parameter, e.Supplied)
}

// ErrStorageBufferUnsupplied reports a declared storage binding that no draw
// parameter fills, either because none names it or because the one that does
// carries a buffer that was never baked.
//
// The draw is dropped. Textures and samplers fall back - an unresolved texture
// renders white and an unset sampler clamps and filters linearly - but a
// storage buffer has no fallback worth having: nothing is emitted for the
// binding, the group comes up one entry short of its layout, CreateBindGroup
// refuses it and the draw encodes with no bindings for that group at all. A
// zero-length dummy would not save it either, because the binding is validated
// against the size the shader's own declaration needs.
type ErrStorageBufferUnsupplied struct {
	Shader    string
	Parameter string
	Group     int
	Binding   int
	// Unbaked separates the two ways in: false when no parameter names the
	// binding, true when one does and its buffer has no id. They are different
	// mistakes - a material that forgot a parameter against one that handed
	// over a buffer it never baked - and the fix differs with them.
	Unbaked bool
}

func (e ErrStorageBufferUnsupplied) Error() string {
	cause := "no parameter supplies it"
	if e.Unbaked {
		cause = "the parameter that names it supplies an unbaked buffer"
	}
	return fmt.Sprintf("gfx: shader %q declares a storage buffer named %q at group %d binding %d, and %s",
		e.Shader, e.Parameter, e.Group, e.Binding, cause)
}

// ErrTextureViewDimensionMismatch reports a texture parameter whose view
// dimension cannot fill the binding its name matched: a single-layer texture
// supplied where the shader declared texture_2d_array, or an array texture
// where it declared texture_2d.
//
// The draw is dropped. This is the one texture case that does not fall back: an
// unfilled or unresolved texture stands for "not there yet" - still loading, or
// never named - and white is the right picture for it at either dimension. A
// texture that is there and is the wrong shape is an authoring error the caller
// can fix, and substituting white for it would hide a mistake rather than show
// a state.
//
// A descriptor that cannot report its layer count is exempt rather than judged.
// Only an allocation names one, so a bare baked id says nothing, and refusing a
// correct draw over a descriptor's silence is the worse direction for a fatal
// error. The backend's refused bind group stays the backstop beneath it.
type ErrTextureViewDimensionMismatch struct {
	Shader    string
	Parameter string
	Group     int
	Binding   int
	Declared  string // the view dimension the shader declared
	Supplied  string // the view dimension the parameter's texture carries
}

func (e ErrTextureViewDimensionMismatch) Error() string {
	return fmt.Sprintf(
		"gfx: shader %q declares %s named %q at group %d binding %d, but the draw supplied a %s texture",
		e.Shader, e.Declared, e.Parameter, e.Group, e.Binding, e.Supplied)
}

// ErrCaptureBusy reports a capture arm made while one is already live. It is
// refused rather than queued or coalesced: queueing turns a boolean into a
// queue for a fifty-millisecond window, and coalescing two waiters onto one
// frame founders on the fact that each names a different file.
//
// Both refusal sites raise it. gfx refuses a second arm synchronously, and the
// backend refuses a second in-flight map through Capture.Err, because capture
// is a public gfx feature and a game's own code may arm one.
type ErrCaptureBusy struct{}

func (ErrCaptureBusy) Error() string {
	return "gfx: a capture is already in flight"
}

// ErrCaptureAbandoned reports a capture the engine stopped before its readback
// resolved. It travels the channel a result would have used, so that a waiter
// learns the answer rather than sitting until its own deadline.
type ErrCaptureAbandoned struct{}

func (ErrCaptureAbandoned) Error() string {
	return "gfx: the engine stopped before the capture was read back"
}

// ErrCaptureUnsupported reports a target a capture cannot be an image of:
// depth, or any format that is not 8-bit RGBA. Depth readback is a real want
// and it is a visualization question rather than a readback one - a depth
// capture is a float field needing a range to be legible.
type ErrCaptureUnsupported struct{ Format TextureFormat }

func (e ErrCaptureUnsupported) Error() string {
	return fmt.Sprintf("gfx: %s cannot be captured; a capture is 8-bit RGBA", e.Format.Name())
}

// ErrCaptureNoTarget reports a capture of something the frame never rendered
// into: a screen capture in a frame that drew nothing to the screen, or a
// texture that has no GPU object yet. It is distinct from an unsupported
// format because the answers differ - one says wait for a frame that draws,
// the other says this can never be an image.
type ErrCaptureNoTarget struct{}

func (ErrCaptureNoTarget) Error() string {
	return "gfx: the capture's target was not rendered into this frame"
}

// ErrPipelineFailed reports a pipeline the backend refused. gfx used to discard
// this diagnosis and return a zero id, which from the caller's seat made "gfx
// refused to build this" and "the backend refused to build this" the same
// silent event.
type ErrPipelineFailed struct {
	Shader string
	Err    error
}

func (e ErrPipelineFailed) Error() string {
	return fmt.Sprintf("gfx: the backend refused a pipeline for shader %q: %v", e.Shader, e.Err)
}

func (e ErrPipelineFailed) Unwrap() error { return e.Err }

// ErrIndexBufferLength reports an index buffer whose byte length is not a
// multiple of the width the mesh declared it at. With two widths in the engine
// a buffer declared uint16 and written as uint32 is a third way to be silently
// wrong, and this is the O(1) half of catching it: the range check - that every
// index is below the vertex count - stays in scene, where a pass over the
// indices already runs.
//
// The draw is dropped, and the report is made once per offending buffer rather
// than once a frame, the way a failed pipeline is.
type ErrIndexBufferLength struct {
	Shader string
	Length int
	Width  int
}

func (e ErrIndexBufferLength) Error() string {
	return fmt.Sprintf("gfx: the index buffer drawn with shader %q is %d bytes, which is not a multiple of its %d-byte index width",
		e.Shader, e.Length, e.Width)
}

// ErrBackendNotReady is reported the first time a frame is rendered before the
// Backend adapter is ready. Nothing reaches the GPU until it is, so it
// distinguishes a driver whose device never arrived from a scene that
// legitimately drew nothing.
type ErrBackendNotReady struct{}

func (ErrBackendNotReady) Error() string {
	return "gfx: the Backend is not ready, so the frame was not rendered"
}

// ErrCaptureAmount reports a burst asking for more stills than one request may
// take. A longer window is bought with the interval, never with the amount.
type ErrCaptureAmount struct{ Amount, Max int }

func (e ErrCaptureAmount) Error() string {
	return fmt.Sprintf("gfx: %d stills is more than the %d one capture may take", e.Amount, e.Max)
}

// ErrCaptureSpan reports a burst whose stills would span more ticks than one
// request may cover. It is the same figure as a time step's cap, because a
// request that outlives its caller is bounded the same way either side.
type ErrCaptureSpan struct{ Ticks, Max int }

func (e ErrCaptureSpan) Error() string {
	return fmt.Sprintf("gfx: %d ticks is a longer span than the %d one capture may cover", e.Ticks, e.Max)
}

// ErrCaptureBurstPaused reports a burst armed against a stopped tick source.
// No new ticks means N byte-identical files, and a silent pile of duplicates is
// exactly the failure a debug facility must not have. A single capture while
// paused stays legal, and costs no tick.
type ErrCaptureBurstPaused struct{}

func (ErrCaptureBurstPaused) Error() string {
	return "gfx: a burst needs ticks, and the engine is paused"
}

// ErrFrameBusy reports a frame-snapshot arm made while one is already live.
// It is refused rather than queued, for the reason a second capture is: the
// window is one tick and the retry is one call.
//
// It is deliberately its own kind. A capture, a frame snapshot and the other
// packages' snapshots are separate slots and may be in flight together -
// refusing across kinds would destroy the one thing arming them together is
// for, which is describing a single tick.
type ErrFrameBusy struct{}

func (ErrFrameBusy) Error() string {
	return "gfx: a frame snapshot is already in flight"
}

// ErrFrameAbandoned reports a frame snapshot the engine stopped before a tick
// completed. It travels the channel a result would have used, so that a
// waiter learns the answer rather than sitting until its own deadline.
type ErrFrameAbandoned struct{}

func (ErrFrameAbandoned) Error() string {
	return "gfx: the engine stopped before a frame was recorded"
}
