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

// ArmCaptureCmd arms a readback of one colour target and hands back the wait.
// It is ordinary gfx API: anything holding a kernel handle may arm a capture,
// and the agent-facing capability is one caller among them.
//
// The response's channel is the only delivery path, and refusals travel it too,
// because Capture carries Err. The alternative - a channel passed in with
// the request - leaves gfx unable to refuse a second arm synchronously.
type ArmCaptureCmd kernel.Command[ArmCaptureRequest, ArmCaptureResponse]

// ArmCaptureRequest names the target and how many stills to take of it.
type ArmCaptureRequest struct {
	// Target is what to read back: the frame buffer, or any colour texture the
	// frame rendered into.
	Target types.CaptureDesc
	// Amount is how many stills to write; zero means one, and the maximum is
	// sixty.
	Amount int
	// Interval is how many ticks apart the stills are; zero means one.
	// Amount x Interval may not exceed six hundred ticks.
	Interval int
	// Paused says the caller knows the engine's tick source is stopped, so no
	// tick can begin after this request and the last completed tick already is
	// the present. The handler does not read the tick source itself: the
	// caller asks app with app.TimeCmd, as gfx_capture does, and says what it
	// was told.
	//
	// A paused capture is served from the next render and costs no tick, which
	// is what makes two captures taken under one pause byte-identical. A burst
	// is refused while paused, because there would be nothing new to photograph.
	Paused bool
}

// ArmCaptureResponse hands back the wait and the window size.
type ArmCaptureResponse struct {
	// Done receives one Capture per still, in order, and is buffered to
	// Amount so the render thread never blocks on a caller that walked away.
	Done <-chan Capture
	// Viewport is the window as of the arm, which a capability body cannot read
	// for itself. The pixel dimensions always come from the capture, so a
	// window resized inside the capture's two-frame window reports a stale
	// window size but never mis-describes the image.
	Viewport types.Viewport
	// Err is the outcome an arm was refused with: a request the capture state
	// would not take, or one that arrived while another was already armed.
	Err error
}

// ArmFrameCmd arms one frame snapshot and hands back the wait. It is ordinary
// gfx API: anything holding a kernel handle may ask what the renderer was told
// to do for a tick, and the agent-facing capability is one caller among them.
//
// The response's channel is the only delivery path, and refusals travel it too,
// because FrameSnapshot carries Err - the same shape ArmCaptureCmd uses, for
// the same reason: a channel passed in with the request would leave gfx unable
// to refuse a second arm synchronously.
type ArmFrameCmd kernel.Command[ArmFrameRequest, ArmFrameResponse]

// ArmFrameRequest carries the filter, because the filter is what bounds the
// work done inside the tick. Nothing about the output is decided before the
// request is known.
type ArmFrameRequest struct {
	// Pass, when set, keeps only the passes whose Label is exactly it. Passes
	// it drops are counted in the result rather than silently missing, and
	// every pass keeps its own declaration index, so an index read off a
	// filtered snapshot still addresses the same pass in an unfiltered one.
	Pass string
}

// ArmFrameResponse hands back the wait and the viewport.
type ArmFrameResponse struct {
	// Done receives exactly one FrameSnapshot and is buffered, so the game's
	// own goroutine never blocks on a caller that walked away.
	Done <-chan FrameSnapshot
	// Viewport is the window as of the arm, which a capability body cannot
	// read for itself. A resize between the arm and the tick it binds to is a
	// stated non-guarantee, exactly as it is for a capture.
	Viewport types.Viewport
	// Err is the outcome an arm was refused with, exactly as it is for a
	// capture.
	Err error
}
