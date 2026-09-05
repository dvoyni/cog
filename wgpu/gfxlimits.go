package wgpu

import (
	cgfx "github.com/dvoyni/cog/gfx"
)

// Limits reports what this device allows. gfx measures shaders against the web
// floor rather than these, and uses them only to say what the machine a build
// ran on happened to permit.
func (b *gfxBackend) Limits() cgfx.Limits {
	device := b.device.Limits()
	return cgfx.Limits{
		MaxBindGroups:                   int(device.MaxBindGroups),
		MaxStorageBuffersPerShaderStage: int(device.MaxStorageBuffersPerShaderStage),
		MaxStorageBufferBindingSize:     int(device.MaxStorageBufferBindingSize),
		MaxUniformBufferBindingSize:     int(device.MaxUniformBufferBindingSize),
		MaxBufferSize:                   int(device.MaxBufferSize),
	}
}
