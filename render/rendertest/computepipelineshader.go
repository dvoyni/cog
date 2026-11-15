package rendertest

import (
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/render"
)

// computePipelineShader is what a compute pipeline runs, in render's
// terms: its shader and compute function, and the shader's module, whose
// bindings name a dispatch's parameters.
type computePipelineShader struct {
	shader     render.Shader
	entryPoint string
	module     gfx.ShaderModule
}

// makeComputePipelineShaders answers what each compute pipeline the
// recording's calls made runs, its shader its module's in shaders.
func makeComputePipelineShaders(calls []recording.Call, shaders map[gfx.ShaderModule]render.Shader) map[gfx.ComputePipeline]computePipelineShader {
	pipelineShaders := map[gfx.ComputePipeline]computePipelineShader{}
	for _, call := range calls {
		if arguments, ok := call.Arguments.(recording.CreateComputePipelineArguments); ok {
			descriptor := arguments.Descriptor
			pipelineShaders[arguments.Pipeline] = computePipelineShader{shader: shaders[descriptor.Module], entryPoint: descriptor.EntryPoint, module: descriptor.Module}
		}
	}
	return pipelineShaders
}
