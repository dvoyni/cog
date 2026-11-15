package render

import (
	"fmt"
	"reflect"
	"unsafe"

	"github.com/dvoyni/cog/gfx"
)

// ComputePass is one compute pass of a command list, from
// CommandList.BeginComputePass to its End: a run of dispatches of the
// compute function SetShader selected, with the parameters set since, by
// name in any group of its shader, with no levels. There is no compute
// material. A compute pass of a list recording into nothing records
// nothing.
type ComputePass struct {
	// frame is the parameter the pass's list was begun from, and listState
	// what the list keeps, its arena chunks and the pass's parameters.
	frame     Frame
	listState *commandListState
	// encoder is the pass's gfx encoder; nil when the pass records nothing
	// or ended.
	encoder gfx.ComputePassEncoder
	// shader and entryPoint are what SetShader selected; bindGroups what
	// the dispatches bind for each group, kept between them; and pipeline
	// the pipeline as last set on encoder.
	shader     Shader
	entryPoint string
	bindGroups bindGroupSet
	pipeline   gfx.ComputePipeline
}

// SetShader selects shader's compute function named entryPoint for the
// dispatches recorded after it, until it is set again or the pass ends, an
// empty name meaning the shader's only compute function, and resets every
// parameter: the dispatches after it read their defaults until they are
// set again. A name the shader has no compute function of, or an empty one
// where it has several, is reported once, at the first dispatch's pipeline,
// and its dispatches are skipped.
func (c *ComputePass) SetShader(shader Shader, entryPoint string) {
	if c.encoder == nil {
		return
	}
	c.shader = shader
	c.entryPoint = entryPoint
	c.listState.computeSettings = c.listState.computeSettings[:0]
	c.bindGroups.resolvedFor = [reservedGroup]shaderSelection{}
}

// SetTexture binds texture whole to the parameter named name, a texture or
// storage texture in any group, as SetTextureView binds the zero view of
// it, but that a storage binding sees its first mip level.
func (c *ComputePass) SetTexture(name string, texture Texture) {
	c.setParameter(parameterSetting{name: name, kind: parameterKindTexture, view: TextureView{Texture: texture}, wholeTexture: true})
}

// SetTextureView binds view to the parameter named name, a texture or
// storage texture in any group, as SetMaterialTextureView binds a
// material's: one that does not fit its binding, such as a view of several
// mip levels bound for storage, is refused, reported once for its shader
// and name, and the binding takes its default.
// A parameter is read by the dispatches recorded after it until it is set
// again, SetShader is called or the pass ends; one not set since reads its
// default. It is checked against the binding of its name in the shader at
// the next dispatch: a name the shader lacks is ignored.
func (c *ComputePass) SetTextureView(name string, view TextureView) {
	c.setParameter(parameterSetting{name: name, kind: parameterKindTexture, view: view})
}

// SetSampler binds sampler, by value, to the parameter named name, a sampler
// in any group, refused as SetMaterialSampler's is.
// A parameter is read by the dispatches recorded after it, as SetTextureView
// says.
func (c *ComputePass) SetSampler(name string, sampler SamplerDescriptor) {
	c.setParameter(parameterSetting{name: name, kind: parameterKindSampler, sampler: sampler})
}

// SetBuffer binds bufferRange, a zero size the rest of its buffer, to the
// parameter named name, a uniform block or storage buffer in any group,
// refused where it does not fit as SetMaterialBuffer's is. It binds as the
// binding's group binds a pass's or draw's: at a dynamic offset in
// @group(0) for a uniform block and in @group(2), so ranges of one buffer
// and one size share a bind group; statically otherwise, in a bind group
// render keeps by what it binds, so a range that moves, such as a temporary
// buffer's from frame to frame, costs a bind group each time it moves.
// A parameter is read by the dispatches recorded after it, as SetTextureView
// says.
func (c *ComputePass) SetBuffer(name string, bufferRange BufferRange) {
	c.setParameter(parameterSetting{name: name, kind: parameterKindBuffer, bufferRange: bufferRange})
}

