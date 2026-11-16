package render

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sync"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/render/internal/handle"
)

// assetTable is the private resource of every asset render holds, behind the
// handles its users keep. Every job reads it; its mutex guards the slots, and
// the loaders and made assets guard themselves.
type assetTable struct {
	reporter
	mutex sync.Mutex
	// textures is every texture render holds, and buffers every buffer, by
	// its handle.
	textures handle.SlotMap[Texture, textureSlot]
	buffers  handle.SlotMap[Buffer, bufferSlot]
	shaders  handle.SlotMap[Shader, shaderSlot]
	// meshes is every mesh render holds, and materials every material, by its
	// handle.
	meshes    handle.SlotMap[Mesh, meshSlot]
	materials handle.SlotMap[Material, materialSlot]
	// loadedTextures, loadedBuffers and loadedShaders share, count and free
	// the textures, buffers and shaders loaded from files.
	loadedTextures textureLoader
	loadedBuffers  bufferLoader
	loadedShaders  shaderLoader
	// layouts shares the layouts of the shaders' bindings among them.
	layouts layoutInterner
	// madeTextures and madeBuffers hold the made textures and buffers, each
	// by its own handle, once, until its maker releases it.
	madeTextures assetCounter[Texture, Texture]
	madeBuffers  assetCounter[Buffer, Buffer]
	// madeMeshes and madeMaterials hold the meshes and materials, as
	// madeTextures holds the made textures.
	madeMeshes    assetCounter[Mesh, Mesh]
	madeMaterials assetCounter[Material, Material]
	// vertexLayouts numbers the meshes' vertex layouts, for pipelines'
	// keys, and pipelines holds the pipelines draws made.
	vertexLayouts    vertexLayoutInterner
	pipelines        pipelineCache[pipelineKey, gfx.RenderPipeline]
	computePipelines pipelineCache[computePipelineKey, computePipeline]
	// unheldReportedMeshes and unheldReportedMaterials are the meshes and
	// materials render does not hold whose skipped draws were reported, so
	// each is reported once; a held one's slot notes it, and forgets it
	// when freed. A handle is never handed out again, so an entry names no
	// asset made later.
	unheldReportedMeshes    map[Mesh]struct{}
	unheldReportedMaterials map[Material]struct{}
	// reportedDispatches are the shaders, each with the compute function
	// run, or none for a shader whose dispatches were skipped for the
	// shader itself, whose skipped dispatches were reported, so each is
	// reported once; a held shader's are forgotten when it is freed.
	reportedDispatches map[shaderSelection]struct{}
	// bindingResources are the samplers parameters bind, shared by value,
	// and what bindings left unbound draw with, kept for the program's life.
	bindingResources bindingResourceCache
	// uniformLayouts are the checks of Go types against uniform blocks, each
	// the error saying why the type does not fit, none for one that does,
	// and reportedParameters the shaders' bindings whose refused values were
	// reported; both forget a shader when it is freed.
	uniformLayouts     map[uniformLayoutKey]error
	reportedParameters map[parameterReportKey]struct{}
	// retiredBindGroups are the material bind groups a changed value
	// replaced, which the frame's draws may still bind, released at its end,
	// and assetsFreed says a texture or buffer was freed since materials
	// were last unbound from the freed.
	retiredBindGroups []gfx.BindGroup
	assetsFreed       bool
	// endedFrameCount is how many frames have ended, which tells a
	// material's drawnFrame of the frame being recorded, and
	// versionedMaterials the materials whose uniform blocks were set after
	// a draw of it, whose buffers take their latest bytes when it ends.
	endedFrameCount    uint64
	versionedMaterials []Material
}

// addTexture makes a gfx texture of descriptor, whose counts are filled in,
// with every usage its format allows and a label naming its handle, and
// answers its handle.
func (t *assetTable) addTexture(device gfx.Device, descriptor TextureDescriptor) (Texture, error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	texture, slot := t.textures.Acquire()
	gfxTexture, err := createLabelledTexture(device, texture, descriptor)
	if err != nil {
		t.textures.Release(texture)
		return Texture{}, err
	}
	slot.texture = gfxTexture
	t.inspector.add(func(inspection *Inspection) {
		inspection.MadeAssets = append(inspection.MadeAssets, InspectedAsset{Texture: texture, Label: descriptor.Label})
	})
	return texture, nil
}

// addBuffer makes a gfx buffer of size bytes rounded up to a multiple of 4,
// the alignment of gfx's writes, with usage and the copy destination render's
// writes need, and a label naming its handle, and answers its handle and the
// gfx buffer; a size of zero makes none, so nothing writes or binds it.
func (t *assetTable) addBuffer(device gfx.Device, label string, size uint64, usage BufferUsageFlags) (Buffer, gfx.Buffer, error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	buffer, gfxBuffer, err := t.insertBuffer(device, label, size, usage)
	if gfxBuffer != (gfx.Buffer{}) {
		t.inspector.add(func(inspection *Inspection) {
			inspection.MadeAssets = append(inspection.MadeAssets, InspectedAsset{Buffer: buffer, Label: label})
		})
	}
	return buffer, gfxBuffer, err
}

// insertBuffer makes a buffer as addBuffer does. t's mutex is held.
func (t *assetTable) insertBuffer(device gfx.Device, label string, size uint64, usage BufferUsageFlags) (Buffer, gfx.Buffer, error) {
	t.requireLocked("insertBuffer")
	buffer, slot := t.buffers.Acquire()
	if size = (size + 3) &^ 3; size > 0 {
		gfxBuffer, err := createLabelledBuffer(device, buffer, label, size, usage)
		if err != nil {
			t.buffers.Release(buffer)
			return Buffer{}, gfx.Buffer{}, err
		}
		slot.buffer = gfxBuffer
		slot.size = size
	}
	return buffer, slot.buffer, nil
}

// addEmptyShader hands out the handle of a shader yet to be compiled, its
// slot empty until fillShader fills it.
func (t *assetTable) addEmptyShader() Shader {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	shader, _ := t.shaders.Acquire()
	return shader
}

// fillShader fills the slot of shader, which addEmptyShader handed out and
// nothing has removed; it panics for another.
func (t *assetTable) fillShader(shader Shader, slot shaderSlot) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	emptySlot, ok := t.shaders.Find(shader)
	if !ok {
		panic(fmt.Sprintf("render: filling shader %v, which no addEmptyShader handed out or which was removed", shader))
	}
	*emptySlot = slot
}

