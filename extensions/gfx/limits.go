package gfx

// checkWebLimits measures a reflected shader against the browser spec floor,
// never against the device it happens to be running on: a desktop adapter
// reports its hardware limits, where 200 storage buffers is ordinary, so a
// check against the real device passes a build no browser can run. The device's
// numbers belong in the report, not in the comparison.
//
// Every reflected binding is emitted for both shader stages, so the per-stage
// storage limit is counted once over the whole shader.
func checkWebLimits(shader string, layout ShaderLayout, device Limits) error {
	floor := DefaultLimits
	storage, groups := 0, 0
	for _, resource := range layout.Resources {
		if resource.StorageBuffer {
			storage++
		}
		groups = max(groups, resource.Group+1)
	}
	if layout.UniformSize > 0 {
		groups = max(groups, layout.UniformGroup+1)
	}
	switch {
	case storage > floor.MaxStorageBuffersPerShaderStage:
		return ErrShaderExceedsWebLimits{
			Shader: shader, Limit: "storage buffers per shader stage",
			Declared: storage, Floor: floor.MaxStorageBuffersPerShaderStage,
			Device: device.MaxStorageBuffersPerShaderStage,
		}
	case groups > floor.MaxBindGroups:
		return ErrShaderExceedsWebLimits{
			Shader: shader, Limit: "bind groups",
			Declared: groups, Floor: floor.MaxBindGroups, Device: device.MaxBindGroups,
		}
	case layout.UniformSize > floor.MaxUniformBufferBindingSize:
		return ErrShaderExceedsWebLimits{
			Shader: shader, Limit: "uniform block bytes",
			Declared: layout.UniformSize, Floor: floor.MaxUniformBufferBindingSize,
			Device: device.MaxUniformBufferBindingSize,
		}
	}
	return nil
}
