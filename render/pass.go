package render

import (
	"cmp"
	"fmt"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/platform"
)

// PassDescriptor is what a pass declares: its label, for reports and
// inspections; its colour targets, up to 8 by WebGPU's default limits; and
// its depth-stencil target, a zero Texture meaning none. Its targets are of
// one size and sample count, each texture's at the mip level drawn into, and
// it has a colour target or a depth-stencil target.
type PassDescriptor struct {
	Label        string
	ColorTargets []ColorTarget
	DepthStencil DepthStencilTarget
}

// ColorTarget is one colour target of a pass: a window, drawn into, never
// sampled; or a texture of a colour format, at one mip level and one layer.
// It names one of the two. Its zero load and store keep what the target
// holds and store what the pass draws.
type ColorTarget struct {
	Window          platform.Window
	Texture         Texture
	MipLevel, Layer uint32
	// Clear clears the target to ClearColor as the pass begins, in place of
	// keeping what it holds.
	Clear      bool
	ClearColor Color
	// Discard throws away what the pass draws into the target as it ends, in
	// place of storing it.
	Discard bool
	// Resolve, of a multisampled target, is where the pass resolves what it
	// draws into the target as it ends; its zero value is none.
	Resolve ResolveTarget
}

// ResolveTarget is where a multisampled colour target resolves: a window, or
// a texture of the target's format, with one sample, at one mip level and
// one layer, of the target's size. It names one of the two, or neither for
// no resolve.
type ResolveTarget struct {
	Window          platform.Window
	Texture         Texture
	MipLevel, Layer uint32
}

// DepthStencilTarget is the depth-stencil target of a pass: a texture of a
// depth or stencil format, at one mip level and one layer, its aspects
// following its format. Its zero load and store keep what the target holds
// and store what the pass draws.
type DepthStencilTarget struct {
	Texture         Texture
	MipLevel, Layer uint32
	// Clear clears the target's depth to ClearDepth and its stencil to
	// ClearStencil as the pass begins, in place of keeping what it holds.
	Clear        bool
	ClearDepth   float32
	ClearStencil uint32
	// Discard throws away what the pass writes into the target as it ends,
	// in place of storing it.
	Discard bool
	// ReadOnly tests the pass's draws against the target without writing
	// it, so they may sample it too. A read-only target is neither cleared
	// nor discarded: a pass declaring one so is refused.
	ReadOnly bool
}

// Color is a linear RGBA colour.
type Color struct {
	R, G, B, A float64
}

// DrawArgs are a draw's instances: InstanceCount of them, a zero count
// drawing one, from FirstInstance, which a shader reads as instance_index.
type DrawArgs struct {
	InstanceCount, FirstInstance uint32
}

// Pass is one pass of a command list, from CommandList.BeginPass to its End.
// A skipped or refused pass records nothing.
type Pass struct {
	// frame is the parameter the pass's list was begun from.
	frame Frame
	// encoder is the pass's gfx encoder; nil when the pass records nothing,
	// skipped, refused or ended.
	encoder gfx.RenderPassEncoder
	// formats are the formats and sample count of the pass's targets.
	formats passFormat
	// targets are the textures the pass writes, the first targetCount of
	// them, which its draws' materials may not read.
	targets     [2*maxColorTargetCount + 1]drawnTarget
	targetCount int
	// material is the material SetMaterial selected, and pipeline and
	// bindGroups the pipeline and the bind group of each group last set on
	// encoder.
	material   Material
	pipeline   gfx.RenderPipeline
	bindGroups [reservedGroup]gfx.BindGroup
}

// SetMaterial selects material for the draws recorded after it, until it is
// set again or the pass ends.
func (p *Pass) SetMaterial(material Material) {
	p.material = material
}

