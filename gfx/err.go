package gfx

import "fmt"

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

// ErrBackendMissing is reported the first time a frame is rendered without an
// installed Backend. Without it nothing reaches the GPU, so it distinguishes a
// missing or failed driver from a scene that legitimately drew nothing.
type ErrBackendMissing struct{}

func (ErrBackendMissing) Error() string {
	return "gfx: no Backend installed; a driver must run SetBackendCmd before rendering"
}
