package gfx

import (
	"github.com/dvoyni/cog/slots/gfx/internal"
	gfxmcp "github.com/dvoyni/cog/slots/gfx/internal/mcp"
)

// PresentCmd finalizes the writable OpQueue: it swaps it into the internal ready
// slot (dropping any still-unconsumed queue, latest-wins) and installs a reset
// queue for further recording. The plugin also runs this last on app.UpdateEvent.
type PresentCmd = internal.PresentCmd

type PresentRequest = internal.PresentRequest

type PresentResponse = internal.PresentResponse

// AcquireCmd advances the internal read queue to the latest completed queue if
// one is pending, otherwise leaves it unchanged.
type AcquireCmd = internal.AcquireCmd

type AcquireRequest = internal.AcquireRequest

type AcquireResponse = internal.AcquireResponse

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
type ReleaseCachedResourceCmd = internal.ReleaseCachedResourceCmd

type ReleaseCachedResourceRequest = internal.ReleaseCachedResourceRequest

type ReleaseCachedResourceResponse = internal.ReleaseCachedResourceResponse

// FreeCachedResourcesCmd queues release of every translator-owned texture,
// pipeline and sampler. Explicit resources - the shaders, sets, textures and
// buffers the ResourceQueue made - remain caller-owned and are not released;
// a pipeline is built again by the next draw that needs it.
type FreeCachedResourcesCmd = internal.FreeCachedResourcesCmd

type FreeCachedResourcesRequest = internal.FreeCachedResourcesRequest

type FreeCachedResourcesResponse = internal.FreeCachedResourcesResponse

// SetViewportCmd updates the Viewport resource with the current render target
// size. A driver calls it when the size changes.
type SetViewportCmd = internal.SetViewportCmd

// SetViewportRequest is the request for SetViewportCmd: the window size in
// device-independent pixels plus the physical framebuffer size.
type SetViewportRequest = internal.SetViewportRequest

// SetViewportResponse reports the resolved logical and window dimensions.
type SetViewportResponse = internal.SetViewportResponse

// SetDesiredViewportCmd selects the logical world-size policy. A game normally
// calls it once during initialization; the handler resolves it against each
// physical window size supplied through SetViewportCmd.
type SetDesiredViewportCmd = internal.SetDesiredViewportCmd

// SetDesiredViewportRequest selects the logical viewport policy. Size is used by
// ViewportFixedWidth and ViewportFixedHeight. Width and Height define the desired
// rectangle for ViewportFit and ViewportCover. Invalid values fall back to
// ViewportWindow.
type SetDesiredViewportRequest = internal.SetDesiredViewportRequest

// SetDesiredViewportResponse reports the viewport resolved against the most
// recently supplied window size.
type SetDesiredViewportResponse = internal.SetDesiredViewportResponse

// ArmCaptureCmd arms a readback of one colour target and hands back the wait.
// It is ordinary gfx API: anything holding a kernel handle may arm a capture,
// and the agent-facing capability is one caller among them.
//
// The response's channel is the only delivery path, and refusals travel it too,
// because Capture carries Err. The alternative - a channel passed in with
// the request - leaves gfx unable to refuse a second arm synchronously.
type ArmCaptureCmd = gfxmcp.ArmCaptureCmd

// ArmCaptureRequest names the target and how many stills to take of it.
type ArmCaptureRequest = gfxmcp.ArmCaptureRequest

// ArmCaptureResponse hands back the wait and the window size.
type ArmCaptureResponse = gfxmcp.ArmCaptureResponse

// ArmFrameCmd arms one frame snapshot and hands back the wait. It is ordinary
// gfx API: anything holding a kernel handle may ask what the renderer was told
// to do for a tick, and the agent-facing capability is one caller among them.
//
// The response's channel is the only delivery path, and refusals travel it too,
// because FrameSnapshot carries Err - the same shape ArmCaptureCmd uses, for
// the same reason: a channel passed in with the request would leave gfx unable
// to refuse a second arm synchronously.
type ArmFrameCmd = gfxmcp.ArmFrameCmd

// ArmFrameRequest carries the filter, because the filter is what bounds the
// work done inside the tick. Nothing about the output is decided before the
// request is known.
type ArmFrameRequest = gfxmcp.ArmFrameRequest

// ArmFrameResponse hands back the wait and the viewport.
type ArmFrameResponse = gfxmcp.ArmFrameResponse

// CompileShaderCmd compiles one shader descriptor on the CPU: it flattens the
// descriptor through the preprocessor, reflects the result through the
// Backend's reflection port, and builds the binding table by WGSL global name.
// The ShaderProgram it returns is pure data, which ResourceQueue.UploadProgram
// hands to a shader ResourceQueue.NewShader reserved.
//
// Its lock is empty, so a System declaring Uses[CompileShaderCmd] widens its
// lock set by nothing and the kernel runs it on the caller's goroutine. It is
// the one gfx call whose failure is a normal outcome - a missing file, an
// include that does not resolve, WGSL that does not parse - so the failure is
// the response's Err and is never also reported.
type CompileShaderCmd = internal.CompileShaderCmd

// CompileShaderRequest names the shader and the filesystem its root and
// includes are read from - ordinarily storage.FileSystem, which the caller
// declares its own read of.
type CompileShaderRequest = internal.CompileShaderRequest

// CompileShaderResponse carries the program, or the reason there is none. A
// failed compile leaves Program the zero value, which UploadProgram refuses.
type CompileShaderResponse = internal.CompileShaderResponse

// ShaderCompiler is how a library that compiles shaders takes the compile: a
// plain func, so the library names neither the kernel's dispatcher nor the ECS.
// A System passes the Execute method of its
// ecs.Uses[CompileShaderCmd, CompileShaderRequest, CompileShaderResponse], a
// plain handler passes what ResourceAccess.Uses[CompileShaderCmd] returns, and
// a test passes a stub. Whoever holds it declared the use, so the lock set that
// covers the compile is the caller's, and CompileShaderCmd's is empty.
type ShaderCompiler = internal.ShaderCompiler