// addMesh holds a mesh of descriptor and answers its handle, its vertex
// layouts numbered by the vertex layout interner. A mesh is refused, with
// an error saying why, for more vertex buffers than device allows, a vertex
// buffer at an offset or of a stride no multiple of 4, an attribute at an
// offset no multiple of its format's alignment, an index buffer of no index
// format or at an offset no multiple of its index's size, or a topology gfx
// has none of.
func (t *assetTable) addMesh(device gfx.Device, descriptor MeshDescriptor) (Mesh, error) {
	if maxCount := min(device.Limits().MaxVertexBuffers, maxVertexBufferCount); uint64(len(descriptor.VertexBuffers)) > uint64(maxCount) {
		return Mesh{}, fmt.Errorf("it has %d vertex buffers, more than %d", len(descriptor.VertexBuffers), maxCount)
	}
	for index, vertexBuffer := range descriptor.VertexBuffers {
		if err := vertexBuffer.check(); err != nil {
			return Mesh{}, fmt.Errorf("vertex buffer %d %w", index, err)
		}
	}
	if indices := descriptor.Indices; indices.Range.Buffer != (Buffer{}) && (indices.Format.size() == 0 || indices.Range.Offset%indices.Format.size() != 0) {
		return Mesh{}, fmt.Errorf("its index buffer is of index format %d at offset %d, where it is of a format with indices of 2 or 4 bytes at a multiple of their size", indices.Format, indices.Range.Offset)
	}
	if descriptor.Topology > PrimitiveTopologyTriangleStrip {
		return Mesh{}, fmt.Errorf("its topology is %d, which gfx has none of", descriptor.Topology)
	}
	vertexLayout := t.vertexLayouts.intern(descriptor.VertexBuffers, gfx.VertexStepModeVertex)
	t.mutex.Lock()
	defer t.mutex.Unlock()
	mesh, slot := t.meshes.Acquire()
	slot.label = descriptor.Label
	slot.vertexBuffers = slot.vertexBuffers[:0]
	for _, vertexBuffer := range descriptor.VertexBuffers {
		slot.vertexBuffers = append(slot.vertexBuffers, meshVertexBuffer{bufferRange: vertexBuffer.Range, stride: vertexBuffer.Layout.Stride})
	}
	slot.indices = descriptor.Indices
	slot.topology = descriptor.Topology
	slot.vertexLayout = vertexLayout
	slot.reported = false
	return mesh, nil
}

// addMaterial holds a material of descriptor and answers its handle, with
// the entry points of its shader it names, when render holds the shader
// compiled; one of a shader render does not hold, or that failed, keeps
// them unresolved, no vertex entry point, and its draws are skipped. A material whose entry points
// its shader cannot resolve is held refused, its draws skipped, and the
// error says why.
func (t *assetTable) addMaterial(descriptor MaterialDescriptor) (Material, error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	material, slot := t.materials.Acquire()
	*slot = materialSlot{label: descriptor.Label, shader: descriptor.Shader, state: descriptor.State}
	shader, ok := t.shaders.Find(descriptor.Shader)
	if !ok || shader.module == (gfx.ShaderModule{}) {
		return material, nil
	}
	vertexEntryPoint, err := shader.findEntryPoint(gfx.ShaderStageFlagVertex, descriptor.VertexEntryPoint)
	if err == nil && vertexEntryPoint == "" {
		err = errors.New("names no function, and its shader has none of the vertex stage")
	}
	if err != nil {
		slot.refused = true
		return material, fmt.Errorf("its vertex entry point %w", err)
	}
	fragmentEntryPoint, err := shader.findEntryPoint(gfx.ShaderStageFlagFragment, descriptor.FragmentEntryPoint)
	if err != nil {
		slot.refused = true
		return material, fmt.Errorf("its fragment entry point %w", err)
	}
	slot.vertexEntryPoint = vertexEntryPoint
	slot.fragmentEntryPoint = fragmentEntryPoint
	slot.parameters = make([]bindingValue, len(shader.findGroupBindings(materialGroup)))
	slot.settings = make([]parameterSetting, len(slot.parameters))
	slot.uniformBlocks = make([]materialUniformBlock, len(slot.parameters))
	return material, nil
}

// setMaterialParameter binds a value to material's binding named name, which
// fill checks against the binding and writes into its parameter, a uniform
// block's bytes being uniformBytes. A name the material's shader lacks is
// ignored, as is a material whose shader failed, was freed or is not
// resolved, whose draws are skipped; a material render does not hold is
// reported once. A value fill refuses, or one naming a binding of another
// group, is reported once for the shader and name, and leaves the binding
// unbound. A uniform block set to the bytes it holds changes nothing. Before
// the material's first draw in the frame being recorded, a uniform block's
// bytes go into the buffer the material keeps for it, and a
// value that changes what the binding binds releases the material's bind
// groups. After that draw, the change makes a version: a uniform block's
// bytes are kept for the next draw to copy into the arena, and the bind
// groups, which the frame's draws may still bind, are released at its end.
// fill runs with t's mutex held, given the buffer the material keeps for the
// binding's uniform block, none until one is set. A texture or buffer value
// is set from setting, which the material keeps while the value holds, to
// fill it from again when a reload replaces its asset's gfx object; any
// other value's setting has no kind.
func (t *assetTable) setMaterialParameter(device gfx.Device, material Material, name string, uniformBytes []byte, setting parameterSetting, fill func(shader Shader, binding gfx.ShaderBinding, parameter *bindingValue, uniformBuffer *gfx.Buffer) error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	slot, ok := t.materials.Find(material)
	if !ok {
		if claimReport(&t.unheldReportedMaterials, material) {
			t.report(ReportKindParameterRefused, fmt.Errorf("render: setting %q of material %v, which render does not hold", name, material))
		}
		return
	}
	shader, ok := t.shaders.Find(slot.shader)
	if !ok || shader.module == (gfx.ShaderModule{}) || slot.vertexEntryPoint == "" {
		return
	}
	index := slices.IndexFunc(shader.bindings, func(binding gfx.ShaderBinding) bool { return binding.Name == name })
	if index < 0 {
		return
	}
	binding := shader.bindings[index]
	reportKey := parameterReportKey{shader: slot.shader, name: name}
	if binding.Group != materialGroup {
		if claimReport(&t.reportedParameters, reportKey) {
			t.report(ReportKindParameterRefused, fmt.Errorf("render: setting %q of material %q: its binding is in @group(%d), and a material's parameters are in @group(%d)", name, slot.label, binding.Group, materialGroup))
		}
		return
	}
	index -= shader.groupStarts[materialGroup]
	parameter := &slot.parameters[index]
	block := &slot.uniformBlocks[index]
	previous := *parameter
	drawn := slot.drawnFrame == t.endedFrameCount+1
	changed := false
	slot.settings[index] = setting
	switch err := fill(slot.shader, binding, parameter, &block.buffer); {
	case err != nil:
		*parameter = bindingValue{}
		slot.settings[index] = parameterSetting{}
		if claimReport(&t.reportedParameters, reportKey) {
			t.report(ReportKindParameterRefused, fmt.Errorf("render: setting %q of material %q: the value %w; its binding draws its default", name, slot.label, err))
		}
	case parameter.kind != parameterKindUniform:
	case previous.kind == parameterKindUniform && bytes.Equal(block.latestBytes, uniformBytes):
		*parameter = previous
	case !drawn:
		block.latestBytes = append(block.latestBytes[:0], uniformBytes...)
		writeBytes(t, device, Buffer{}, block.buffer, 0, uniformBytes)
	default:
		if !slices.ContainsFunc(slot.uniformBlocks, func(other materialUniformBlock) bool { return other.bufferBehind }) {
			t.versionedMaterials = append(t.versionedMaterials, material)
			t.inspector.add(func(inspection *Inspection) {
				inspection.VersionedMaterials = append(inspection.VersionedMaterials, material)
			})
		}
		block.latestBytes = append(block.latestBytes[:0], uniformBytes...)
		block.bufferBehind = true
		changed = true
	}
	if changed || *parameter != previous {
		if drawn {
			slot.dropBindGroups(func(group gfx.BindGroup) { t.retiredBindGroups = append(t.retiredBindGroups, group) })
		} else {
			slot.dropBindGroups(device.ReleaseBindGroup)
		}
	}
}

