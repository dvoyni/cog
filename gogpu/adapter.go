package gogpu

import (
	"github.com/dvoyni/cog/gfx"
	host "github.com/gogpu/gogpu"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// adapter is gogpu's adapter, which the started HostLoop brought up with no
// device.
type adapter struct {
	loop    *host.HostLoop
	app     *host.App
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
// features and limits, and answers it. The limits gfx's Limits lacks are the
// adapter's own: they are ray tracing's, push constants' and the like, which
// gfx never uses, and Metal reports some as zero, below WebGPU's defaults. A
// feature or limit the adapter lacks answers the fork's error naming it.
func (a *adapter) RequestDevice(descriptor gfx.DeviceDescriptor) (gfx.BackendDevice, error) {
	required := descriptor.RequiredLimits
	limits := a.adapter.Limits()
	limits.MaxTextureDimension1D = required.MaxTextureDimension1D
	limits.MaxTextureDimension2D = required.MaxTextureDimension2D
	limits.MaxTextureDimension3D = required.MaxTextureDimension3D
	limits.MaxTextureArrayLayers = required.MaxTextureArrayLayers
	limits.MaxBindGroups = required.MaxBindGroups
	limits.MaxBindGroupsPlusVertexBuffers = required.MaxBindGroupsPlusVertexBuffers
	limits.MaxBindingsPerBindGroup = required.MaxBindingsPerBindGroup
	limits.MaxDynamicUniformBuffersPerPipelineLayout = required.MaxDynamicUniformBuffersPerPipelineLayout
	limits.MaxDynamicStorageBuffersPerPipelineLayout = required.MaxDynamicStorageBuffersPerPipelineLayout
	limits.MaxSampledTexturesPerShaderStage = required.MaxSampledTexturesPerShaderStage
	limits.MaxSamplersPerShaderStage = required.MaxSamplersPerShaderStage
	limits.MaxStorageBuffersPerShaderStage = required.MaxStorageBuffersPerShaderStage
	limits.MaxStorageTexturesPerShaderStage = required.MaxStorageTexturesPerShaderStage
	limits.MaxUniformBuffersPerShaderStage = required.MaxUniformBuffersPerShaderStage
	limits.MaxUniformBufferBindingSize = required.MaxUniformBufferBindingSize
	limits.MaxStorageBufferBindingSize = required.MaxStorageBufferBindingSize
	limits.MinUniformBufferOffsetAlignment = required.MinUniformBufferOffsetAlignment
	limits.MinStorageBufferOffsetAlignment = required.MinStorageBufferOffsetAlignment
	limits.MaxVertexBuffers = required.MaxVertexBuffers
	limits.MaxBufferSize = required.MaxBufferSize
	limits.MaxVertexAttributes = required.MaxVertexAttributes
	limits.MaxVertexBufferArrayStride = required.MaxVertexBufferArrayStride
	limits.MaxInterStageShaderVariables = required.MaxInterStageShaderVariables
	limits.MaxColorAttachments = required.MaxColorAttachments
	limits.MaxColorAttachmentBytesPerSample = required.MaxColorAttachmentBytesPerSample
	limits.MaxComputeWorkgroupStorageSize = required.MaxComputeWorkgroupStorageSize
	limits.MaxComputeInvocationsPerWorkgroup = required.MaxComputeInvocationsPerWorkgroup
	limits.MaxComputeWorkgroupSizeX = required.MaxComputeWorkgroupSizeX
	limits.MaxComputeWorkgroupSizeY = required.MaxComputeWorkgroupSizeY
	limits.MaxComputeWorkgroupSizeZ = required.MaxComputeWorkgroupSizeZ
	limits.MaxComputeWorkgroupsPerDimension = required.MaxComputeWorkgroupsPerDimension
	if err := a.loop.RequestDevice(gputypes.Features(descriptor.RequiredFeatures), &limits); err != nil {
		return nil, err
	}
	return &device{device: wgpu.DeviceFromHandle(a.app.GPUContextProvider().Device())}, nil
}

// Release does nothing: gogpu's adapter goes with the HostLoop.
func (*adapter) Release() {}
