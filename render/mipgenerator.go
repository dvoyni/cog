package render

import (
	"fmt"
	"io/fs"
	"sync"

	"github.com/dvoyni/cog/gfx"
)

// mipGenerator generates the mip levels of textures in render's own command
// list: each level of each layer drawn from the level above it, of the same
// layer, by the mip shader through a linear filter. Its mutex guards what it
// holds: the textures whose mips are requested; the shader, read from
// mipShaderPath in platform's FS, and the layouts and sampler, made at its
// first generation; a pipeline for each format it has
// drawn into; and the bind groups of the frame being recorded, released when
// the frame ends. It takes the asset table's mutex inside its own.
type mipGenerator struct {
	mutex             sync.Mutex
	requestedTextures []Texture
	module            gfx.ShaderModule
	bindGroupLayout   gfx.BindGroupLayout
	pipelineLayout    gfx.PipelineLayout
	sampler           gfx.Sampler
	pipelines         map[gfx.TextureFormat]gfx.RenderPipeline
	frameBindGroups   []gfx.BindGroup
}

// request has the mips of textures generated at the next generation.
func (g *mipGenerator) request(textures ...Texture) {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.requestedTextures = append(g.requestedTextures, textures...)
}

// generate records into frame's encoder at render's own order every mip level
// below the first of each texture requested that table still holds, level by
// level, each layer at a level in a pass of its own. It takes the encoder,
// and makes what it draws with, only for a texture of more than one level. A
// failure to make what it draws with, or to take the encoder, is logged
// through table, unless the device is lost, and generates no texture's mips,
// as a texture's own failure generates none of its.
func (g *mipGenerator) generate(table *assetTable, device gfx.Device, files fs.FS, frame gfx.Frame) {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	textures := g.requestedTextures
	g.requestedTextures = g.requestedTextures[:0]
	var encoder gfx.CommandEncoder
	for _, texture := range textures {
		gfxTexture, ok := table.findTexture(texture)
		if !ok {
			continue
		}
		descriptor := device.TextureDescriptor(gfxTexture)
		if descriptor.MipLevelCount == 1 {
			continue
		}
		if encoder == nil {
			var err error
			if encoder, err = g.takeEncoder(device, files, frame); err != nil {
				table.logUnlessLost(err)
				return
			}
		}
		if err := g.generateTexture(table, device, encoder, texture, descriptor); err != nil {
			table.logUnlessLost(fmt.Errorf("render: generating the mips of texture %q: %w", descriptor.Label, err))
		}
	}
}

// endFrame releases the bind groups the frame's generation made, once gfx has
// submitted it.
func (g *mipGenerator) endFrame(device gfx.Device) {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	for _, group := range g.frameBindGroups {
		device.ReleaseBindGroup(group)
	}
	g.frameBindGroups = g.frameBindGroups[:0]
}

// takeEncoder makes what g draws with from files, unless it holds it, and
// answers frame's encoder at render's own order. g's mutex is held.
func (g *mipGenerator) takeEncoder(device gfx.Device, files fs.FS, frame gfx.Frame) (gfx.CommandEncoder, error) {
	if err := g.init(device, files); err != nil {
		return nil, fmt.Errorf("render: making the mip shader: %w", err)
	}
	encoder, err := device.Encoder(frame, gfx.SubmitOrder(ownOrder))
	if err != nil {
		return nil, fmt.Errorf("render: taking render's own command list: %w", err)
	}
	return encoder, nil
}