// allocateTemporary answers a range of listState's arena chunks holding
// data, as commandListState.allocate does, taking t's mutex only when the
// list's current chunk lacks room, so concurrent lists contend only when one
// takes a chunk.
func (t *assetTable) allocateTemporary(listState *commandListState, data []byte, device gfx.Device) (BufferRange, error) {
	if bufferRange, ok := listState.placeInCurrentChunk(data); ok {
		return bufferRange, nil
	}
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return listState.allocate(data, t, device)
}

// findSlot answers a copy of h's slot among slots, a shader's the zero slot
// for one that failed; false when render holds no such asset.
func (t *assetTable) findSlot[H handle.Handle, S any](slots *handle.SlotMap[H, S], h H) (S, bool) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	slot, ok := slots.Find(h)
	if !ok {
		var none S
		return none, false
	}
	return *slot, true
}

// findDrawBindGroups fills bindGroups with what a draw of material, of
// shader, binds for each group of its layout into a pass writing targets,
// whose pass and draw parameters listState holds, and notes material drawn
// in the frame: the material's own bind group of @group(1), made the first
// time for the textures the targets leave at their defaults, a version's
// uniform blocks copied into listState's arena; and of @group(0) and @group(2), unless bindGroups holds
// them resolved for its shader and functions, a bind group of the pass's or the draw's parameters with
// their dynamic offsets, shared by every draw binding the same, or the
// layout's default where none is set. With each goes the name of a storage
// binding of its group left unbound that a function material runs reads,
// for which the draw is skipped. A value that cannot fill its binding, names
// a binding of another group, or is a texture overlapping one of targets,
// draws its default, reported once for its shader and name. The error is
// gfx's, when a bind group or what it binds cannot be made, or says
// material is no longer held.
func (t *assetTable) findDrawBindGroups(device gfx.Device, material Material, shader shaderSlot, targets []drawnTarget, listState *commandListState, bindGroups *bindGroupSet) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	slot, ok := t.materials.Find(material)
	if !ok {
		return fmt.Errorf("render: material %v is no longer held", material)
	}
	slot.drawnFrame = t.endedFrameCount + 1
	functions := slot.makeShaderSelection()
	for group, groupLayout := range shader.layout.groups {
		var err error
		switch {
		case group == materialGroup:
			var materialBindGroup materialBindGroup
			materialBindGroup, err = t.findMaterialBindGroup(device, slot, shader, targets, listState)
			binding := groupBinding{bindGroup: materialBindGroup.group}
			if binding.bindGroup == (gfx.BindGroup{}) && err == nil {
				binding.bindGroup, err = t.layouts.findDefaultBindGroup(device, &t.bindingResources, groupLayout, t.inspector)
			}
			bindGroups.groupBindings[group] = binding
			bindGroups.unboundReads[group] = materialBindGroup.unboundRead
			if listState.inspectedList != nil {
				bindGroups.inspectedParameters[group] = t.makeInspectedMaterialParameters(slot, shader, materialBindGroup, listState.arena)
			}
		case bindGroups.resolvedFor[group] != functions:
			level := "pass parameter"
			if group == drawGroup {
				level = "draw parameter"
			}
			settings := listState.parameterSettings[group]
			for _, setting := range settings {
				index := slices.IndexFunc(shader.bindings, func(binding gfx.ShaderBinding) bool { return binding.Name == setting.name })
				if index >= 0 && shader.bindings[index].Group != uint32(group) && claimReport(&t.reportedParameters, parameterReportKey{shader: slot.shader, name: setting.name}) {
					t.report(ReportKindParameterRefused, fmt.Errorf("render: %s %q names a binding of shader %v in @group(%d), and %ss are in @group(%d)", level, setting.name, slot.shader, shader.bindings[index].Group, level, group))
				}
			}
			bindGroups.groupBindings[group], bindGroups.unboundReads[group], bindGroups.inspectedParameters[group], err = t.makeParameterGroupBinding(device, uint32(group), settings, level, functions, shader, targets, listState)
			if err == nil {
				bindGroups.resolvedFor[group] = functions
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// findDispatchBindGroups fills bindGroups with what a dispatch running
// selection, of shader, binds for each group of its layout, unless
// bindGroups holds it resolved for selection: a bind group of the compute
// pass's parameters, which listState holds, with their dynamic offsets,
// shared by every dispatch or draw binding the same, or the layout's
// default where none is set. With each goes the name of a storage binding
// of its group left unbound that selection's function reads, for which the
// dispatch is skipped. A value that cannot fill its binding draws its
// default, reported once for its shader and name. t's mutex is taken only
// when a group is not resolved. The error is gfx's, when a bind group or
// what it binds cannot be made.
func (t *assetTable) findDispatchBindGroups(device gfx.Device, selection shaderSelection, shader shaderSlot, listState *commandListState, bindGroups *bindGroupSet) error {
	if !slices.ContainsFunc(bindGroups.resolvedFor[:len(shader.layout.groups)], func(resolved shaderSelection) bool { return resolved != selection }) {
		return nil
	}
	t.mutex.Lock()
	defer t.mutex.Unlock()
	for group := range shader.layout.groups {
		if bindGroups.resolvedFor[group] == selection {
			continue
		}
		var err error
		bindGroups.groupBindings[group], bindGroups.unboundReads[group], bindGroups.inspectedParameters[group], err = t.makeParameterGroupBinding(device, uint32(group), listState.computeSettings, "compute parameter", selection, shader, nil, listState)
		if err != nil {
			return err
		}
		bindGroups.resolvedFor[group] = selection
	}
	return nil
}

// unbindFreed unbinds, when a texture or buffer was freed since it last ran,
// every material value naming one render no longer holds, releasing its
// material's bind groups, so the binding draws its default, and releases
// every bind group of pass or draw parameters naming one, so render keeps no
// handle it released. It runs at the end of each frame, after every free.
func (t *assetTable) unbindFreed(device gfx.Device) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if !t.assetsFreed {
		return
	}
	t.assetsFreed = false
	for _, slot := range t.materials.All() {
		changed := false
		for index := range slot.parameters {
			parameter := &slot.parameters[index]
			var held bool
			switch parameter.kind {
			case parameterKindTexture:
				_, held = t.textures.Find(parameter.texture)
			case parameterKindBuffer:
				_, held = t.buffers.Find(parameter.bufferRange.Buffer)
			default:
				continue
			}
			if !held {
				*parameter = bindingValue{}
				slot.settings[index] = parameterSetting{}
				changed = true
			}
		}
		if changed {
			slot.dropBindGroups(device.ReleaseBindGroup)
		}
	}
	t.layouts.releaseBindGroups(device, func(cached *parameterBindGroup) bool {
		return slices.ContainsFunc(cached.namedTextures, func(texture Texture) bool {
			_, held := t.textures.Find(texture)
			return !held
		}) || slices.ContainsFunc(cached.namedBuffers, func(buffer Buffer) bool {
			_, held := t.buffers.Find(buffer)
			return !held
		})
	})
}

// endMaterialVersions ends the frame for materials: each uniform block set
// after a draw of the frame has its latest bytes written into the buffer its
// material keeps for it, landing before gfx's next submit, and binds that
// buffer again, its material's bind groups, which name the arena, released;
// and no material is drawn in the next frame yet. It runs at the end of
// each frame, after the present.
func (t *assetTable) endMaterialVersions(device gfx.Device) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	for _, material := range t.versionedMaterials {
		slot, ok := t.materials.Find(material)
		if !ok {
			continue
		}
		for index := range slot.uniformBlocks {
			block := &slot.uniformBlocks[index]
			if !block.bufferBehind {
				continue
			}
			writeBytes(t, device, Buffer{}, block.buffer, 0, block.latestBytes)
			block.bufferBehind = false
			if parameter := &slot.parameters[index]; parameter.kind == parameterKindUniform {
				parameter.bufferRange = BufferRange{}
			}
		}
		slot.dropBindGroups(device.ReleaseBindGroup)
	}
	t.versionedMaterials = t.versionedMaterials[:0]
	t.endedFrameCount++
}

// refillDefaultTexturesAndUniforms fills again what render fills itself, after a device loss left it
// empty: the default textures, and the buffer of each uniform block a
// material keeps with the block's latest bytes, which its maker cannot
// write again, as setting the same bytes changes nothing. A write that
// fails is logged, unless the device is lost again.
func (t *assetTable) refillDefaultTexturesAndUniforms(device gfx.Device) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if err := t.bindingResources.refillTextures(device); err != nil {
		t.logUnlessLost(fmt.Errorf("render: refilling the default textures: %w", err))
	}
	for _, slot := range t.materials.All() {
		for _, block := range slot.uniformBlocks {
			if block.buffer != (gfx.Buffer{}) {
				writeBytes(t, device, Buffer{}, block.buffer, 0, block.latestBytes)
			}
		}
	}
}

