package recording_test

import (
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/platform"
)

func TestACreateNamingAMissingHandleAnswersAnError(t *testing.T) {
	for _, test := range []struct {
		name   string
		create func(device gfx.BackendDevice, live, missing objects) error
	}{
		{"a view of a missing texture", func(device gfx.BackendDevice, _, missing objects) error {
			return device.CreateTextureView(missing.view, missing.texture, gfx.TextureViewDescriptor{})
		}},
		{"a pipeline layout of a missing group layout", func(device gfx.BackendDevice, _, missing objects) error {
			return device.CreatePipelineLayout(missing.pipelineLayout, gfx.PipelineLayoutDescriptor{BindGroupLayouts: []gfx.BindGroupLayout{missing.groupLayout}})
		}},
		{"a bind group of a missing layout", func(device gfx.BackendDevice, live, missing objects) error {
			return device.CreateBindGroup(missing.group, gfx.BindGroupDescriptor{Layout: missing.groupLayout, Entries: []gfx.BindGroupEntry{{Binding: 0, Buffer: live.buffer}}})
		}},
		{"a bind group of a missing buffer", func(device gfx.BackendDevice, live, missing objects) error {
			return device.CreateBindGroup(missing.group, gfx.BindGroupDescriptor{Layout: live.groupLayout, Entries: []gfx.BindGroupEntry{{Binding: 0, Buffer: missing.buffer}}})
		}},
		{"a bind group of a missing sampler", func(device gfx.BackendDevice, live, missing objects) error {
			return device.CreateBindGroup(missing.group, gfx.BindGroupDescriptor{Layout: live.groupLayout, Entries: []gfx.BindGroupEntry{{Binding: 1, Sampler: missing.sampler}}})
		}},
		{"a bind group of a missing view", func(device gfx.BackendDevice, live, missing objects) error {
			return device.CreateBindGroup(missing.group, gfx.BindGroupDescriptor{Layout: live.groupLayout, Entries: []gfx.BindGroupEntry{{Binding: 2, TextureView: missing.view}}})
		}},
		{"a render pipeline of a missing layout", func(device gfx.BackendDevice, live, missing objects) error {
			descriptor := renderPipelineDescriptor(live)
			descriptor.Layout = missing.pipelineLayout
			return device.CreateRenderPipeline(missing.renderPipeline, descriptor)
		}},
		{"a render pipeline of a missing vertex module", func(device gfx.BackendDevice, live, missing objects) error {
			descriptor := renderPipelineDescriptor(live)
			descriptor.Vertex.Module = missing.module
			return device.CreateRenderPipeline(missing.renderPipeline, descriptor)
		}},
		{"a render pipeline of a missing fragment module", func(device gfx.BackendDevice, live, missing objects) error {
			descriptor := renderPipelineDescriptor(live)
			descriptor.Fragment.Module = missing.module
			return device.CreateRenderPipeline(missing.renderPipeline, descriptor)
		}},
		{"a compute pipeline of a missing layout", func(device gfx.BackendDevice, live, missing objects) error {
			return device.CreateComputePipeline(missing.computePipeline, gfx.ComputePipelineDescriptor{Layout: missing.pipelineLayout, Module: live.module, EntryPoint: "cs"})
		}},
		{"a compute pipeline of a missing module", func(device gfx.BackendDevice, live, missing objects) error {
			return device.CreateComputePipeline(missing.computePipeline, gfx.ComputePipelineDescriptor{Layout: live.pipelineLayout, Module: missing.module, EntryPoint: "cs"})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance, device := requestDevice(t)
			live := createObjects(t, device, everyBufferUsage, everyTextureUsage)

			if err := test.create(device, live, missingObjects()); err == nil {
				t.Fatal("the create answered no error")
			}
			calls := instance.Recording().Calls
			if last := calls[len(calls)-1]; last.Error == nil {
				t.Errorf("the create was recorded with no error: %+v", last)
			}
		})
	}
}

