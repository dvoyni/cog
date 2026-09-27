package internal

import (
	"io/fs"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/gfx/internal/shader"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// CompileShaderCmd compiles one shader descriptor on the CPU: it flattens the
// descriptor through the preprocessor, reflects the result through the Backend's
// reflection port, and builds the binding table by WGSL global name. What it
// returns is a ShaderProgram - pure data, which ResourceQueue.UploadProgram
// then hands to a shader NewShader reserved.
//
// Its lock is empty. It reads only the filesystem the request carries, which
// the caller's System holds its own read of, and the reflection port is pure
// and safe from any thread, so the kernel runs it on the caller's goroutine with
// no coordinator round-trip: a System that declares Uses[CompileShaderCmd]
// widens its lock set by nothing and loses no parallelism, however long a naga
// parse takes.
//
// It is the one gfx call whose failure is a normal outcome - a missing file, an
// include that does not resolve, WGSL that does not parse - so the failure is
// the response's Err and is never also reported. The caller decides what a
// missing shader means.
type CompileShaderCmd kernel.Command[CompileShaderRequest, CompileShaderResponse]

// CompileShaderRequest names the shader and the filesystem its root and
// includes are read from - ordinarily storage.FileSystem, which the caller
// declares its own read of.
type CompileShaderRequest struct {
	FS    fs.FS
	Descr shader.ShaderDescr
}

// CompileShaderResponse carries the program, or the reason there is none. A
// failed compile leaves Program the zero value, which UploadProgram refuses.
type CompileShaderResponse struct {
	Program shader.ShaderProgram
	Err     error
}

// ShaderCompiler is how a library that compiles shaders takes the compile: a
// plain func, so the library names neither the kernel's dispatcher nor the ECS.
// A System passes the Execute method of its
// ecs.Uses[CompileShaderCmd, CompileShaderRequest, CompileShaderResponse], a
// plain handler passes what ResourceAccess.Uses[CompileShaderCmd] returns, and
// a test passes a stub. Whoever holds it declared the use, so the lock set that
// covers the compile is the caller's, and CompileShaderCmd's is empty.
type ShaderCompiler = func(kernel.Kernel, CompileShaderRequest) CompileShaderResponse

// PresentCmd finalizes the writable OpQueue: it swaps it into the internal ready
// slot (dropping any still-unconsumed queue, latest-wins) and installs a reset
// queue for further recording. The plugin also runs this last on app.UpdateEvent.
type PresentCmd kernel.Command[PresentRequest, PresentResponse]

type PresentRequest struct{}

type PresentResponse struct{}

// AcquireCmd advances the internal read queue to the latest completed queue if
// one is pending, otherwise leaves it unchanged.
type AcquireCmd kernel.Command[AcquireRequest, AcquireResponse]

type AcquireRequest struct{}

type AcquireResponse struct{ Advanced bool }

// ReleaseCachedResourceCmd queues release of the translator-owned texture
// cached for Path. Cleanup runs on the render thread before the latest frame; a
// later use of the path loads it again. A shader is not a cache entry: it is the
// caller's, created and released through the ResourceQueue, so reloading one is
// releasing it and compiling it again.
//
// It is also the only retry there is. A read that failed is cached as failed and
// reported once, so a path whose file was missing stays missing as far as gfx is
// concerned until this command drops the entry - which also forgets the report,
// so the next attempt can speak again.
type ReleaseCachedResourceCmd kernel.Command[ReleaseCachedResourceRequest, ReleaseCachedResourceResponse]

type ReleaseCachedResourceRequest struct{ Path string }

type ReleaseCachedResourceResponse struct{}

// FreeCachedResourcesCmd queues release of every translator-owned texture,
// pipeline and sampler. Explicit resources - the shaders, sets, textures and
// buffers the ResourceQueue made - remain caller-owned and are not released;
// a pipeline is built again by the next draw that needs it.
type FreeCachedResourcesCmd kernel.Command[FreeCachedResourcesRequest, FreeCachedResourcesResponse]

type FreeCachedResourcesRequest struct{}

type FreeCachedResourcesResponse struct{}

// SetViewportCmd updates the Viewport resource with the current render target
// size. A driver calls it when the size changes.
type SetViewportCmd kernel.Command[SetViewportRequest, SetViewportResponse]

// SetViewportRequest is the request for SetViewportCmd: the window size in
// device-independent pixels plus the physical framebuffer size.
type SetViewportRequest struct {
	Width, Height                       float32
	FramebufferWidth, FramebufferHeight float32
}

// SetViewportResponse reports the resolved logical and window dimensions.
type SetViewportResponse struct{ Viewport types.Viewport }

// SetDesiredViewportCmd selects the logical world-size policy. A game normally
// calls it once during initialization; the handler resolves it against each
// physical window size supplied through SetViewportCmd.
type SetDesiredViewportCmd kernel.Command[SetDesiredViewportRequest, SetDesiredViewportResponse]

// SetDesiredViewportRequest selects the logical viewport policy. Size is used by
// ViewportFixedWidth and ViewportFixedHeight. Width and Height define the desired
// rectangle for ViewportFit and ViewportCover. Invalid values fall back to
// ViewportWindow.
type SetDesiredViewportRequest struct {
	Mode          types.ViewportMode
	Width, Height float32
	Size          float32
}

// SetDesiredViewportResponse reports the viewport resolved against the most
// recently supplied window size.
type SetDesiredViewportResponse struct{ Viewport types.Viewport }
