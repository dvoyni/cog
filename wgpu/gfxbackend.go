package wgpu

import (
	"cmp"
	"errors"
	"log"
	"slices"
	"sync/atomic"

	cgfx "github.com/dvoyni/cog/gfx"
	"github.com/gogpu/gogpu"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// gfxbUniformSize is the per-draw shader-parameter uniform buffer size: mat4 MVP
// plus material params. The renderer writes only the used prefix each draw.
const gfxbUniformSize = 256

// depthFormat is the depth buffer format used by the high-level renderer. It
// includes a stencil aspect because the browser WebGPU binding always emits
// stencilLoadOp/stencilStoreOp for the depth attachment, which WebGPU rejects on
// a stencil-less format (native Dawn tolerates it, browsers do not).
const depthFormat = gputypes.TextureFormatDepth24PlusStencil8

// gfxBackend implements gfx.Backend over gogpu/wgpu. TextureID and BufferID key
// native textures and buffers directly; backend-minted IDs remain only for
// shaders, pipelines, and samplers. All methods run on the render thread.
type gfxBackend struct {
	device        *wgpu.Device
	queue         *wgpu.Queue
	surfaceFormat gputypes.TextureFormat

	nextID        uint32
	nextTextureID atomic.Uint32
	nextBufferID  atomic.Uint32
	samplers      map[cgfx.SamplerID]*wgpu.Sampler
	shaders       map[cgfx.ShaderID]*gfxbShader
	pipelines     map[cgfx.PipelineID]*gfxbPipeline

	bakedBuffers       map[cgfx.BufferID]*wgpu.Buffer
	bakedBufferDescs   map[cgfx.BufferID]cgfx.BufferDesc
	bufferGenerations  map[cgfx.BufferID]uint32
	bakedTextures      map[cgfx.TextureID]*gfxbTexture
	bakedTextureDescs  map[cgfx.TextureID]cgfx.TextureDesc
	textureGenerations map[cgfx.TextureID]uint32
	replacedBuffers    []*wgpu.Buffer
	replacedTextures   []*gfxbTexture

	white          *gfxbTexture
	defaultSampler *wgpu.Sampler

	uniforms []*wgpu.Buffer

	// Per-draw bind-group state: acc holds pending logical/native entries per
	// group; bindGroups reuses exact resource combinations across frames.
	acc        [][]gfxbBindEntry
	bindGroups *gfxbBindGroupCache

	depthTex       *wgpu.Texture
	depthView      *wgpu.TextureView
	depthW, depthH int

	// screen is the current frame's surface render target, refreshed by setScreen
	// before each render and exposed to the plugin as screenID.
	screenID   cgfx.TextureViewID
	screenView *wgpu.TextureView
	screenW    int
	screenH    int

	prevCmd *wgpu.CommandBuffer
}

type gfxbTexture struct {
	tex  *wgpu.Texture
	view *wgpu.TextureView
}

// gfxbShader is a compiled shader module, its reflected uniform layout, and the
// GPU bind-group + pipeline layouts built from reflection.
type gfxbShader struct {
	module     *wgpu.ShaderModule
	layout     cgfx.ShaderLayout
	bgLayouts  []*wgpu.BindGroupLayout // indexed by bind group
	pipeLayout *wgpu.PipelineLayout
}

// gfxbPipeline is a render pipeline plus the shader it was built from (whose
// reflected layout drives bind-group construction at draw time).
type gfxbPipeline struct {
	pipeline *wgpu.RenderPipeline
	shader   *gfxbShader
}

type gfxRenderPass struct {
	backend      *gfxBackend
	pass         *wgpu.RenderPassEncoder
	shader       *gfxbShader
	uniformIndex int
}

func (s *gfxRenderPass) SetPipeline(id cgfx.PipelineID) {
	if pipeline, ok := s.backend.pipelines[id]; ok {
		s.pass.SetPipeline(pipeline.pipeline)
		s.shader = pipeline.shader
	}
	s.backend.resetAcc()
}

func (s *gfxRenderPass) SetParams(params []byte) {
	if s.shader == nil {
		return
	}
	slot := s.uniformIndex
	buffer := s.backend.uniform(slot)
	s.uniformIndex++
	if buffer != nil {
		_ = s.backend.queue.WriteBuffer(buffer, 0, params)
		binding := uint32(s.shader.layout.UniformBinding)
		s.backend.addEntry(s.shader.layout.UniformGroup, gfxbBindEntry{
			key:    gfxbBindingKey{kind: gfxbBindUniform, binding: uint16(binding), id: uint32(slot), size: gfxbUniformSize},
			native: wgpu.BindGroupEntry{Binding: binding, Buffer: buffer, Size: gfxbUniformSize},
		})
	}
}

func (s *gfxRenderPass) SetTexture(texture cgfx.TextureID, sampler cgfx.SamplerID, group, textureBinding, samplerBinding int) {
	view, textureID, generation := s.backend.textureBinding(texture)
	s.backend.addEntry(group, gfxbBindEntry{
		key:    gfxbBindingKey{kind: gfxbBindTexture, binding: uint16(textureBinding), id: textureID, generation: generation},
		native: wgpu.BindGroupEntry{Binding: uint32(textureBinding), TextureView: view},
	})
	if samplerBinding >= 0 {
		native, samplerID := s.backend.samplerBinding(sampler)
		s.backend.addEntry(group, gfxbBindEntry{
			key:    gfxbBindingKey{kind: gfxbBindSampler, binding: uint16(samplerBinding), id: samplerID},
			native: wgpu.BindGroupEntry{Binding: uint32(samplerBinding), Sampler: native},
		})
	}
}

func (s *gfxRenderPass) SetVertexBuffer(id cgfx.BufferID, offset int) {
	if buffer, ok := s.backend.bakedBuffers[id]; ok {
		s.pass.SetVertexBuffer(0, buffer, uint64(offset))
	}
}

func (s *gfxRenderPass) SetIndexBuffer(id cgfx.BufferID, offset int) {
	if buffer, ok := s.backend.bakedBuffers[id]; ok {
		s.pass.SetIndexBuffer(buffer, gputypes.IndexFormatUint32, uint64(offset))
	}
}

func (s *gfxRenderPass) SetBuffer(group, binding int, id cgfx.BufferID, offset, size int) {
	if buffer, ok := s.backend.bakedBuffers[id]; ok {
		if size == 0 {
			size = s.backend.bakedBufferDescs[id].Size - offset
		}
		s.backend.addEntry(group, gfxbBindEntry{
			key: gfxbBindingKey{
				kind: gfxbBindBuffer, binding: uint16(binding), id: uint32(id),
				generation: s.backend.bufferGenerations[id], offset: uint32(offset), size: uint32(size),
			},
			native: wgpu.BindGroupEntry{
				Binding: uint32(binding), Buffer: buffer, Offset: uint64(offset), Size: uint64(size),
			},
		})
	}
}

func (s *gfxRenderPass) Draw(first, count, instances int, indexed bool) {
	if s.shader != nil {
		s.backend.flushBinds(s.pass, s.shader)
	}
	if instances < 1 {
		instances = 1
	}
	if indexed {
		s.pass.DrawIndexed(uint32(count), uint32(instances), uint32(first), 0, 0)
	} else {
		s.pass.Draw(uint32(count), uint32(instances), uint32(first), 0)
	}
	s.backend.resetAcc()
}

var _ cgfx.Backend = (*gfxBackend)(nil)

// newGfxBackend builds the shared layouts, default sampler, and white texture. It
// returns errDeviceNotReady until the GPU device/queue exist (async on browser).
func newGfxBackend(dp gogpu.DeviceProvider) (*gfxBackend, error) {
	b := &gfxBackend{
		device:             dp.Device(),
		queue:              dp.Queue(),
		surfaceFormat:      dp.SurfaceFormat(),
		samplers:           map[cgfx.SamplerID]*wgpu.Sampler{},
		shaders:            map[cgfx.ShaderID]*gfxbShader{},
		pipelines:          map[cgfx.PipelineID]*gfxbPipeline{},
		bakedBuffers:       map[cgfx.BufferID]*wgpu.Buffer{},
		bakedBufferDescs:   map[cgfx.BufferID]cgfx.BufferDesc{},
		bufferGenerations:  map[cgfx.BufferID]uint32{},
		bakedTextures:      map[cgfx.TextureID]*gfxbTexture{},
		bakedTextureDescs:  map[cgfx.TextureID]cgfx.TextureDesc{},
		textureGenerations: map[cgfx.TextureID]uint32{},
	}
	if b.device == nil || b.queue == nil {
		return nil, errDeviceNotReady
	}
	b.bindGroups = newGfxBindGroupCache(
		func(layout *wgpu.BindGroupLayout, entries []wgpu.BindGroupEntry) (*wgpu.BindGroup, error) {
			return b.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
				Label: "gfx.bind", Layout: layout, Entries: entries,
			})
		},
		func(group *wgpu.BindGroup) { group.Release() },
	)

	var err error
	b.defaultSampler, err = b.device.CreateSampler(&wgpu.SamplerDescriptor{
		Label:        "gfx.defaultSampler",
		AddressModeU: gputypes.AddressModeClampToEdge, AddressModeV: gputypes.AddressModeClampToEdge, AddressModeW: gputypes.AddressModeClampToEdge,
		MagFilter: gputypes.FilterModeLinear, MinFilter: gputypes.FilterModeLinear,
	})
	if err != nil {
		return nil, err
	}

	view, err := createTextureView(b.device, b.queue, 1, 1, []byte{255, 255, 255, 255})
	if err != nil {
		return nil, err
	}
	b.white = &gfxbTexture{tex: view.Texture(), view: view}
	b.screenID = cgfx.TextureViewID(b.id())
	return b, nil
}

