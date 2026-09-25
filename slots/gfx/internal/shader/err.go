package shader

import (
	"fmt"
	"strconv"
	"strings"
)

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

// ErrUniformBlockTooLarge reports a shader with a uniform block larger than the
// slot gfx binds for it on every draw. Block names the first such block. Unlike a web-floor report it is fatal
// to the shader: the module is freed and every draw through it is dropped,
// because what would otherwise render is the block cut to the slot, read partly
// from outside its binding, with nothing saying why.
//
// It is reported once, when the shader is reflected, not once a draw.
type ErrUniformBlockTooLarge struct {
	Shader   string
	Block    string
	Declared int
	Max      int
}

func (e ErrUniformBlockTooLarge) Error() string {
	return fmt.Sprintf("gfx: shader %q declares %d-byte uniform block %q; gfx binds %d bytes per block",
		e.Shader, e.Declared, e.Block, e.Max)
}
