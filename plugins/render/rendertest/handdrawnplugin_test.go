package rendertest_test

import (
	"encoding/binary"
	"errors"
	"io/fs"
	"math"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/plugins/gfx"
)

// handDrawnOrder is the order of the encoder the hand-drawn frame records
// into.
const handDrawnOrder gfx.SubmitOrder = 1

// handDrawnPlugin is the benchmarks' plugin drawing by hand on gfx the frame
// a gatePlugin of the mesh shape draws on render with its draws alone, in
// one list and one pass: the camera bound once at a dynamic offset, two
// materials' bind groups switched every benchmarkSwitchPeriod draws, the
// mesh's vertex and index buffers and the instance buffer bound once, and
// each draw's object uniform block written into one buffer, written to gfx
// once a frame, and bound at its dynamic offset.
type handDrawnPlugin struct {
	files     fs.FS
	drawCount int
	scene     handDrawnScene
	// sceneScheduled is set once the handler of gfx.DrawEvent has scheduled
	// the scene's making, in the first frame.
	sceneScheduled bool
}

// handDrawnScene is what the hand-drawn frame draws with, made in its first
// frame: the pipeline, the bind groups of the pass, the two materials and
// the draws, the buffers they read, the bytes of the uniform blocks, and the
// pass with the dynamic offsets it binds.
type handDrawnScene struct {
	pipeline                   gfx.RenderPipeline
	passGroup, objectGroup     gfx.BindGroup
	materialGroups             [2]gfx.BindGroup
	vertices, indices, offsets gfx.Buffer
	uniforms                   gfx.Buffer
	uniformBytes               []byte
	pass                       gfx.RenderPassDescriptor
	passOffsets, objectOffsets [1]uint32
}

func (*handDrawnPlugin) Name() kernel.PluginName { return "hand-drawn plugin" }

func (*handDrawnPlugin) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{gfx.PluginName}
}

func (p *handDrawnPlugin) Init(configurator *kernel.Configurator, _ any) error {
	setup := configurator.RegisterTask1(func(k *kernel.Kernel, _ kernel.NoArg, device kernel.Read[gfx.Device]) kernel.NoResult {
		if err := p.makeScene(device.Get()); err != nil {
			k.Logger().LogErr(err)
		}
		return kernel.NoResult{}
	})
	record := configurator.RegisterTask1(func(k *kernel.Kernel, frame gfx.Frame, device kernel.Read[gfx.Device]) kernel.NoResult {
		if err := p.record(device.Get(), frame); err != nil {
			k.Logger().LogErr(err)
		}
		return kernel.NoResult{}
	})
	configurator.Subscribe(func(k *kernel.Kernel, ex *kernel.Executor, in kernel.JobHandle, event gfx.DrawEvent) (gfx.DrawEvent, kernel.JobHandle) {
		ready := event.Ready
		if !p.sceneScheduled {
			ready = ex.Schedule(setup, kernel.NoArg{}, ready).Handle()
			p.sceneScheduled = true
		}
		return event, ex.Join(in, ex.Schedule(record, event.Frame, ready).Handle())
	})
	return nil
}

func (*handDrawnPlugin) Start(*kernel.Kernel) error { return nil }

func (*handDrawnPlugin) Stop(*kernel.Kernel) error { return nil }

func (*handDrawnPlugin) Destroy() error { return nil }