// init makes the shader, read from mipShaderPath in files, and the layouts
// and sampler g draws with, unless it holds them; a failure releases those
// it made, in reverse order. g's mutex is held.
func (g *mipGenerator) init(device gfx.Device, files fs.FS) (err error) {
	if g.module != (gfx.ShaderModule{}) {
		return nil
	}
	var module gfx.ShaderModule
	var bindGroupLayout gfx.BindGroupLayout
	var pipelineLayout gfx.PipelineLayout
	defer func() {
		if err == nil {
			return
		}
		if pipelineLayout != (gfx.PipelineLayout{}) {
			device.ReleasePipelineLayout(pipelineLayout)
		}
		if bindGroupLayout != (gfx.BindGroupLayout{}) {
			device.ReleaseBindGroupLayout(bindGroupLayout)
		}
		if module != (gfx.ShaderModule{}) {
			device.ReleaseShaderModule(module)
		}
	}()
	source, err := device.PreprocessShader(files, mipShaderPath, nil)
	if err != nil {
		return err
	}
	if module, err = device.CreateShaderModule(gfx.ShaderModuleDescriptor{Label: mipShaderPath, WGSL: source}); err != nil {
		return err
	}
	if bindGroupLayout, err = device.CreateBindGroupLayout(gfx.BindGroupLayoutDescriptor{Label: "render: mips", Entries: []gfx.BindGroupLayoutEntry{
		{Binding: 0, Visibility: gfx.ShaderStageFlagFragment, Texture: gfx.TextureBindingLayout{SampleType: gfx.TextureSampleTypeFloat, ViewDimension: gfx.TextureViewDimension2D}},
		{Binding: 1, Visibility: gfx.ShaderStageFlagFragment, Sampler: gfx.SamplerBindingLayout{Type: gfx.SamplerBindingTypeFiltering}},
	}}); err != nil {
		return err
	}
	if pipelineLayout, err = device.CreatePipelineLayout(gfx.PipelineLayoutDescriptor{Label: "render: mips", BindGroupLayouts: []gfx.BindGroupLayout{bindGroupLayout}}); err != nil {
		return err
	}
	sampler, err := device.CreateSampler(gfx.SamplerDescriptor{
		Label:        "render: mips",
		AddressModeU: gfx.AddressModeClampToEdge,
		AddressModeV: gfx.AddressModeClampToEdge,
		AddressModeW: gfx.AddressModeClampToEdge,
		MagFilter:    gfx.FilterModeLinear,
		MinFilter:    gfx.FilterModeLinear,
		MipmapFilter: gfx.FilterModeNearest,
		LodMaxClamp:  32,
	})
	if err != nil {
		return err
	}
	g.module = module
	g.bindGroupLayout = bindGroupLayout
	g.pipelineLayout = pipelineLayout
	g.sampler = sampler
	g.pipelines = map[gfx.TextureFormat]gfx.RenderPipeline{}
	return nil
}

// generateTexture records into encoder the mip levels below the first of
// texture, of descriptor, each from the level above it at the same layer. g's
// mutex is held.
func (g *mipGenerator) generateTexture(table *assetTable, device gfx.Device, encoder gfx.CommandEncoder, texture Texture, descriptor gfx.TextureDescriptor) error {
	pipeline, ok := g.pipelines[descriptor.Format]
	if !ok {
		var err error
		pipeline, err = device.CreateRenderPipeline(gfx.RenderPipelineDescriptor{
			Label:       "render: mips",
			Layout:      g.pipelineLayout,
			Vertex:      gfx.VertexState{Module: g.module, EntryPoint: "vertex_main"},
			Multisample: gfx.MultisampleState{Count: 1, Mask: ^uint64(0)},
			Fragment: gfx.FragmentState{Module: g.module, EntryPoint: "fragment_main", Targets: []gfx.ColorTargetState{
				{Format: descriptor.Format, WriteMask: gfx.ColorWriteFlagAll},
			}},
		})
		if err != nil {
			return err
		}
		g.pipelines[descriptor.Format] = pipeline
	}
	// sources are the views of the level above, by layer: each pass's target
	// is its next level's source.
	sources := make([]gfx.TextureView, descriptor.Size.DepthOrArrayLayers)
	for layer := range sources {
		var err error
		if sources[layer], _, err = table.findLevelView(device, texture, 0, uint32(layer)); err != nil {
			return err
		}
	}
	for mipLevel := uint32(1); mipLevel < descriptor.MipLevelCount; mipLevel++ {
		for layer, source := range sources {
			target, _, err := table.findLevelView(device, texture, mipLevel, uint32(layer))
			if err != nil {
				return err
			}
			group, err := device.CreateBindGroup(gfx.BindGroupDescriptor{Label: "render: mips", Layout: g.bindGroupLayout, Entries: []gfx.BindGroupEntry{
				{Binding: 0, TextureView: source},
				{Binding: 1, Sampler: g.sampler},
			}})
			if err != nil {
				return err
			}
			g.frameBindGroups = append(g.frameBindGroups, group)
			pass := encoder.BeginRenderPass(gfx.RenderPassDescriptor{
				Label:            fmt.Sprintf("render: mip level %d, layer %d, of %s", mipLevel, layer, descriptor.Label),
				ColorAttachments: []gfx.RenderPassColorAttachment{{View: target, LoadOp: gfx.LoadOpClear, StoreOp: gfx.StoreOpStore}},
			})
			pass.SetPipeline(pipeline)
			pass.SetBindGroup(0, group, nil)
			pass.Draw(gfx.DrawArgs{VertexCount: 3, InstanceCount: 1})
			pass.End()
			sources[layer] = target
		}
	}
	return nil
}