// swapReloads swaps in, under their handles, the textures and buffers
// ReloadChanged reloaded since it last ran, so the frame about to record
// draws every one of them new: a texture or buffer reloaded at another size
// gets the gfx object made for it, its predecessor released with its views
// and every bind group naming it, and its material values filled again, as
// refillReplaced says; and its new contents are written, for gfx's next
// submit, into the one behind its handle. A reload of an asset render no
// longer holds is dropped, its replacement released. It answers the
// textures reloaded with Mipmaps, whose mips are yet to be generated. It
// runs at the start of each frame, before the frame records and after the
// last one's present, so the last frame that drew a predecessor has ended.
func (t *assetTable) swapReloads(device gfx.Device) []Texture {
	textureReloads := t.loadedTextures.takeReloads(&t.loadedTextures.pendingReloads)
	bufferReloads := t.loadedBuffers.takeReloads(&t.loadedBuffers.pendingReloads)
	if len(textureReloads) == 0 && len(bufferReloads) == 0 {
		return nil
	}
	t.mutex.Lock()
	defer t.mutex.Unlock()
	var unmippedTextures, replacedTextures []Texture
	var replacedBuffers []Buffer
	for _, reload := range textureReloads {
		slot, ok := t.textures.Find(reload.handle)
		switch {
		case !ok && reload.replacement != (gfx.Texture{}):
			device.ReleaseTexture(reload.replacement)
			continue
		case !ok:
			continue
		case reload.replacement != (gfx.Texture{}):
			slot.releaseTexture(device)
			slot.texture = reload.replacement
			replacedTextures = append(replacedTextures, reload.handle)
		}
		writeTexels(t, device, reload.handle, gfx.ImageCopyTexture{Texture: slot.texture, Aspect: gfx.TextureAspectAll}, reload.contents, 4, reload.size)
		if reload.mipmaps {
			unmippedTextures = append(unmippedTextures, reload.handle)
		}
	}
	for _, reload := range bufferReloads {
		slot, ok := t.buffers.Find(reload.handle)
		switch {
		case !ok && reload.replacement != (gfx.Buffer{}):
			device.ReleaseBuffer(reload.replacement)
			continue
		case !ok:
			continue
		case reload.replacement != (gfx.Buffer{}):
			if slot.buffer != (gfx.Buffer{}) {
				device.ReleaseBuffer(slot.buffer)
			}
			slot.buffer = reload.replacement
			slot.size = uint64(len(reload.contents))
			replacedBuffers = append(replacedBuffers, reload.handle)
		}
		writeBytes(t, device, reload.handle, slot.buffer, 0, reload.contents)
	}
	t.refillReplaced(device, replacedTextures, replacedBuffers)
	return unmippedTextures
}

// refillReplaced fills again, from the setting it was set from, each
// material value naming one of textures or buffers, whose gfx objects a
// reload replaced, through the checks its setter ran, so a view of every mip
// level sees every one of a new texture and a range of the rest of a buffer
// ends at its new end; a value that no longer fits is unbound, drawing its
// binding's default, and reported, its setting kept, so a later reload it
// fits binds it again. It releases the bind groups of each
// material with such a value, and every bind group of pass or draw
// parameters naming one of textures or buffers, each made again at its next
// use. t's mutex is held.
func (t *assetTable) refillReplaced(device gfx.Device, textures []Texture, buffers []Buffer) {
	t.requireLocked("refillReplaced")
	if len(textures) == 0 && len(buffers) == 0 {
		return
	}
	for _, slot := range t.materials.All() {
		shader, ok := t.shaders.Find(slot.shader)
		if !ok {
			continue
		}
		bindings := shader.findGroupBindings(materialGroup)
		replaced := false
		for index, setting := range slot.settings {
			if !(setting.kind == parameterKindTexture && slices.Contains(textures, setting.view.Texture)) && !(setting.kind == parameterKindBuffer && slices.Contains(buffers, setting.bufferRange.Buffer)) {
				continue
			}
			replaced = true
			parameter := &slot.parameters[index]
			*parameter = bindingValue{}
			if err := t.fillSettingValue(device, slot.shader, bindings[index], setting, parameter); err != nil {
				t.report(ReportKindParameterRefused, fmt.Errorf("render: %q of material %q, after a reload: the value %w; its binding draws its default", bindings[index].Name, slot.label, err))
			}
		}
		if replaced {
			slot.dropBindGroups(device.ReleaseBindGroup)
		}
	}
	t.layouts.releaseBindGroups(device, func(cached *parameterBindGroup) bool {
		return slices.ContainsFunc(cached.namedTextures, func(texture Texture) bool { return slices.Contains(textures, texture) }) ||
			slices.ContainsFunc(cached.namedBuffers, func(buffer Buffer) bool { return slices.Contains(buffers, buffer) })
	})
}

