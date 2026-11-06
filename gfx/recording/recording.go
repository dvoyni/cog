package recording

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/platform"
)

// AdapterInfo is what the recording adapter answers for Info.
var AdapterInfo = gfx.AdapterInfo{
	Name:       "recording",
	Vendor:     "cog",
	DeviceType: gfx.DeviceTypeCPU,
	Backend:    gfx.BackendEmpty,
}

// CallKind names a kind of backend call, for AnswerNext and for the calls
// a Recording holds.
type CallKind uint16

const (
	// CallKindRequestAdapter is InstanceSocket.RequestAdapter, its Arguments
	// the gfx.RequestAdapterOptions.
	CallKindRequestAdapter CallKind = iota
	// CallKindRequestDevice is BackendAdapter.RequestDevice, its Arguments
	// the gfx.DeviceDescriptor.
	CallKindRequestDevice
	// CallKindWaitIdle is BackendDevice.WaitIdle, with no Arguments.
	CallKindWaitIdle
	// CallKindReleaseDevice is BackendDevice.Release, with no Arguments.
	CallKindReleaseDevice
	// CallKindReleaseAdapter is BackendAdapter.Release, with no Arguments.
	CallKindReleaseAdapter
	// CallKindReleaseInstance is InstanceSocket.Release, with no Arguments.
	CallKindReleaseInstance
	// CallKindCreateBuffer is BackendDevice.CreateBuffer, its Arguments a
	// CreateBufferArguments.
	CallKindCreateBuffer
	// CallKindReleaseBuffer is BackendDevice.ReleaseBuffer, its Arguments the
	// gfx.Buffer.
	CallKindReleaseBuffer
	// CallKindCreateTexture is BackendDevice.CreateTexture, its Arguments a
	// CreateTextureArguments.
	CallKindCreateTexture
	// CallKindReleaseTexture is BackendDevice.ReleaseTexture, its Arguments
	// the gfx.Texture.
	CallKindReleaseTexture
	// CallKindCreateTextureView is BackendDevice.CreateTextureView, its
	// Arguments a CreateTextureViewArguments.
	CallKindCreateTextureView
	// CallKindReleaseTextureView is BackendDevice.ReleaseTextureView, its
	// Arguments the gfx.TextureView.
	CallKindReleaseTextureView
	// CallKindCreateSampler is BackendDevice.CreateSampler, its Arguments a
	// CreateSamplerArguments.
	CallKindCreateSampler
	// CallKindReleaseSampler is BackendDevice.ReleaseSampler, its Arguments the
	// gfx.Sampler.
	CallKindReleaseSampler
	// CallKindCreateShaderModule is BackendDevice.CreateShaderModule, its
	// Arguments a CreateShaderModuleArguments.
	CallKindCreateShaderModule
	// CallKindReleaseShaderModule is BackendDevice.ReleaseShaderModule, its
	// Arguments the gfx.ShaderModule.
	CallKindReleaseShaderModule
	// CallKindCreateBindGroupLayout is BackendDevice.CreateBindGroupLayout, its
	// Arguments a CreateBindGroupLayoutArguments.
	CallKindCreateBindGroupLayout
	// CallKindReleaseBindGroupLayout is BackendDevice.ReleaseBindGroupLayout,
	// its Arguments the gfx.BindGroupLayout.
	CallKindReleaseBindGroupLayout
	// CallKindCreateBindGroup is BackendDevice.CreateBindGroup, its Arguments a
	// CreateBindGroupArguments.
	CallKindCreateBindGroup
	// CallKindReleaseBindGroup is BackendDevice.ReleaseBindGroup, its Arguments
	// the gfx.BindGroup.
	CallKindReleaseBindGroup
	// CallKindCreatePipelineLayout is BackendDevice.CreatePipelineLayout, its
	// Arguments a CreatePipelineLayoutArguments.
	CallKindCreatePipelineLayout
	// CallKindReleasePipelineLayout is BackendDevice.ReleasePipelineLayout, its
	// Arguments the gfx.PipelineLayout.
	CallKindReleasePipelineLayout
	// CallKindCreateRenderPipeline is BackendDevice.CreateRenderPipeline, its
	// Arguments a CreateRenderPipelineArguments.
	CallKindCreateRenderPipeline
	// CallKindReleaseRenderPipeline is BackendDevice.ReleaseRenderPipeline, its
	// Arguments the gfx.RenderPipeline.
	CallKindReleaseRenderPipeline
	// CallKindCreateComputePipeline is BackendDevice.CreateComputePipeline, its
	// Arguments a CreateComputePipelineArguments.
	CallKindCreateComputePipeline
	// CallKindReleaseComputePipeline is BackendDevice.ReleaseComputePipeline,
	// its Arguments the gfx.ComputePipeline.
	CallKindReleaseComputePipeline
)