// makeScene makes the hand-drawn frame's scene on device; the first error.
func (p *handDrawnPlugin) makeScene(device gfx.Device) error {
	scene := &p.scene
	source, err := device.PreprocessShader(p.files, "gatemesh.wgsl", nil)
	if err != nil {
		return err
	}
	module, err := device.CreateShaderModule(gfx.ShaderModuleDescriptor{Label: "gatemesh.wgsl", WGSL: source})
	if err != nil {
		return err
	}
	var failures []error
	collect := func(err error) { failures = append(failures, err) }
	createBuffer := func(label string, size uint64, usage gfx.BufferUsageFlags) gfx.Buffer {
		buffer, err := device.CreateBuffer(gfx.BufferDescriptor{Label: label, Size: size, Usage: usage | gfx.BufferUsageFlagCopyDst})
		collect(err)
		return buffer
	}
	createView := func(label string, format gfx.TextureFormat, usage gfx.TextureUsageFlags) gfx.TextureView {
		texture, err := device.CreateTexture(gfx.TextureDescriptor{Label: label, Size: gfx.Extent3D{Width: 64, Height: 64, DepthOrArrayLayers: 1}, MipLevelCount: 1, SampleCount: 1, Dimension: gfx.TextureDimension2D, Format: format, Usage: usage})
		collect(err)
		view, err := device.CreateTextureView(texture, gfx.TextureViewDescriptor{Dimension: gfx.TextureViewDimension2D, Aspect: gfx.TextureAspectAll, MipLevelCount: 1, ArrayLayerCount: 1})
		collect(err)
		return view
	}
	createLayout := func(entries ...gfx.BindGroupLayoutEntry) gfx.BindGroupLayout {
		layout, err := device.CreateBindGroupLayout(gfx.BindGroupLayoutDescriptor{Entries: entries})
		collect(err)
		return layout
	}
	createGroup := func(layout gfx.BindGroupLayout, entries ...gfx.BindGroupEntry) gfx.BindGroup {
		group, err := device.CreateBindGroup(gfx.BindGroupDescriptor{Layout: layout, Entries: entries})
		collect(err)
		return group
	}

	color := createView("colour", gfx.TextureFormatRGBA8Unorm, gfx.TextureUsageFlagRenderAttachment)
	depth := createView("depth", gfx.TextureFormatDepth32Float, gfx.TextureUsageFlagRenderAttachment)
	albedo := createView("albedo", gfx.TextureFormatRGBA8Unorm, gfx.TextureUsageFlagTextureBinding|gfx.TextureUsageFlagCopyDst)
	sampler, err := device.CreateSampler(gfx.SamplerDescriptor{AddressModeU: gfx.AddressModeClampToEdge, AddressModeV: gfx.AddressModeClampToEdge, AddressModeW: gfx.AddressModeClampToEdge, MagFilter: gfx.FilterModeLinear, MinFilter: gfx.FilterModeLinear, LodMaxClamp: 32})
	collect(err)
	scene.vertices = createBuffer("vertices", 48, gfx.BufferUsageFlagVertex)
	scene.indices = createBuffer("indices", 12, gfx.BufferUsageFlagIndex)
	collect(device.WriteBuffer(scene.indices, 0, []byte{0, 0, 1, 0, 2, 0, 2, 0, 1, 0, 3, 0}))
	scene.offsets = createBuffer("offsets", 16*gateRowCount, gfx.BufferUsageFlagVertex)
	scene.uniformBytes = make([]byte, (p.drawCount+1)*256)
	scene.uniforms = createBuffer("uniforms", uint64(len(scene.uniformBytes)), gfx.BufferUsageFlagUniform)
	tints := createBuffer("tints", 512, gfx.BufferUsageFlagUniform)

	passLayout := createLayout(gfx.BindGroupLayoutEntry{Binding: 0, Visibility: gfx.ShaderStageFlagVertex, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, HasDynamicOffset: true, MinBindingSize: 64}})
	materialLayout := createLayout(
		gfx.BindGroupLayoutEntry{Binding: 0, Visibility: gfx.ShaderStageFlagFragment, Texture: gfx.TextureBindingLayout{SampleType: gfx.TextureSampleTypeFloat, ViewDimension: gfx.TextureViewDimension2D}},
		gfx.BindGroupLayoutEntry{Binding: 1, Visibility: gfx.ShaderStageFlagFragment, Sampler: gfx.SamplerBindingLayout{Type: gfx.SamplerBindingTypeFiltering}},
		gfx.BindGroupLayoutEntry{Binding: 2, Visibility: gfx.ShaderStageFlagFragment, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, MinBindingSize: 16}},
	)
	objectLayout := createLayout(gfx.BindGroupLayoutEntry{Binding: 0, Visibility: gfx.ShaderStageFlagVertex, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform, HasDynamicOffset: true, MinBindingSize: 16}})
	pipelineLayout, err := device.CreatePipelineLayout(gfx.PipelineLayoutDescriptor{BindGroupLayouts: []gfx.BindGroupLayout{passLayout, materialLayout, objectLayout}})
	collect(err)
	scene.pipeline, err = device.CreateRenderPipeline(gfx.RenderPipelineDescriptor{
		Layout: pipelineLayout,
		Vertex: gfx.VertexState{Module: module, EntryPoint: "vertex_main", Buffers: []gfx.VertexBufferLayout{
			{ArrayStride: 12, StepMode: gfx.VertexStepModeVertex, Attributes: []gfx.VertexAttribute{{Format: gfx.VertexFormatFloat32x3}}},
			{ArrayStride: 16, StepMode: gfx.VertexStepModeInstance, Attributes: []gfx.VertexAttribute{{Format: gfx.VertexFormatFloat32x4, ShaderLocation: 1}}},
		}},
		Primitive:    gfx.PrimitiveState{Topology: gfx.PrimitiveTopologyTriangleList},
		DepthStencil: gfx.DepthStencilState{Format: gfx.TextureFormatDepth32Float, DepthWriteEnabled: true, DepthCompare: gfx.CompareFunctionLessEqual},
		Multisample:  gfx.MultisampleState{Count: 1, Mask: math.MaxUint64},
		Fragment:     gfx.FragmentState{Module: module, EntryPoint: "fragment_main", Targets: []gfx.ColorTargetState{{Format: gfx.TextureFormatRGBA8Unorm, WriteMask: gfx.ColorWriteFlagAll}}},
	})
	collect(err)

	scene.passGroup = createGroup(passLayout, gfx.BindGroupEntry{Binding: 0, Buffer: scene.uniforms, Size: 64})
	for index := range scene.materialGroups {
		scene.materialGroups[index] = createGroup(materialLayout,
			gfx.BindGroupEntry{Binding: 0, TextureView: albedo},
			gfx.BindGroupEntry{Binding: 1, Sampler: sampler},
			gfx.BindGroupEntry{Binding: 2, Buffer: tints, Offset: uint64(index) * 256, Size: 16},
		)
	}
	scene.objectGroup = createGroup(objectLayout, gfx.BindGroupEntry{Binding: 0, Buffer: scene.uniforms, Size: 16})
	scene.pass = gfx.RenderPassDescriptor{
		Label:                  "hand drawn",
		ColorAttachments:       []gfx.RenderPassColorAttachment{{View: color, LoadOp: gfx.LoadOpClear, StoreOp: gfx.StoreOpStore}},
		DepthStencilAttachment: gfx.RenderPassDepthStencilAttachment{View: depth, DepthLoadOp: gfx.LoadOpClear, DepthStoreOp: gfx.StoreOpStore, DepthClearValue: 1},
	}
	return errors.Join(failures...)
}

