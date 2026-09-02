package gfx

import "fmt"

// ErrShaderNotFound is reported to the kernel when a draw's shader source is
// unavailable from storage.FileSystem.
type ErrShaderNotFound struct{ Name string }

func (e ErrShaderNotFound) Error() string {
	return fmt.Sprintf("gfx: shader source %q unavailable", e.Name)
}

// ErrBackendMissing is reported the first time a frame is rendered without an
// installed Backend. Without it nothing reaches the GPU, so it distinguishes a
// missing or failed driver from a scene that legitimately drew nothing.
type ErrBackendMissing struct{}

func (ErrBackendMissing) Error() string {
	return "gfx: no Backend installed; a driver must run SetBackendCmd before rendering"
}
