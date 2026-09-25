package internal

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"

	"github.com/dvoyni/cog/slots/gfx"
	"github.com/gogpu/gogpu"
	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

// gfxBackend implements gfx.Backend over gogpu/wgpu. TextureID and BufferID key
// native textures and buffers directly; backend-minted IDs remain only for
// shaders, pipelines, and samplers. All methods run on the render thread,
// except NewTexture, NewBuffer and Ready.
//
// It is one stable value for the plugin's whole life. The plugin provides it to
// gfx at registration, before the GPU device exists - the device is created
// asynchronously, inside the render loop - and attaches the device to it once
// the device does. Until then it is not Ready, and gfx calls nothing on it but
// Ready and the id reservations, which need no device.
type gfxBackend struct {
	// ready is set once attach has built everything the render-thread methods
	// use. It is the one field read across threads besides the id counters.
	ready atomic.Bool

	device        *wgpu.Device
	queue         *wgpu.Queue
	surfaceFormat gputypes.TextureFormat

	nextID        uint32
	nextTextureID atomic.Uint32
	nextBufferID  atomic.Uint32
	samplers      map[gfx.SamplerID]*wgpu.Sampler
	shaders       map[gfx.ShaderID]*gfxbShader
	pipelines     map[gfx.PipelineID]*gfxbPipeline

	bakedBuffers       map[gfx.BufferID]*wgpu.Buffer
	bakedBufferDescs   map[gfx.BufferID]gfx.BufferDesc
	bufferGenerations  map[gfx.BufferID]uint32
	bakedTextures      map[gfx.TextureID]*gfxbTexture
	bakedTextureDescs  map[gfx.TextureID]gfx.TextureDesc
	textureGenerations map[gfx.TextureID]uint32
	replacedBuffers    []*wgpu.Buffer
	replacedTextures   []*gfxbTexture
	// barriers is scratch for one TransitionTextures call, reused so a frame
	// with render targets allocates nothing per pass.
	barriers []wgpu.TextureBarrier

	white *gfxbTexture
	// whiteArray is a second view of white's own texture, declared at
	// TextureViewDimension2DArray with one layer, because a binding declared
	// texture_2d_array refuses a 2D view outright rather than sampling it. In
	// WGSL an out-of-range array_index is clamped, so every layer a shader asks
	// for lands on the one white texel.
	whiteArray     *wgpu.TextureView
	defaultSampler *wgpu.Sampler

	// uniforms is the frame's shader-parameter blocks: one buffer at 256-strided
	// offsets, staged CPU-side and written once. It is nil until a device is
	// attached, and a frame with no uniform-carrying draw never gives it one.
	uniforms *gfxbUniformArena

	// Per-draw bind-group state: acc holds pending logical/native entries per
	// group; bindGroups reuses exact resource combinations across frames; bound
	// filters out redundant SetBindGroup calls and is dropped whenever it stops
	// being sound - a shader change or a pass boundary.
	acc        [][]gfxbBindEntry
	bindGroups *gfxbBindGroupCache
	bound      []*wgpu.BindGroup

	// The frame's encoder, live only inside Execute so that every pass writes
	// into the same command buffer.
	encoder *wgpu.CommandEncoder

	// depths are the DepthAuto textures, one per target size, and views caches
	// renderable views by texture, mip and layer.
	depths map[gfxbDepthKey]*gfxbTexture
	views  map[gfxbViewKey]*gfxbView
	viewID map[gfx.TextureViewID]*gfxbView

	// screen is the current frame's surface render target, refreshed by setScreen
	// before each render and exposed to the plugin as screenID.
	screenID   gfx.TextureViewID
	screenView *wgpu.TextureView
	screenW    int
	screenH    int

	// frame is the frame buffer every screen pass renders into, allocated at
	// the surface's size on first use and dropped when the surface resizes.
	// present is the pipeline that puts it on the surface, built once, and
	// presentBind names the current buffer to it. The module and both layouts
	// are held for the same reason a shader's are: a pipeline reads its layout
	// back when a bind group is set, so releasing them is a use-after-free.
	frame              *gfxbTexture
	frameW             int
	frameH             int
	present            *wgpu.RenderPipeline
	presentModule      *wgpu.ShaderModule
	presentGroupLayout *wgpu.BindGroupLayout
	presentPipeLayout  *wgpu.PipelineLayout
	presentBind        *wgpu.BindGroup
	presentFailed      bool

	// depthOnlyPasses is whether the selected backend can encode a pass with a
	// depth attachment and no colour attachment. It is a property of the HAL
	// that was chosen rather than of the platform - see gfxdepthonly.go, where
	// the platform used to be the axis and stopped being one.
	depthOnlyPasses bool
	// backendName is gogpu's display name for that HAL, kept so a refusal can
	// say which backend refused rather than leaving a reader to guess.
	backendName string

	// captures are the readbacks this backend owes an answer for. Empty is the
	// overwhelmingly common case, and the per-frame cost of that case is one
	// length check.
	captures captureRing

	// refusedDepthOnly records that this backend has already declined a
	// depth-only pass, and refusal holds the error until the plugin takes it to
	// report on the update thread. The backend has no kernel handle of its own,
	// which is also why this and refusedBindGroups are the backend's own state
	// rather than kernel.ReportErrorOnce keys: what they gate is the error
	// parked for takeRefusal to hand over, not a report.
	refusedDepthOnly bool
	refusal          error
	// refusedBindGroups is the set of (shader, group) sites already named, so a
	// material that cannot fill a group is reported once rather than at frame
	// rate. It is cleared per shader when that shader is freed, alongside the
	// bind groups cached for it.
	refusedBindGroups map[gfxbRefusedGroup]struct{}

	prevCmd *wgpu.CommandBuffer
}

