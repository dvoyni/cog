package gfx

import (
	"github.com/dvoyni/cog/slots/gfx/internal"
)

// ErrShaderExceedsWebLimits reports a shader that fits the device it is running
// on but not the WebGPU floor every browser guarantees. It is a portability
// report, not a failure: the shader is kept and the frame renders, because
// dropping a draw that works here would be the wrong trade for a warning about
// somewhere else.
type ErrShaderExceedsWebLimits = internal.ErrShaderExceedsWebLimits

// ErrUniformBlockTooLarge reports a shader whose uniform block is larger than
// the slot gfx binds for it on every draw. Unlike a web-floor report it is fatal
// to the shader: the module is freed and every draw through it is dropped,
// because what would otherwise render is the block cut to the slot, read partly
// from outside its binding, with nothing saying why.
//
// It is reported once, when the shader is reflected, not once a draw.
type ErrUniformBlockTooLarge = internal.ErrUniformBlockTooLarge

// ErrDrawWithoutPass is reported when a frame records draws before declaring a
// pass. There is no implicit pass to absorb them, so they are dropped: a draw
// with no pass has no target, no depth attachment and no place in the frame's
// order.
type ErrDrawWithoutPass = internal.ErrDrawWithoutPass

// ErrDrawSamplesAttachment is reported when a draw samples a texture its own
// pass renders into. The draw is dropped: reading a live attachment is
// undefined, and the frame is more useful with the mistake named than with one
// silently wrong draw in it.
type ErrDrawSamplesAttachment = internal.ErrDrawSamplesAttachment

// ErrParameterKindMismatch reports a parameter whose name matched a binding its
// kind cannot fill - a buffer supplied where the shader declared a uniform
// member, say, or a value where it declared a storage buffer.
//
// The draw is dropped. Parameters resolve by name and ignore kind, so without
// this the wrong descriptor is bound into the slot and the draw renders garbage
// with nothing reported anywhere; a missing storage binding is worse still,
// because the bind group is rejected and the draw encodes with no bindings at
// all. One name has one frequency, and naming it at two is an authoring error.
type ErrParameterKindMismatch = internal.ErrParameterKindMismatch

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
type ErrStorageBufferUnsupplied = internal.ErrStorageBufferUnsupplied

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
type ErrTextureViewDimensionMismatch = internal.ErrTextureViewDimensionMismatch

// ErrVertexInputUnsupplied reports a shader input no attribute of the bound
// vertex layout fills. The draw is dropped: WebGPU hands the input
// (0, 0, 0, 1) and the software rasterizer hands it a zero value, so what
// renders is wrong shading rather than an absence anyone would notice.
//
// It goes to the fatal error path rather than the web-limits diagnostic path.
// A diagnostic says "this renders here but would not on the web"; a vertex
// interface mismatch renders wrongly, everywhere.
type ErrVertexInputUnsupplied = internal.ErrVertexInputUnsupplied

// ErrVertexInputMismatch reports a shader input the bound layout supplies at a
// different type. The draw is dropped, for the reason an unsupplied one is: the
// components that do not line up are filled in or dropped silently.
//
// Declared and Supplied are WGSL spellings rather than format names because the
// rule is over what a format decodes to, not over how its bytes are stored -
// Unorm8x4 and Float32x4 both supply vec4<f32> and both are legal under a
// vec4<f32> declaration.
type ErrVertexInputMismatch = internal.ErrVertexInputMismatch

// ErrVertexStrideAlignment reports a vertex stride that is not a multiple of 4.
// WebGPU requires that unconditionally, and the platforms disagree about it:
// a 30-byte stride succeeds on Vulkan, Apple-silicon Metal and D3D12 and fails
// on js/wasm, on GLES and on older Apple GPUs. The draw is dropped here so that
// a Windows dev machine and a browser give the same answer.
type ErrVertexStrideAlignment = internal.ErrVertexStrideAlignment

// ErrCaptureBusy reports a capture arm made while one is already live. It is
// refused rather than queued or coalesced: queueing turns a boolean into a
// queue for a fifty-millisecond window, and coalescing two waiters onto one
// frame founders on the fact that each names a different file.
//
// Both refusal sites raise it. gfx refuses a second arm synchronously, and the
// backend refuses a second in-flight map through Capture.Err, because capture
// is a public gfx feature and a game's own code may arm one.
type ErrCaptureBusy = internal.ErrCaptureBusy

