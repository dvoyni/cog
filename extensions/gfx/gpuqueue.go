package gfx

import "github.com/dvoyni/cog/extensions/gfx/internal"

// GpuQueue owns a translated command sequence. Commands are constructed as
// local values and appended once. Bakes are hoisted ahead of every pass; render
// commands belong to the pass that was open when they were recorded.
type GpuQueue = internal.GpuQueue

// TextureUsage names the role a texture is in as far as the GPU's memory
// pipeline is concerned. It is deliberately just the two roles gfx can put a
// texture in, not a mirror of the backend's usage flags.
type TextureUsage = internal.TextureUsage

const (
	// TextureUsageRenderAttachment is a texture being written as a pass's
	// colour or depth attachment.
	TextureUsageRenderAttachment = internal.TextureUsageRenderAttachment
	// TextureUsageTextureBinding is a texture being read by a shader.
	TextureUsageTextureBinding = internal.TextureUsageTextureBinding
	// TextureUsageCopySrc is a texture being read back into CPU-visible
	// memory. It is the third role rather than a reuse of the other two
	// because From must name the usage a texture is actually in: a layout
	// transition that names the wrong old layout is undefined behaviour, and
	// a backend silently inserting an unnamed barrier for a capture is
	// precisely the undeclared hazard this type exists to abolish.
	TextureUsageCopySrc = internal.TextureUsageCopySrc
)

// TextureTransition orders one texture's writes against its reads, or the other
// way round. The backend derives none of these itself: it tracks resources for
// lifetime and submit-time validation only, so a texture written as an
// attachment and then sampled is not ordered against those writes - not within
// one command encoder, and not across a submit boundary either. On Vulkan the
// sample then reads the image mid-write, which shows as a flicker that looks
// random, is not a CPU/GPU race, and is invisible to a capture taken while the
// app is redrawing.
//
// gfx is the layer that can see the hazard, because by translation time the
// frame's passes are sorted and merged and the write-then-read pairs are
// computable. From is the usage the texture is actually in, not a guess: a
// layout transition that names the wrong old layout is undefined behaviour.
type TextureTransition = internal.TextureTransition

// GpuPassDesc is one render pass for the backend to encode. Screen selects the
// frame buffer, which only the backend can resolve because it is sized from the
// surface; Target names any other colour attachment, and zero means none.
type GpuPassDesc = internal.GpuPassDesc

// GpuCaptureDesc names one colour target to read back. Screen selects the frame
// buffer, which only the backend can resolve; Texture names any other colour
// texture, and zero means none. It mirrors GpuPassDesc's addressing exactly.
//
// A capture always reads mip 0, layer 0. Texture is a TextureID rather than a
// TextureViewID because a texture-to-buffer copy names a texture, and because
// TextureTransition.Texture already names one.
type GpuCaptureDesc = internal.GpuCaptureDesc

// GpuPassSink receives the frame's passes. BeginPass returns the RenderPass its
// commands go to, so the backend owns encoder and pass lifetime entirely.
type GpuPassSink = internal.GpuPassSink

// GpuBakeSink receives resource uploads before render-pass encoding.
type GpuBakeSink = internal.GpuBakeSink

// RenderPass receives render commands in recording order.
type RenderPass = internal.RenderPass

// GpuReleaseSink receives resource releases after submission.
type GpuReleaseSink = internal.GpuReleaseSink
