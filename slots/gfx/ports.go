package gfx

import "github.com/dvoyni/cog/kernel"

// Backend is the low-level realization interface: a vendor-neutral,
// "wgpu-shaped" API that a driver (e.g. gogpu) implements. The gfx plugin holds
// one Backend and never imports a GPU library. All methods are called on the
// driver's render thread. Create methods mint an opaque handle synchronously.
type Backend interface {
	// NewTexture and NewBuffer reserve logical IDs. They are CPU-only and must be
	// safe to call from the recording thread; native objects are created lazily
	// when Execute processes the corresponding bake op.
	NewTexture() TextureID
	NewBuffer() BufferID

	NewSampler(SamplerDesc) (SamplerID, error)
	FreeSampler(id SamplerID)

	NewShader(ShaderDesc) (ShaderID, error)
	FreeShader(id ShaderID)
	// ShaderLayout returns the reflected uniform parameter layout of a shader.
	ShaderLayout(id ShaderID) ShaderLayout

	NewPipeline(PipelineDesc) (PipelineID, error)
	FreePipeline(id PipelineID)

	// ScreenFramebuffer returns the surface the present pass draws into and its
	// physical size, which is also the size the frame buffer is allocated at. It
	// is not what a screen pass renders into - that is the frame buffer - and it
	// is valid only while a frame is being rendered (the driver makes the
	// surface current before triggering the render).
	ScreenFramebuffer() (view TextureViewID, width, height int)

	// TextureView returns a renderable view of one mip level of one layer of a
	// baked texture, cached per (texture, mip, layer).
	TextureView(texture TextureID, mip, layer int) TextureViewID

	// TextureFormat reports the format a texture was allocated or baked in,
	// and whether the backend knows the texture at all. It is what keys a
	// pipeline to the pass it renders into: a pipeline declares its target's
	// format and the descriptor naming that target carries only an id.
	//
	// A texture is unknown until Execute replays the bake that allocates it, so
	// the frame that allocates a target cannot answer for it. gfx keys that
	// frame's pipelines to FrameBufferFormat and loses nothing by it:
	// TextureView above returns 0 on the same condition, leaving the pass with
	// no attachment to begin, so the pipeline keyed there never renders.
	TextureFormat(texture TextureID) (TextureFormat, bool)

	// Limits reports the device's own limits. gfx checks shaders against
	// DefaultLimits, the web floor, and never against these: they are here to
	// say, in the report, what the device this build ran on allowed.
	Limits() Limits

	// Execute replays one already-translated queue: it performs bake operations,
	// encodes each pass in turn with its own attachments and load/store ops,
	// submits once, then performs releases. queue is owned by the caller and
	// valid only for the duration of the call.
	Execute(queue *Queue)

	// TakeCapture returns a completed capture, if one is ready, and clears it.
	// gfx calls it once per frame immediately after Execute; a capture armed in
	// the previous frame is normally ready by the time the current frame's
	// submit has triaged it, so neither thread ever waits for a map.
	//
	// Its cost with nothing outstanding is a nil check. The returned value
	// carries either the mapped bytes or the reason there are none, so a caller
	// cannot handle a result and forget a failure.
	TakeCapture() (Capture, bool)

	// Ready reports whether the backend can render. A driver whose GPU device
	// arrives asynchronously provides its Backend at registration, before the
	// device exists, and reports false until it does. NewTexture and NewBuffer
	// are the only methods gfx calls on a backend that is not ready; a frame
	// rendered before it is ready is skipped. It must be safe to call from any
	// goroutine.
	Ready() bool
}

// BackendPort is the Port gfx requires exactly one Adapter for: the GPU backend
// a driver such as gogpu provides. A composition without one fails with
// kernel.ErrMissingAdapter.
type BackendPort kernel.RequiredPort[Backend]