// Draw draws mesh whole with the material SetMaterial selected: as many
// vertices as its vertex buffers hold, or with an index buffer as many
// indices as it holds, as many instances as arguments ask for. It draws with
// a pipeline of the material's shader, entry points and render state, the
// mesh's vertex layouts and topology, and the pass's target formats and
// sample count, shared by every draw of the same, render states that draw
// alike in the pass sharing one. The first draw of such a
// pipeline makes it, synchronously, in the recording job, which stalls that
// recording: a game warms its pipelines by drawing each early, such as
// behind a loading screen. A draw is skipped and reported once for what it
// names: a mesh or material render does not hold, no material selected, a
// shader that failed or was freed, or a buffer that failed, was freed, or
// ends before its range. A draw of a material refused when made is skipped,
// as reported then. A pipeline that cannot be made, such as one whose mesh's
// vertex buffers do not feed its shader's inputs, or one with no fragment
// stage drawn into a pass with colour targets, is reported once, and every
// draw of it is skipped until its shader is freed.
func (p *Pass) Draw(mesh Mesh, arguments DrawArgs) {
	if p.encoder == nil {
		return
	}
	table := p.frame.table.Get()
	material, ok := table.findSlot(&table.materials, p.material)
	switch {
	case !ok:
		if table.claimMaterialReport(p.material) {
			table.report(ReportKindDrawSkipped, fmt.Errorf("render: a draw of mesh %v with material %v, which render does not hold, or with no material selected", mesh, p.material))
		}
		return
	case material.refused:
		return
	}
	shader, ok := table.findSlot(&table.shaders, material.shader)
	if !ok || material.vertexEntryPoint == "" {
		if table.claimMaterialReport(p.material) {
			table.report(ReportKindDrawSkipped, fmt.Errorf("render: a draw of material %q, whose shader %v failed or was freed", material.label, material.shader))
		}
		return
	}
	meshSlot, ok := table.findSlot(&table.meshes, mesh)
	if !ok {
		if table.claimMeshReport(mesh) {
			table.report(ReportKindDrawSkipped, fmt.Errorf("render: a draw of mesh %v, which render does not hold", mesh))
		}
		return
	}
	var buffers meshBinding
	vertexCount, indexCount, err := meshSlot.findElements(table, &buffers)
	if err != nil {
		if table.claimMeshReport(mesh) {
			table.report(ReportKindDrawSkipped, fmt.Errorf("render: a draw of mesh %q, whose %w", meshSlot.label, err))
		}
		return
	}
	indexed := meshSlot.indices.Range.Buffer != (Buffer{})
	key := pipelineKey{
		shader:             material.shader,
		vertexEntryPoint:   material.vertexEntryPoint,
		fragmentEntryPoint: material.fragmentEntryPoint,
		state:              material.state.makeKeyState(p.formats),
		vertexLayout:       meshSlot.vertexLayout,
		formats:            p.formats,
		topology:           meshSlot.topology,
	}
	if indexed && (meshSlot.topology == PrimitiveTopologyLineStrip || meshSlot.topology == PrimitiveTopologyTriangleStrip) {
		key.stripIndexFormat = meshSlot.indices.Format
	}
	device := p.frame.device.Get()
	pipeline, ok := table.pipelines.find(table, device, key, shader)
	if !ok {
		return
	}
	bindGroups, err := table.findDrawBindGroups(device, p.material, shader, p.targets[:p.targetCount])
	switch {
	case err != nil:
		table.logUnlessLost(fmt.Errorf("render: binding material %q: %w", material.label, err))
		return
	case bindGroups.unboundRead != "":
		if table.claimMaterialReport(p.material) {
			table.report(ReportKindDrawSkipped, fmt.Errorf("render: a draw of material %q, whose storage binding %q, which a function it runs reads, is unbound", material.label, bindGroups.unboundRead))
		}
		return
	}
	if pipeline != p.pipeline {
		p.encoder.SetPipeline(pipeline)
		p.pipeline = pipeline
	}
	for group, bindGroup := range bindGroups.groups[:len(shader.layout.groups)] {
		if bindGroup != p.bindGroups[group] {
			p.encoder.SetBindGroup(uint32(group), bindGroup, zeroDynamicOffsets[:shader.layout.groups[group].dynamicOffsetCount])
			p.bindGroups[group] = bindGroup
		}
	}
	for slot, vertexBuffer := range meshSlot.vertexBuffers {
		p.encoder.SetVertexBuffer(uint32(slot), buffers.vertexBuffers[slot], vertexBuffer.bufferRange.Offset)
	}
	instanceCount := cmp.Or(arguments.InstanceCount, 1)
	if !indexed {
		p.encoder.Draw(gfx.DrawArgs{VertexCount: vertexCount, InstanceCount: instanceCount, FirstInstance: arguments.FirstInstance})
		return
	}
	p.encoder.SetIndexBuffer(buffers.indexBuffer, gfx.IndexFormat(meshSlot.indices.Format), meshSlot.indices.Range.Offset)
	p.encoder.DrawIndexed(gfx.DrawIndexedArgs{IndexCount: indexCount, InstanceCount: instanceCount, FirstInstance: arguments.FirstInstance})
}

// End ends the pass: nothing more is recorded into it.
func (p *Pass) End() {
	if p.encoder != nil {
		p.encoder.End()
		p.encoder = nil
	}
}

// addTarget notes target among those p writes; one naming no texture, a
// window's, is left out.
func (p *Pass) addTarget(target drawnTarget) {
	if target.texture != (Texture{}) {
		p.targets[p.targetCount] = target
		p.targetCount++
	}
}

// drawnTarget is a texture a pass writes, at one mip level and layer; a
// read-only one is its depth-stencil target, which its draws may sample.
type drawnTarget struct {
	texture         Texture
	mipLevel, layer uint32
	readOnly        bool
}
