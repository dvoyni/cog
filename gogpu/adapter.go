package gogpu

import (
	"github.com/dvoyni/cog/gfx"
	fork "github.com/gogpu/gogpu"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// adapter is gogpu's adapter, which the started HostLoop brought up with no
// device.
type adapter struct {
	loop    *fork.HostLoop
	app     *fork.App
	adapter *wgpu.Adapter
}

// Info answers gogpu's adapter's information.
func (a *adapter) Info() gfx.AdapterInfo {
	info := a.adapter.Info()
	return gfx.AdapterInfo{
		Name:       info.Name,
		Vendor:     info.Vendor,
		VendorID:   info.VendorID,
		DeviceID:   info.DeviceID,
		DeviceType: gfx.DeviceType(info.DeviceType),
		Driver:     info.Driver,
		DriverInfo: info.DriverInfo,
		Backend:    gfx.Backend(info.Backend),
	}
}

// Features answers gogpu's adapter's features, whose bits are gfx's.
func (a *adapter) Features() gfx.FeatureFlags { return gfx.FeatureFlags(a.adapter.Features()) }

// Limits answers gogpu's adapter's limits.
func (a *adapter) Limits() gfx.Limits {
	limits := a.adapter.Limits()
	return gfx.Limits{
		MaxTextureDimension1D:                     limits.MaxTextureDimension1D,
		MaxTextureDimension2D:                     limits.MaxTextureDimension2D,
		MaxTextureDimension3D:                     limits.MaxTextureDimension3D,
		MaxTextureArrayLayers:                     limits.MaxTextureArrayLayers,
		MaxBindGroups:                             limits.MaxBindGroups,
		MaxBindGroupsPlusVertexBuffers:            limits.MaxBindGroupsPlusVertexBuffers,
		MaxBindingsPerBindGroup:                   limits.MaxBindingsPerBindGroup,
		MaxDynamicUniformBuffersPerPipelineLayout: limits.MaxDynamicUniformBuffersPerPipelineLayout,
		MaxDynamicStorageBuffersPerPipelineLayout: limits.MaxDynamicStorageBuffersPerPipelineLayout,
		MaxSampledTexturesPerShaderStage:          limits.MaxSampledTexturesPerShaderStage,
		MaxSamplersPerShaderStage:                 limits.MaxSamplersPerShaderStage,
		MaxStorageBuffersPerShaderStage:           limits.MaxStorageBuffersPerShaderStage,
		MaxStorageTexturesPerShaderStage:          limits.MaxStorageTexturesPerShaderStage,
		MaxUniformBuffersPerShaderStage:           limits.MaxUniformBuffersPerShaderStage,
		MaxUniformBufferBindingSize:               limits.MaxUniformBufferBindingSize,
		MaxStorageBufferBindingSize:               limits.MaxStorageBufferBindingSize,
		MinUniformBufferOffsetAlignment:           limits.MinUniformBufferOffsetAlignment,
		MinStorageBufferOffsetAlignment:           limits.MinStorageBufferOffsetAlignment,
		MaxVertexBuffers:                          limits.MaxVertexBuffers,
		MaxBufferSize:                             limits.MaxBufferSize,
		MaxVertexAttributes:                       limits.MaxVertexAttributes,
		MaxVertexBufferArrayStride:                limits.MaxVertexBufferArrayStride,
		MaxInterStageShaderVariables:              limits.MaxInterStageShaderVariables,
		MaxColorAttachments:                       limits.MaxColorAttachments,
		MaxColorAttachmentBytesPerSample:          limits.MaxColorAttachmentBytesPerSample,
		MaxComputeWorkgroupStorageSize:            limits.MaxComputeWorkgroupStorageSize,
		MaxComputeInvocationsPerWorkgroup:         limits.MaxComputeInvocationsPerWorkgroup,
		MaxComputeWorkgroupSizeX:                  limits.MaxComputeWorkgroupSizeX,
		MaxComputeWorkgroupSizeY:                  limits.MaxComputeWorkgroupSizeY,
		MaxComputeWorkgroupSizeZ:                  limits.MaxComputeWorkgroupSizeZ,
		MaxComputeWorkgroupsPerDimension:          limits.MaxComputeWorkgroupsPerDimension,
	}
}

// RequestDevice has the HostLoop request gogpu's device with descriptor's
// features, and answers it. Its limits are the adapter's own.
func (a *adapter) RequestDevice(descriptor gfx.DeviceDescriptor) (gfx.BackendDevice, error) {
	if err := a.loop.RequestDevice(gputypes.Features(descriptor.RequiredFeatures), nil); err != nil {
		return nil, err
	}
	return &device{device: wgpu.DeviceFromHandle(a.app.GPUContextProvider().Device())}, nil
}

// Release does nothing: gogpu's adapter goes with the HostLoop.
func (*adapter) Release() {}