func TestAnOptionalHandleLeftZeroNamesNothing(t *testing.T) {
	_, device := requestDevice(t)
	live := createObjects(t, device, everyBufferUsage, everyTextureUsage)
	descriptor := renderPipelineDescriptor(live)
	descriptor.Fragment = gfx.FragmentState{}

	if err := device.CreateRenderPipeline(handleAt[gfx.RenderPipeline](2), descriptor); err != nil {
		t.Fatalf("a render pipeline with no fragment stage answered %v", err)
	}
	encoder := createEncoder(t, device)
	encoder.BeginRenderPass(gfx.RenderPassDescriptor{ColorAttachments: []gfx.RenderPassColorAttachment{{View: live.view}}}).End()
	if _, err := encoder.Finish(); err != nil {
		t.Fatalf("a pass with no resolve target or depth attachment answered %v at Finish", err)
	}
}

func TestReleasingAMissingHandleRecordsAnError(t *testing.T) {
	instance, device := requestDevice(t)

	device.ReleaseBuffer(missingObjects().buffer)

	if calls := instance.Recording().Calls; calls[len(calls)-1].Error == nil {
		t.Fatalf("the release was recorded with no error: %+v", calls[len(calls)-1])
	}
}

func TestABindGroupNeedsTheUsageItsLayoutBindsEachResourceFor(t *testing.T) {
	for _, test := range []struct {
		name              string
		layout            gfx.BindGroupLayoutEntry
		lacksBufferUsage  gfx.BufferUsageFlags
		lacksTextureUsage gfx.TextureUsageFlags
		bindsTheView      bool
	}{
		{name: "a uniform buffer", layout: gfx.BindGroupLayoutEntry{Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform}}, lacksBufferUsage: gfx.BufferUsageFlagUniform},
		{name: "a storage buffer", layout: gfx.BindGroupLayoutEntry{Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeStorage}}, lacksBufferUsage: gfx.BufferUsageFlagStorage},
		{name: "a read-only storage buffer", layout: gfx.BindGroupLayoutEntry{Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeReadOnlyStorage}}, lacksBufferUsage: gfx.BufferUsageFlagStorage},
		{name: "a storage texture", layout: gfx.BindGroupLayoutEntry{StorageTexture: gfx.StorageTextureBindingLayout{Access: gfx.StorageTextureAccessWriteOnly, Format: gfx.TextureFormatRGBA8Unorm}}, lacksTextureUsage: gfx.TextureUsageFlagStorageBinding, bindsTheView: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, lacking := range []bool{false, true} {
				_, device := requestDevice(t)
				bufferUsage := everyBufferUsage
				textureUsage := everyTextureUsage
				if lacking {
					bufferUsage &^= test.lacksBufferUsage
					textureUsage &^= test.lacksTextureUsage
				}
				live := objectsAt(2)
				mustSucceed(t, device.CreateBuffer(live.buffer, gfx.BufferDescriptor{Size: 256, Usage: bufferUsage}))
				mustSucceed(t, device.CreateTexture(live.texture, gfx.TextureDescriptor{Format: gfx.TextureFormatRGBA8Unorm, Usage: textureUsage}))
				mustSucceed(t, device.CreateTextureView(live.view, live.texture, gfx.TextureViewDescriptor{}))
				layout := handleAt[gfx.BindGroupLayout](2)
				mustSucceed(t, device.CreateBindGroupLayout(layout, gfx.BindGroupLayoutDescriptor{Entries: []gfx.BindGroupLayoutEntry{test.layout}}))
				entry := gfx.BindGroupEntry{Buffer: live.buffer}
				if test.bindsTheView {
					entry = gfx.BindGroupEntry{TextureView: live.view}
				}

				err := device.CreateBindGroup(handleAt[gfx.BindGroup](2), gfx.BindGroupDescriptor{Layout: layout, Entries: []gfx.BindGroupEntry{entry}})
				if lacking && err == nil {
					t.Error("binding a resource that lacks the usage answered no error")
				}
				if !lacking && err != nil {
					t.Errorf("binding a resource with the usage answered %v", err)
				}
			}
		})
	}
}

func TestEveryCommandOnLiveObjectsWithTheirUsageFinishes(t *testing.T) {
	for _, test := range commandTests {
		t.Run(test.name, func(t *testing.T) {
			_, device := requestDevice(t)
			live := createObjects(t, device, everyBufferUsage, everyTextureUsage)
			encoder := createEncoder(t, device)

			test.record(encoder, live, live)

			if _, err := encoder.Finish(); err != nil {
				t.Fatalf("Finish answered %v", err)
			}
		})
	}
}