// ErrCaptureAbandoned reports a capture the engine stopped before its readback
// resolved. It travels the channel a result would have used, so that a waiter
// learns the answer rather than sitting until its own deadline.
type ErrCaptureAbandoned = internal.ErrCaptureAbandoned

// ErrCaptureUnsupported reports a target a capture cannot be an image of:
// depth, or any format that is not 8-bit RGBA. Depth readback is a real want
// and it is a visualization question rather than a readback one - a depth
// capture is a float field needing a range to be legible.
type ErrCaptureUnsupported = internal.ErrCaptureUnsupported

// ErrCaptureNoTarget reports a capture of something the frame never rendered
// into: a screen capture in a frame that drew nothing to the screen, or a
// texture that has no GPU object yet. It is distinct from an unsupported
// format because the answers differ - one says wait for a frame that draws,
// the other says this can never be an image.
type ErrCaptureNoTarget = internal.ErrCaptureNoTarget

// ErrPipelineFailed reports a pipeline the backend refused. gfx used to discard
// this diagnosis and return a zero id, which from the caller's seat made "gfx
// refused to build this" and "the backend refused to build this" the same
// silent event.
type ErrPipelineFailed = internal.ErrPipelineFailed

// ErrIndexBufferLength reports an index buffer whose byte length is not a
// multiple of the width the mesh declared it at. With two widths in the engine
// a buffer declared uint16 and written as uint32 is a third way to be silently
// wrong, and this is the O(1) half of catching it: the range check - that every
// index is below the vertex count - stays in scene, where a pass over the
// indices already runs.
//
// The draw is dropped, and the report is made once per offending buffer rather
// than once a frame, the way a failed pipeline is.
type ErrIndexBufferLength = internal.ErrIndexBufferLength

// ErrBackendNotReady is reported the first time a frame is rendered before the
// Backend adapter is ready. Nothing reaches the GPU until it is, so it
// distinguishes a driver whose device never arrived from a scene that
// legitimately drew nothing.
type ErrBackendNotReady = internal.ErrBackendNotReady

// ErrCaptureAmount reports a burst asking for more stills than one request may
// take. A longer window is bought with the interval, never with the amount.
type ErrCaptureAmount = internal.ErrCaptureAmount

// ErrCaptureSpan reports a burst whose stills would span more ticks than one
// request may cover. It is the same figure as a time step's cap, because a
// request that outlives its caller is bounded the same way either side.
type ErrCaptureSpan = internal.ErrCaptureSpan

// ErrCaptureBurstPaused reports a burst armed against a stopped tick source.
// No new ticks means N byte-identical files, and a silent pile of duplicates is
// exactly the failure a debug facility must not have. A single capture while
// paused stays legal, and costs no tick.
type ErrCaptureBurstPaused = internal.ErrCaptureBurstPaused

// ErrFrameBusy reports a frame-snapshot arm made while one is already live.
// It is refused rather than queued, for the reason a second capture is: the
// window is one tick and the retry is one call.
//
// It is deliberately its own kind. A capture, a frame snapshot and the other
// packages' snapshots are separate slots and may be in flight together -
// refusing across kinds would destroy the one thing arming them together is
// for, which is describing a single tick.
type ErrFrameBusy = internal.ErrFrameBusy

// ErrFrameAbandoned reports a frame snapshot the engine stopped before a tick
// completed. It travels the channel a result would have used, so that a
// waiter learns the answer rather than sitting until its own deadline.
type ErrFrameAbandoned = internal.ErrFrameAbandoned

// ErrShaderSource reports a shader the preprocessor refused, or one the backend
// refused after flattening.
//
// It is one type rather than the struct-per-failure gfx uses elsewhere. That
// convention earns its keep when a caller might branch on the failure, and
// nothing can recover from a shader that will not compile - so nothing ever
// will branch, and a type per directive plus a Kind enum would be pure surface.
type ErrShaderSource = internal.ErrShaderSource
