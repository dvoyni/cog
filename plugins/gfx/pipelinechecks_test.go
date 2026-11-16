package gfx_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dvoyni/cog/plugins/gfx"
	"github.com/dvoyni/cog/plugins/gfx/recording"
)

func TestAVertexLayoutNotMatchingTheShadersInputsIsAnErrorNamingBoth(t *testing.T) {
	for _, test := range []struct {
		name       string
		attributes []gfx.VertexAttribute
		inputs     []gfx.VertexInput
		named      []string
	}{
		{"an input with no attribute at its location",
			[]gfx.VertexAttribute{{Format: gfx.VertexFormatFloat32x3, ShaderLocation: 0}},
			[]gfx.VertexInput{{Location: 1, Type: gfx.ScalarTypeFloat32, Components: 2}},
			[]string{"vec2<f32>", "location 1"}},
		{"a uint attribute for a float input",
			[]gfx.VertexAttribute{{Format: gfx.VertexFormatUint32x2, ShaderLocation: 0}},
			[]gfx.VertexInput{{Location: 0, Type: gfx.ScalarTypeFloat32, Components: 2}},
			[]string{"Uint32x2", "vec2<f32>", "location 0"}},
		{"a float attribute for a sint input",
			[]gfx.VertexAttribute{{Format: gfx.VertexFormatFloat32, ShaderLocation: 2}},
			[]gfx.VertexInput{{Location: 2, Type: gfx.ScalarTypeSint32, Components: 1}},
			[]string{"Float32", "i32", "location 2"}},
		{"a sint attribute for a uint input",
			[]gfx.VertexAttribute{{Format: gfx.VertexFormatSint8x4, ShaderLocation: 0}},
			[]gfx.VertexInput{{Location: 0, Type: gfx.ScalarTypeUint32, Components: 4}},
			[]string{"Sint8x4", "vec4<u32>", "location 0"}},
		{"a normalized attribute for a uint input",
			[]gfx.VertexAttribute{{Format: gfx.VertexFormatUnorm8x4, ShaderLocation: 0}},
			[]gfx.VertexInput{{Location: 0, Type: gfx.ScalarTypeUint32, Components: 4}},
			[]string{"Unorm8x4", "vec4<u32>", "location 0"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustSpawn(nil)
			h.instance.AnswerNext(recording.CallKindReflectVertexInputs, recording.Answer{VertexInputs: test.inputs})

			h.readDevice(func(device gfx.Device) {
				live := createFixture(t, device)
				descriptor := renderPipelineDescriptor(live)
				descriptor.Vertex.Buffers[0].Attributes = test.attributes
				before := len(h.instance.Recording().Calls)
				pipeline, err := device.CreateRenderPipeline(descriptor)

				if !errors.Is(err, gfx.ErrVertexMismatch) || pipeline != (gfx.RenderPipeline{}) {
					t.Errorf("the pipeline answered %+v, %v, want the zero handle and %v", pipeline, err, gfx.ErrVertexMismatch)
					return
				}
				for _, name := range test.named {
					if !strings.Contains(err.Error(), name) {
						t.Errorf("the error %q does not name %q", err, name)
					}
				}
				calls := h.instance.Recording().Calls[before:]
				want := []recording.Call{{Kind: recording.CallKindReflectVertexInputs, Arguments: recording.ReflectVertexInputsArguments{Module: live.module, EntryPoint: "vs"}}}
				if !reflect.DeepEqual(calls, want) {
					t.Errorf("the pipeline made calls %+v, want the vertex reflection alone", calls)
				}
			})
		})
	}
}

