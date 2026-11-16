package gfx

import (
	"errors"
	"strconv"
	"strings"
)

// ErrReleased is answered for a handle whose resource was released.
var ErrReleased = errors.New("gfx: resource released")

// ErrOrderTaken is answered by Device.Encoder for a submit order already
// handed out in the same frame.
var ErrOrderTaken = errors.New("gfx: submit order already taken in this frame")

// ErrFrameFlushed is answered for a Frame whose frame was already flushed.
var ErrFrameFlushed = errors.New("gfx: frame already flushed")

// ErrDeviceLost is answered, from any call that answers an error but
// PreprocessShader, once the device is lost and until its recovery.
var ErrDeviceLost = errors.New("gfx: device lost")

// ErrCaptureBusy is answered by Device.RequestCapture for a window whose
// capture is still in flight.
var ErrCaptureBusy = errors.New("gfx: a capture of this window is in flight")

// ErrCaptureUnsupported is answered by Device.RequestCapture for a window
// whose surface is not yet configured, or whose format is not 8-bit BGRA or
// RGBA.
var ErrCaptureUnsupported = errors.New("gfx: this window cannot be captured")

// ErrVertexMismatch is answered by Device.CreateRenderPipeline for vertex
// buffers that do not feed the vertex shader's inputs: an input with no
// attribute at its location, or one whose format is of another type.
var ErrVertexMismatch = errors.New("gfx: the vertex buffers do not match the shader's inputs")

// ErrLimitExceeded is answered by the pipeline creations for a pipeline past
// the device's limits: its layout, or a render pipeline's vertex buffers.
var ErrLimitExceeded = errors.New("gfx: past the device's limits")

// ErrQuerySetsUnavailable is answered by Device.CreateQuerySet on every
// backend until the wgpu fork exposes query sets.
var ErrQuerySetsUnavailable = errors.New("gfx: query sets are unavailable until the wgpu fork exposes them")

// ShaderSourceLocation names one line of one shader source: its path in the
// filesystem preprocessed, and its line, from 1. The zero location is the
// defines given, which have no line.
type ShaderSourceLocation struct {
	Source string
	Line   int
}

// String answers "source:line", or "the defines" for the zero location.
func (l ShaderSourceLocation) String() string {
	if l == (ShaderSourceLocation{}) {
		return "the defines"
	}
	return l.Source + ":" + strconv.Itoa(l.Line)
}

// ErrShaderSource is answered by Device.PreprocessShader for a shader the
// backend refused. Shader names it: its path, followed by its defines in
// brackets, sorted by name, as "scene.wgsl [N=8 SKIN]". At is where, the
// offending line first: one line, or the chain of #include lines down to it,
// or the line a declaration conflicts with. It is empty for a define with no
// legal spelling.
type ErrShaderSource struct {
	Shader  string
	At      []ShaderSourceLocation
	Message string
}

// Error reads as one line, naming the shader and every location.
func (e ErrShaderSource) Error() string {
	var builder strings.Builder
	builder.WriteString("shader: ")
	builder.WriteString(strconv.Quote(e.Shader))
	builder.WriteString(": ")
	builder.WriteString(e.Message)
	for index, at := range e.At {
		if index == 0 {
			builder.WriteString(" (at ")
		} else {
			builder.WriteString(", ")
		}
		builder.WriteString(at.String())
	}
	if len(e.At) > 0 {
		builder.WriteByte(')')
	}
	return builder.String()
}
