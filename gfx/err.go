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