func (b *gfxBackend) id() uint32 { b.nextID++; return b.nextID }

// NewTexture reserves a logical texture ID. Native creation stays deferred to
// the render thread when Execute processes its first bake op.
func (b *gfxBackend) NewTexture() cgfx.TextureID {
	return cgfx.TextureID(b.nextTextureID.Add(1))
}

// NewBuffer reserves a logical buffer ID. Native creation stays deferred to
// the render thread when Execute processes its first bake op.
func (b *gfxBackend) NewBuffer() cgfx.BufferID {
	return cgfx.BufferID(b.nextBufferID.Add(1))
}

func (b *gfxBackend) newTexture(desc cgfx.TextureDesc) (*gfxbTexture, error) {
	layers := max(desc.Layers, 1)
	levels := uint32(1)
	if desc.Mipmaps {
		levels = uint32(mipLevelCount(desc.Width, desc.Height))
	}
	tex, err := b.device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         desc.Label,
		Size:          wgpu.Extent3D{Width: uint32(desc.Width), Height: uint32(desc.Height), DepthOrArrayLayers: uint32(layers)},
		MipLevelCount: levels, SampleCount: 1,
		Dimension: gputypes.TextureDimension2D,
		Format:    gputypes.TextureFormatRGBA8Unorm,
		Usage:     gputypes.TextureUsageTextureBinding | gputypes.TextureUsageCopyDst,
	})
	if err != nil {
		return nil, err
	}
	viewDimension := gputypes.TextureViewDimension2D
	if layers > 1 {
		viewDimension = gputypes.TextureViewDimension2DArray
	}
	view, err := b.device.CreateTextureView(tex, &wgpu.TextureViewDescriptor{
		Label: desc.Label, Format: gputypes.TextureFormatRGBA8Unorm,
		Dimension: viewDimension, Aspect: gputypes.TextureAspectAll,
		MipLevelCount: levels, ArrayLayerCount: uint32(layers),
	})
	if err != nil {
		tex.Release()
		return nil, err
	}
	return &gfxbTexture{tex: tex, view: view}, nil
}