// record records the hand-drawn frame into frame on device; gfx's error.
func (p *handDrawnPlugin) record(device gfx.Device, frame gfx.Frame) error {
	scene := &p.scene
	for drawIndex := range p.drawCount {
		binary.LittleEndian.PutUint32(scene.uniformBytes[(drawIndex+1)*256:], math.Float32bits(float32(drawIndex)))
	}
	if err := device.WriteBuffer(scene.uniforms, 0, scene.uniformBytes); err != nil {
		return err
	}
	encoder, err := device.Encoder(frame, handDrawnOrder, "hand drawn")
	if err != nil {
		return err
	}
	pass := encoder.BeginRenderPass(scene.pass)
	pass.SetPipeline(scene.pipeline)
	pass.SetBindGroup(0, scene.passGroup, scene.passOffsets[:])
	pass.SetVertexBuffer(0, scene.vertices, 0)
	pass.SetVertexBuffer(1, scene.offsets, 0)
	pass.SetIndexBuffer(scene.indices, gfx.IndexFormatUint16, 0)
	for drawIndex := range p.drawCount {
		if drawIndex%benchmarkSwitchPeriod == 0 {
			pass.SetBindGroup(1, scene.materialGroups[drawIndex/benchmarkSwitchPeriod%2], nil)
		}
		scene.objectOffsets[0] = uint32((drawIndex + 1) * 256)
		pass.SetBindGroup(2, scene.objectGroup, scene.objectOffsets[:])
		pass.DrawIndexed(gfx.DrawIndexedArgs{IndexCount: 6, InstanceCount: 1, FirstInstance: uint32(drawIndex % gateRowCount)})
	}
	pass.End()
	return nil
}