// claimMeshReport answers whether a skipped draw of mesh is to be reported,
// noting that it is, so later draws of mesh are not: true the first time
// for each mesh, held or not.
func (t *assetTable) claimMeshReport(mesh Mesh) bool {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if slot, ok := t.meshes.Find(mesh); ok {
		claimed := !slot.reported
		slot.reported = true
		return claimed
	}
	return claimReport(&t.unheldReportedMeshes, mesh)
}

// claimMaterialReport answers whether a skipped draw of material is to be
// reported, as claimMeshReport does for a mesh.
func (t *assetTable) claimMaterialReport(material Material) bool {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	if slot, ok := t.materials.Find(material); ok {
		claimed := !slot.reported
		slot.reported = true
		return claimed
	}
	return claimReport(&t.unheldReportedMaterials, material)
}

// claimDispatchReport answers whether a skipped dispatch of selection is to
// be reported, noting that it is, so later dispatches of selection are not.
func (t *assetTable) claimDispatchReport(selection shaderSelection) bool {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return claimReport(&t.reportedDispatches, selection)
}

// findTexture answers the gfx texture behind texture; false when render holds
// no such texture.
func (t *assetTable) findTexture(texture Texture) (gfx.Texture, bool) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	slot, ok := t.textures.Find(texture)
	if !ok {
		return gfx.Texture{}, false
	}
	return slot.texture, true
}

// findBuffer answers the gfx buffer behind buffer and its size; false when
// render holds no such buffer, or it has no bytes.
func (t *assetTable) findBuffer(buffer Buffer) (gfx.Buffer, uint64, bool) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	slot, ok := t.buffers.Find(buffer)
	if !ok || slot.buffer == (gfx.Buffer{}) {
		return gfx.Buffer{}, 0, false
	}
	return slot.buffer, slot.size, true
}

// findLevelView answers the view of texture's mip level and layer, made the
// first time a pass draws into or samples it, with the descriptor gfx made the texture
// from; an error naming what it lacks when render holds no such texture, or
// it has no such mip level or layer.
func (t *assetTable) findLevelView(device gfx.Device, texture Texture, mipLevel, layer uint32) (gfx.TextureView, gfx.TextureDescriptor, error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	slot, ok := t.textures.Find(texture)
	if !ok {
		return gfx.TextureView{}, gfx.TextureDescriptor{}, errors.New("a texture render does not hold")
	}
	descriptor := device.TextureDescriptor(slot.texture)
	if mipLevel >= descriptor.MipLevelCount || layer >= descriptor.Size.DepthOrArrayLayers {
		return gfx.TextureView{}, gfx.TextureDescriptor{}, fmt.Errorf("%q at mip level %d and layer %d, of %d mip levels and %d layers", descriptor.Label, mipLevel, layer, descriptor.MipLevelCount, descriptor.Size.DepthOrArrayLayers)
	}
	view, err := slot.findTextureView(device, makeLevelViewDescriptor(mipLevel, layer))
	if err != nil {
		return gfx.TextureView{}, gfx.TextureDescriptor{}, err
	}
	return view, descriptor, nil
}

// writeBuffer writes data into buffer at offset, for gfx's next submit. A
// write that does not fit is refused and reported: into a buffer render does
// not hold or of no bytes, at an offset or of a length not a multiple of 4,
// or ending past the buffer.
func (t *assetTable) writeBuffer(device gfx.Device, buffer Buffer, offset uint64, data []byte) {
	gfxBuffer, size, ok := t.findBuffer(buffer)
	if !ok {
		t.report(ReportKindWriteRefused, fmt.Errorf("render: writing buffer %v, which render does not hold, or which has no bytes", buffer))
		return
	}
	length := uint64(len(data))
	if offset%4 != 0 || length%4 != 0 || offset > size || length > size-offset {
		t.report(ReportKindWriteRefused, fmt.Errorf("render: writing %d bytes at offset %d into buffer %v, of %d bytes, where the offset and the length are multiples of 4 and the write ends within the buffer", length, offset, buffer, size))
		return
	}
	writeBytes(t, device, buffer, gfxBuffer, offset, data)
}

// writeTexture writes data, rows of texels packed tight, into region of
// texture, for gfx's next submit. A write that does not fit is refused and
// reported: into a texture render does not hold, of several samples or of a
// format gfx's writes cannot fill, at a mip level or layer it lacks, into a
// region past its level's edges, or of data not as long as the region.
func (t *assetTable) writeTexture(device gfx.Device, texture Texture, region TextureRegion, data []byte) {
	gfxTexture, ok := t.findTexture(texture)
	if !ok {
		t.report(ReportKindWriteRefused, fmt.Errorf("render: writing texture %v, which render does not hold", texture))
		return
	}
	descriptor := device.TextureDescriptor(gfxTexture)
	texelSize := TextureFormat(descriptor.Format).info().texelSize
	if texelSize == 0 || descriptor.SampleCount > 1 {
		t.report(ReportKindWriteRefused, fmt.Errorf("render: writing texture %q, of %d samples in format %d, where it has one sample and a format gfx's writes fill", descriptor.Label, descriptor.SampleCount, descriptor.Format))
		return
	}
	width, height, err := region.findSize(descriptor)
	if err == nil && uint64(len(data)) != uint64(width)*uint64(height)*uint64(texelSize) {
		err = fmt.Errorf("is %d×%d texels of %d bytes each, which %d bytes do not fill", width, height, texelSize, len(data))
	}
	if err != nil {
		t.report(ReportKindWriteRefused, fmt.Errorf("render: writing a region that %w", err))
		return
	}
	writeTexels(t, device, texture, makeImageCopyTexture(gfxTexture, region), data, texelSize, gfx.Extent3D{Width: width, Height: height, DepthOrArrayLayers: 1})
}

// freeReleased removes every loaded texture, buffer and shader whose last
// acquire was released since the last frame ended and is not acquired again,
// and every made texture, buffer, mesh and material released since then.
func (t *assetTable) freeReleased(device gfx.Device) {
	removeTexture := func(texture Texture) { t.removeTexture(device, texture) }
	removeBuffer := func(buffer Buffer) { t.removeBuffer(device, buffer) }
	t.loadedTextures.freeReleased(removeTexture)
	t.loadedBuffers.freeReleased(removeBuffer)
	t.loadedShaders.freeReleased(func(shader Shader) { t.removeShader(device, shader) })
	t.madeTextures.freeReleased(removeTexture)
	t.madeBuffers.freeReleased(removeBuffer)
	t.madeMeshes.freeReleased(t.removeMesh)
	t.madeMaterials.freeReleased(func(material Material) { t.removeMaterial(device, material) })
	t.mutex.Lock()
	defer t.mutex.Unlock()
	for _, group := range t.retiredBindGroups {
		device.ReleaseBindGroup(group)
	}
	t.retiredBindGroups = t.retiredBindGroups[:0]
}