func (b *gfxBackend) uploadTexture(texture *gfxbTexture, layer int, region cgfx.Region, pixels []byte) {
	if texture == nil {
		return
	}
	_ = b.queue.WriteTexture(
		&wgpu.ImageCopyTexture{Texture: texture.tex, MipLevel: 0, Origin: wgpu.Origin3D{X: uint32(region.X), Y: uint32(region.Y), Z: uint32(layer)}, Aspect: gputypes.TextureAspectAll},
		pixels,
		&wgpu.ImageDataLayout{Offset: 0, BytesPerRow: uint32(region.Width * 4), RowsPerImage: uint32(region.Height)},
		&wgpu.Extent3D{Width: uint32(region.Width), Height: uint32(region.Height), DepthOrArrayLayers: 1},
	)
}

// uploadMipChain box-filters level-0 RGBA pixels and writes each smaller mip.
func (b *gfxBackend) uploadMipChain(texture *gfxbTexture, width, height int, pixels []byte) {
	if texture == nil {
		return
	}
	src, w, h := pixels, width, height
	for level := 1; w > 1 || h > 1; level++ {
		src, w, h = downsampleRGBA(src, w, h)
		_ = b.queue.WriteTexture(
			&wgpu.ImageCopyTexture{Texture: texture.tex, MipLevel: uint32(level), Origin: wgpu.Origin3D{}, Aspect: gputypes.TextureAspectAll},
			src,
			&wgpu.ImageDataLayout{Offset: 0, BytesPerRow: uint32(w * 4), RowsPerImage: uint32(h)},
			&wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
		)
	}
}

// mipLevelCount is floor(log2(max(w,h))) + 1: the full chain down to 1x1.
func mipLevelCount(width, height int) int {
	levels := 1
	for width > 1 || height > 1 {
		width = max(1, width/2)
		height = max(1, height/2)
		levels++
	}
	return levels
}

// downsampleRGBA box-filters a straight-RGBA image to half size (min 1px).
func downsampleRGBA(src []byte, width, height int) (dst []byte, dw, dh int) {
	dw, dh = max(1, width/2), max(1, height/2)
	dst = make([]byte, dw*dh*4)
	for y := 0; y < dh; y++ {
		sy0, sy1 := min(y*2, height-1), min(y*2+1, height-1)
		for x := 0; x < dw; x++ {
			sx0, sx1 := min(x*2, width-1), min(x*2+1, width-1)
			for c := 0; c < 4; c++ {
				sum := int(src[(sy0*width+sx0)*4+c]) + int(src[(sy0*width+sx1)*4+c]) +
					int(src[(sy1*width+sx0)*4+c]) + int(src[(sy1*width+sx1)*4+c])
				dst[(y*dw+x)*4+c] = byte(sum / 4)
			}
		}
	}
	return dst, dw, dh
}

func (b *gfxBackend) freeTexture(texture *gfxbTexture) {
	if texture == nil {
		return
	}
	texture.view.Release()
	texture.tex.Release()
}