func TestACommandNamingAMissingHandleFailsAtFinish(t *testing.T) {
	for _, test := range commandTests {
		t.Run(test.name, func(t *testing.T) {
			_, device := requestDevice(t)
			live := createObjects(t, device, everyBufferUsage, everyTextureUsage)
			encoder := createEncoder(t, device)

			test.record(encoder, live, missingObjects())

			if _, err := encoder.Finish(); err == nil {
				t.Fatal("Finish answered no error")
			}
		})
	}
}

func TestACommandOnAResourceLackingItsUsageFailsAtFinish(t *testing.T) {
	for _, test := range commandTests {
		if test.bufferUsage == 0 && test.textureUsage == 0 {
			continue
		}
		t.Run(test.name, func(t *testing.T) {
			_, device := requestDevice(t)
			live := createObjects(t, device, everyBufferUsage, everyTextureUsage)
			lacking := createObjects(t, device, everyBufferUsage&^test.bufferUsage, everyTextureUsage&^test.textureUsage, 2)
			encoder := createEncoder(t, device)

			test.record(encoder, live, lacking)

			if _, err := encoder.Finish(); err == nil {
				t.Fatal("Finish answered no error")
			}
		})
	}
}

func TestAnAcquiredFramebufferIsARenderAttachmentUntilItsPresent(t *testing.T) {
	instance, device := requestDevice(t)
	window := platform.Window{}
	framebuffer := handleAt[gfx.TextureView](5)
	mustSucceed(t, instance.CreateSurface(window, platform.NativeHandle{}))
	mustSucceed(t, device.ConfigureSurface(window, gfx.SurfaceConfiguration{}))
	if _, err := device.AcquireFramebuffer(window, framebuffer); err != nil {
		t.Fatalf("AcquireFramebuffer answered %v", err)
	}
	attachments := []gfx.RenderPassColorAttachment{{View: framebuffer}}

	acquired := createEncoder(t, device)
	acquired.BeginRenderPass(gfx.RenderPassDescriptor{ColorAttachments: attachments}).End()
	if _, err := acquired.Finish(); err != nil {
		t.Fatalf("a pass on the acquired framebuffer answered %v at Finish", err)
	}
	mustSucceed(t, device.Present(window))
	presented := createEncoder(t, device)
	presented.BeginRenderPass(gfx.RenderPassDescriptor{ColorAttachments: attachments}).End()
	if _, err := presented.Finish(); err == nil {
		t.Fatal("a pass on the presented framebuffer answered no error at Finish")
	}
}

func TestAViewKeepsItsTexturesUsageOnceTheTextureIsReleased(t *testing.T) {
	_, device := requestDevice(t)
	live := createObjects(t, device, everyBufferUsage, everyTextureUsage)
	device.ReleaseTexture(live.texture)

	encoder := createEncoder(t, device)
	encoder.BeginRenderPass(gfx.RenderPassDescriptor{ColorAttachments: []gfx.RenderPassColorAttachment{{View: live.view}}}).End()
	if _, err := encoder.Finish(); err != nil {
		t.Fatalf("a pass on a view of a released texture answered %v at Finish", err)
	}
}

func TestAFailedCommandLeavesTheEncoderSpent(t *testing.T) {
	_, device := requestDevice(t)
	encoder := createEncoder(t, device)
	encoder.ClearBuffer(missingObjects().buffer, 0, 4)

	if _, err := encoder.Finish(); err == nil {
		t.Fatal("Finish answered no error")
	}
	if _, err := encoder.Finish(); err == nil {
		t.Fatal("a second Finish answered no error")
	}
}