// removeTexture releases the gfx texture behind texture and its views, and
// frees its slot for a handle of the next generation.
func (t *assetTable) removeTexture(device gfx.Device, texture Texture) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	slot, ok := t.textures.Find(texture)
	if !ok {
		return
	}
	slot.releaseTexture(device)
	t.textures.Release(texture)
	t.assetsFreed = true
}

// removeBuffer releases the gfx buffer behind buffer, and frees its slot for
// a handle of the next generation.
func (t *assetTable) removeBuffer(device gfx.Device, buffer Buffer) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	slot, ok := t.buffers.Find(buffer)
	if !ok {
		return
	}
	if slot.buffer != (gfx.Buffer{}) {
		device.ReleaseBuffer(slot.buffer)
	}
	*slot = bufferSlot{}
	t.buffers.Release(buffer)
	t.assetsFreed = true
}

// removeMesh frees mesh's slot for a handle of the next generation, keeping
// its arrays for the next mesh made.
func (t *assetTable) removeMesh(mesh Mesh) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.meshes.Release(mesh)
}

// removeMaterial releases material's bind groups and the buffers of its
// uniform blocks, and frees its slot for a handle of the next generation.
func (t *assetTable) removeMaterial(device gfx.Device, material Material) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	slot, ok := t.materials.Find(material)
	if !ok {
		return
	}
	slot.dropBindGroups(device.ReleaseBindGroup)
	for _, block := range slot.uniformBlocks {
		if block.buffer != (gfx.Buffer{}) {
			device.ReleaseBuffer(block.buffer)
		}
	}
	t.materials.Release(material)
}

// removeShader releases the module behind shader, its holding of its layout
// and its pipelines, and frees its slot for a handle of the next generation.
func (t *assetTable) removeShader(device gfx.Device, shader Shader) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	slot, ok := t.shaders.Find(shader)
	if !ok {
		return
	}
	t.pipelines.removeShader(shader, device.ReleaseRenderPipeline)
	t.computePipelines.removeShader(shader, func(pipeline computePipeline) { device.ReleaseComputePipeline(pipeline.pipeline) })
	maps.DeleteFunc(t.reportedDispatches, func(key shaderSelection, _ struct{}) bool { return key.shader == shader })
	maps.DeleteFunc(t.uniformLayouts, func(key uniformLayoutKey, _ error) bool { return key.shader == shader })
	maps.DeleteFunc(t.reportedParameters, func(key parameterReportKey, _ struct{}) bool { return key.shader == shader })
	if slot.module != (gfx.ShaderModule{}) {
		device.ReleaseShaderModule(slot.module)
		t.layouts.release(device, slot.layout)
	}
	*slot = shaderSlot{}
	t.shaders.Release(shader)
}

// checkUniform answers why a value of goType cannot fill binding, of
// shader: a binding of no uniform block, or a layout checkUniformLayout
// refuses, checked once for each type, shader and binding. t's mutex is
// held.
func (t *assetTable) checkUniform(goType reflect.Type, shader Shader, binding gfx.ShaderBinding) error {
	t.requireLocked("checkUniform")
	if binding.Buffer.Type != gfx.BufferBindingTypeUniform {
		return fmt.Errorf("is a uniform block, where its binding is a %s", describeBindingKind(binding))
	}
	key := uniformLayoutKey{goType: goType, shader: shader, name: binding.Name}
	err, checked := t.uniformLayouts[key]
	if !checked {
		err = checkUniformLayout(goType, binding)
		if t.uniformLayouts == nil {
			t.uniformLayouts = map[uniformLayoutKey]error{}
		}
		t.uniformLayouts[key] = err
	}
	return err
}

// makeMaterialBindGroup answers slot's bind group of its shader's
// @group(1), binding each binding to its value, or to its default when it is
// unbound, names an asset render no longer holds or one of no bytes, or is a
// texture of overlapMask; none when the group is empty. With it goes the
// name of a storage binding of it left at its default that a function slot
// runs reads, empty for none. t's mutex is held.
func (t *assetTable) makeMaterialBindGroup(device gfx.Device, slot *materialSlot, shader shaderSlot, overlapMask uint64) (materialBindGroup, error) {
	t.requireLocked("makeMaterialBindGroup")
	bindGroup := materialBindGroup{defaultedMask: overlapMask}
	bindings := shader.findGroupBindings(materialGroup)
	if len(bindings) == 0 {
		return bindGroup, nil
	}
	layout := shader.layout.groups[materialGroup]
	entries := make([]gfx.BindGroupEntry, len(bindings))
	for index, binding := range bindings {
		value := slot.parameters[index]
		if overlapMask&(1<<index) != 0 {
			value = bindingValue{}
		}
		entry, defaulted, err := t.makeValueEntry(device, value, binding, layout.entries[index], slot.uniformBlocks[index].buffer)
		if err != nil {
			return materialBindGroup{}, err
		}
		if defaulted && index < 64 {
			bindGroup.drawnDefaultMask |= 1 << index
		}
		if defaulted && bindGroup.unboundRead == "" && isStorage(binding) && slot.makeShaderSelection().usesBinding(binding) {
			bindGroup.unboundRead = binding.Name
		}
		entries[index] = entry
	}
	group, err := BindGroupKindMaterial.createBindGroup(device, t.inspector, layout.layout, entries)
	bindGroup.group = group
	return bindGroup, err
}

// makeInspectedMaterialParameters answers the values slot binds to its
// shader's @group(1) through bindGroup, as an inspection writes them down,
// those bindGroup draws the defaults of as defaults, its uniform blocks'
// bytes in arena or its own buffers. t's mutex is held.
func (t *assetTable) makeInspectedMaterialParameters(slot *materialSlot, shader shaderSlot, bindGroup materialBindGroup, arena *temporaryArena) []InspectedParameter {
	t.requireLocked("makeInspectedMaterialParameters")
	var parameters []InspectedParameter
	for index, binding := range shader.findGroupBindings(materialGroup) {
		block := slot.uniformBlocks[index]
		parameters = append(parameters, slot.parameters[index].makeInspectedParameter(binding, bindGroup.drawnDefaultMask&(1<<index) != 0, block.latestBytes, arena))
	}
	return parameters
}

// findMaterialBindGroup answers slot's bind group of its shader's @group(1)
// for a pass writing targets, made the first time for the textures the
// targets leave at their defaults, each such texture reported once for its
// shader and name, with the latest bytes of each uniform block set after a
// draw of the frame first copied into listState's arena. t's mutex is held.
func (t *assetTable) findMaterialBindGroup(device gfx.Device, slot *materialSlot, shader shaderSlot, targets []drawnTarget, listState *commandListState) (materialBindGroup, error) {
	t.requireLocked("findMaterialBindGroup")
	bindings := shader.findGroupBindings(materialGroup)
	overlapMask := slot.findOverlapMask(bindings, targets)
	if bindGroup, ok := slot.findBindGroup(overlapMask); ok {
		return bindGroup, nil
	}
	for index, binding := range bindings {
		if overlapMask&(1<<index) != 0 && claimReport(&t.reportedParameters, parameterReportKey{shader: slot.shader, name: binding.Name}) {
			t.report(ReportKindParameterRefused, fmt.Errorf("render: %q of material %q is a texture a target of its pass overlaps; its binding draws its default", binding.Name, slot.label))
		}
	}
	if err := slot.placeLatestUniforms(listState, t, device); err != nil {
		return materialBindGroup{}, err
	}
	bindGroup, err := t.makeMaterialBindGroup(device, slot, shader, overlapMask)
	if err != nil {
		return materialBindGroup{}, err
	}
	slot.bindGroups = append(slot.bindGroups, bindGroup)
	return bindGroup, nil
}