type gfxbTexture struct {
	tex  *wgpu.Texture
	view *wgpu.TextureView
}

// gfxbPipeline is a render pipeline plus the shader it was built from (whose
// reflected layout drives bind-group construction at draw time).
type gfxbPipeline struct {
	pipeline *wgpu.RenderPipeline
	shader   *gfxbShader
}

type gfxRenderPass struct {
	backend *gfxBackend
	pass    *wgpu.RenderPassEncoder
	shader  *gfxbShader
}

func (s *gfxRenderPass) SetPipeline(id gfx.PipelineID) {
	if pipeline, ok := s.backend.pipelines[id]; ok {
		s.pass.SetPipeline(pipeline.pipeline)
		if s.shader != pipeline.shader {
			// Bind-group layout compatibility across shaders cannot be inferred
			// from object identity, so a bound group stops counting as bound.
			s.backend.resetBound()
		}
		s.shader = pipeline.shader
	}
	s.backend.resetAcc()
}

func (s *gfxRenderPass) SetUniformBlock(offset, size int) {
	if s.shader == nil || s.backend.uniforms == nil {
		return
	}
	// The block is already in the buffer: BakeUniforms wrote the frame's whole
	// arena before the encoder opened. A block that did not make it emits no
	// binding, which leaves the uniform's group unfilled - and flushBinds
	// refuses that and drops the draw, rather than rendering it with whatever
	// an earlier frame left there.
	if !s.backend.uniforms.bound(offset, size) {
		return
	}
	binding := uint32(s.shader.layout.UniformBinding)
	s.backend.addEntry(s.shader.layout.UniformGroup, gfxbBindEntry{
		key: gfxbBindingKey{
			kind: gfxbBindUniform, binding: uint16(binding),
			id: gfxbUniformArenaID, generation: s.backend.uniforms.generation,
			offset: uint32(offset), size: uint32(size),
		},
		native: wgpu.BindGroupEntry{
			Binding: binding, Buffer: s.backend.uniforms.buffer,
			Offset: uint64(offset), Size: uint64(size),
		},
	})
}

func (s *gfxRenderPass) SetTexture(texture gfx.TextureID, group, binding int) {
	dimension := gfx.TextureView2D
	if s.shader != nil {
		dimension = s.shader.textureViewDimension(group, binding)
	}
	view, textureID, generation := s.backend.textureBinding(texture, dimension)
	s.backend.addEntry(group, gfxbBindEntry{
		key:    gfxbBindingKey{kind: gfxbBindTexture, binding: uint16(binding), id: textureID, generation: generation},
		native: wgpu.BindGroupEntry{Binding: uint32(binding), TextureView: view},
	})
}

func (s *gfxRenderPass) SetSampler(sampler gfx.SamplerID, group, binding int) {
	native, samplerID := s.backend.samplerBinding(sampler)
	s.backend.addEntry(group, gfxbBindEntry{
		key:    gfxbBindingKey{kind: gfxbBindSampler, binding: uint16(binding), id: samplerID},
		native: wgpu.BindGroupEntry{Binding: uint32(binding), Sampler: native},
	})
}

func (s *gfxRenderPass) SetVertexBuffer(id gfx.BufferID, offset int) {
	if buffer, ok := s.backend.bakedBuffers[id]; ok {
		s.pass.SetVertexBuffer(0, buffer, uint64(offset))
	}
}

func (s *gfxRenderPass) SetIndexBuffer(id gfx.BufferID, offset int, width gfx.IndexWidth) {
	if buffer, ok := s.backend.bakedBuffers[id]; ok {
		s.pass.SetIndexBuffer(buffer, indexFormat(width), uint64(offset))
	}
}