// SetUniform binds value, plain data with no pointers mirroring a WGSL
// struct, to the uniform block named name, in any group, checked as
// SetMaterialUniform's is. Its bytes go into the list's temporary arena,
// and to gfx at the list's End. A block of @group(0) or @group(2) binds at a
// dynamic offset, making no bind group; one of @group(1) binds statically,
// costing a bind group each frame its block moves.
// A parameter is read by the dispatches recorded after it, as SetTextureView
// says.
func (c *ComputePass) SetUniform[T any](name string, value T) {
	if c.encoder != nil {
		c.setParameter(makeUniformSetting(c.frame, c.listState, name, reflect.TypeFor[T](), castToBytes(unsafe.Slice(&value, 1))))
	}
}

// Dispatch runs the compute function SetShader selected over x by y by z
// workgroups, with a pipeline of the shader and the function, shared by
// every dispatch of the same. The first dispatch of such a pipeline makes
// it, synchronously, in the recording job, which stalls that recording, as
// a draw's first pipeline does. It binds the parameters set before it. A
// dispatch is skipped and reported once for what it names: no shader
// selected, or one render does not hold, failed or was freed; so is one
// whose function reads a storage binding left unbound. A pipeline that
// cannot be made, such as one of a missing or ambiguous entry point, is
// reported once, and every dispatch of it is skipped until its shader is
// freed.
func (c *ComputePass) Dispatch(x, y, z uint32) {
	if c.encoder == nil {
		return
	}
	table := c.frame.table.Get()
	shader, ok := table.findSlot(&table.shaders, c.shader)
	if !ok || shader.layout == nil {
		if table.claimDispatchReport(shaderSelection{shader: c.shader}) {
			table.report(ReportKindDispatchSkipped, fmt.Errorf("render: a dispatch of shader %v, which render does not hold, failed or was freed, or with no shader selected", c.shader))
		}
		return
	}
	device := c.frame.device.Get()
	pipeline, ok := table.computePipelines.find(table, device, computePipelineKey{shader: c.shader, entryPoint: c.entryPoint}, shader)
	if !ok {
		return
	}
	selection := shaderSelection{shader: c.shader, entryPoints: [2]string{pipeline.entryPoint}}
	if err := table.findDispatchBindGroups(device, selection, shader, c.listState, &c.bindGroups); err != nil {
		table.logUnlessLost(fmt.Errorf("render: binding the parameters of shader %v: %w", c.shader, err))
		return
	}
	groupCount := len(shader.layout.groups)
	if name := c.bindGroups.findUnboundRead(groupCount); name != "" {
		if table.claimDispatchReport(selection) {
			table.report(ReportKindDispatchSkipped, fmt.Errorf("render: a dispatch of shader %v, whose storage binding %q, which its function %q reads, is unbound", c.shader, name, pipeline.entryPoint))
		}
		return
	}
	if pipeline.pipeline != c.pipeline {
		c.encoder.SetPipeline(pipeline.pipeline)
		c.pipeline = pipeline.pipeline
	}
	c.bindGroups.setChangedGroups(c.encoder, groupCount, &c.listState.dynamicOffsets)
	c.encoder.Dispatch(x, y, z)
}

// End ends the compute pass: nothing more is recorded into it.
func (c *ComputePass) End() {
	if c.encoder != nil {
		c.encoder.End()
		c.encoder = nil
	}
}

// setParameter puts setting among the pass's parameters, in place of the
// one of its name, for the dispatches after it to resolve.
func (c *ComputePass) setParameter(setting parameterSetting) {
	if c.encoder == nil {
		return
	}
	c.listState.computeSettings = putSetting(c.listState.computeSettings, setting)
	c.bindGroups.resolvedFor = [reservedGroup]shaderSelection{}
}