// commandTests record, each, one command on the objects under test, other
// objects it needs from live; bufferUsage and textureUsage are what the
// objects under test need for it, zero when it needs none.
var commandTests = []struct {
	name         string
	bufferUsage  gfx.BufferUsageFlags
	textureUsage gfx.TextureUsageFlags
	record       func(encoder gfx.CommandEncoder, live, tested objects)
}{
	{name: "a color attachment", textureUsage: gfx.TextureUsageFlagRenderAttachment, record: func(encoder gfx.CommandEncoder, _, tested objects) {
		encoder.BeginRenderPass(gfx.RenderPassDescriptor{ColorAttachments: []gfx.RenderPassColorAttachment{{View: tested.view}}}).End()
	}},
	{name: "a resolve target", textureUsage: gfx.TextureUsageFlagRenderAttachment, record: func(encoder gfx.CommandEncoder, live, tested objects) {
		encoder.BeginRenderPass(gfx.RenderPassDescriptor{ColorAttachments: []gfx.RenderPassColorAttachment{{View: live.view, ResolveTarget: tested.view}}}).End()
	}},
	{name: "a depth-stencil attachment", textureUsage: gfx.TextureUsageFlagRenderAttachment, record: func(encoder gfx.CommandEncoder, live, tested objects) {
		encoder.BeginRenderPass(gfx.RenderPassDescriptor{
			ColorAttachments:       []gfx.RenderPassColorAttachment{{View: live.view}},
			DepthStencilAttachment: gfx.RenderPassDepthStencilAttachment{View: tested.view},
		}).End()
	}},
	{name: "a render pipeline", record: func(encoder gfx.CommandEncoder, live, tested objects) {
		renderPass(encoder, live).SetPipeline(tested.renderPipeline)
	}},
	{name: "a render pass's bind group", record: func(encoder gfx.CommandEncoder, live, tested objects) {
		renderPass(encoder, live).SetBindGroup(0, tested.group, nil)
	}},
	{name: "a vertex buffer", bufferUsage: gfx.BufferUsageFlagVertex, record: func(encoder gfx.CommandEncoder, live, tested objects) {
		renderPass(encoder, live).SetVertexBuffer(0, tested.buffer, 0)
	}},
	{name: "an index buffer", bufferUsage: gfx.BufferUsageFlagIndex, record: func(encoder gfx.CommandEncoder, live, tested objects) {
		renderPass(encoder, live).SetIndexBuffer(tested.buffer, gfx.IndexFormatUint16, 0)
	}},
	{name: "an indirect draw", record: func(encoder gfx.CommandEncoder, live, tested objects) {
		renderPass(encoder, live).DrawIndirect(tested.buffer, 0)
	}},
	{name: "an indexed indirect draw", record: func(encoder gfx.CommandEncoder, live, tested objects) {
		renderPass(encoder, live).DrawIndexedIndirect(tested.buffer, 0)
	}},
	{name: "indirect draws", record: func(encoder gfx.CommandEncoder, live, tested objects) {
		renderPass(encoder, live).MultiDrawIndirect(tested.buffer, 0, 2)
	}},
	{name: "indexed indirect draws", record: func(encoder gfx.CommandEncoder, live, tested objects) {
		renderPass(encoder, live).MultiDrawIndexedIndirect(tested.buffer, 0, 2)
	}},
	{name: "a compute pipeline", record: func(encoder gfx.CommandEncoder, _, tested objects) {
		encoder.BeginComputePass(gfx.ComputePassDescriptor{}).SetPipeline(tested.computePipeline)
	}},
	{name: "a compute pass's bind group", record: func(encoder gfx.CommandEncoder, _, tested objects) {
		encoder.BeginComputePass(gfx.ComputePassDescriptor{}).SetBindGroup(0, tested.group, nil)
	}},
	{name: "an indirect dispatch", record: func(encoder gfx.CommandEncoder, _, tested objects) {
		encoder.BeginComputePass(gfx.ComputePassDescriptor{}).DispatchIndirect(tested.buffer, 0)
	}},
	{name: "a buffer copy's source", bufferUsage: gfx.BufferUsageFlagCopySrc, record: func(encoder gfx.CommandEncoder, live, tested objects) {
		encoder.CopyBufferToBuffer(tested.buffer, 0, live.buffer, 0, 4)
	}},
	{name: "a buffer copy's destination", bufferUsage: gfx.BufferUsageFlagCopyDst, record: func(encoder gfx.CommandEncoder, live, tested objects) {
		encoder.CopyBufferToBuffer(live.buffer, 0, tested.buffer, 0, 4)
	}},
	{name: "a buffer-to-texture copy's source", bufferUsage: gfx.BufferUsageFlagCopySrc, record: func(encoder gfx.CommandEncoder, live, tested objects) {
		encoder.CopyBufferToTexture(tested.buffer, live.texture, nil)
	}},
	{name: "a buffer-to-texture copy's destination", textureUsage: gfx.TextureUsageFlagCopyDst, record: func(encoder gfx.CommandEncoder, live, tested objects) {
		encoder.CopyBufferToTexture(live.buffer, tested.texture, nil)
	}},
	{name: "a texture-to-buffer copy's source", textureUsage: gfx.TextureUsageFlagCopySrc, record: func(encoder gfx.CommandEncoder, live, tested objects) {
		encoder.CopyTextureToBuffer(tested.texture, live.buffer, nil)
	}},
	{name: "a texture-to-buffer copy's destination", bufferUsage: gfx.BufferUsageFlagCopyDst, record: func(encoder gfx.CommandEncoder, live, tested objects) {
		encoder.CopyTextureToBuffer(live.texture, tested.buffer, nil)
	}},
	{name: "a texture copy's source", textureUsage: gfx.TextureUsageFlagCopySrc, record: func(encoder gfx.CommandEncoder, live, tested objects) {
		encoder.CopyTextureToTexture(tested.texture, live.texture, nil)
	}},
	{name: "a texture copy's destination", textureUsage: gfx.TextureUsageFlagCopyDst, record: func(encoder gfx.CommandEncoder, live, tested objects) {
		encoder.CopyTextureToTexture(live.texture, tested.texture, nil)
	}},
	{name: "a cleared buffer", bufferUsage: gfx.BufferUsageFlagCopyDst, record: func(encoder gfx.CommandEncoder, _, tested objects) {
		encoder.ClearBuffer(tested.buffer, 0, 4)
	}},
}