func (s *gfxRenderPass) SetBuffer(group, binding int, id gfx.BufferID, offset, size int) {
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

func (s *gfxRenderPass) Draw(first, count, instances, firstInstance int, indexed bool) {
	if s.shader != nil && !s.backend.flushBinds(s.pass, s.shader) {
		s.backend.resetAcc()
		return
	}
	if instances < 1 {
		instances = 1
	}
	if indexed {
		s.pass.DrawIndexed(gputypes.DrawIndexedArgs{
			IndexCount:    uint32(count),
			InstanceCount: uint32(instances),
			FirstIndex:    uint32(first),
			BaseVertex:    0,
			FirstInstance: uint32(firstInstance),
		})
	} else {
		s.pass.Draw(gputypes.DrawArgs{
			VertexCount:   uint32(count),
			InstanceCount: uint32(instances),
			FirstVertex:   uint32(first),
			FirstInstance: uint32(firstInstance),
		})
	}
	s.backend.resetAcc()
}

var _ gfx.Backend = (*gfxBackend)(nil)

// newGfxBackend builds the backend with no device, which is what the plugin
// provides to gfx at registration.
func newGfxBackend() *gfxBackend {
	return &gfxBackend{
		samplers:           map[gfx.SamplerID]*wgpu.Sampler{},
		refusedBindGroups:  map[gfxbRefusedGroup]struct{}{},
		shaders:            map[gfx.ShaderID]*gfxbShader{},
		pipelines:          map[gfx.PipelineID]*gfxbPipeline{},
		bakedBuffers:       map[gfx.BufferID]*wgpu.Buffer{},
		bakedBufferDescs:   map[gfx.BufferID]gfx.BufferDesc{},
		bufferGenerations:  map[gfx.BufferID]uint32{},
		bakedTextures:      map[gfx.TextureID]*gfxbTexture{},
		bakedTextureDescs:  map[gfx.TextureID]gfx.TextureDesc{},
		textureGenerations: map[gfx.TextureID]uint32{},
		depths:             map[gfxbDepthKey]*gfxbTexture{},
		views:              map[gfxbViewKey]*gfxbView{},
		viewID:             map[gfx.TextureViewID]*gfxbView{},
	}
}

// Ready reports whether a device is attached. It is safe from any goroutine.
func (b *gfxBackend) Ready() bool { return b.ready.Load() }

// attach builds the shared layouts, default sampler, and white texture on the
// device, and makes the backend Ready. It returns errDeviceNotReady until the
// GPU device/queue exist (async on browser), and is retried each frame until it
// succeeds. It runs on the render thread.
func (b *gfxBackend) attach(dp gogpu.DeviceProvider, backend string) error {
	b.device, b.queue = dp.Device(), dp.Queue()
	if b.device == nil || b.queue == nil {
		return errDeviceNotReady
	}
	b.surfaceFormat = dp.SurfaceFormat()
	b.backendName = backend
	b.depthOnlyPasses = depthOnlyPassesWork(backend)
	b.bindGroups = newGfxBindGroupCache(
		func(layout *wgpu.BindGroupLayout, entries []wgpu.BindGroupEntry) (*wgpu.BindGroup, error) {
			return b.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
				Label: "gfx.bind", Layout: layout, Entries: entries,
			})
		},
		func(group *wgpu.BindGroup) { group.Release() },
	)
	// After the cache, because the arena drops the cache's entries when it
	// replaces its buffer.
	b.uniforms = newGfxbUniformArenaOn(b)

	var err error
	b.defaultSampler, err = b.device.CreateSampler(&wgpu.SamplerDescriptor{
		Label:        "gfx.defaultSampler",
		AddressModeU: gputypes.AddressModeClampToEdge, AddressModeV: gputypes.AddressModeClampToEdge, AddressModeW: gputypes.AddressModeClampToEdge,
		MagFilter: gputypes.FilterModeLinear, MinFilter: gputypes.FilterModeLinear,
	})
	if err != nil {
		return err
	}

	view, err := createTextureView(b.device, b.queue, 1, 1, []byte{255, 255, 255, 255})
	if err != nil {
		return err
	}
	b.white = &gfxbTexture{tex: view.Texture(), view: view}
	b.whiteArray, err = b.device.CreateTextureView(b.white.tex, &wgpu.TextureViewDescriptor{
		Label:           "gfx.textureView.array",
		Format:          gputypes.TextureFormatRGBA8Unorm,
		Dimension:       gputypes.TextureViewDimension2DArray,
		Aspect:          gputypes.TextureAspectAll,
		MipLevelCount:   1,
		ArrayLayerCount: 1,
	})
	if err != nil {
		return err
	}
	b.screenID = gfx.TextureViewID(b.id())
	b.ready.Store(true)
	return nil
}

func (b *gfxBackend) id() uint32 { b.nextID++; return b.nextID }

// NewTexture reserves a logical texture ID. Native creation stays deferred to
// the render thread when Execute processes its first bake op.
func (b *gfxBackend) NewTexture() gfx.TextureID {
	return gfx.TextureID(b.nextTextureID.Add(1))
}

// NewBuffer reserves a logical buffer ID. Native creation stays deferred to
// the render thread when Execute processes its first bake op.
func (b *gfxBackend) NewBuffer() gfx.BufferID {
	return gfx.BufferID(b.nextBufferID.Add(1))
}

func (b *gfxBackend) newTexture(desc gfx.TextureDesc) (*gfxbTexture, error) {
	layers := max(desc.Layers, 1)
	levels := uint32(1)
	if desc.Mipmaps && mipmapsSupported(desc.Format) {
		levels = uint32(mipLevelCount(desc.Width, desc.Height))
	}
	format := textureFormat(desc.Format)
	tex, err := b.device.CreateTexture(&wgpu.TextureDescriptor{
		Label:         desc.Label,
		Size:          wgpu.Extent3D{Width: uint32(desc.Width), Height: uint32(desc.Height), DepthOrArrayLayers: uint32(layers)},
		MipLevelCount: levels, SampleCount: 1,
		Dimension: gputypes.TextureDimension2D,
		Format:    format,
		Usage:     textureUsage(desc),
	})
	if err != nil {
		return nil, err
	}
	viewDimension := gputypes.TextureViewDimension2D
	if layers > 1 {
		viewDimension = gputypes.TextureViewDimension2DArray
	}
	view, err := b.device.CreateTextureView(tex, &wgpu.TextureViewDescriptor{
		Label: desc.Label, Format: format,
		Dimension: viewDimension, Aspect: gputypes.TextureAspectAll,
		MipLevelCount: levels, ArrayLayerCount: uint32(layers),
	})
	if err != nil {
		tex.Release()
		return nil, err
	}
	return &gfxbTexture{tex: tex, view: view}, nil
}