// Answer is what AnswerNext makes a call answer: Status for an acquire,
// Error for any call.
type Answer struct {
	Status uint8
	Error  error
}

// Recording is every call that reached the adapter, as typed values. The
// order of Submissions and of the surface calls is deterministic, since gfx
// sorts and chains them; the order of Calls from parallel tasks is not, so a
// test looks them up by handle.
type Recording struct {
	// Calls are the lifecycle, write, map and surface calls, each with its
	// handle and a deep copy of its descriptor.
	Calls       []Call
	Submissions []Submission
}

// Frames answers the submissions and surface calls between each acquire and
// its present.
func (recording *Recording) Frames() []Frame { panic("gfx listing: no bodies") }

// Call is one recorded call outside command buffers.
type Call struct {
	Kind CallKind
	// Arguments is the call's handle and descriptor, as the gfx types.
	Arguments any
	Error     error
}

// CreateBufferArguments are what CreateBuffer was called with.
type CreateBufferArguments struct {
	Buffer     gfx.Buffer
	Descriptor gfx.BufferDescriptor
}

// CreateTextureArguments are what CreateTexture was called with, the
// descriptor's ViewFormats copied.
type CreateTextureArguments struct {
	Texture    gfx.Texture
	Descriptor gfx.TextureDescriptor
}

// CreateTextureViewArguments are what CreateTextureView was called with.
type CreateTextureViewArguments struct {
	View       gfx.TextureView
	Texture    gfx.Texture
	Descriptor gfx.TextureViewDescriptor
}

// CreateSamplerArguments are what CreateSampler was called with.
type CreateSamplerArguments struct {
	Sampler    gfx.Sampler
	Descriptor gfx.SamplerDescriptor
}

// CreateShaderModuleArguments are what CreateShaderModule was called with.
type CreateShaderModuleArguments struct {
	Module     gfx.ShaderModule
	Descriptor gfx.ShaderModuleDescriptor
}

// CreateBindGroupLayoutArguments are what CreateBindGroupLayout was called
// with.
type CreateBindGroupLayoutArguments struct {
	Layout     gfx.BindGroupLayout
	Descriptor gfx.BindGroupLayoutDescriptor
}

// CreateBindGroupArguments are what CreateBindGroup was called with.
type CreateBindGroupArguments struct {
	Group      gfx.BindGroup
	Descriptor gfx.BindGroupDescriptor
}

// CreatePipelineLayoutArguments are what CreatePipelineLayout was called
// with.
type CreatePipelineLayoutArguments struct {
	Layout     gfx.PipelineLayout
	Descriptor gfx.PipelineLayoutDescriptor
}

// CreateRenderPipelineArguments are what CreateRenderPipeline was called
// with.
type CreateRenderPipelineArguments struct {
	Pipeline   gfx.RenderPipeline
	Descriptor gfx.RenderPipelineDescriptor
}

// CreateComputePipelineArguments are what CreateComputePipeline was called
// with.
type CreateComputePipelineArguments struct {
	Pipeline   gfx.ComputePipeline
	Descriptor gfx.ComputePipelineDescriptor
}

// Submission is one submit's command buffers, in the order submitted.
type Submission struct {
	Buffers []CommandBuffer
}

// CommandBuffer is one finished encoder: its passes, and the copies and
// clears recorded outside them, in order.
type CommandBuffer struct {
	Commands []Command
}

// Command is one recorded encoder or pass command with its arguments; a
// pass's commands are under its BeginRenderPass or BeginComputePass.
type Command struct {
	Name      string
	Arguments any
	Commands  []Command
}

// Frame is one frame as the adapter saw it: each window's acquire status, the
// submissions, and the presents.
type Frame struct {
	Acquired    map[platform.Window]uint8
	Submissions []Submission
	Presented   []platform.Window
}