func (b *gfxBackend) NewSampler(desc cgfx.SamplerDesc) (cgfx.SamplerID, error) {
	addressU := gputypes.AddressModeClampToEdge
	if desc.Address&cgfx.AddressRepeatX != 0 {
		addressU = gputypes.AddressModeRepeat
	}
	addressV := gputypes.AddressModeClampToEdge
	if desc.Address&cgfx.AddressRepeatY != 0 {
		addressV = gputypes.AddressModeRepeat
	}
	filter := gputypes.FilterModeLinear
	if desc.Filter == cgfx.FilterNearest {
		filter = gputypes.FilterModeNearest
	}
	s, err := b.device.CreateSampler(&wgpu.SamplerDescriptor{
		Label:        desc.Label,
		AddressModeU: addressU, AddressModeV: addressV, AddressModeW: addressU,
		MagFilter: filter, MinFilter: filter, MipmapFilter: filter,
		LodMaxClamp: 1000,
	})
	if err != nil {
		return 0, err
	}
	id := cgfx.SamplerID(b.id())
	b.samplers[id] = s
	return id, nil
}

func (b *gfxBackend) FreeSampler(id cgfx.SamplerID) {
	if s, ok := b.samplers[id]; ok {
		b.bindGroups.invalidateResource(gfxbBindSampler, uint32(id))
		s.Release()
		delete(b.samplers, id)
	}
}

func (b *gfxBackend) NewShader(desc cgfx.ShaderDesc) (cgfx.ShaderID, error) {
	if len(desc.Code) == 0 {
		return 0, errors.New("gfx: shader has no source code")
	}
	label := desc.Label
	if label == "" {
		label = "gfx.shader"
	}
	module, err := b.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: label, WGSL: string(desc.Code)})
	if err != nil {
		return 0, err
	}
	sh := &gfxbShader{module: module}
	if mod, rerr := lowerWGSL(string(desc.Code)); rerr != nil {
		log.Printf("wgpu: shader %q reflection failed: %v", label, rerr)
	} else {
		sh.layout = shaderLayoutFrom(mod)
	}
	if err := b.buildShaderLayouts(sh); err != nil {
		log.Printf("wgpu: shader %q layout build failed: %v", label, err)
	}
	id := cgfx.ShaderID(b.id())
	b.shaders[id] = sh
	return id, nil
}

// buildShaderLayouts creates the GPU bind-group layouts (indexed by group) and the
// pipeline layout from a shader's reflected uniform + resource bindings.
func (b *gfxBackend) buildShaderLayouts(sh *gfxbShader) error {
	l := sh.layout
	groups := map[int][]gputypes.BindGroupLayoutEntry{}
	maxGroup := -1
	note := func(g int) {
		if g > maxGroup {
			maxGroup = g
		}
	}
	if l.UniformSize > 0 {
		groups[l.UniformGroup] = append(groups[l.UniformGroup], gputypes.BindGroupLayoutEntry{
			Binding:    uint32(l.UniformBinding),
			Visibility: gputypes.ShaderStageVertex | gputypes.ShaderStageFragment,
			Buffer:     &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform},
		})
		note(l.UniformGroup)
	}
	for _, r := range l.Resources {
		e := gputypes.BindGroupLayoutEntry{Binding: uint32(r.Binding), Visibility: gputypes.ShaderStageVertex | gputypes.ShaderStageFragment}
		if r.Sampler {
			e.Sampler = &gputypes.SamplerBindingLayout{Type: gputypes.SamplerBindingTypeFiltering}
		} else if r.StorageBuffer {
			bindingType := gputypes.BufferBindingTypeReadOnlyStorage
			if r.WritableBuffer {
				bindingType = gputypes.BufferBindingTypeStorage
			}
			e.Buffer = &gputypes.BufferBindingLayout{Type: bindingType}
		} else {
			view := gputypes.TextureViewDimension2D
			if r.TextureView == cgfx.TextureView2DArray {
				view = gputypes.TextureViewDimension2DArray
			}
			e.Texture = &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeFloat, ViewDimension: view}
		}
		groups[r.Group] = append(groups[r.Group], e)
		note(r.Group)
	}
	sh.bgLayouts = make([]*wgpu.BindGroupLayout, maxGroup+1)
	for g := 0; g <= maxGroup; g++ {
		bgl, err := b.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Label: "gfx.bgl", Entries: groups[g]})
		if err != nil {
			return err
		}
		sh.bgLayouts[g] = bgl
	}
	pl, err := b.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{Label: "gfx", BindGroupLayouts: sh.bgLayouts})
	if err != nil {
		return err
	}
	sh.pipeLayout = pl
	return nil
}

func (b *gfxBackend) FreeShader(id cgfx.ShaderID) {
	s, ok := b.shaders[id]
	if !ok {
		return
	}
	b.bindGroups.invalidateShader(s)
	if s.pipeLayout != nil {
		s.pipeLayout.Release()
	}
	for _, bgl := range s.bgLayouts {
		if bgl != nil {
			bgl.Release()
		}
	}
	s.module.Release()
	delete(b.shaders, id)
}

// ShaderLayout returns the reflected uniform layout cached at shader creation.
func (b *gfxBackend) ShaderLayout(id cgfx.ShaderID) cgfx.ShaderLayout {
	if s, ok := b.shaders[id]; ok {
		return s.layout
	}
	return cgfx.ShaderLayout{}
}