func (b *gfxBackend) uploadTexture(texture *gfxbTexture, layer int, region gfx.Region, format gfx.TextureFormat, pixels []byte) {
	if texture == nil {
		return
	}
	_ = b.queue.WriteTexture(
		&wgpu.ImageCopyTexture{Texture: texture.tex, MipLevel: 0, Origin: wgpu.Origin3D{X: uint32(region.X), Y: uint32(region.Y), Z: uint32(layer)}, Aspect: gputypes.TextureAspectAll},
		pixels,
		&wgpu.ImageDataLayout{Offset: 0, BytesPerRow: uint32(region.Width * bytesPerTexel(format)), RowsPerImage: uint32(region.Height)},
		&wgpu.Extent3D{Width: uint32(region.Width), Height: uint32(region.Height), DepthOrArrayLayers: 1},
	)
}

// uploadMipChain box-filters level-0 pixels in the format's own colour space
// and writes each smaller mip.
func (b *gfxBackend) uploadMipChain(texture *gfxbTexture, width, height int, format gfx.TextureFormat, pixels []byte) {
	if texture == nil || !mipmapsSupported(format) {
		return
	}
	stride := bytesPerTexel(format)
	src, w, h := pixels, width, height
	for level := 1; w > 1 || h > 1; level++ {
		src, w, h = downsampleTexels(src, w, h, format)
		_ = b.queue.WriteTexture(
			&wgpu.ImageCopyTexture{Texture: texture.tex, MipLevel: uint32(level), Origin: wgpu.Origin3D{}, Aspect: gputypes.TextureAspectAll},
			src,
			&wgpu.ImageDataLayout{Offset: 0, BytesPerRow: uint32(w * stride), RowsPerImage: uint32(h)},
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

func (b *gfxBackend) NewSampler(desc gfx.SamplerDesc) (gfx.SamplerID, error) {
	if err := validateSampler(desc); err != nil {
		return 0, err
	}
	s, err := b.device.CreateSampler(samplerDescriptor(desc))
	if err != nil {
		return 0, err
	}
	id := gfx.SamplerID(b.id())
	b.samplers[id] = s
	return id, nil
}

func (b *gfxBackend) FreeSampler(id gfx.SamplerID) {
	if s, ok := b.samplers[id]; ok {
		b.bindGroups.invalidateResource(gfxbBindSampler, uint32(id))
		s.Release()
		delete(b.samplers, id)
	}
}

func (b *gfxBackend) NewShader(desc gfx.ShaderDesc) (gfx.ShaderID, error) {
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
	// A shader whose bindings cannot be reflected, or whose layouts the device
	// refuses, is unusable: nothing would bind and every draw through it would
	// render undefined. Both are failures rather than warnings, and the module
	// is released rather than leaked behind an ID nobody gets.
	layout, err := reflectShaderLayout(string(desc.Code))
	if err != nil {
		module.Release()
		return 0, fmt.Errorf("gogpu: shader %q reflection failed: %w", label, err)
	}
	sh := newGfxbShader(label, module, layout)
	if err := b.buildShaderLayouts(sh); err != nil {
		module.Release()
		return 0, fmt.Errorf("gogpu: shader %q layout build failed: %w", label, err)
	}
	id := gfx.ShaderID(b.id())
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
			samplerType := gputypes.SamplerBindingTypeFiltering
			if r.Comparison {
				samplerType = gputypes.SamplerBindingTypeComparison
			}
			e.Sampler = &gputypes.SamplerBindingLayout{Type: samplerType}
		} else if r.StorageBuffer {
			bindingType := gputypes.BufferBindingTypeReadOnlyStorage
			if r.WritableBuffer {
				bindingType = gputypes.BufferBindingTypeStorage
			}
			e.Buffer = &gputypes.BufferBindingLayout{Type: bindingType}
		} else {
			view := gputypes.TextureViewDimension2D
			if r.TextureView == gfx.TextureView2DArray {
				view = gputypes.TextureViewDimension2DArray
			}
			sampleType := gputypes.TextureSampleTypeFloat
			if r.Depth {
				sampleType = gputypes.TextureSampleTypeDepth
			}
			e.Texture = &gputypes.TextureBindingLayout{SampleType: sampleType, ViewDimension: view}
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

func (b *gfxBackend) FreeShader(id gfx.ShaderID) {
	s, ok := b.shaders[id]
	if !ok {
		return
	}
	b.bindGroups.invalidateShader(s)
	b.forgetRefusedBindGroups(s)
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
func (b *gfxBackend) ShaderLayout(id gfx.ShaderID) gfx.ShaderLayout {
	if s, ok := b.shaders[id]; ok {
		return s.layout
	}
	return gfx.ShaderLayout{}
}

func (b *gfxBackend) newBuffer(desc gfx.BufferDesc) (*wgpu.Buffer, error) {
	usage := gputypes.BufferUsageCopyDst
	switch desc.Kind {
	case gfx.BufferVertex:
		usage |= gputypes.BufferUsageVertex
	case gfx.BufferIndex:
		usage |= gputypes.BufferUsageIndex
	case gfx.BufferUniform:
		usage |= gputypes.BufferUsageUniform
	case gfx.BufferStorage:
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

func (b *gfxBackend) NewPipeline(desc gfx.PipelineDesc) (gfx.PipelineID, error) {
	sh, ok := b.shaders[desc.Shader]
	if !ok {
		return 0, errors.New("gfx: unknown shader for pipeline")
	}
	module := sh.module
	var buffers []gputypes.VertexBufferLayout
	if desc.Stride > 0 && len(desc.Attributes) > 0 {
		attrs := make([]gputypes.VertexAttribute, len(desc.Attributes))
		for i, a := range desc.Attributes {
			attrs[i] = gputypes.VertexAttribute{
				Format:         vertexFormat(a.Type),
				Offset:         uint64(a.Offset),
				ShaderLocation: uint32(a.Location),
			}
		}
		buffers = []gputypes.VertexBufferLayout{{
			ArrayStride: uint64(desc.Stride),
			StepMode:    gputypes.VertexStepModeVertex,
			Attributes:  attrs,
		}}
	}
	pipeline, err := b.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  desc.Label,
		Layout: sh.pipeLayout,
		Vertex: wgpu.VertexState{
			Module: module, EntryPoint: vertexEntryPoint,
			Buffers: buffers,
		},
		Primitive: gputypes.PrimitiveState{
			Topology:         primitiveTopology(desc.Topology),
			StripIndexFormat: stripIndexFormat(desc.Topology, desc.IndexWidth),
			CullMode:         cullMode(desc.State.Cull),
			FrontFace:        frontFace(desc.State.FrontFace),
		},
		DepthStencil: depthStencilState(desc),
		Fragment:     fragmentState(module, desc),
	})
	if err != nil {
		return 0, err
	}
	id := gfx.PipelineID(b.id())
	b.pipelines[id] = &gfxbPipeline{pipeline: pipeline, shader: sh}
	return id, nil
}

// depthStencilState builds the pipeline's depth state, and returns nil for a
// pipeline that has no depth target.
//
// A pipeline's depth state and its pass's depth attachment are validated
// against each other at setPipeline time, as the colour side is: a DepthNone
// pass has no attachment - passDepth returns nil and BeginPass writes none - so
// a pipeline declaring one is rejected by browser WebGPU, and native Dawn lets
// it through, which is why only the browser loses the frame. Compare and write
// are independent: the transparent pass tests without writing.
func depthStencilState(desc gfx.PipelineDesc) *wgpu.DepthStencilState {
	if desc.NoDepthTarget {
		return nil
	}
	return &wgpu.DepthStencilState{
		Format:            textureFormat(desc.DepthFormat),
		DepthCompare:      compareFunc(desc.State.DepthCompare),
		DepthWriteEnabled: desc.State.DepthWrite,
	}
}

// fragmentState builds the pipeline's fragment stage, and returns nil for a
// pipeline that has no colour target.
//
// nil rather than an empty Targets slice, for two reasons. A render pass and a
// pipeline are validated against each other at setPipeline time, and a
// depth-only pass declares no colour attachment - which BeginPass already
// encodes - so a pipeline that names a target it will never be given is
// rejected and takes the frame's whole command buffer with it. And there is no
// fragment entry point to name: dropping the stage is what lets a depth-only
// shader declare no fs_main at all, which is the shape a shadow or prepass
// shader wants.
func fragmentState(module *wgpu.ShaderModule, desc gfx.PipelineDesc) *wgpu.FragmentState {
	if desc.NoColorTarget {
		return nil
	}
	return &wgpu.FragmentState{
		Module: module, EntryPoint: "fs_main",
		// FormatScreen resolves to the frame buffer's format, not the
		// surface's: a screen pass renders into the frame buffer, and the
		// present pipeline is the only one built for the surface.
		Targets: []gputypes.ColorTargetState{{
			Format:    textureFormat(desc.ColorFormat),
			WriteMask: gputypes.ColorWriteMaskAll,
			Blend:     gfxBlendState(desc.State.Blend),
		}},
	}
}

func gfxBlendState(mode gfx.BlendMode) *gputypes.BlendState {
	component := func(src, dst gputypes.BlendFactor) gputypes.BlendComponent {
		return gputypes.BlendComponent{Operation: gputypes.BlendOperationAdd, SrcFactor: src, DstFactor: dst}
	}
	switch mode {
	case gfx.BlendAlpha:
		return &gputypes.BlendState{
			Color: component(gputypes.BlendFactorSrcAlpha, gputypes.BlendFactorOneMinusSrcAlpha),
			Alpha: component(gputypes.BlendFactorOne, gputypes.BlendFactorOneMinusSrcAlpha),
		}
	case gfx.BlendAdditive:
		return &gputypes.BlendState{
			Color: component(gputypes.BlendFactorSrcAlpha, gputypes.BlendFactorOne),
			Alpha: component(gputypes.BlendFactorOne, gputypes.BlendFactorOne),
		}
	case gfx.BlendMultiply:
		return &gputypes.BlendState{
			Color: component(gputypes.BlendFactorDst, gputypes.BlendFactorZero),
			Alpha: component(gputypes.BlendFactorOne, gputypes.BlendFactorOneMinusSrcAlpha),
		}
	default:
		return nil
	}
}

func (b *gfxBackend) FreePipeline(id gfx.PipelineID) {
	if p, ok := b.pipelines[id]; ok {
		p.pipeline.Release()
		delete(b.pipelines, id)
	}
}

// setScreen refreshes the current frame's surface render target. The driver calls
// it on the render thread after acquiring the surface view, before triggering the
// render.
func (b *gfxBackend) setScreen(view *wgpu.TextureView, w, h int) {
	if w != b.screenW || h != b.screenH {
		// The shared depth textures are keyed by size, and nothing renders at
		// the old one any more; the frame buffer is frame-sized, so it goes
		// with them and the next screen pass allocates one that fits.
		b.releaseDepths()
		b.releaseFrameBuffer()
	}
	b.screenView, b.screenW, b.screenH = view, w, h
}

// ScreenFramebuffer returns the current frame's screen render target and size.
func (b *gfxBackend) ScreenFramebuffer() (gfx.TextureViewID, int, int) {
	return b.screenID, b.screenW, b.screenH
}

// resolveTarget maps a TextureViewID to its native view and size. Only the screen
// target exists today; offscreen render targets slot in here later.
func (b *gfxBackend) resolveTarget(target gfx.TextureViewID) (*wgpu.TextureView, int, int) {
	if target == b.screenID {
		return b.screenView, b.screenW, b.screenH
	}
	return nil, 0, 0
}

// Execute performs the frame's bakes, encodes every pass into one command
// encoder, submits once, then performs releases.
func (b *gfxBackend) Execute(queue *gfx.Queue) {
	b.replacedBuffers = b.replacedBuffers[:0]
	b.replacedTextures = b.replacedTextures[:0]
	// Ahead of the encoder, and the uniform arena first within it, because a
	// resize replaces the uniform buffer and drops every bind group naming it:
	// doing that once the encoder is open would be invalidating groups already
	// recorded into it.
	queue.ReplayBakes(b)

	encoder, err := b.device.CreateCommandEncoder(&wgpu.CommandEncoderDescriptor{Label: "gfx"})
	if err != nil {
		return
	}
	b.encoder = encoder
	queue.ReplayPasses(b)
	b.encoder = nil

	cmd, err := encoder.Finish()
	if err != nil {
		return
	}
	if b.prevCmd != nil {
		b.prevCmd.Release()
	}
	_, _ = b.queue.Submit(cmd)
	b.prevCmd = cmd
	// After the submit, not before it: the map needs a submission to resolve
	// against, and Submit's own tail poll is what triages the previous frame's.
	b.armCapture()
	b.releaseReplacedBaked()
	queue.ReplayReleases(b)
}

// BakeUniforms writes the frame's uniform arena into the uniform buffer.
func (b *gfxBackend) BakeUniforms(arena []byte) {
	if b.uniforms != nil {
		b.uniforms.upload(arena)
	}
}

func (b *gfxBackend) BakeBuffer(id gfx.BufferID, kind gfx.BufferKind, size int, data []byte) {
	b.bakeBuffer(id, kind, size, data)
}

func (b *gfxBackend) BakeTexture(id gfx.TextureID, width, height int, format gfx.TextureFormat, pixels []byte, mipmaps bool) {
	b.bakeTexture(id, width, height, format, pixels, mipmaps)
}

func (b *gfxBackend) AllocateTexture(id gfx.TextureID, desc gfx.TextureDesc) {
	b.allocateTexture(id, desc)
}

// TextureFormat reports what format a texture was allocated or baked in, from
// the descriptors this backend already keeps for every texture it owns. A
// texture it has not created yet - one whose allocation is a bake this same
// Execute has not replayed - is unknown, which is the condition TextureView
// answers with the zero view.
func (b *gfxBackend) TextureFormat(id gfx.TextureID) (gfx.TextureFormat, bool) {
	if _, ok := b.bakedTextures[id]; !ok {
		return 0, false
	}
	return b.bakedTextureDescs[id].Format, true
}

func (b *gfxBackend) UpdateTexture(id gfx.TextureID, layer int, region gfx.Region, pixels []byte) {
	texture, ok := b.bakedTextures[id]
	desc := b.bakedTextureDescs[id]
	if !ok || layer < 0 || layer >= max(desc.Layers, 1) || region.X < 0 || region.Y < 0 ||
		region.Width <= 0 || region.Height <= 0 || region.X+region.Width > desc.Width || region.Y+region.Height > desc.Height || len(pixels) == 0 {
		return
	}
	b.uploadTexture(texture, layer, region, desc.Format, pixels)
}

func (b *gfxBackend) allocateTexture(id gfx.TextureID, desc gfx.TextureDesc) *gfxbTexture {
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
		b.releaseTextureViews(id)
		b.textureGenerations[id]++
		b.replacedTextures = append(b.replacedTextures, old)
	} else {
		b.textureGenerations[id] = 1
	}
	b.bakedTextures[id] = texture
	b.bakedTextureDescs[id] = desc
	return texture
}

func (b *gfxBackend) bakeBuffer(id gfx.BufferID, kind gfx.BufferKind, size int, data []byte) {
	if id == 0 || size <= 0 || len(data) == 0 {
		return
	}
	desc := gfx.BufferDesc{Kind: kind, Size: size, Label: "gfx.baked"}
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

func (b *gfxBackend) bakeTexture(id gfx.TextureID, width, height int, format gfx.TextureFormat, pixels []byte, mipmaps bool) {
	if id == 0 || width <= 0 || height <= 0 || len(pixels) == 0 {
		return
	}
	texture := b.allocateTexture(id, gfx.TextureDesc{Width: width, Height: height, Layers: 1, Format: format, Mipmaps: mipmaps})
	if texture == nil {
		return
	}
	b.uploadTexture(texture, 0, gfx.Region{X: 0, Y: 0, Width: width, Height: height}, format, pixels)
	if mipmaps {
		b.uploadMipChain(texture, width, height, format, pixels)
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

func (b *gfxBackend) ReleaseBuffer(id gfx.BufferID) {
	if buffer, ok := b.bakedBuffers[id]; ok {
		b.bindGroups.invalidateResource(gfxbBindBuffer, uint32(id))
		b.freeBuffer(buffer)
		delete(b.bakedBuffers, id)
		delete(b.bakedBufferDescs, id)
		delete(b.bufferGenerations, id)
	}
}

func (b *gfxBackend) ReleaseTexture(id gfx.TextureID) {
	if texture, ok := b.bakedTextures[id]; ok {
		b.bindGroups.invalidateResource(gfxbBindTexture, uint32(id))
		b.releaseTextureViews(id)
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

// resetBound forgets which bind groups are set, which is what every caller that
// invalidates the redundant-bind filter needs: a pass boundary drops the
// binding state, and a shader change means the layouts a group was bound
// against no longer hold.
func (b *gfxBackend) resetBound() {
	clear(b.bound)
}

// addEntry records a pending bind-group entry for a group in the current draw.
func (b *gfxBackend) addEntry(group int, e gfxbBindEntry) {
	for len(b.acc) <= group {
		b.acc = append(b.acc, nil)
	}
	b.acc[group] = append(b.acc[group], e)
}

// flushBinds reuses or creates one bind group per group the shader declares,
// and reports whether every one of them bound. A false is the caller's cue to
// drop the draw: a group that did not bind leaves its bindings unset, and
// encoding into that produces a second validation error over the first.
//
// It walks the shader's groups rather than the accumulator's, because a group
// with no pending entries at all is the one case the accumulator cannot show.
// A group the shader declares bindings for and nothing filled is a refusal like
// any other: skipping it - which is what walking the accumulator did - encodes
// the draw with that group unset and reports nothing, the quietest way a draw
// can be wrong. A group the shader declares nothing for is skipped, because
// there is nothing there to have gone missing; see gfxbShader.groupSizes for
// why a shader has such a group at all.
func (b *gfxBackend) flushBinds(rp *wgpu.RenderPassEncoder, shader *gfxbShader) bool {
	bound := true
	groups := len(b.acc)
	if declared := len(shader.bgLayouts); declared > groups {
		groups = declared
	}
	for g := 0; g < groups; g++ {
		if g >= len(shader.bgLayouts) || shader.bgLayouts[g] == nil {
			continue
		}
		if g >= len(b.acc) || len(b.acc[g]) == 0 {
			if shader.declaredEntries(g) == 0 {
				continue
			}
			if err := b.noteRefusedBindGroup(shader, g); err != nil && b.refusal == nil {
				b.refusal = err
			}
			bound = false
			continue
		}
		slices.SortFunc(b.acc[g], func(a, b gfxbBindEntry) int {
			return cmp.Compare(a.key.binding, b.key.binding)
		})
		bg := b.bindGroups.get(shader, g, b.acc[g])
		if bg == nil {
			// Every route to a short or malformed entry list ends here, this
			// one included: gfx checks what it emits, but a binding filled by a
			// buffer this backend no longer holds is emitted and then dropped
			// by SetBuffer, and only the refused group shows it.
			if err := b.noteRefusedBindGroup(shader, g); err != nil && b.refusal == nil {
				b.refusal = err
			}
			bound = false
			continue
		}
		for len(b.bound) <= g {
			b.bound = append(b.bound, nil)
		}
		if b.bound[g] == bg {
			continue
		}
		rp.SetBindGroup(uint32(g), bg, nil)
		b.bound[g] = bg
	}
	return bound
}

// textureBinding resolves a texture id to the view a binding of the given
// declared dimension is filled from. An id this backend does not hold is every
// unresolved texture - one still loading, one that failed to load, and a
// binding no parameter named - and it takes the white of that dimension.
func (b *gfxBackend) textureBinding(
	id gfx.TextureID, dimension gfx.TextureViewDimension,
) (*wgpu.TextureView, uint32, uint32) {
	if texture, ok := b.bakedTextures[id]; ok {
		return texture.view, uint32(id), b.textureGenerations[id]
	}
	if dimension == gfx.TextureView2DArray {
		return b.whiteArray, 0, 0
	}
	return b.white.view, 0, 0
}

func (b *gfxBackend) samplerBinding(id gfx.SamplerID) (*wgpu.Sampler, uint32) {
	if s, ok := b.samplers[id]; ok && id != 0 {
		return s, uint32(id)
	}
	return b.defaultSampler, 0
}

func compareFunc(f gfx.CompareFunc) gputypes.CompareFunction {
	switch f {
	case gfx.CompareNever:
		return gputypes.CompareFunctionNever
	case gfx.CompareLess:
		return gputypes.CompareFunctionLess
	case gfx.CompareLessEqual:
		return gputypes.CompareFunctionLessEqual
	case gfx.CompareGreater:
		return gputypes.CompareFunctionGreater
	case gfx.CompareGreaterEqual:
		return gputypes.CompareFunctionGreaterEqual
	case gfx.CompareEqual:
		return gputypes.CompareFunctionEqual
	case gfx.CompareNotEqual:
		return gputypes.CompareFunctionNotEqual
	default:
		return gputypes.CompareFunctionAlways
	}
}

func cullMode(mode gfx.CullMode) gputypes.CullMode {
	switch mode {
	case gfx.CullFront:
		return gputypes.CullModeFront
	case gfx.CullBack:
		return gputypes.CullModeBack
	default:
		return gputypes.CullModeNone
	}
}

func frontFace(face gfx.FrontFace) gputypes.FrontFace {
	if face == gfx.FrontCW {
		return gputypes.FrontFaceCW
	}
	return gputypes.FrontFaceCCW
}

// indexFormat is the WebGPU spelling of one of gfx's two index widths. There
// is no uint8 member to map: WebGPU has none.
func indexFormat(width gfx.IndexWidth) gputypes.IndexFormat {
	if width == gfx.IndexUint16 {
		return gputypes.IndexFormatUint16
	}
	return gputypes.IndexFormatUint32
}

// stripIndexFormat is the format that cuts a strip, which WebGPU requires a
// pipeline to declare before an indexed strip draw is legal, and forbids on any
// other topology. It is the one place a pipeline sees an index width at all -
// a list pipeline never does, which is why only the strip format enters gfx's
// pipeline key.
func stripIndexFormat(topology gfx.PrimitiveTopology, width gfx.IndexWidth) *gputypes.IndexFormat {
	if topology != gfx.TopologyTriangleStrip {
		return nil
	}
	format := indexFormat(width)
	return &format
}

func primitiveTopology(t gfx.PrimitiveTopology) gputypes.PrimitiveTopology {
	switch t {
	case gfx.TopologyTriangleStrip:
		return gputypes.PrimitiveTopologyTriangleStrip
	case gfx.TopologyLineList:
		return gputypes.PrimitiveTopologyLineList
	default:
		return gputypes.PrimitiveTopologyTriangleList
	}
}

func vertexFormat(t gfx.VertexType) gputypes.VertexFormat {
	switch t {
	case gfx.Float32x2:
		return gputypes.VertexFormatFloat32x2
	case gfx.Float32x3:
		return gputypes.VertexFormatFloat32x3
	case gfx.Float32x4:
		return gputypes.VertexFormatFloat32x4
	case gfx.Float16x2:
		return gputypes.VertexFormatFloat16x2
	case gfx.Float16x4:
		return gputypes.VertexFormatFloat16x4
	case gfx.Uint8x2:
		return gputypes.VertexFormatUint8x2
	case gfx.Uint8x4:
		return gputypes.VertexFormatUint8x4
	case gfx.Sint8x2:
		return gputypes.VertexFormatSint8x2
	case gfx.Sint8x4:
		return gputypes.VertexFormatSint8x4
	case gfx.Unorm8x2:
		return gputypes.VertexFormatUnorm8x2
	case gfx.Unorm8x4:
		return gputypes.VertexFormatUnorm8x4
	case gfx.Snorm8x2:
		return gputypes.VertexFormatSnorm8x2
	case gfx.Snorm8x4:
		return gputypes.VertexFormatSnorm8x4
	case gfx.Uint16x2:
		return gputypes.VertexFormatUint16x2
	case gfx.Uint16x4:
		return gputypes.VertexFormatUint16x4
	case gfx.Sint16x2:
		return gputypes.VertexFormatSint16x2
	case gfx.Sint16x4:
		return gputypes.VertexFormatSint16x4
	case gfx.Unorm16x2:
		return gputypes.VertexFormatUnorm16x2
	case gfx.Unorm16x4:
		return gputypes.VertexFormatUnorm16x4
	case gfx.Snorm16x2:
		return gputypes.VertexFormatSnorm16x2
	case gfx.Snorm16x4:
		return gputypes.VertexFormatSnorm16x4
	case gfx.Uint32:
		return gputypes.VertexFormatUint32
	case gfx.Uint32x2:
		return gputypes.VertexFormatUint32x2
	case gfx.Uint32x3:
		return gputypes.VertexFormatUint32x3
	case gfx.Uint32x4:
		return gputypes.VertexFormatUint32x4
	case gfx.Sint32:
		return gputypes.VertexFormatSint32
	case gfx.Sint32x2:
		return gputypes.VertexFormatSint32x2
	case gfx.Sint32x3:
		return gputypes.VertexFormatSint32x3
	case gfx.Sint32x4:
		return gputypes.VertexFormatSint32x4
	case gfx.Unorm1010102:
		return gputypes.VertexFormatUnorm1010102
	default:
		return gputypes.VertexFormatFloat32
	}
}