// everyBufferUsage and everyTextureUsage are every usage a buffer and a
// texture can be created with.
const (
	everyBufferUsage = gfx.BufferUsageFlagMapRead | gfx.BufferUsageFlagMapWrite | gfx.BufferUsageFlagCopySrc |
		gfx.BufferUsageFlagCopyDst | gfx.BufferUsageFlagIndex | gfx.BufferUsageFlagVertex |
		gfx.BufferUsageFlagUniform | gfx.BufferUsageFlagStorage | gfx.BufferUsageFlagIndirect
	everyTextureUsage = gfx.TextureUsageFlagCopySrc | gfx.TextureUsageFlagCopyDst | gfx.TextureUsageFlagTextureBinding |
		gfx.TextureUsageFlagStorageBinding | gfx.TextureUsageFlagRenderAttachment
)

// missingIndex is the index of missingObjects' handles, where no test creates
// anything.
const missingIndex = 9

// objects are one handle of each type, all at one index.
type objects struct {
	buffer          gfx.Buffer
	texture         gfx.Texture
	view            gfx.TextureView
	sampler         gfx.Sampler
	module          gfx.ShaderModule
	groupLayout     gfx.BindGroupLayout
	group           gfx.BindGroup
	pipelineLayout  gfx.PipelineLayout
	renderPipeline  gfx.RenderPipeline
	computePipeline gfx.ComputePipeline
}

// objectsAt answers a handle of each type at index.
func objectsAt(index uint32) objects {
	return objects{
		buffer:          handleAt[gfx.Buffer](index),
		texture:         handleAt[gfx.Texture](index),
		view:            handleAt[gfx.TextureView](index),
		sampler:         handleAt[gfx.Sampler](index),
		module:          handleAt[gfx.ShaderModule](index),
		groupLayout:     handleAt[gfx.BindGroupLayout](index),
		group:           handleAt[gfx.BindGroup](index),
		pipelineLayout:  handleAt[gfx.PipelineLayout](index),
		renderPipeline:  handleAt[gfx.RenderPipeline](index),
		computePipeline: handleAt[gfx.ComputePipeline](index),
	}
}

// missingObjects answers handles at an index where nothing was created.
func missingObjects() objects { return objectsAt(missingIndex) }