func (b *gfxBackend) newBuffer(desc cgfx.BufferDesc) (*wgpu.Buffer, error) {
	usage := gputypes.BufferUsageCopyDst
	switch desc.Kind {
	case cgfx.BufferVertex:
		usage |= gputypes.BufferUsageVertex
	case cgfx.BufferIndex:
		usage |= gputypes.BufferUsageIndex
	case cgfx.BufferUniform:
		usage |= gputypes.BufferUsageUniform
	case cgfx.BufferStorage:
		// ResourceQueue bakes persistent buffers (e.g. the canvas quad) as Storage
		// regardless of later use, so also allow vertex/index binding. Browser
		// WebGPU enforces usage flags; native Dawn does not.
		usage |= gputypes.BufferUsageStorage | gputypes.BufferUsageVertex | gputypes.BufferUsageIndex
	}
	buf, err := b.device.CreateBuffer(&wgpu.BufferDescriptor{Label: desc.Label, Size: uint64(desc.Size), Usage: usage})
	if err != nil {
		return nil, err
	}
	return buf, nil
}

func (b *gfxBackend) uploadBuffer(buffer *wgpu.Buffer, offset int, data []byte) {
	if buffer != nil {
		_ = b.queue.WriteBuffer(buffer, uint64(offset), data)
	}
}

func (b *gfxBackend) freeBuffer(buffer *wgpu.Buffer) {
	if buffer != nil {
		buffer.Release()
	}
}

func (b *gfxBackend) NewPipeline(desc cgfx.PipelineDesc) (cgfx.PipelineID, error) {
	sh, ok := b.shaders[desc.Shader]
	if !ok {
		return 0, errors.New("gfx: unknown shader for pipeline")
	}
	module := sh.module
	blend := gfxBlendState(desc.Blend)
	// Every pipeline must declare a depth-stencil state matching the render pass
	// attachment (browser WebGPU enforces this). When the pipeline does not depth
	// test, depth/stencil are effectively disabled (compare Always, no write).
	keep := gputypes.StencilOperationKeep
	stencilOff := wgpu.StencilFaceState{
		Compare: gputypes.CompareFunctionAlways, FailOp: keep, DepthFailOp: keep, PassOp: keep,
	}
	depth := &wgpu.DepthStencilState{
		Format:       depthFormat,
		DepthCompare: gputypes.CompareFunctionAlways,
		StencilFront: stencilOff,
		StencilBack:  stencilOff,
	}
	if desc.DepthTest {
		depth.DepthWriteEnabled = true
		depth.DepthCompare = gputypes.CompareFunctionLess
	}
	var buffers []wgpu.VertexBufferLayout
	if desc.Stride > 0 && len(desc.Attributes) > 0 {
		attrs := make([]gputypes.VertexAttribute, len(desc.Attributes))
		for i, a := range desc.Attributes {
			attrs[i] = gputypes.VertexAttribute{
				Format:         vertexFormat(a.Type),
				Offset:         uint64(a.Offset),
				ShaderLocation: uint32(a.Location),
			}
		}
		buffers = []wgpu.VertexBufferLayout{{
			ArrayStride: uint64(desc.Stride),
			StepMode:    gputypes.VertexStepModeVertex,
			Attributes:  attrs,
		}}
	}
	pipeline, err := b.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  desc.Label,
		Layout: sh.pipeLayout,
		Vertex: wgpu.VertexState{
			Module: module, EntryPoint: "vs_main",
			Buffers: buffers,
		},
		Primitive:    gputypes.PrimitiveState{Topology: primitiveTopology(desc.Topology), CullMode: gputypes.CullModeNone},
		DepthStencil: depth,
		Fragment: &wgpu.FragmentState{
			Module: module, EntryPoint: "fs_main",
			Targets: []gputypes.ColorTargetState{{Format: b.surfaceFormat, WriteMask: gputypes.ColorWriteMaskAll, Blend: blend}},
		},
	})
	if err != nil {
		return 0, err
	}
	id := cgfx.PipelineID(b.id())
	b.pipelines[id] = &gfxbPipeline{pipeline: pipeline, shader: sh}
	return id, nil
}

func gfxBlendState(mode cgfx.BlendMode) *gputypes.BlendState {
	component := func(src, dst gputypes.BlendFactor) gputypes.BlendComponent {
		return gputypes.BlendComponent{Operation: gputypes.BlendOperationAdd, SrcFactor: src, DstFactor: dst}
	}
	switch mode {
	case cgfx.BlendAlpha:
		return &gputypes.BlendState{
			Color: component(gputypes.BlendFactorSrcAlpha, gputypes.BlendFactorOneMinusSrcAlpha),
			Alpha: component(gputypes.BlendFactorOne, gputypes.BlendFactorOneMinusSrcAlpha),
		}
	case cgfx.BlendAdditive:
		return &gputypes.BlendState{
			Color: component(gputypes.BlendFactorSrcAlpha, gputypes.BlendFactorOne),
			Alpha: component(gputypes.BlendFactorOne, gputypes.BlendFactorOne),
		}
	case cgfx.BlendMultiply:
		return &gputypes.BlendState{
			Color: component(gputypes.BlendFactorDst, gputypes.BlendFactorZero),
			Alpha: component(gputypes.BlendFactorOne, gputypes.BlendFactorOneMinusSrcAlpha),
		}
	default:
		return nil
	}
}