func TestAVertexLayoutMatchingTheShadersInputsCreatesThePipeline(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)
	h.instance.AnswerNext(recording.CallKindReflectVertexInputs, recording.Answer{VertexInputs: []gfx.VertexInput{
		{Location: 0, Type: gfx.ScalarTypeFloat32, Components: 4},
		{Location: 1, Type: gfx.ScalarTypeFloat16, Components: 2},
		{Location: 2, Type: gfx.ScalarTypeFloat32, Components: 4},
		{Location: 3, Type: gfx.ScalarTypeUint32, Components: 1},
		{Location: 4, Type: gfx.ScalarTypeSint32, Components: 2},
	}})

	h.readDevice(func(device gfx.Device) {
		live := createFixture(t, device)
		descriptor := renderPipelineDescriptor(live)
		descriptor.Vertex.Buffers = []gfx.VertexBufferLayout{
			{ArrayStride: 32, StepMode: gfx.VertexStepModeVertex, Attributes: []gfx.VertexAttribute{
				{Format: gfx.VertexFormatFloat32x3, ShaderLocation: 0},
				{Format: gfx.VertexFormatFloat32x2, Offset: 12, ShaderLocation: 1},
				{Format: gfx.VertexFormatUnorm8x4, Offset: 20, ShaderLocation: 2},
				{Format: gfx.VertexFormatFloat32, Offset: 24, ShaderLocation: 5},
			}},
			{ArrayStride: 12, StepMode: gfx.VertexStepModeInstance, Attributes: []gfx.VertexAttribute{
				{Format: gfx.VertexFormatUint32, ShaderLocation: 3},
				{Format: gfx.VertexFormatSint16x2, Offset: 4, ShaderLocation: 4},
			}},
		}
		before := len(h.instance.Recording().Calls)
		pipeline, err := device.CreateRenderPipeline(descriptor)
		if err != nil {
			t.Errorf("the pipeline answered %v", err)
			return
		}

		want := []recording.Call{
			{Kind: recording.CallKindReflectVertexInputs, Arguments: recording.ReflectVertexInputsArguments{Module: live.module, EntryPoint: "vs"}},
			{Kind: recording.CallKindCreateRenderPipeline, Arguments: recording.CreateRenderPipelineArguments{Pipeline: pipeline, Descriptor: descriptor}},
		}
		if calls := h.instance.Recording().Calls[before:]; !reflect.DeepEqual(calls, want) {
			t.Errorf("the pipeline made calls %+v, want %+v", calls, want)
		}
	})
}

