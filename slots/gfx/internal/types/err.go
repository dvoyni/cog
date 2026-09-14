package types

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