func (b *gfxBackend) FreePipeline(id cgfx.PipelineID) {
	if p, ok := b.pipelines[id]; ok {
		p.pipeline.Release()
		delete(b.pipelines, id)
	}
}

// setScreen refreshes the current frame's surface render target. The driver calls
// it on the render thread after acquiring the surface view, before triggering the
// render.
func (b *gfxBackend) setScreen(view *wgpu.TextureView, w, h int) {
	b.screenView, b.screenW, b.screenH = view, w, h
}

// ScreenFramebuffer returns the current frame's screen render target and size.
func (b *gfxBackend) ScreenFramebuffer() (cgfx.TextureViewID, int, int) {
	return b.screenID, b.screenW, b.screenH
}

// resolveTarget maps a TextureViewID to its native view and size. Only the screen
// target exists today; offscreen render targets slot in here later.
func (b *gfxBackend) resolveTarget(target cgfx.TextureViewID) (*wgpu.TextureView, int, int) {
	if target == b.screenID {
		return b.screenView, b.screenW, b.screenH
	}
	return nil, 0, 0
}

// Execute replays the GPU queue into a single render pass on target's view.
func (b *gfxBackend) Execute(target cgfx.TextureViewID, queue *cgfx.GpuQueue) {
	view, w, h := b.resolveTarget(target)
	if view == nil {
		return
	}
	b.ensureDepth(w, h)

	b.replacedBuffers = b.replacedBuffers[:0]
	b.replacedTextures = b.replacedTextures[:0]
	queue.ReplayBakes(b)
	c := queue.ClearColor()
	depth := queue.ClearDepthValue()
	clearColor, clearDepth := queue.Clears()
	clearValue := gputypes.Color{R: float64(c.R), G: float64(c.G), B: float64(c.B), A: float64(c.A)}
	colorLoadOp := gputypes.LoadOpLoad
	if clearColor {
		colorLoadOp = gputypes.LoadOpClear
	}
	depthLoadOp := gputypes.LoadOpLoad
	if clearDepth {
		depthLoadOp = gputypes.LoadOpClear
	}

	encoder, err := b.device.CreateCommandEncoder(&wgpu.CommandEncoderDescriptor{Label: "gfx"})
	if err != nil {
		return
	}
	rp, err := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View: view, LoadOp: colorLoadOp, StoreOp: gputypes.StoreOpStore, ClearValue: clearValue,
		}},
		DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{
			View: b.depthView, DepthLoadOp: depthLoadOp, DepthStoreOp: gputypes.StoreOpStore, DepthClearValue: depth,
		},
	})
	if err != nil {
		return
	}

	b.resetAcc()
	queue.ReplayRenderPass(&gfxRenderPass{backend: b, pass: rp})
	_ = rp.End()
	cmd, err := encoder.Finish()
	if err != nil {
		return
	}
	if b.prevCmd != nil {
		b.prevCmd.Release()
	}
	_, _ = b.queue.Submit(cmd)
	b.prevCmd = cmd
	b.releaseReplacedBaked()
	queue.ReplayReleases(b)
}

func (b *gfxBackend) BakeBuffer(id cgfx.BufferID, kind cgfx.BufferKind, size int, data []byte) {
	b.bakeBuffer(id, kind, size, data)
}

func (b *gfxBackend) BakeTexture(id cgfx.TextureID, width, height int, format cgfx.TextureFormat, pixels []byte, mipmaps bool) {
	b.bakeTexture(id, width, height, format, pixels, mipmaps)
}

func (b *gfxBackend) AllocateTexture(id cgfx.TextureID, desc cgfx.TextureDesc) {
	b.allocateTexture(id, desc)
}

func (b *gfxBackend) UpdateTexture(id cgfx.TextureID, layer int, region cgfx.Region, pixels []byte) {
	texture, ok := b.bakedTextures[id]
	desc := b.bakedTextureDescs[id]
	if !ok || layer < 0 || layer >= max(desc.Layers, 1) || region.X < 0 || region.Y < 0 ||
		region.Width <= 0 || region.Height <= 0 || region.X+region.Width > desc.Width || region.Y+region.Height > desc.Height || len(pixels) == 0 {
		return
	}
	b.uploadTexture(texture, layer, region, pixels)
}