func TestALayoutPastTheDevicesLimitsIsAnError(t *testing.T) {
	everyStage := gfx.ShaderStageFlagVertex | gfx.ShaderStageFlagFragment | gfx.ShaderStageFlagCompute
	group := func(entries ...gfx.BindGroupLayoutEntry) gfx.BindGroupLayoutDescriptor {
		return gfx.BindGroupLayoutDescriptor{Entries: entries}
	}
	buffer := func(binding uint32, bindingType gfx.BufferBindingType, dynamic bool) gfx.BindGroupLayoutEntry {
		return gfx.BindGroupLayoutEntry{Binding: binding, Visibility: everyStage, Buffer: gfx.BufferBindingLayout{Type: bindingType, HasDynamicOffset: dynamic}}
	}
	sampler := gfx.BindGroupLayoutEntry{Binding: 1, Visibility: everyStage, Sampler: gfx.SamplerBindingLayout{Type: gfx.SamplerBindingTypeFiltering}}
	texture := gfx.BindGroupLayoutEntry{Binding: 1, Visibility: everyStage, Texture: gfx.TextureBindingLayout{SampleType: gfx.TextureSampleTypeFloat, ViewDimension: gfx.TextureViewDimension2D}}
	storageTexture := gfx.BindGroupLayoutEntry{Binding: 1, Visibility: everyStage, StorageTexture: gfx.StorageTextureBindingLayout{Access: gfx.StorageTextureAccessWriteOnly, Format: gfx.TextureFormatRGBA8Unorm, ViewDimension: gfx.TextureViewDimension2D}}
	attribute := func(location uint32) gfx.VertexAttribute {
		return gfx.VertexAttribute{Format: gfx.VertexFormatFloat32, ShaderLocation: location}
	}
	vertexBuffer := func(stride uint64, attributes ...gfx.VertexAttribute) gfx.VertexBufferLayout {
		return gfx.VertexBufferLayout{ArrayStride: stride, StepMode: gfx.VertexStepModeVertex, Attributes: attributes}
	}

	for _, test := range []struct {
		name  string
		limit string
		set   func(limits *gfx.Limits, value uint32)
		used  uint32
		// groups are the layout reflected for the pipeline, and the groups of
		// an explicit pipeline layout; nil keeps the fixture's explicit
		// layout.
		groups []gfx.BindGroupLayoutDescriptor
		// buffers replace a render pipeline's vertex buffers; a case with
		// buffers is a render pipeline's alone.
		buffers []gfx.VertexBufferLayout
	}{
		{name: "a reflected layout's groups", limit: "MaxBindGroups",
			set: func(limits *gfx.Limits, value uint32) { limits.MaxBindGroups = value }, used: 2,
			groups: []gfx.BindGroupLayoutDescriptor{group(), group()}},
		{name: "a binding number", limit: "MaxBindingsPerBindGroup",
			set: func(limits *gfx.Limits, value uint32) { limits.MaxBindingsPerBindGroup = value }, used: 5,
			groups: []gfx.BindGroupLayoutDescriptor{group(buffer(4, gfx.BufferBindingTypeUniform, false))}},
		{name: "uniform buffers in a stage, across groups", limit: "MaxUniformBuffersPerShaderStage",
			set: func(limits *gfx.Limits, value uint32) { limits.MaxUniformBuffersPerShaderStage = value }, used: 2,
			groups: []gfx.BindGroupLayoutDescriptor{group(buffer(0, gfx.BufferBindingTypeUniform, false)), group(buffer(0, gfx.BufferBindingTypeUniform, false))}},
		{name: "uniform buffers each in their own stage", limit: "MaxUniformBuffersPerShaderStage",
			set: func(limits *gfx.Limits, value uint32) { limits.MaxUniformBuffersPerShaderStage = value }, used: 2,
			groups: []gfx.BindGroupLayoutDescriptor{group(
				gfx.BindGroupLayoutEntry{Binding: 0, Visibility: gfx.ShaderStageFlagVertex | gfx.ShaderStageFlagCompute, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform}},
				gfx.BindGroupLayoutEntry{Binding: 1, Visibility: gfx.ShaderStageFlagVertex | gfx.ShaderStageFlagCompute, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform}},
				gfx.BindGroupLayoutEntry{Binding: 2, Visibility: gfx.ShaderStageFlagFragment, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform}},
			)}},
		{name: "storage buffers in a stage", limit: "MaxStorageBuffersPerShaderStage",
			set: func(limits *gfx.Limits, value uint32) { limits.MaxStorageBuffersPerShaderStage = value }, used: 2,
			groups: []gfx.BindGroupLayoutDescriptor{group(buffer(0, gfx.BufferBindingTypeStorage, false), buffer(1, gfx.BufferBindingTypeReadOnlyStorage, false))}},
		{name: "samplers in a stage", limit: "MaxSamplersPerShaderStage",
			set: func(limits *gfx.Limits, value uint32) { limits.MaxSamplersPerShaderStage = value }, used: 1,
			groups: []gfx.BindGroupLayoutDescriptor{group(sampler)}},
		{name: "sampled textures in a stage", limit: "MaxSampledTexturesPerShaderStage",
			set: func(limits *gfx.Limits, value uint32) { limits.MaxSampledTexturesPerShaderStage = value }, used: 1,
			groups: []gfx.BindGroupLayoutDescriptor{group(texture)}},
		{name: "storage textures in a stage", limit: "MaxStorageTexturesPerShaderStage",
			set: func(limits *gfx.Limits, value uint32) { limits.MaxStorageTexturesPerShaderStage = value }, used: 1,
			groups: []gfx.BindGroupLayoutDescriptor{group(storageTexture)}},
		{name: "dynamic uniform buffers", limit: "MaxDynamicUniformBuffersPerPipelineLayout",
			set: func(limits *gfx.Limits, value uint32) { limits.MaxDynamicUniformBuffersPerPipelineLayout = value }, used: 2,
			groups: []gfx.BindGroupLayoutDescriptor{group(buffer(0, gfx.BufferBindingTypeUniform, true), buffer(1, gfx.BufferBindingTypeUniform, true), buffer(2, gfx.BufferBindingTypeUniform, false))}},
		{name: "dynamic storage buffers", limit: "MaxDynamicStorageBuffersPerPipelineLayout",
			set: func(limits *gfx.Limits, value uint32) { limits.MaxDynamicStorageBuffersPerPipelineLayout = value }, used: 1,
			groups: []gfx.BindGroupLayoutDescriptor{group(buffer(0, gfx.BufferBindingTypeReadOnlyStorage, true), buffer(1, gfx.BufferBindingTypeStorage, false))}},
		{name: "vertex buffers", limit: "MaxVertexBuffers",
			set: func(limits *gfx.Limits, value uint32) { limits.MaxVertexBuffers = value }, used: 2,
			buffers: []gfx.VertexBufferLayout{vertexBuffer(4, attribute(0)), vertexBuffer(4, attribute(1))}},
		{name: "vertex attributes counted across buffers, their locations within", limit: "MaxVertexAttributes",
			set: func(limits *gfx.Limits, value uint32) { limits.MaxVertexAttributes = value }, used: 3,
			buffers: []gfx.VertexBufferLayout{vertexBuffer(8, attribute(0), attribute(1)), vertexBuffer(4, attribute(0))}},
		{name: "a vertex attribute's location", limit: "MaxVertexAttributes",
			set: func(limits *gfx.Limits, value uint32) { limits.MaxVertexAttributes = value }, used: 5,
			buffers: []gfx.VertexBufferLayout{vertexBuffer(4, attribute(4))}},
		{name: "a vertex buffer's stride", limit: "MaxVertexBufferArrayStride",
			set: func(limits *gfx.Limits, value uint32) { limits.MaxVertexBufferArrayStride = value }, used: 12,
			buffers: []gfx.VertexBufferLayout{vertexBuffer(4, attribute(0)), vertexBuffer(12, attribute(1))}},
		{name: "groups and vertex buffers together", limit: "MaxBindGroupsPlusVertexBuffers",
			set: func(limits *gfx.Limits, value uint32) { limits.MaxBindGroupsPlusVertexBuffers = value }, used: 4,
			groups:  []gfx.BindGroupLayoutDescriptor{group(), group()},
			buffers: []gfx.VertexBufferLayout{vertexBuffer(4, attribute(0)), vertexBuffer(4, attribute(1))}},
	} {
		// creations are the ways the case makes what the limit refuses: each
		// kind of pipeline, and, for a layout with no vertex buffers, a
		// pipeline layout of it, which CreatePipelineLayout checks as WebGPU's
		// createPipelineLayout does. Past the limit a creation makes only the
		// calls of madeKinds.
		type creation struct {
			name      string
			reflect   recording.CallKind
			create    func(device gfx.Device, live fixture) (any, error)
			madeKinds []recording.CallKind
		}
		var creations []creation
		for _, kind := range pipelineKinds {
			if test.buffers != nil && kind.name != "render" {
				continue
			}
			creations = append(creations, creation{name: kind.name + " pipeline", reflect: kind.reflect, madeKinds: []recording.CallKind{recording.CallKindReflectVertexInputs, kind.reflect},
				create: func(device gfx.Device, live fixture) (any, error) {
					if test.buffers == nil {
						_, pipeline, err := kind.createPipeline(device, live, gfx.PipelineLayout{})
						return pipeline, err
					}
					descriptor := renderPipelineDescriptor(live)
					descriptor.Layout = gfx.PipelineLayout{}
					if test.groups == nil {
						descriptor.Layout = live.pipelineLayout
					}
					descriptor.Vertex.Buffers = test.buffers
					return device.CreateRenderPipeline(descriptor)
				}})
		}
		if test.groups != nil && test.buffers == nil {
			creations = append(creations, creation{name: "explicit pipeline layout", madeKinds: []recording.CallKind{recording.CallKindCreateBindGroupLayout},
				create: func(device gfx.Device, _ fixture) (any, error) {
					var groupLayouts []gfx.BindGroupLayout
					for _, group := range test.groups {
						groupLayout, err := device.CreateBindGroupLayout(group)
						if err != nil {
							return nil, err
						}
						groupLayouts = append(groupLayouts, groupLayout)
					}
					return device.CreatePipelineLayout(gfx.PipelineLayoutDescriptor{BindGroupLayouts: groupLayouts})
				}})
		}
		for _, creation := range creations {
			for _, past := range []bool{false, true} {
				value := test.used
				name := creation.name + ", " + test.name + " at " + test.limit
				if past {
					value--
					name = creation.name + ", " + test.name + " past " + test.limit
				}
				t.Run(name, func(t *testing.T) {
					h := newHarness(t)
					limits := gfx.DefaultLimits()
					test.set(&limits, value)
					h.mustSpawn(&gfx.Config{RequiredLimits: limits})
					if test.groups != nil && creation.reflect != 0 {
						h.instance.AnswerNext(creation.reflect, recording.Answer{BindGroupLayouts: test.groups})
					}

					h.readDevice(func(device gfx.Device) {
						live := createFixture(t, device)
						before := len(h.instance.Recording().Calls)
						createdObject, err := creation.create(device, live)

						if !past {
							if err != nil {
								t.Errorf("at the limit the %s answered %v", creation.name, err)
							}
							return
						}
						if !errors.Is(err, gfx.ErrLimitExceeded) || !reflect.ValueOf(createdObject).IsZero() {
							t.Errorf("the %s answered %+v, %v, want the zero handle and %v", creation.name, createdObject, err, gfx.ErrLimitExceeded)
							return
						}
						if !strings.Contains(err.Error(), test.limit) {
							t.Errorf("the error %q does not name %s", err, test.limit)
						}
						for _, call := range h.instance.Recording().Calls[before:] {
							if !slices.Contains(creation.madeKinds, call.Kind) {
								t.Errorf("past the limit the %s made %+v, want only %v", creation.name, call, creation.madeKinds)
							}
						}
					})
				})
			}
		}
	}
}
