package render

import (
	"cmp"
	"fmt"
	"reflect"
	"slices"
	"unsafe"

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
	// keeping what it holds; clearing what an earlier pass of the frame drew
	// into is reported, as BeginPass says.
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

// InstanceBuffer is a vertex buffer read once per instance: a range of a
// buffer, its offset where instance 0 is, and the layout its instances are
// read in. A zero Buffer is none.
type InstanceBuffer struct {
	Range  BufferRange
	Layout VertexLayout
}

// ScissorRectangle is the part of a pass's targets its draws may write, in
// pixels from the targets' top left.
type ScissorRectangle struct {
	X, Y, Width, Height uint32
}

// Pass is one pass of a command list, from CommandList.BeginPass to its End,
// with the material its draws draw with, its pass parameters, of @group(0),
// read by all its draws, the draw parameters, of @group(2), and the instance
// buffers, read by the draws after them, and its viewport, scissor
// rectangle, stencil reference and blend constant. A skipped or refused pass
// records nothing.
type Pass struct {
	// frame is the parameter the pass's list was begun from, and listState
	// what the list keeps, its arena chunks and the pass's parameters.
	frame     Frame
	listState *commandListState
	// encoder is the pass's gfx encoder; nil when the pass records nothing,
	// skipped, refused or ended.
	encoder gfx.RenderPassEncoder
	// formats are the formats and sample count of the pass's targets, and
	// width and height their size.
	formats       passFormat
	width, height uint32
	// targets are the textures the pass writes, the first targetCount of
	// them, which its draws' materials may not read.
	targets     [2*maxColorTargetCount + 1]drawnTarget
	targetCount int
	// noteIndex is the index of the pass's note among its list's, which
	// says whether it drew, and inspectedPass that of its inspection among
	// its list's inspected passes, while the list is inspected.
	noteIndex     int
	inspectedPass int
	// material is the material SetMaterial selected; bindGroups what the
	// draws bind for each group, kept between them; and pipeline the
	// pipeline as last set on encoder.
	material   Material
	bindGroups bindGroupSet
	pipeline   gfx.RenderPipeline
	// drawnMaterials are what the pass's draws found of the materials they
	// drew lately, nextDrawnMaterial the one a material not among them
	// replaces, and drawnMesh what they found of the mesh drawn last, so a
	// draw of what a draw before it drew finds none of it again.
	drawnMaterials    [drawnMaterialCount]drawnMaterial
	nextDrawnMaterial int
	drawnMesh         drawnMesh
	// boundMaterial is one more than the index among drawnMaterials of the
	// material whose bind groups bindGroups holds, resolved and checked by a
	// draw of it, while nothing since changed what it resolved; zero for
	// none. An index, as a pointer into the pass would move it to the heap.
	boundMaterial int
	// boundVertexBuffers are the vertex buffers as last set on encoder, by
	// slot, at boundVertexOffsets, and boundIndex the index buffer.
	boundVertexBuffers [maxVertexBufferCount]gfx.Buffer
	boundVertexOffsets [maxVertexBufferCount]uint64
	boundIndex         indexBinding
	// instanceBuffers are the instance buffers set, by slot, a zero Buffer
	// meaning none; instanceSlots the slots set, in order, the first
	// instanceCount of them, the order a draw binds them in; and
	// instanceLayout the number of their layouts, in that order, in the
	// vertex layout interner.
	instanceBuffers [maxVertexBufferCount]VertexBuffer
	instanceSlots   [maxVertexBufferCount]uint8
	instanceCount   int
	instanceLayout  uint32
	// foundInstanceBuffers are the gfx buffers behind the instance buffers
	// set, in the order a draw binds them, once a draw found them since they
	// were set, as instanceBuffersFound says.
	foundInstanceBuffers [maxVertexBufferCount]gfx.Buffer
	instanceBuffersFound bool
	// pendingDrawSettings marks, by index, the draw parameters, and
	// pendingInstanceSlots, by slot, the instance buffers that SetMaterial
	// reset and no setter set since. The next draw clears them, so one set
	// again as it was before SetMaterial leaves what the draws found and
	// resolved of it standing.
	pendingDrawSettings  uint64
	pendingInstanceSlots uint8
}

// SetMaterial selects material for the draws recorded after it, until it is
// set again or the pass ends, and resets every draw parameter and instance
// buffer: the draws after it read their defaults, and no instance buffer,
// until they are set again.
//
// A pass remembers what its draws found of the last four materials it
// drew. Switching back to one of them costs little, but switching to
// any other repeats the lookup of its shader, pipeline and bind groups. So
// a caller drawing many materials sorts its draws by shader, then material,
// then mesh, as a pass remembers only its last mesh too.
func (p *Pass) SetMaterial(material Material) {
	p.material = material
	if p.encoder == nil {
		return
	}
	settings := p.listState.parameterSettings[drawGroup]
	if len(settings) > 64 {
		p.listState.parameterSettings[drawGroup] = settings[:0]
		p.forgetResolution(drawGroup)
		p.pendingDrawSettings = 0
	} else {
		p.pendingDrawSettings = 1<<len(settings) - 1
	}
	p.pendingInstanceSlots = 0
	for _, slot := range p.instanceSlots[:p.instanceCount] {
		p.pendingInstanceSlots |= 1 << slot
	}
}

// SetInstanceBuffer sets instanceBuffer at slot, from 0 to 7, for the draws
// recorded after it, until it is set again, SetMaterial is called or the
// pass ends; one with no buffer clears the slot. A draw binds the instance
// buffers set after its mesh's vertex buffers, in slot order, each stepped
// once per instance, so its shader reads them by @location alone; their
// layouts join its pipeline's key, so a draw of new layouts makes a
// pipeline, as Draw says. One at a slot past 7, or at an offset or of a
// stride no multiple of 4, or with an attribute misaligned as MakeMesh
// refuses one, is refused and reported, leaving the slot as it was. A draw
// whose instance buffer failed, was freed or ends before its range is
// skipped and reported once for its material.
func (p *Pass) SetInstanceBuffer(slot uint32, instanceBuffer InstanceBuffer) {
	if p.encoder == nil {
		return
	}
	table := p.frame.table.Get()
	vertexBuffer := VertexBuffer(instanceBuffer)
	if slot >= maxVertexBufferCount {
		table.report(ReportKindInstanceBufferRefused, fmt.Errorf("render: an instance buffer at slot %d, past the last, %d", slot, maxVertexBufferCount-1))
		return
	}
	if err := vertexBuffer.check(); err != nil {
		table.report(ReportKindInstanceBufferRefused, fmt.Errorf("render: the instance buffer at slot %d %w", slot, err))
		return
	}
	p.pendingInstanceSlots &^= 1 << slot
	replacedBuffer := p.instanceBuffers[slot]
	p.instanceBuffers[slot] = vertexBuffer
	if replacedBuffer.Range != vertexBuffer.Range {
		p.instanceBuffersFound = false
	}
	if (replacedBuffer.Range.Buffer == (Buffer{})) == (vertexBuffer.Range.Buffer == (Buffer{})) &&
		replacedBuffer.Layout.Stride == vertexBuffer.Layout.Stride && slices.Equal(replacedBuffer.Layout.Attributes, vertexBuffer.Layout.Attributes) {
		// The same slots are set, in the same layouts, so the draws' key stands.
		return
	}
	p.orderInstanceBuffers(table)
}

// SetViewport sets viewport for the draws recorded after it, until it is
// set again or the pass ends; a pass begins with the whole of its targets,
// at depths 0 to 1. One WebGPU refuses, of a negative size or one past the
// device's largest texture, reaching past twice that from the targets, or
// with a depth range outside 0 to 1 or reversed, is refused and reported,
// keeping the viewport before it.
func (p *Pass) SetViewport(viewport Viewport) {
	if p.encoder == nil {
		return
	}
	if err := viewport.check(float32(p.frame.device.Get().Limits().MaxTextureDimension2D)); err != nil {
		p.frame.state.Get().report(ReportKindPassStateRefused, fmt.Errorf("render: a viewport of %+v %w", viewport, err))
		return
	}
	p.encoder.SetViewport(gfx.Viewport(viewport))
}

// SetScissorRectangle sets rectangle, clamped to the pass's targets, as the
// part of them the draws recorded after it may write, until it is set again
// or the pass ends; a pass begins with the whole of its targets. A
// rectangle wholly past them clamps to an empty one, which writes nothing.
func (p *Pass) SetScissorRectangle(rectangle ScissorRectangle) {
	if p.encoder == nil {
		return
	}
	x := min(rectangle.X, p.width)
	y := min(rectangle.Y, p.height)
	p.encoder.SetScissorRect(gfx.ScissorRect{X: x, Y: y, Width: min(rectangle.Width, p.width-x), Height: min(rectangle.Height, p.height-y)})
}

// SetStencilReference sets reference as the value the stencil tests and
// writes of the draws recorded after it compare with and write, until it is
// set again or the pass ends; a pass begins with zero.
func (p *Pass) SetStencilReference(reference uint32) {
	if p.encoder != nil {
		p.encoder.SetStencilReference(reference)
	}
}

// SetBlendConstant sets color as the constant the blending of the draws
// recorded after it reads, until it is set again or the pass ends; a pass
// begins with transparent black.
func (p *Pass) SetBlendConstant(color Color) {
	if p.encoder != nil {
		p.encoder.SetBlendConstant(gfx.Color(color))
	}
}

// SetPassTexture binds texture whole to the pass parameter named name, a
// texture or storage texture of @group(0), as SetPassTextureView binds the
// zero view of it, but that a storage binding sees its first mip level.
func (p *Pass) SetPassTexture(name string, texture Texture) {
	p.setParameter(passGroup, &parameterSetting{name: name, kind: parameterKindTexture, view: TextureView{Texture: texture}, wholeTexture: true})
}

// SetPassTextureView binds view, part of a texture or the texture seen
// another way, to the pass parameter named name, a texture or storage
// texture of @group(0), as SetMaterialTextureView binds a material's: it
// is refused, reported once for its shader and name, where it does not fit
// its binding, and a texture the pass writes, unless it is its read-only
// depth-stencil target sampled, is caught and reported once; each draws the
// binding's default in its place.
// A pass parameter is read by every draw recorded after it in the pass,
// whatever its material, until it is set again or the pass ends; one not
// set reads its default. It is checked against the binding of its name in
// each shader drawn, at the first draw after it is set: a name a shader
// lacks is ignored, so one set of values serves every shader, and one of a
// binding outside @group(0) is refused and reported once.
func (p *Pass) SetPassTextureView(name string, view TextureView) {
	p.setParameter(passGroup, &parameterSetting{name: name, kind: parameterKindTexture, view: view})
}

// SetPassSampler binds sampler, by value, to the pass parameter named name,
// a sampler of @group(0), as SetMaterialSampler binds a material's: one
// that does not fit its binding is refused, reported once for its shader
// and name, and the binding draws its default.
// A pass parameter is read by every draw recorded after it in the pass, as
// SetPassTextureView says.
func (p *Pass) SetPassSampler(name string, sampler SamplerDescriptor) {
	p.setParameter(passGroup, &parameterSetting{name: name, kind: parameterKindSampler, sampler: sampler})
}

// SetPassBuffer binds bufferRange, a zero size the rest of its buffer, to
// the pass parameter named name, a uniform block or storage buffer of
// @group(0), refused where it does not fit as SetMaterialBuffer's is. A
// uniform block binds at a dynamic offset. A storage buffer binds
// statically, in the pass's bind group, which render keeps by what it
// binds: a made or loaded buffer costs nothing from frame to frame, but a
// temporary buffer, whose range moves from frame to frame, costs the pass a
// new bind group in each frame it moves. Data a whole
// pass reads goes in a made buffer written with Inventory.WriteBuffer each
// frame, which allocates nothing, and data that varies by draw in a draw
// parameter.
// A pass parameter is read by every draw recorded after it in the pass, as
// SetPassTextureView says.
func (p *Pass) SetPassBuffer(name string, bufferRange BufferRange) {
	p.setParameter(passGroup, &parameterSetting{name: name, kind: parameterKindBuffer, bufferRange: bufferRange})
}

// SetPassUniform binds value, plain data with no pointers mirroring a WGSL
// struct, to the pass's uniform block named name, of @group(0), checked as
// SetMaterialUniform's is. Its bytes go into the list's temporary arena,
// bound at a dynamic offset, and to gfx at the list's End.
// A pass parameter is read by every draw recorded after it in the pass, as
// SetPassTextureView says.
func (p *Pass) SetPassUniform[T any](name string, value T) {
	p.setUniform(passGroup, name, reflect.TypeFor[T](), castToBytes(unsafe.Slice(&value, 1)))
}

// SetTexture binds texture whole to the draw parameter named name, a
// texture or storage texture of @group(2), as SetTextureView binds the zero
// view of it, but that a storage binding sees its first mip level.
func (p *Pass) SetTexture(name string, texture Texture) {
	p.setParameter(drawGroup, &parameterSetting{name: name, kind: parameterKindTexture, view: TextureView{Texture: texture}, wholeTexture: true})
}

// SetTextureView binds view to the draw parameter named name, a texture or
// storage texture of @group(2), refused and caught as SetPassTextureView's
// is, drawing the binding's default in its place.
// A draw parameter is read by the draws recorded after it until it is set
// again, SetMaterial is called or the pass ends; one not set since reads
// its default. It is checked against the binding of its name in the
// material's shader at the next draw: a name the shader lacks is ignored,
// and one of a binding outside @group(2) is refused and reported once.
func (p *Pass) SetTextureView(name string, view TextureView) {
	p.setParameter(drawGroup, &parameterSetting{name: name, kind: parameterKindTexture, view: view})
}

// SetSampler binds sampler, by value, to the draw parameter named name, a
// sampler of @group(2), refused as SetPassSampler's is.
// A draw parameter is read by the draws recorded after it, as SetTextureView
// says.
func (p *Pass) SetSampler(name string, sampler SamplerDescriptor) {
	p.setParameter(drawGroup, &parameterSetting{name: name, kind: parameterKindSampler, sampler: sampler})
}

// SetBuffer binds bufferRange, a zero size the rest of its buffer, to the
// draw parameter named name, a uniform block or storage buffer of
// @group(2), refused where it does not fit as SetMaterialBuffer's is. It
// binds at a dynamic offset, so draws given ranges of one buffer, of one
// size, such as a temporary buffer's rows for each draw, rebind one bind
// group at new offsets; a range of another buffer or size takes a bind
// group of its own.
// A draw parameter is read by the draws recorded after it, as SetTextureView
// says.
func (p *Pass) SetBuffer(name string, bufferRange BufferRange) {
	p.setParameter(drawGroup, &parameterSetting{name: name, kind: parameterKindBuffer, bufferRange: bufferRange})
}

// SetUniform binds value, plain data with no pointers mirroring a WGSL
// struct, to the draw's uniform block named name, of @group(2), checked as
// SetMaterialUniform's is. Its bytes go into the list's temporary arena, and
// to gfx at the list's End; draws setting it rebind one bind group at new
// dynamic offsets, making none.
// A draw parameter is read by the draws recorded after it, as SetTextureView
// says.
func (p *Pass) SetUniform[T any](name string, value T) {
	p.setUniform(drawGroup, name, reflect.TypeFor[T](), castToBytes(unsafe.Slice(&value, 1)))
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
// behind a loading screen. It binds the material's values, the pass
// parameters and the draw parameters set before it. A draw is skipped and reported once for what it
// names: a mesh or material render does not hold, no material selected, a
// shader that failed or was freed, or a buffer that failed, was freed, or
// ends before its range; so is one whose shader reads a storage binding left
// unbound. A draw of a material refused when made is skipped,
// as reported then. A pipeline that cannot be made, such as one whose mesh's
// vertex buffers do not feed its shader's inputs, or one with no fragment
// stage drawn into a pass with colour targets, is reported once, and every
// draw of it is skipped until its shader is freed.
func (p *Pass) Draw(mesh Mesh, arguments DrawArgs) {
	if p.encoder == nil {
		return
	}
	table := p.frame.table.Get()
	p.clearPendingResets(table)
	materialIndex, ok := p.findDrawnMaterial(table)
	if !ok {
		return
	}
	material := &p.drawnMaterials[materialIndex]
	if (mesh != p.drawnMesh.mesh || mesh == Mesh{}) && !p.findDrawnMesh(table, mesh) {
		return
	}
	drawnMesh := &p.drawnMesh
	vertexBuffers := &drawnMesh.vertexBuffers
	if !p.findInstanceBuffers(table, material.label, vertexBuffers.vertexBufferCount) {
		return
	}
	layout := drawnMesh.layout
	layout.instanceLayout = p.instanceLayout
	if !p.bindDraw(table, materialIndex, layout) {
		return
	}
	for slot := range vertexBuffers.vertexBufferCount {
		p.setVertexBuffer(slot, vertexBuffers.vertexBuffers[slot], vertexBuffers.vertexOffsets[slot])
	}
	p.setInstanceBuffers(vertexBuffers.vertexBufferCount)
	p.listState.passes[p.noteIndex].drew = true
	instanceCount := cmp.Or(arguments.InstanceCount, 1)
	if drawnMesh.index.buffer == (gfx.Buffer{}) {
		p.encoder.Draw(gfx.DrawArgs{VertexCount: drawnMesh.vertexCount, InstanceCount: instanceCount, FirstInstance: arguments.FirstInstance})
		if p.listState.inspectedList != nil {
			p.addInspectedDraw(material, InspectedDraw{VertexCount: drawnMesh.vertexCount, InstanceCount: instanceCount, FirstInstance: arguments.FirstInstance})
		}
		return
	}
	if drawnMesh.index != p.boundIndex {
		p.encoder.SetIndexBuffer(drawnMesh.index.buffer, drawnMesh.index.format, drawnMesh.index.offset)
		p.boundIndex = drawnMesh.index
	}
	p.encoder.DrawIndexed(gfx.DrawIndexedArgs{IndexCount: drawnMesh.indexCount, InstanceCount: instanceCount, FirstInstance: arguments.FirstInstance})
	if p.listState.inspectedList != nil {
		p.addInspectedDraw(material, InspectedDraw{IndexCount: drawnMesh.indexCount, InstanceCount: instanceCount, FirstInstance: arguments.FirstInstance})
	}
}

// DrawVertices draws vertexCount vertices in topology, its zero a triangle
// list, with no mesh, with the material SetMaterial selected: its shader
// places each from its vertex_index alone, as a full-screen triangle, a UI
// quad or a debug line does, and reads the instance buffers set, as many
// instances as arguments ask for. It binds no per-vertex buffer, so a
// shader whose vertex stage reads an input no instance buffer feeds is
// refused: its pipeline is reported once, and every draw of it skipped
// until its shader is freed. It draws, binds and is skipped otherwise as
// Draw does.
func (p *Pass) DrawVertices(topology PrimitiveTopology, vertexCount uint32, arguments DrawArgs) {
	if p.encoder == nil {
		return
	}
	table := p.frame.table.Get()
	p.clearPendingResets(table)
	materialIndex, ok := p.findDrawnMaterial(table)
	if !ok {
		return
	}
	material := &p.drawnMaterials[materialIndex]
	if !p.findInstanceBuffers(table, material.label, 0) {
		return
	}
	if !p.bindDraw(table, materialIndex, drawLayout{instanceLayout: p.instanceLayout, topology: topology}) {
		return
	}
	p.setInstanceBuffers(0)
	p.listState.passes[p.noteIndex].drew = true
	instanceCount := cmp.Or(arguments.InstanceCount, 1)
	p.encoder.Draw(gfx.DrawArgs{VertexCount: vertexCount, InstanceCount: instanceCount, FirstInstance: arguments.FirstInstance})
	if p.listState.inspectedList != nil {
		p.addInspectedDraw(material, InspectedDraw{VertexCount: vertexCount, InstanceCount: instanceCount, FirstInstance: arguments.FirstInstance})
	}
}

// End ends the pass: nothing more is recorded into it.
func (p *Pass) End() {
	if p.encoder != nil {
		p.encoder.End()
		p.encoder = nil
	}
}

// setParameter puts setting among the pass or draw parameters, by group,
// in place of the one of its name, for the draws after it to resolve: a
// setting the same as the one it replaces changes nothing, and a uniform
// block set again in the same arena chunk, outside an inspected list, moves
// only its dynamic offset.
func (p *Pass) setParameter(group int, setting *parameterSetting) {
	if p.encoder == nil {
		return
	}
	settings := p.listState.parameterSettings[group]
	index := findSetting(settings, setting.name)
	if index < 0 {
		p.listState.parameterSettings[group] = append(settings, *setting)
		p.forgetResolution(group)
		return
	}
	if group == drawGroup {
		p.pendingDrawSettings &^= 1 << index
	}
	previous := &settings[index]
	if p.listState.inspectedList == nil && p.bindGroups.moveDynamicOffset(group, index, previous, setting) {
		previous.bufferRange.Offset = setting.bufferRange.Offset
		return
	}
	if *previous != *setting {
		*previous = *setting
		p.forgetResolution(group)
	}
}

// setUniform puts a uniform block of uniformType named name, its bytes data
// copied into the list's arena, among the pass or draw parameters, by group;
// one gfx refuses an arena chunk for is logged, and leaves the block
// unbound.
func (p *Pass) setUniform(group int, name string, uniformType reflect.Type, data []byte) {
	if p.encoder != nil {
		setting := parameterSetting{name: name}
		fillUniformSetting(&setting, p.frame, p.listState, uniformType, data)
		p.setParameter(group, &setting)
	}
}

// findMaterial answers the slot of the material SetMaterial selected and
// that of its shader; false when a draw of it is skipped: one render does
// not hold, or none selected, or whose shader failed or was freed, reported
// once for the material, or one refused when made, reported then.
func (p *Pass) findMaterial(table *assetTable) (materialSlot, shaderSlot, bool) {
	material, ok := table.findSlot(&table.materials, p.material)
	switch {
	case !ok:
		if table.claimMaterialReport(p.material) {
			table.report(ReportKindDrawSkipped, fmt.Errorf("render: a draw with material %v, which render does not hold, or with no material selected", p.material))
		}
		return materialSlot{}, shaderSlot{}, false
	case material.refused:
		return materialSlot{}, shaderSlot{}, false
	}
	shader, ok := table.findSlot(&table.shaders, material.shader)
	if !ok || material.vertexEntryPoint == "" {
		if table.claimMaterialReport(p.material) {
			table.report(ReportKindDrawSkipped, fmt.Errorf("render: a draw of material %q, whose shader %v failed or was freed", material.label, material.shader))
		}
		return materialSlot{}, shaderSlot{}, false
	}
	return material, shader, true
}

// clearPendingResets clears what SetMaterial reset and no setter set
// since: the draw parameters pendingDrawSettings marks, leaving the draw
// parameters to be resolved again, and the instance buffers
// pendingInstanceSlots marks, their layouts interned in table.
func (p *Pass) clearPendingResets(table *assetTable) {
	if p.pendingDrawSettings != 0 {
		settings := p.listState.parameterSettings[drawGroup]
		keptSettings := settings[:0]
		for index := range settings {
			if p.pendingDrawSettings&(1<<index) == 0 {
				keptSettings = append(keptSettings, settings[index])
			}
		}
		p.listState.parameterSettings[drawGroup] = keptSettings
		p.forgetResolution(drawGroup)
		p.pendingDrawSettings = 0
	}
	if p.pendingInstanceSlots != 0 {
		for slot := range p.instanceBuffers {
			if p.pendingInstanceSlots&(1<<slot) != 0 {
				p.instanceBuffers[slot] = VertexBuffer{}
			}
		}
		p.pendingInstanceSlots = 0
		p.instanceBuffersFound = false
		p.orderInstanceBuffers(table)
	}
}

// forgetResolution leaves the parameters of group to be resolved again by
// the next draw.
func (p *Pass) forgetResolution(group int) {
	p.bindGroups.resolvedFor[group] = shaderSelection{}
	p.boundMaterial = 0
}

// orderInstanceBuffers orders the instance buffers set by slot, the order a
// draw binds them in, and interns their layouts in table.
func (p *Pass) orderInstanceBuffers(table *assetTable) {
	var orderedBuffers [maxVertexBufferCount]VertexBuffer
	p.instanceCount = 0
	for index, instanceBuffer := range p.instanceBuffers {
		if instanceBuffer.Range.Buffer != (Buffer{}) {
			orderedBuffers[p.instanceCount] = instanceBuffer
			p.instanceSlots[p.instanceCount] = uint8(index)
			p.instanceCount++
		}
	}
	p.instanceLayout = table.vertexLayouts.intern(orderedBuffers[:p.instanceCount], gfx.VertexStepModeInstance)
}

// findDrawnMaterial answers the index among p's drawn materials of what
// its draws found of the material SetMaterial selected, found again when they found none of it, a material
// changed since, or the list is inspected, so each inspected draw writes
// its material's parameters down; false when a draw of it is skipped, as
// findMaterial says.
func (p *Pass) findDrawnMaterial(table *assetTable) (int, bool) {
	changeCount := table.materialChangeCount.Load()
	index := -1
	for candidate := range p.drawnMaterials {
		if p.drawnMaterials[candidate].material == p.material && p.material != (Material{}) {
			index = candidate
			break
		}
	}
	if index >= 0 && p.drawnMaterials[index].changeCount == changeCount && p.listState.inspectedList == nil {
		return index, true
	}
	material, shader, ok := p.findMaterial(table)
	if !ok {
		return 0, false
	}
	if index < 0 {
		index = p.nextDrawnMaterial
		p.nextDrawnMaterial = (index + 1) % drawnMaterialCount
	}
	p.drawnMaterials[index] = makeDrawnMaterial(p.material, &material, shader, p.formats, changeCount)
	p.boundMaterial = 0
	return index, true
}

// findDrawnMesh finds mesh, and the gfx buffers behind it, as p's drawn
// mesh; false when a draw of it is skipped, reported once for the mesh:
// render does not hold it, or a buffer of it failed, was freed, has no
// bytes, or ends before its range.
func (p *Pass) findDrawnMesh(table *assetTable, mesh Mesh) bool {
	meshSlot, ok := table.findSlot(&table.meshes, mesh)
	if !ok {
		if table.claimMeshReport(mesh) {
			table.report(ReportKindDrawSkipped, fmt.Errorf("render: a draw of mesh %v, which render does not hold", mesh))
		}
		return false
	}
	drawn := drawnMesh{mesh: mesh, layout: drawLayout{vertexLayout: meshSlot.vertexLayout, topology: meshSlot.topology}}
	var err error
	if drawn.vertexCount, drawn.indexCount, err = meshSlot.findElements(table, &drawn.vertexBuffers); err != nil {
		if table.claimMeshReport(mesh) {
			table.report(ReportKindDrawSkipped, fmt.Errorf("render: a draw of mesh %q, whose %w", meshSlot.label, err))
		}
		return false
	}
	if drawn.vertexBuffers.indexBuffer != (gfx.Buffer{}) {
		drawn.index = indexBinding{buffer: drawn.vertexBuffers.indexBuffer, format: gfx.IndexFormat(meshSlot.indices.Format), offset: meshSlot.indices.Range.Offset}
		if meshSlot.topology == PrimitiveTopologyLineStrip || meshSlot.topology == PrimitiveTopologyTriangleStrip {
			drawn.layout.stripIndexFormat = meshSlot.indices.Format
		}
	}
	p.drawnMesh = drawn
	return true
}

// findInstanceBuffers finds the gfx buffers behind p's instance buffers,
// unless a draw found them since they were set; false when a draw of
// vertexBufferCount per-vertex buffers, of a material labelled label, is
// skipped, reported once for the material: an instance buffer failed or was
// freed, or the vertex and instance buffers are more than a draw binds.
func (p *Pass) findInstanceBuffers(table *assetTable, label string, vertexBufferCount int) bool {
	var err error
	if vertexBufferCount+p.instanceCount > maxVertexBufferCount {
		err = fmt.Errorf("its vertex and instance buffers are more than %d", maxVertexBufferCount)
	}
	if err == nil && !p.instanceBuffersFound {
		for index, slot := range p.instanceSlots[:p.instanceCount] {
			if p.foundInstanceBuffers[index], _, err = p.instanceBuffers[slot].Range.findBuffer(table); err != nil {
				err = fmt.Errorf("its instance buffer at slot %d %w", slot, err)
				break
			}
		}
	}
	if err != nil {
		if table.claimMaterialReport(p.material) {
			table.report(ReportKindDrawSkipped, fmt.Errorf("render: a draw of material %q, where %w", label, err))
		}
		return false
	}
	p.instanceBuffersFound = true
	return true
}

// bindDraw sets on p's encoder the pipeline the material at materialIndex
// among p's drawn materials draws with for layout, a draw's, made or found the first time a draw of
// it gives that layout, and the bind groups the draw binds, those p does
// not hold resolved as resolveDrawBindGroups says; false when the draw is
// skipped, reported once for the material, or once for its pipeline.
func (p *Pass) bindDraw(table *assetTable, materialIndex int, layout drawLayout) bool {
	material := &p.drawnMaterials[materialIndex]
	if material.pipeline == (gfx.RenderPipeline{}) || material.layout != layout {
		key := material.key
		key.drawLayout = layout
		pipeline, ok := table.pipelines.find(table, p.frame.device.Get(), key, material.shader)
		if !ok {
			return false
		}
		material.pipeline = pipeline
		material.layout = layout
	}
	groupCount := len(material.shader.layout.groups)
	if materialIndex+1 != p.boundMaterial {
		// The material's bind group is found, and each group of parameters
		// its shader has resolved for its shader and functions.
		resolved := material.bindingFound &&
			(groupCount <= passGroup || p.bindGroups.resolvedFor[passGroup] == material.selection) &&
			(groupCount <= drawGroup || p.bindGroups.resolvedFor[drawGroup] == material.selection)
		if !resolved {
			if err := table.resolveDrawBindGroups(p.frame.device.Get(), material, p.targets[:p.targetCount], p.listState, &p.bindGroups); err != nil {
				table.logUnlessLost(fmt.Errorf("render: binding material %q: %w", material.label, err))
				return false
			}
		}
		p.bindGroups.putMaterialBinding(material)
		if name := p.bindGroups.findUnboundRead(groupCount); name != "" {
			if table.claimMaterialReport(p.material) {
				table.report(ReportKindDrawSkipped, fmt.Errorf("render: a draw of material %q, whose storage binding %q, which a function it runs reads, is unbound", material.label, name))
			}
			return false
		}
		p.boundMaterial = materialIndex + 1
	}
	if material.pipeline != p.pipeline {
		p.encoder.SetPipeline(material.pipeline)
		p.pipeline = material.pipeline
	}
	p.bindGroups.setChangedGroups(p.encoder, groupCount, &p.listState.dynamicOffsets)
	return true
}

// addInspectedDraw writes draw, recorded with a pipeline of material, found
// by p's draws, its counts filled in, down in the inspection of p's list,
// with its material, its parameters and its instance buffers; nothing when
// the list is not inspected, which its callers check first, so a draw of a
// frame not inspected builds nothing for it.
func (p *Pass) addInspectedDraw(material *drawnMaterial, draw InspectedDraw) {
	inspectedList := p.listState.inspectedList
	if inspectedList == nil {
		return
	}
	key := &material.key
	groupParameters := p.bindGroups.inspectedParameters
	clear(groupParameters[len(material.shader.layout.groups):])
	draw.Material = InspectedMaterial{
		Material:           p.material,
		Shader:             key.shader,
		VertexEntryPoint:   key.vertexEntryPoint,
		FragmentEntryPoint: key.fragmentEntryPoint,
		State:              key.state,
		Parameters:         groupParameters[materialGroup],
	}
	draw.PassParameters = groupParameters[passGroup]
	draw.DrawParameters = groupParameters[drawGroup]
	for _, slot := range p.instanceSlots[:p.instanceCount] {
		instanceRange := p.instanceBuffers[slot].Range
		draw.InstanceBuffers = append(draw.InstanceBuffers, BufferRange{Buffer: instanceRange.Buffer, Offset: instanceRange.Offset})
	}
	inspectedPass := &inspectedList.Passes[p.inspectedPass]
	inspectedPass.Draws = append(inspectedPass.Draws, draw)
}

// setInstanceBuffers sets on p's encoder each instance buffer found, from
// firstSlot on, the slot after a draw's per-vertex buffers, where the slot
// does not hold it already.
func (p *Pass) setInstanceBuffers(firstSlot int) {
	for index, slot := range p.instanceSlots[:p.instanceCount] {
		p.setVertexBuffer(firstSlot+index, p.foundInstanceBuffers[index], p.instanceBuffers[slot].Range.Offset)
	}
}

// setVertexBuffer sets buffer at offset on p's encoder at slot, unless the
// slot holds it there already.
func (p *Pass) setVertexBuffer(slot int, buffer gfx.Buffer, offset uint64) {
	if buffer != p.boundVertexBuffers[slot] || offset != p.boundVertexOffsets[slot] {
		p.encoder.SetVertexBuffer(uint32(slot), buffer, offset)
		p.boundVertexBuffers[slot] = buffer
		p.boundVertexOffsets[slot] = offset
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

// makeInspectedPass answers the pass descriptor declares, begun with
// gfxDescriptor, at position in its list, as an inspection writes it down:
// its loads and stores as gfx's attachments have them, a window's target at
// mip level and layer zero.
func makeInspectedPass(label string, descriptor PassDescriptor, gfxDescriptor gfx.RenderPassDescriptor, position int) InspectedPass {
	pass := InspectedPass{Position: position, Label: label}
	for index, target := range descriptor.ColorTargets {
		attachment := gfxDescriptor.ColorAttachments[index]
		colorTarget := ColorTarget{
			Window:     target.Window,
			Texture:    target.Texture,
			Clear:      attachment.LoadOp == gfx.LoadOpClear,
			ClearColor: Color(attachment.ClearValue),
			Discard:    attachment.StoreOp == gfx.StoreOpDiscard,
			Resolve:    ResolveTarget{Window: target.Resolve.Window, Texture: target.Resolve.Texture},
		}
		if target.Texture != (Texture{}) {
			colorTarget.MipLevel = target.MipLevel
			colorTarget.Layer = target.Layer
		}
		if target.Resolve.Texture != (Texture{}) {
			colorTarget.Resolve.MipLevel = target.Resolve.MipLevel
			colorTarget.Resolve.Layer = target.Resolve.Layer
		}
		pass.ColorTargets = append(pass.ColorTargets, colorTarget)
	}
	if target := descriptor.DepthStencil; target.Texture != (Texture{}) {
		attachment := gfxDescriptor.DepthStencilAttachment
		pass.DepthStencil = DepthStencilTarget{
			Texture:      target.Texture,
			MipLevel:     target.MipLevel,
			Layer:        target.Layer,
			Clear:        attachment.DepthLoadOp == gfx.LoadOpClear || attachment.StencilLoadOp == gfx.LoadOpClear,
			ClearDepth:   attachment.DepthClearValue,
			ClearStencil: attachment.StencilClearValue,
			Discard:      attachment.DepthStoreOp == gfx.StoreOpDiscard || attachment.StencilStoreOp == gfx.StoreOpDiscard,
			ReadOnly:     attachment.DepthReadOnly || attachment.StencilReadOnly,
		}
	}
	return pass
}

// drawnMesh is what a pass's draws found of the mesh drawn last, kept for
// its later draws of it: the mesh; the gfx buffers behind its vertex
// buffers; its index buffer as a pass binds it, none for a mesh without
// one; how many vertices and indices it draws; and its part of a draw's
// layout.
type drawnMesh struct {
	mesh                    Mesh
	vertexBuffers           drawBufferSet
	index                   indexBinding
	vertexCount, indexCount uint32
	layout                  drawLayout
}

// indexBinding is an index buffer as a pass binds it: the gfx buffer, its
// indices' format and the offset they start at.
type indexBinding struct {
	buffer gfx.Buffer
	format gfx.IndexFormat
	offset uint64
}

// drawnTarget is a texture a pass writes, at one mip level and layer; a
// read-only one is its depth-stencil target, which its draws may sample.
type drawnTarget struct {
	texture         Texture
	mipLevel, layer uint32
	readOnly        bool
}
