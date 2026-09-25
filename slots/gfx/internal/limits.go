package internal

import (
	"github.com/dvoyni/cog/slots/gfx/internal/shader"
	"github.com/dvoyni/cog/slots/gfx/internal/types"
)

// checkWebLimits measures a reflected shader against the browser spec floor,
// never against the device it happens to be running on: a desktop adapter
// reports its hardware limits, where 200 storage buffers is ordinary, so a
// check against the real device passes a build no browser can run. The device's
// numbers belong in the report, not in the comparison.
//
// Every reflected binding is emitted for both shader stages, so the per-stage
// storage and uniform limits are counted once over the whole shader.
func checkWebLimits(label string, layout shader.ShaderLayout, device types.Limits) error {
	floor := DefaultLimits()
	storage, uniforms, groups, uniformSize := 0, 0, 0, 0
	for _, resource := range layout.Resources {
		switch resource.Kind.Base() {
		case shader.ResourceStorageBuffer:
			storage++
		case shader.ResourceUniformBuffer:
			uniforms++
			uniformSize = max(uniformSize, resource.Size)
		}
		groups = max(groups, resource.Group+1)
	}
	switch {
	case storage > floor.MaxStorageBuffersPerShaderStage:
		return shader.ErrShaderExceedsWebLimits{
			Shader: label, Limit: "storage buffers per shader stage",
			Declared: storage, Floor: floor.MaxStorageBuffersPerShaderStage,
			Device: device.MaxStorageBuffersPerShaderStage,
		}
	case uniforms > floor.MaxUniformBuffersPerShaderStage:
		return shader.ErrShaderExceedsWebLimits{
			Shader: label, Limit: "uniform buffers per shader stage",
			Declared: uniforms, Floor: floor.MaxUniformBuffersPerShaderStage,
			Device: device.MaxUniformBuffersPerShaderStage,
		}
	case groups > floor.MaxBindGroups:
		return shader.ErrShaderExceedsWebLimits{
			Shader: label, Limit: "bind groups",
			Declared: groups, Floor: floor.MaxBindGroups, Device: device.MaxBindGroups,
		}
	case uniformSize > floor.MaxUniformBufferBindingSize:
		return shader.ErrShaderExceedsWebLimits{
			Shader: label, Limit: "uniform block bytes",
			Declared: uniformSize, Floor: floor.MaxUniformBufferBindingSize,
			Device: device.MaxUniformBufferBindingSize,
		}
	}
	return nil
}
