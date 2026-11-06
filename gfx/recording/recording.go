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
	// CallKindCreateSurface is InstanceSocket.CreateSurface, its Arguments a
	// CreateSurfaceArguments.
	CallKindCreateSurface
	// CallKindReleaseSurface is InstanceSocket.ReleaseSurface, its Arguments
	// the platform.Window.
	CallKindReleaseSurface
	// CallKindAcquireFramebuffer is BackendDevice.AcquireFramebuffer, its
	// Arguments an AcquireFramebufferArguments.
	CallKindAcquireFramebuffer
	// CallKindPresent is BackendDevice.Present, its Arguments the
	// platform.Window.
	CallKindPresent
	// CallKindFinish is an encoder's Finish, with no Arguments; an Error
	// answered fails it.
	CallKindFinish
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
	// submittedAfter is, for each submission, how many calls were recorded
	// before it.
	submittedAfter []int
}

// Frames answers the frames gfx drew, in order: the acquires, submissions and
// presents from each frame's first acquire to its last present. A frame with
// no window acquired is its one submission.
func (recording *Recording) Frames() []Frame {
	var frames []Frame
	// current answers the frame being grouped, a new one when begin says the
	// last is over.
	current := func(begin func(last *Frame) bool) *Frame {
		if count := len(frames); count == 0 || begin(&frames[count-1]) {
			frames = append(frames, Frame{})
		}
		return &frames[len(frames)-1]
	}
	submission := 0
	for index := 0; index <= len(recording.Calls); index++ {
		for ; submission < len(recording.Submissions) && recording.submittedAfter[submission] == index; submission++ {
			frame := current(func(last *Frame) bool {
				return len(last.Presented) > 0 || (len(last.Acquired) == 0 && len(last.Submissions) > 0)
			})
			frame.Submissions = append(frame.Submissions, recording.Submissions[submission])
		}
		if index == len(recording.Calls) {
			break
		}
		call := recording.Calls[index]
		switch call.Kind {
		case CallKindAcquireFramebuffer:
			frame := current(func(last *Frame) bool { return len(last.Submissions) > 0 || len(last.Presented) > 0 })
			if frame.Acquired == nil {
				frame.Acquired = map[platform.Window]uint8{}
			}
			frame.Acquired[call.Arguments.(AcquireFramebufferArguments).Window] = call.Status
		case CallKindPresent:
			frame := current(func(*Frame) bool { return false })
			frame.Presented = append(frame.Presented, call.Arguments.(platform.Window))
		}
	}
	return frames
}

// Call is one recorded call outside command buffers.
type Call struct {
	Kind CallKind
	// Arguments is the call's handle and descriptor, as the gfx types.
	Arguments any
	// Status is the status an acquire answered.
	Status uint8
	Error  error
}

// CreateSurfaceArguments are what CreateSurface was called with.
type CreateSurfaceArguments struct {
	Window platform.Window
	Native platform.NativeHandle
}

// AcquireFramebufferArguments are what AcquireFramebuffer was called with.
type AcquireFramebufferArguments struct {
	Window platform.Window
	View   gfx.TextureView
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

// Frame is one frame as the adapter saw it: each window's acquire status, the
// submissions, and the presents.
type Frame struct {
	Acquired    map[platform.Window]uint8
	Submissions []Submission
	Presented   []platform.Window
}
