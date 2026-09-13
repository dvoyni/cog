package gfx

import (
	"fmt"
	"strconv"
	"strings"
)

// ErrShaderNotFound is reported to the kernel when a draw's shader source is
// unavailable from storage.FileSystem.
type ErrShaderNotFound struct{ Name string }

func (e ErrShaderNotFound) Error() string {
	return fmt.Sprintf("gfx: shader source %q unavailable", e.Name)
}

// ErrShaderExceedsWebLimits reports a shader that fits the device it is running
// on but not the WebGPU floor every browser guarantees. It is a portability
// report, not a failure: the shader is kept and the frame renders, because
// dropping a draw that works here would be the wrong trade for a warning about
// somewhere else.
type ErrShaderExceedsWebLimits struct {
	Shader   string
	Limit    string
	Declared int
	Floor    int
	Device   int
}

func (e ErrShaderExceedsWebLimits) Error() string {
	return fmt.Sprintf("gfx: shader %q declares %d %s; the web floor is %d (this device allows %d)",
		e.Shader, e.Declared, e.Limit, e.Floor, e.Device)
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

// ErrVertexInputUnsupplied reports a shader input no attribute of the bound
// vertex layout fills. The draw is dropped: WebGPU hands the input
// (0, 0, 0, 1) and the software rasterizer hands it a zero value, so what
// renders is wrong shading rather than an absence anyone would notice.
//
// It goes to the fatal error path rather than the web-limits diagnostic path.
// A diagnostic says "this renders here but would not on the web"; a vertex
// interface mismatch renders wrongly, everywhere.
type ErrVertexInputUnsupplied struct {
	Shader   string
	Input    string
	Location int
	Declared string // the WGSL type the shader declared
}

func (e ErrVertexInputUnsupplied) Error() string {
	return fmt.Sprintf("gfx: shader %q reads %s %s at @location(%d), which the bound vertex layout does not supply",
		e.Shader, e.Declared, e.Input, e.Location)
}

// ErrVertexInputMismatch reports a shader input the bound layout supplies at a
// different type. The draw is dropped, for the reason an unsupplied one is: the
// components that do not line up are filled in or dropped silently.
//
// Declared and Supplied are WGSL spellings rather than format names because the
// rule is over what a format decodes to, not over how its bytes are stored -
// Unorm8x4 and Float32x4 both supply vec4<f32> and both are legal under a
// vec4<f32> declaration.
type ErrVertexInputMismatch struct {
	Shader   string
	Input    string
	Location int
	Declared string
	Supplied string
}

func (e ErrVertexInputMismatch) Error() string {
	return fmt.Sprintf("gfx: shader %q reads %s %s at @location(%d), which the bound vertex layout supplies as %s",
		e.Shader, e.Declared, e.Input, e.Location, e.Supplied)
}

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

// ErrBackendMissing is reported the first time a frame is rendered without an
// installed Backend. Without it nothing reaches the GPU, so it distinguishes a
// missing or failed driver from a scene that legitimately drew nothing.
type ErrBackendMissing struct{}

func (ErrBackendMissing) Error() string {
	return "gfx: no Backend installed; a driver must run SetBackendCmd before rendering"
}

// ErrCaptureBusy reports a capture arm made while one is already live. It is
// refused rather than queued or coalesced: queueing turns a boolean into a
// queue for a fifty-millisecond window, and coalescing two waiters onto one
// frame founders on the fact that each names a different file.
//
// Both refusal sites raise it. gfx refuses a second arm synchronously, and the
// backend refuses a second in-flight map through GpuCapture.Err, because
// capture is a public gfx feature and a game's own code may arm one.
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
	return fmt.Sprintf("gfx: %s cannot be captured; a capture is 8-bit RGBA", formatName(e.Format))
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

// formatName names a texture format for a message. TextureFormat is a small
// closed enum with no String of its own, and giving it one would put a
// rendering of every member into gfx's public surface for one error.
func formatName(format TextureFormat) string {
	switch format.Resolve() {
	case FormatRGBA8:
		return "RGBA8"
	case FormatRGBA8Srgb:
		return "RGBA8 sRGB"
	case FormatDepth32F:
		return "depth"
	default:
		return fmt.Sprintf("texture format %d", format)
	}
}

// ErrShaderSource reports a shader the preprocessor refused, or one the backend
// refused after flattening.
//
// It is one type rather than the struct-per-failure gfx uses elsewhere. That
// convention earns its keep when a caller might branch on the failure, and
// nothing can recover from a shader that will not compile - so nothing ever
// will branch, and a type per directive plus a Kind enum would be pure surface.
type ErrShaderSource struct {
	Shader  string           // the label: "path [SUPPLY]"
	At      []ShaderLocation // where; may be empty
	Message string
	Err     error // the backend's error, when it was the backend that refused

	// flattened is the rendered segment table, present when the backend refused
	// a flattened module. No line number ever crosses the backend boundary as
	// data - gogpu returns an internal parse error type, so errors.As can never
	// recover a line, and gfx must not import a backend's parser in any case -
	// so gfx appends the table and lets the reader subtract.
	flattened string
}

func (e ErrShaderSource) Error() string {
	var b strings.Builder
	b.WriteString("gfx: shader ")
	b.WriteString(strconv.Quote(e.Shader))
	b.WriteString(": ")
	b.WriteString(e.Message)
	for i, at := range e.At {
		if i == 0 {
			b.WriteString(" (at ")
		} else {
			b.WriteString(", ")
		}
		b.WriteString(at.String())
	}
	if len(e.At) > 0 {
		b.WriteByte(')')
	}
	if e.Err != nil {
		b.WriteString(":\n")
		b.WriteString(e.Err.Error())
	}
	if e.flattened != "" {
		b.WriteString("\n  flattened: ")
		b.WriteString(e.flattened)
	}
	return b.String()
}

func (e ErrShaderSource) Unwrap() error { return e.Err }