// makeParameterGroupBinding answers the bind group of group of a draw or
// dispatch running selection, of shader, into a pass writing targets, with
// its dynamic offsets: each binding bound to the value of settings, the
// pass's, the draw's or the dispatch's parameters, by its name, or to its
// default when none is set, it is refused, or it is a texture overlapping
// one of targets, the last two reported once for the shader and the name,
// as values of level, such as "pass parameter".
// It is built in draft and shared by every draw or dispatch binding the
// same; the layout's default when nothing is set. With it goes the name of a
// storage binding of the group left at its default that a function
// selection runs reads, empty for none, and, while listState's list is
// inspected, the value bound to each binding as the inspection writes it
// down. t's mutex is held.
func (t *assetTable) makeParameterGroupBinding(device gfx.Device, group uint32, settings []parameterSetting, level string, selection shaderSelection, shader shaderSlot, targets []drawnTarget, listState *commandListState) (groupBinding, string, []InspectedParameter, error) {
	t.requireLocked("makeParameterGroupBinding")
	draft := &listState.bindGroupDraft
	var inspectedParameters []InspectedParameter
	layout := shader.layout.groups[group]
	binding := groupBinding{dynamicOffsetCount: layout.dynamicOffsetCount}
	var unboundRead string
	draft.entries = draft.entries[:0]
	draft.namedTextures = draft.namedTextures[:0]
	draft.namedBuffers = draft.namedBuffers[:0]
	dynamicOffsetCount := 0
	for index, shaderBinding := range shader.findGroupBindings(group) {
		var value bindingValue
		if settingIndex := slices.IndexFunc(settings, func(setting parameterSetting) bool { return setting.name == shaderBinding.Name }); settingIndex >= 0 {
			err := t.fillSettingValue(device, selection.shader, shaderBinding, settings[settingIndex], &value)
			if err == nil && value.overlapsTargets(shaderBinding.Texture.SampleType != gfx.TextureSampleTypeUndefined, targets) {
				err = errors.New("is a texture a target of its pass overlaps")
			}
			if err != nil {
				value = bindingValue{}
				if claimReport(&t.reportedParameters, parameterReportKey{shader: selection.shader, name: shaderBinding.Name}) {
					t.report(ReportKindParameterRefused, fmt.Errorf("render: %s %q under shader %v: the value %w; its binding draws its default", level, shaderBinding.Name, selection.shader, err))
				}
			}
		}
		entry, defaulted, err := t.makeValueEntry(device, value, shaderBinding, layout.entries[index], gfx.Buffer{})
		if err != nil {
			return groupBinding{}, "", nil, err
		}
		if listState.inspectedList != nil {
			inspectedParameters = append(inspectedParameters, value.makeInspectedParameter(shaderBinding, defaulted, nil, listState.arena))
		}
		switch {
		case defaulted:
			if unboundRead == "" && isStorage(shaderBinding) && selection.usesBinding(shaderBinding) {
				unboundRead = shaderBinding.Name
			}
		case value.kind == parameterKindTexture:
			draft.namedTextures = append(draft.namedTextures, value.texture)
		case value.kind == parameterKindBuffer || value.kind == parameterKindUniform:
			draft.namedBuffers = append(draft.namedBuffers, value.bufferRange.Buffer)
		}
		if layout.entries[index].Buffer.HasDynamicOffset {
			binding.dynamicOffsets[dynamicOffsetCount] = uint32(entry.Offset)
			dynamicOffsetCount++
			entry.Offset = 0
		}
		draft.entries = append(draft.entries, entry)
	}
	var err error
	if len(settings) == 0 {
		binding.bindGroup, err = t.layouts.findDefaultBindGroup(device, &t.bindingResources, layout, t.inspector)
	} else {
		binding.bindGroup, err = t.layouts.findParameterBindGroup(device, layout, draft, t.inspector)
	}
	return binding, unboundRead, inspectedParameters, err
}

// makeValueEntry answers the entry binding value to binding, of layout entry
// layoutEntry: a texture's view, a sampler, a range of a buffer, or a uniform
// block, in value's range of the arena, its offset in the entry, or with no
// range in uniformBuffer, a material's; or, when value is unbound or names an asset
// render no longer holds or one of no bytes, the entry the binding draws
// unbound with, and true. t's mutex is held.
func (t *assetTable) makeValueEntry(device gfx.Device, value bindingValue, binding gfx.ShaderBinding, layoutEntry gfx.BindGroupLayoutEntry, uniformBuffer gfx.Buffer) (gfx.BindGroupEntry, bool, error) {
	t.requireLocked("makeValueEntry")
	entry := gfx.BindGroupEntry{Binding: binding.Binding}
	var err error
	switch {
	case value.kind == parameterKindTexture:
		if textureSlot, ok := t.textures.Find(value.texture); ok {
			entry.TextureView, err = textureSlot.findTextureView(device, value.view)
		}
	case value.kind == parameterKindSampler:
		entry.Sampler, err = t.bindingResources.findSampler(device, value.sampler)
	case value.kind == parameterKindUniform && value.bufferRange.Buffer == (Buffer{}):
		entry.Buffer = uniformBuffer
		entry.Size = binding.Buffer.MinBindingSize
	case value.kind == parameterKindBuffer || value.kind == parameterKindUniform:
		if bufferSlot, ok := t.buffers.Find(value.bufferRange.Buffer); ok {
			entry.Buffer = bufferSlot.buffer
			entry.Offset = value.bufferRange.Offset
			entry.Size = value.bufferRange.Size
		}
	}
	if err != nil || entry.TextureView != (gfx.TextureView{}) || entry.Sampler != (gfx.Sampler{}) || entry.Buffer != (gfx.Buffer{}) {
		return entry, false, err
	}
	entry, err = t.bindingResources.makeEntry(device, layoutEntry)
	return entry, true, err
}

// fillTextureValue binds view, of a texture render holds, to value, of
// binding, or answers why it does not fit, wholeTexture saying view names its
// texture alone, which a storage binding sees at its first mip level. t's
// mutex is held.
func (t *assetTable) fillTextureValue(device gfx.Device, value *bindingValue, binding gfx.ShaderBinding, view TextureView, wholeTexture bool) error {
	t.requireLocked("fillTextureValue")
	textureSlot, ok := t.textures.Find(view.Texture)
	if !ok {
		return fmt.Errorf("names texture %v, which render does not hold", view.Texture)
	}
	if wholeTexture && binding.StorageTexture.Access != gfx.StorageTextureAccessUndefined {
		view.MipLevelCount = 1
	}
	return value.fillTexture(view, device.TextureDescriptor(textureSlot.texture), binding)
}

