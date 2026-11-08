package gogpu

import (
	"fmt"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/platform"
	"github.com/gogpu/wgpu"
)

// device is gogpu's device as gfx's backend device. A method not built yet
// answers an error naming it, and does nothing where it answers no error.
type device struct {
	device *wgpu.Device
}

func (*device) CreateBuffer(gfx.Buffer, gfx.BufferDescriptor) error { return unbuilt("CreateBuffer") }
func (*device) ReleaseBuffer(gfx.Buffer)                            {}
func (*device) CreateTexture(gfx.Texture, gfx.TextureDescriptor) error {
	return unbuilt("CreateTexture")
}
func (*device) ReleaseTexture(gfx.Texture) {}
func (*device) CreateTextureView(gfx.TextureView, gfx.Texture, gfx.TextureViewDescriptor) error {
	return unbuilt("CreateTextureView")
}
func (*device) ReleaseTextureView(gfx.TextureView) {}
func (*device) CreateSampler(gfx.Sampler, gfx.SamplerDescriptor) error {
	return unbuilt("CreateSampler")
}
func (*device) ReleaseSampler(gfx.Sampler) {}
func (*device) CreateQuerySet(gfx.QuerySet, gfx.QuerySetDescriptor) error {
	return unbuilt("CreateQuerySet")
}
func (*device) ReleaseQuerySet(gfx.QuerySet) {}
func (*device) TimestampPeriod() float32     { return 0 }
func (*device) CreateShaderModule(gfx.ShaderModule, gfx.ShaderModuleDescriptor) error {
	return unbuilt("CreateShaderModule")
}
func (*device) ReleaseShaderModule(gfx.ShaderModule) {}
func (*device) CreateBindGroupLayout(gfx.BindGroupLayout, gfx.BindGroupLayoutDescriptor) error {
	return unbuilt("CreateBindGroupLayout")
}
func (*device) ReleaseBindGroupLayout(gfx.BindGroupLayout) {}
func (*device) CreateBindGroup(gfx.BindGroup, gfx.BindGroupDescriptor) error {
	return unbuilt("CreateBindGroup")
}
func (*device) ReleaseBindGroup(gfx.BindGroup) {}
func (*device) CreatePipelineLayout(gfx.PipelineLayout, gfx.PipelineLayoutDescriptor) error {
	return unbuilt("CreatePipelineLayout")
}
func (*device) ReleasePipelineLayout(gfx.PipelineLayout) {}
func (*device) ReflectRenderPipelineLayout(gfx.RenderPipelineDescriptor) ([]gfx.BindGroupLayoutDescriptor, error) {
	return nil, unbuilt("ReflectRenderPipelineLayout")
}
func (*device) ReflectComputePipelineLayout(gfx.ComputePipelineDescriptor) ([]gfx.BindGroupLayoutDescriptor, error) {
	return nil, unbuilt("ReflectComputePipelineLayout")
}
func (*device) ReflectVertexInputs(gfx.ShaderModule, string) ([]gfx.VertexInput, error) {
	return nil, unbuilt("ReflectVertexInputs")
}
func (*device) CreateRenderPipeline(gfx.RenderPipeline, gfx.RenderPipelineDescriptor) error {
	return unbuilt("CreateRenderPipeline")
}
func (*device) ReleaseRenderPipeline(gfx.RenderPipeline) {}
func (*device) CreateComputePipeline(gfx.ComputePipeline, gfx.ComputePipelineDescriptor) error {
	return unbuilt("CreateComputePipeline")
}
func (*device) ReleaseComputePipeline(gfx.ComputePipeline)   {}
func (*device) WriteBuffer(gfx.Buffer, uint64, []byte) error { return unbuilt("WriteBuffer") }
func (*device) WriteTexture(gfx.ImageCopyTexture, []byte, gfx.ImageDataLayout, gfx.Extent3D) error {
	return unbuilt("WriteTexture")
}
func (*device) MapBuffer(gfx.Buffer, gfx.MapMode, uint64, uint64) error { return unbuilt("MapBuffer") }
func (*device) MapStatus(gfx.Buffer) (bool, error)                      { return false, unbuilt("MapStatus") }
func (*device) MappedRange(gfx.Buffer, uint64, uint64) ([]byte, error) {
	return nil, unbuilt("MappedRange")
}
func (*device) UnmapBuffer(gfx.Buffer) error { return unbuilt("UnmapBuffer") }
func (*device) Poll() uint64                 { return 0 }
func (*device) CreateCommandEncoder(gfx.CommandEncoderDescriptor) (gfx.StandaloneCommandEncoder, error) {
	return nil, unbuilt("CreateCommandEncoder")
}
func (*device) Submit(...gfx.CommandBuffer) (uint64, error) { return 0, unbuilt("Submit") }
func (*device) SurfaceCapabilities(platform.Window) gfx.SurfaceCapabilities {
	return gfx.SurfaceCapabilities{}
}
func (*device) ConfigureSurface(platform.Window, gfx.SurfaceConfiguration) error {
	return unbuilt("ConfigureSurface")
}
func (*device) AcquireFramebuffer(platform.Window, gfx.Texture, gfx.TextureView, bool) (gfx.AcquireStatus, error) {
	return 0, unbuilt("AcquireFramebuffer")
}
func (*device) Present(platform.Window) error { return unbuilt("Present") }
func (*device) WaitIdle() error               { return unbuilt("WaitIdle") }

// Release does nothing: gogpu's device goes with the HostLoop.
func (*device) Release() {}

// unbuilt answers the error of a backend method not built yet, naming it.
func unbuilt(method string) error { return fmt.Errorf("gogpu: %s is not built yet", method) }
