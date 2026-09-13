package internal

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