// fillBufferValue binds bufferRange, of a buffer render holds, to value, of
// binding, or answers why it does not fit. t's mutex is held.
func (t *assetTable) fillBufferValue(device gfx.Device, value *bindingValue, binding gfx.ShaderBinding, bufferRange BufferRange) error {
	t.requireLocked("fillBufferValue")
	bufferSlot, ok := t.buffers.Find(bufferRange.Buffer)
	if !ok || bufferSlot.buffer == (gfx.Buffer{}) {
		return fmt.Errorf("names buffer %v, which render does not hold, or which has no bytes", bufferRange.Buffer)
	}
	return value.fillBuffer(bufferRange, device.BufferDescriptor(bufferSlot.buffer), binding, device.Limits())
}

// fillSettingValue binds setting, a pass or draw parameter, to value, of
// binding, of shader, or answers why it does not fit; a setting of no kind
// leaves value unbound. t's mutex is held.
func (t *assetTable) fillSettingValue(device gfx.Device, shader Shader, binding gfx.ShaderBinding, setting parameterSetting, value *bindingValue) error {
	t.requireLocked("fillSettingValue")
	switch setting.kind {
	case parameterKindTexture:
		return t.fillTextureValue(device, value, binding, setting.view, setting.wholeTexture)
	case parameterKindSampler:
		return value.fillSampler(setting.sampler, binding)
	case parameterKindBuffer:
		return t.fillBufferValue(device, value, binding, setting.bufferRange)
	case parameterKindUniform:
		if err := t.checkUniform(setting.uniformType, shader, binding); err != nil {
			return err
		}
		value.kind = parameterKindUniform
		value.bufferRange = setting.bufferRange
	}
	return nil
}

// requireLocked panics, naming operation, when t's mutex is free: a call
// that must hold it does not.
func (t *assetTable) requireLocked(operation string) {
	if t.mutex.TryLock() {
		t.mutex.Unlock()
		panic("render: the asset table's " + operation + " is called without its mutex held")
	}
}

// bufferSlot is the slot of one buffer: the gfx buffer behind its handle,
// none for a buffer of no bytes, and its size.
type bufferSlot struct {
	buffer gfx.Buffer
	size   uint64
}

// parameterReportKey is a binding of one shader, by its name, whose refused
// values were reported.
type parameterReportKey struct {
	shader Shader
	name   string
}

// writeTexels writes texels, rows of texels of texelSize bytes each packed
// tight, layer after layer, into the size region at destination, of
// texture, which the write goes into the inspection under, the zero Texture
// for one of render's own. A write gfx refuses is logged through table,
// unless the device is lost.
func writeTexels(table *assetTable, device gfx.Device, texture Texture, destination gfx.ImageCopyTexture, texels []byte, texelSize uint32, size gfx.Extent3D) {
	err := device.WriteTexture(destination, texels, gfx.ImageDataLayout{BytesPerRow: texelSize * size.Width, RowsPerImage: size.Height}, size)
	switch {
	case err != nil:
		table.logUnlessLost(fmt.Errorf("render: writing texture %q: %w", device.TextureDescriptor(destination.Texture).Label, err))
	case texture != (Texture{}):
		table.inspector.add(func(inspection *Inspection) {
			inspection.Writes = append(inspection.Writes, InspectedWrite{Texture: texture, Region: makeInspectedTextureRegion(destination, size), LayerCount: size.DepthOrArrayLayers})
		})
	}
}

// writeBytes writes data, whose length is a multiple of 4, into gfxBuffer at
// offset, a multiple of 4, the gfx buffer behind buffer, which the write
// goes into the inspection under, the zero Buffer for one of render's own.
// A write gfx refuses is logged through table, unless the device is lost.
func writeBytes(table *assetTable, device gfx.Device, buffer Buffer, gfxBuffer gfx.Buffer, offset uint64, data []byte) {
	err := device.WriteBuffer(gfxBuffer, offset, data)
	switch {
	case err != nil:
		table.logUnlessLost(fmt.Errorf("render: writing buffer %q: %w", device.BufferDescriptor(gfxBuffer).Label, err))
	case buffer != (Buffer{}):
		table.inspector.add(func(inspection *Inspection) {
			inspection.Writes = append(inspection.Writes, InspectedWrite{Buffer: BufferRange{Buffer: buffer, Offset: offset, Size: uint64(len(data))}})
		})
	}
}

// createLabelledTexture makes on device the gfx texture of texture, of
// descriptor, whose counts are filled in, with every usage its format
// allows and a label naming texture.
func createLabelledTexture(device gfx.Device, texture Texture, descriptor TextureDescriptor) (gfx.Texture, error) {
	return device.CreateTexture(gfx.TextureDescriptor{
		Label:         handle.MakeLabel(texture, descriptor.Label),
		Size:          gfx.Extent3D{Width: descriptor.Width, Height: descriptor.Height, DepthOrArrayLayers: descriptor.LayerCount},
		MipLevelCount: descriptor.MipLevelCount,
		SampleCount:   descriptor.SampleCount,
		Dimension:     gfx.TextureDimension2D,
		Format:        gfx.TextureFormat(descriptor.Format),
		Usage:         descriptor.Format.makeTextureUsage(descriptor.SampleCount),
	})
}

// createLabelledBuffer makes on device the gfx buffer of buffer, of size
// bytes, a multiple of 4, with usage and the copy destination render's
// writes need, and a label naming buffer and label.
func createLabelledBuffer(device gfx.Device, buffer Buffer, label string, size uint64, usage BufferUsageFlags) (gfx.Buffer, error) {
	return device.CreateBuffer(gfx.BufferDescriptor{Label: handle.MakeLabel(buffer, label), Size: size, Usage: gfx.BufferUsageFlags(usage) | gfx.BufferUsageFlagCopyDst})
}

// makeLevelViewDescriptor answers the descriptor of the view of one mip
// level and one layer of a texture, as a pass draws into it or the mips
// draw from it.
func makeLevelViewDescriptor(mipLevel, layer uint32) gfx.TextureViewDescriptor {
	return gfx.TextureViewDescriptor{
		Dimension:       gfx.TextureViewDimension2D,
		Aspect:          gfx.TextureAspectAll,
		BaseMipLevel:    mipLevel,
		MipLevelCount:   1,
		BaseArrayLayer:  layer,
		ArrayLayerCount: 1,
	}
}

// padBytes answers data with zeros appended up to a multiple of 4 bytes,
// leaving data's own array as it was.
func padBytes(data []byte) []byte {
	if size := (uint64(len(data)) + 3) &^ 3; size != uint64(len(data)) {
		return append(data[:len(data):len(data)], make([]byte, size-uint64(len(data)))...)
	}
	return data
}

// claimReport answers whether a mistake about key, such as a skipped draw of
// a handle render does not hold, is to be reported: true unless reported
// holds key, which it then does.
func claimReport[H comparable](reported *map[H]struct{}, h H) bool {
	if _, ok := (*reported)[h]; ok {
		return false
	}
	if *reported == nil {
		*reported = map[H]struct{}{}
	}
	(*reported)[h] = struct{}{}
	return true
}
