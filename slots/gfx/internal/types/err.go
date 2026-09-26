package types

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

// ErrDrawWithoutPass is reported when a frame records draws naming no pass
// declared that frame. There is no implicit pass to absorb them, so they are
// dropped: a draw with no pass has no target, no depth attachment and no place
// in the frame's order.
type ErrDrawWithoutPass struct{ Count int }

func (e ErrDrawWithoutPass) Error() string {
	return fmt.Sprintf("gfx: %d draws naming no pass declared this frame were dropped; declare one with OpQueue.NewPass and draw into its PassRef", e.Count)
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

// ErrCaptureNoTarget reports a capture of something the frame never rendered
// into: a screen capture in a frame that drew nothing to the screen, or a
// texture that has no GPU object yet. It is distinct from an unsupported
// format because the answers differ - one says wait for a frame that draws,
// the other says this can never be an image.
type ErrCaptureNoTarget struct{}

func (ErrCaptureNoTarget) Error() string {
	return "gfx: the capture's target was not rendered into this frame"
}

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

// ErrShaderNotLive reports a ResourceQueue call naming a shader id that is not
// live: one NewShader never reserved, or one ReleaseShader already released.
// The call is ignored. Ids are never reused, so the check is exact, and it is a
// programmer mistake rather than a state the frame can be in.
type ErrShaderNotLive struct {
	Shader ShaderID
	// Call is the ResourceQueue method that named it.
	Call string
	// Released separates the two ways in: true when the id was live once and
	// has been released, false when nothing ever reserved it.
	Released bool
}

func (e ErrShaderNotLive) Error() string {
	state := "was never reserved by NewShader"
	if e.Released {
		state = "has been released"
	}
	return fmt.Sprintf("gfx: %s names shader %d, which %s", e.Call, e.Shader, state)
}

// ErrShaderProgramInvalid reports an UploadProgram handed no program - the zero
// ShaderProgram a failed CompileShaderCmd returns beside its error. The upload
// is ignored and the shader stays reserved, so a later upload of a program that
// compiled still takes.
type ErrShaderProgramInvalid struct{ Shader ShaderID }

func (e ErrShaderProgramInvalid) Error() string {
	return fmt.Sprintf("gfx: UploadProgram hands shader %d no program; CompileShaderCmd returned an error for it", e.Shader)
}

// ErrShaderUploadedTwice reports a second UploadProgram to one shader. A shader
// takes one program for its life; to reload one, release it and create it
// again. The second upload is ignored.
type ErrShaderUploadedTwice struct {
	Shader ShaderID
	// Label names the program the shader already holds.
	Label string
}

func (e ErrShaderUploadedTwice) Error() string {
	return fmt.Sprintf("gfx: shader %d already holds program %q; a shader takes one upload - release it and create a new one to reload", e.Shader, e.Label)
}

// ErrShaderHasNoProgram reports a set built on a shader that is reserved but
// has no program yet. A set is resolved against its shader's binding table the
// moment it is created, and a shader has one only from UploadProgram on, so the
// set is created failed: it exists, and draws nothing.
type ErrShaderHasNoProgram struct {
	Shader ShaderID
	// Call is the ResourceQueue method that named it.
	Call string
}

func (e ErrShaderHasNoProgram) Error() string {
	return fmt.Sprintf("gfx: %s names shader %d, which has no program yet; upload one with UploadProgram first", e.Call, e.Shader)
}

// ErrDrawParamsNotLive reports a call naming a set of draw params that is not
// live: one NewDrawParams never created, or one ReleaseDrawParams already
// released. The call is ignored. Set ids are never reused, so the check is
// exact.
type ErrDrawParamsNotLive struct {
	Set uint32
	// Call is the method that named it.
	Call string
	// Released is true when the set was live once and has been released, and
	// false when nothing ever created it.
	Released bool
}

func (e ErrDrawParamsNotLive) Error() string {
	state := "was never created by NewDrawParams"
	if e.Released {
		state = "has been released"
	}
	return fmt.Sprintf("gfx: %s names draw params %d, which %s", e.Call, e.Set, state)
}

// ErrDrawParamUnknown reports a param naming no binding its set's shader
// declares. A param is a whole binding, named by the binding's WGSL global
// name, so a name the shader does not declare is a typo or a param meant for
// another shader. The param is ignored.
type ErrDrawParamUnknown struct {
	Shader    string
	Parameter string
	// Call is the method the param was handed to.
	Call string
}

func (e ErrDrawParamUnknown) Error() string {
	return fmt.Sprintf("gfx: %s supplies %q, which shader %q declares no binding named", e.Call, e.Parameter, e.Shader)
}

// ErrUniformSizeMismatch reports uniform bytes whose size is not the size the
// binding they name was reflected at. A param supplies a whole uniform, so the
// bytes are the binding: shorter leaves its tail unset and longer spills into
// nothing, and either is a Go value that does not mirror the WGSL one. The
// param is ignored.
type ErrUniformSizeMismatch struct {
	Shader    string
	Parameter string
	Supplied  int
	Declared  int
}

func (e ErrUniformSizeMismatch) Error() string {
	return fmt.Sprintf("gfx: shader %q declares the uniform %q at %d bytes, but the param supplies %d",
		e.Shader, e.Parameter, e.Declared, e.Supplied)
}

// ErrDrawParamTemporary reports a durable set handed a resource id the
// ResourceQueue did not mint: a temporary buffer, texture or target from an
// OpQueue, which lives one frame while the set outlives it. The param is
// ignored. A value that lives one frame belongs in the frame's version, through
// OpQueue.SetDrawParams.
type ErrDrawParamTemporary struct {
	Shader    string
	Parameter string
	// Call is the ResourceQueue method the param was handed to.
	Call string
}

func (e ErrDrawParamTemporary) Error() string {
	return fmt.Sprintf("gfx: %s gives shader %q's %q a resource the ResourceQueue did not create; a durable set names durable resources only - set a temporary through OpQueue.SetDrawParams",
		e.Call, e.Shader, e.Parameter)
}
