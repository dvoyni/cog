package internal

// uniformMax caps the uniform block a shader may declare. The arena packs
// blocks of any size, so the cap is a policy rather than a stride; raising it
// toward the 64 KiB web floor is #100.
const uniformMax = 256

// checkUniformBlock measures a reflected shader's uniform block against
// uniformMax. It is not a web-floor check: the floor is 64 KiB. The caller
// treats a failure as fatal to the shader.
func checkUniformBlock(shader string, layout ShaderLayout) error {
	if block := layout.UniformBlock(); block != nil && block.Size > uniformMax {
		return ErrUniformBlockTooLarge{Shader: shader, Declared: block.Size, Max: uniformMax}
	}
	return nil
}

// checkWebLimits measures a reflected shader against the browser spec floor,
// never against the device it happens to be running on: a desktop adapter
// reports its hardware limits, where 200 storage buffers is ordinary, so a
// check against the real device passes a build no browser can run. The device's
// numbers belong in the report, not in the comparison.
//
// Every reflected binding is emitted for both shader stages, so the per-stage
// storage limit is counted once over the whole shader.
func checkWebLimits(shader string, layout ShaderLayout, device Limits) error {
	floor := DefaultLimits()
	storage, groups, uniformSize := 0, 0, 0
	for _, resource := range layout.Resources {
		switch resource.Kind.Base() {
		case ResourceStorageBuffer:
			storage++
		case ResourceUniformBuffer:
			uniformSize = max(uniformSize, resource.Size)
		}
		groups = max(groups, resource.Group+1)
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
	case uniformSize > floor.MaxUniformBufferBindingSize:
		return ErrShaderExceedsWebLimits{
			Shader: shader, Limit: "uniform block bytes",
			Declared: uniformSize, Floor: floor.MaxUniformBufferBindingSize,
			Device: device.MaxUniformBufferBindingSize,
		}
	}
	return nil
}
