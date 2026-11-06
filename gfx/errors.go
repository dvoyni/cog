package gfx

import "errors"

// ErrReleased is answered for a handle whose resource was released.
var ErrReleased = errors.New("gfx: resource released")

// ErrOrderTaken is answered by Device.Encoder for a submit order already
// handed out in the same frame.
var ErrOrderTaken = errors.New("gfx: submit order already taken in this frame")

// ErrFrameFlushed is answered for a Frame whose frame was already flushed.
var ErrFrameFlushed = errors.New("gfx: frame already flushed")

// ErrDeviceLost is answered, from any call that answers an error, once the
// device is lost and until its recovery.
var ErrDeviceLost = errors.New("gfx: device lost")

// ErrCaptureBusy is answered by Device.RequestCapture for a window whose
// capture is still in flight.
var ErrCaptureBusy = errors.New("gfx: a capture of this window is in flight")

// ErrCaptureUnsupported is answered by Device.RequestCapture for a window
// whose surface is not capturable, or whose format is not 8-bit BGRA or RGBA.
var ErrCaptureUnsupported = errors.New("gfx: this window cannot be captured")

// ErrVertexMismatch is answered by Device.CreateRenderPipeline for vertex
// buffers that do not feed the vertex shader's inputs: an input with no
// attribute at its location, or one whose format is of another type.
var ErrVertexMismatch = errors.New("gfx: the vertex buffers do not match the shader's inputs")

// ErrLimitExceeded is answered by the pipeline creations for a pipeline past
// the device's limits: its layout, or a render pipeline's vertex buffers.
var ErrLimitExceeded = errors.New("gfx: past the device's limits")