func (b *gfxBackend) allocateTexture(id cgfx.TextureID, desc cgfx.TextureDesc) *gfxbTexture {
	if id == 0 || desc.Width <= 0 || desc.Height <= 0 || desc.Layers < 0 {
		return nil
	}
	desc.Layers = max(desc.Layers, 1)
	desc.Label = "gfx.baked"
	if old, ok := b.bakedTextures[id]; ok && b.bakedTextureDescs[id] == desc {
		return old
	}
	texture, err := b.newTexture(desc)
	if err != nil {
		return nil
	}
	if old, ok := b.bakedTextures[id]; ok {
		b.bindGroups.invalidateResource(gfxbBindTexture, uint32(id))
		b.textureGenerations[id]++
		b.replacedTextures = append(b.replacedTextures, old)
	} else {
		b.textureGenerations[id] = 1
	}
	b.bakedTextures[id] = texture
	b.bakedTextureDescs[id] = desc
	return texture
}

func (b *gfxBackend) bakeBuffer(id cgfx.BufferID, kind cgfx.BufferKind, size int, data []byte) {
	if id == 0 || size <= 0 || len(data) == 0 {
		return
	}
	desc := cgfx.BufferDesc{Kind: kind, Size: size, Dynamic: kind == cgfx.BufferVertex || kind == cgfx.BufferIndex, Label: "gfx.baked"}
	if old, ok := b.bakedBuffers[id]; ok && b.bakedBufferDescs[id] == desc {
		b.uploadBuffer(old, 0, data)
		return
	}
	buffer, err := b.newBuffer(desc)
	if err != nil {
		return
	}
	b.uploadBuffer(buffer, 0, data)
	if old, ok := b.bakedBuffers[id]; ok {
		b.bindGroups.invalidateResource(gfxbBindBuffer, uint32(id))
		b.bufferGenerations[id]++
		b.replacedBuffers = append(b.replacedBuffers, old)
	} else {
		b.bufferGenerations[id] = 1
	}
	b.bakedBuffers[id] = buffer
	b.bakedBufferDescs[id] = desc
}

func (b *gfxBackend) bakeTexture(id cgfx.TextureID, width, height int, format cgfx.TextureFormat, pixels []byte, mipmaps bool) {
	if id == 0 || width <= 0 || height <= 0 || len(pixels) == 0 {
		return
	}
	texture := b.allocateTexture(id, cgfx.TextureDesc{Width: width, Height: height, Layers: 1, Format: format, Mipmaps: mipmaps})
	if texture == nil {
		return
	}
	b.uploadTexture(texture, 0, cgfx.Region{X: 0, Y: 0, Width: width, Height: height}, pixels)
	if mipmaps {
		b.uploadMipChain(texture, width, height, pixels)
	}
}

func (b *gfxBackend) releaseReplacedBaked() {
	for _, id := range b.replacedBuffers {
		b.freeBuffer(id)
	}
	for _, id := range b.replacedTextures {
		b.freeTexture(id)
	}
}

func (b *gfxBackend) ReleaseBuffer(id cgfx.BufferID) {
	if buffer, ok := b.bakedBuffers[id]; ok {
		b.bindGroups.invalidateResource(gfxbBindBuffer, uint32(id))
		b.freeBuffer(buffer)
		delete(b.bakedBuffers, id)
		delete(b.bakedBufferDescs, id)
		delete(b.bufferGenerations, id)
	}
}

func (b *gfxBackend) ReleaseTexture(id cgfx.TextureID) {
	if texture, ok := b.bakedTextures[id]; ok {
		b.bindGroups.invalidateResource(gfxbBindTexture, uint32(id))
		b.freeTexture(texture)
		delete(b.bakedTextures, id)
		delete(b.bakedTextureDescs, id)
		delete(b.textureGenerations, id)
	}
}

// resetAcc clears the per-draw bind entry accumulator, keeping capacity.
func (b *gfxBackend) resetAcc() {
	for g := range b.acc {
		b.acc[g] = b.acc[g][:0]
	}
}

// addEntry records a pending bind-group entry for a group in the current draw.
func (b *gfxBackend) addEntry(group int, e gfxbBindEntry) {
	for len(b.acc) <= group {
		b.acc = append(b.acc, nil)
	}
	b.acc[group] = append(b.acc[group], e)
}

// flushBinds reuses or creates one bind group per group with pending entries.
func (b *gfxBackend) flushBinds(rp *wgpu.RenderPassEncoder, shader *gfxbShader) {
	for g := range b.acc {
		if len(b.acc[g]) == 0 || g >= len(shader.bgLayouts) || shader.bgLayouts[g] == nil {
			continue
		}
		slices.SortFunc(b.acc[g], func(a, b gfxbBindEntry) int {
			return cmp.Compare(a.key.binding, b.key.binding)
		})
		bg := b.bindGroups.get(shader, g, b.acc[g])
		if bg == nil {
			continue
		}
		rp.SetBindGroup(uint32(g), bg, nil)
	}
}

// uniform returns the pooled uniform buffer at index i, creating it on demand. A
// distinct buffer per draw lets all per-draw writes precede the single submit.
func (b *gfxBackend) uniform(i int) *wgpu.Buffer {
	for i >= len(b.uniforms) {
		buffer, err := b.device.CreateBuffer(&wgpu.BufferDescriptor{
			Label: "gfx.uniform", Size: gfxbUniformSize,
			Usage: gputypes.BufferUsageUniform | gputypes.BufferUsageCopyDst,
		})
		if err != nil {
			return nil
		}
		b.uniforms = append(b.uniforms, buffer)
	}
	return b.uniforms[i]
}