// createObjects creates one object of each type at index, 1 unless given, the
// buffer and the texture with the usage given, the group layout binding a
// uniform buffer, a sampler and a sampled texture.
func createObjects(t *testing.T, device gfx.BackendDevice, bufferUsage gfx.BufferUsageFlags, textureUsage gfx.TextureUsageFlags, index ...uint32) objects {
	t.Helper()
	live := objectsAt(1)
	if len(index) > 0 {
		live = objectsAt(index[0])
	}
	mustSucceed(t, device.CreateBuffer(live.buffer, gfx.BufferDescriptor{Size: 256, Usage: bufferUsage}))
	mustSucceed(t, device.CreateTexture(live.texture, gfx.TextureDescriptor{
		Size:          gfx.Extent3D{Width: 4, Height: 4, DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     gfx.TextureDimension2D,
		Format:        gfx.TextureFormatRGBA8Unorm,
		Usage:         textureUsage,
	}))
	mustSucceed(t, device.CreateTextureView(live.view, live.texture, gfx.TextureViewDescriptor{}))
	mustSucceed(t, device.CreateSampler(live.sampler, gfx.SamplerDescriptor{}))
	mustSucceed(t, device.CreateShaderModule(live.module, gfx.ShaderModuleDescriptor{}))
	mustSucceed(t, device.CreateBindGroupLayout(live.groupLayout, gfx.BindGroupLayoutDescriptor{Entries: []gfx.BindGroupLayoutEntry{
		{Binding: 0, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform}},
		{Binding: 1, Sampler: gfx.SamplerBindingLayout{Type: gfx.SamplerBindingTypeFiltering}},
		{Binding: 2, Texture: gfx.TextureBindingLayout{SampleType: gfx.TextureSampleTypeFloat}},
	}}))
	mustSucceed(t, device.CreateBindGroup(live.group, gfx.BindGroupDescriptor{Layout: live.groupLayout, Entries: []gfx.BindGroupEntry{
		{Binding: 0, Buffer: live.buffer},
		{Binding: 1, Sampler: live.sampler},
		{Binding: 2, TextureView: live.view},
	}}))
	mustSucceed(t, device.CreatePipelineLayout(live.pipelineLayout, gfx.PipelineLayoutDescriptor{BindGroupLayouts: []gfx.BindGroupLayout{live.groupLayout}}))
	mustSucceed(t, device.CreateRenderPipeline(live.renderPipeline, renderPipelineDescriptor(live)))
	mustSucceed(t, device.CreateComputePipeline(live.computePipeline, gfx.ComputePipelineDescriptor{Layout: live.pipelineLayout, Module: live.module, EntryPoint: "cs"}))
	return live
}

// renderPipelineDescriptor answers a pipeline of live's layout and module, in
// both stages.
func renderPipelineDescriptor(live objects) gfx.RenderPipelineDescriptor {
	return gfx.RenderPipelineDescriptor{
		Layout:   live.pipelineLayout,
		Vertex:   gfx.VertexState{Module: live.module, EntryPoint: "vs"},
		Fragment: gfx.FragmentState{Module: live.module, EntryPoint: "fs", Targets: []gfx.ColorTargetState{{Format: gfx.TextureFormatRGBA8Unorm}}},
	}
}

// renderPass begins a render pass on live's view, for a command to record
// into.
func renderPass(encoder gfx.CommandEncoder, live objects) gfx.RenderPassEncoder {
	return encoder.BeginRenderPass(gfx.RenderPassDescriptor{ColorAttachments: []gfx.RenderPassColorAttachment{{View: live.view}}})
}

// requestDevice answers a recording instance and its device, as gfx's start
// requests them.
func requestDevice(t *testing.T) (*recording.Instance, gfx.BackendDevice) {
	t.Helper()
	instance := recording.NewInstance()
	adapter, err := instance.RequestAdapter(gfx.RequestAdapterOptions{})
	mustSucceed(t, err)
	device, err := adapter.RequestDevice(gfx.DeviceDescriptor{})
	mustSucceed(t, err)
	return instance, device
}

// createEncoder answers an encoder of device.
func createEncoder(t *testing.T, device gfx.BackendDevice) gfx.StandaloneCommandEncoder {
	t.Helper()
	encoder, err := device.CreateCommandEncoder(gfx.CommandEncoderDescriptor{})
	mustSucceed(t, err)
	return encoder
}

// handleAt answers a handle of type H at index, as gfx's table hands one out
// with its first generation. Only gfx makes handles, so it writes the index
// and generation over the handle's two words.
func handleAt[H gfx.Buffer | gfx.Texture | gfx.TextureView | gfx.Sampler | gfx.ShaderModule | gfx.BindGroupLayout |
	gfx.BindGroup | gfx.PipelineLayout | gfx.RenderPipeline | gfx.ComputePipeline](index uint32) H {
	var handle H
	if unsafe.Sizeof(handle) != unsafe.Sizeof([2]uint32{}) {
		panic("recording: a gfx handle is no longer an index and a generation")
	}
	*(*[2]uint32)(unsafe.Pointer(&handle)) = [2]uint32{index, 1}
	return handle
}

// mustSucceed fails the test at once on err.
func mustSucceed(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
