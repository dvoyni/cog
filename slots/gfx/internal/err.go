package internal

import (
	"fmt"
)

// ErrCaptureUnsupported reports a target a capture cannot be an image of:
// depth, or any format that is not 8-bit RGBA. Depth readback is a real want
// and it is a visualization question rather than a readback one - a depth
// capture is a float field needing a range to be legible.
type ErrCaptureUnsupported struct{ Format TextureFormat }

func (e ErrCaptureUnsupported) Error() string {
	return fmt.Sprintf("gfx: %s cannot be captured; a capture is 8-bit RGBA", e.Format.String())
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