func (b *gfxBackend) textureBinding(id cgfx.TextureID) (*wgpu.TextureView, uint32, uint32) {
	if texture, ok := b.bakedTextures[id]; ok {
		return texture.view, uint32(id), b.textureGenerations[id]
	}
	return b.white.view, 0, 0
}

func (b *gfxBackend) samplerBinding(id cgfx.SamplerID) (*wgpu.Sampler, uint32) {
	if s, ok := b.samplers[id]; ok && id != 0 {
		return s, uint32(id)
	}
	return b.defaultSampler, 0
}

// ensureDepth (re)creates the shared depth buffer when the target size changes.
func (b *gfxBackend) ensureDepth(w, h int) {
	if w <= 0 || h <= 0 || (b.depthView != nil && b.depthW == w && b.depthH == h) {
		return
	}
	if b.depthView != nil {
		b.depthView.Release()
		b.depthTex.Release()
	}
	tex, err := b.device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         "gfx.depth",
		Size:          wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
		MipLevelCount: 1, SampleCount: 1,
		Dimension: gputypes.TextureDimension2D,
		Format:    depthFormat,
		Usage:     gputypes.TextureUsageRenderAttachment,
	})
	if err != nil {
		return
	}
	view, err := b.device.CreateTextureView(tex, &wgpu.TextureViewDescriptor{
		Label: "gfx.depth", Format: depthFormat,
		Dimension: gputypes.TextureViewDimension2D, Aspect: gputypes.TextureAspectAll,
		MipLevelCount: 1, ArrayLayerCount: 1,
	})
	if err != nil {
		tex.Release()
		return
	}
	b.depthTex, b.depthView, b.depthW, b.depthH = tex, view, w, h
}

func primitiveTopology(t cgfx.PrimitiveTopology) gputypes.PrimitiveTopology {
	switch t {
	case cgfx.TopologyTriangleStrip:
		return gputypes.PrimitiveTopologyTriangleStrip
	case cgfx.TopologyLineList:
		return gputypes.PrimitiveTopologyLineList
	default:
		return gputypes.PrimitiveTopologyTriangleList
	}
}

func vertexFormat(t cgfx.VertexType) gputypes.VertexFormat {
	switch t {
	case cgfx.Float32x2:
		return gputypes.VertexFormatFloat32x2
	case cgfx.Float32x3:
		return gputypes.VertexFormatFloat32x3
	case cgfx.Float32x4:
		return gputypes.VertexFormatFloat32x4
	case cgfx.Float16x2:
		return gputypes.VertexFormatFloat16x2
	case cgfx.Float16x4:
		return gputypes.VertexFormatFloat16x4
	case cgfx.Uint8x2:
		return gputypes.VertexFormatUint8x2
	case cgfx.Uint8x4:
		return gputypes.VertexFormatUint8x4
	case cgfx.Sint8x2:
		return gputypes.VertexFormatSint8x2
	case cgfx.Sint8x4:
		return gputypes.VertexFormatSint8x4
	case cgfx.Unorm8x2:
		return gputypes.VertexFormatUnorm8x2
	case cgfx.Unorm8x4:
		return gputypes.VertexFormatUnorm8x4
	case cgfx.Snorm8x2:
		return gputypes.VertexFormatSnorm8x2
	case cgfx.Snorm8x4:
		return gputypes.VertexFormatSnorm8x4
	case cgfx.Uint16x2:
		return gputypes.VertexFormatUint16x2
	case cgfx.Uint16x4:
		return gputypes.VertexFormatUint16x4
	case cgfx.Sint16x2:
		return gputypes.VertexFormatSint16x2
	case cgfx.Sint16x4:
		return gputypes.VertexFormatSint16x4
	case cgfx.Unorm16x2:
		return gputypes.VertexFormatUnorm16x2
	case cgfx.Unorm16x4:
		return gputypes.VertexFormatUnorm16x4
	case cgfx.Snorm16x2:
		return gputypes.VertexFormatSnorm16x2
	case cgfx.Snorm16x4:
		return gputypes.VertexFormatSnorm16x4
	case cgfx.Uint32:
		return gputypes.VertexFormatUint32
	case cgfx.Uint32x2:
		return gputypes.VertexFormatUint32x2
	case cgfx.Uint32x3:
		return gputypes.VertexFormatUint32x3
	case cgfx.Uint32x4:
		return gputypes.VertexFormatUint32x4
	case cgfx.Sint32:
		return gputypes.VertexFormatSint32
	case cgfx.Sint32x2:
		return gputypes.VertexFormatSint32x2
	case cgfx.Sint32x3:
		return gputypes.VertexFormatSint32x3
	case cgfx.Sint32x4:
		return gputypes.VertexFormatSint32x4
	case cgfx.Unorm1010102:
		return gputypes.VertexFormatUnorm1010102
	default:
		return gputypes.VertexFormatFloat32
	}
}
