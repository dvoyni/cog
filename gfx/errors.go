package gfx

import "errors"

// ErrReleased is answered for a handle whose resource was released.
var ErrReleased = errors.New("gfx: resource released")

// ErrOrderTaken is answered by Device.Encoder for a submit order already
// handed out in the same frame.
var ErrOrderTaken = errors.New("gfx: submit order already taken in this frame")

// ErrFrameFlushed is answered for a Frame whose frame was already flushed.
var ErrFrameFlushed = errors.New("gfx: frame already flushed")
