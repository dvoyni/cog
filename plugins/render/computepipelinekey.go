package render

import (
	"errors"
	"fmt"

	"github.com/dvoyni/cog/plugins/gfx"
)

// computePipelineKey is what a compute pipeline is shared by: the variant,
// and the entry point a compute pass's SetShader named, an empty one
// meaning the shader's only compute function.
type computePipelineKey struct {
	shader     Shader
	entryPoint string
}

// String names k's pipeline in reports.
func (k computePipelineKey) String() string {
	return fmt.Sprintf("the compute pipeline of shader %v and entry point %q", k.shader, k.entryPoint)
}

// findShader answers k's shader.
func (k computePipelineKey) findShader() Shader {
	return k.shader
}

// makePipeline makes k's pipeline from shader, the slot of k's shader, with
// the layout of the shader's bindings, running the compute function k's
// entry point names. A name the shader has no compute function of, or an
// empty one where it has none or several, is refused; so is what gfx
// refuses.
func (k computePipelineKey) makePipeline(_ *assetTable, device gfx.Device, shader shaderSlot) (computePipeline, error) {
	entryPoint, err := shader.findEntryPoint(gfx.ShaderStageFlagCompute, k.entryPoint)
	switch {
	case err != nil:
		return computePipeline{}, fmt.Errorf("its entry point %w", err)
	case entryPoint == "":
		return computePipeline{}, errors.New("its entry point names none of the shader's compute functions, of which it has none")
	}
	pipeline, err := device.CreateComputePipeline(gfx.ComputePipelineDescriptor{Label: "render: a compute pipeline", Layout: shader.layout.layout, Module: shader.module, EntryPoint: entryPoint})
	if err != nil {
		return computePipeline{}, err
	}
	return computePipeline{pipeline: pipeline, entryPoint: entryPoint}, nil
}

// makeInspectedPipeline answers pipeline, k's, as an inspection writes it
// down: its shader and the compute function it runs.
func (k computePipelineKey) makeInspectedPipeline(pipeline computePipeline) InspectedPipeline {
	return InspectedPipeline{Shader: k.shader, ComputeEntryPoint: pipeline.entryPoint}
}

// computePipeline is a compute pipeline a cache holds, with the name of the
// compute function it runs, its key's entry point resolved.
type computePipeline struct {
	pipeline   gfx.ComputePipeline
	entryPoint string
}
