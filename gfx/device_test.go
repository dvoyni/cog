package gfx_test

import (
	"errors"
	"reflect"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
)

func TestCreateBufferIsRecordedAndAnswersItsDescriptor(t *testing.T) {
	descriptor := gfx.BufferDescriptor{Label: "vertices", Size: 256, Usage: gfx.BufferUsageFlagVertex | gfx.BufferUsageFlagCopyDst}
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		buffer, err := device.CreateBuffer(descriptor)
		if err != nil {
			t.Fatalf("CreateBuffer answered %v", err)
		}
		if buffer == (gfx.Buffer{}) {
			t.Fatal("CreateBuffer answered the zero handle")
		}
		if got := device.BufferDescriptor(buffer); got != descriptor {
			t.Errorf("BufferDescriptor = %+v, want %+v", got, descriptor)
		}
		call := lastCall(h)
		want := recording.Call{Kind: recording.CallKindCreateBuffer, Arguments: recording.CreateBufferArguments{Buffer: buffer, Descriptor: descriptor}}
		if call != want {
			t.Errorf("last call = %+v, want %+v", call, want)
		}
	})
}

func TestReleaseBufferIsRecordedAndStalesTheHandle(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		buffer, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 16})
		if err != nil {
			t.Fatal(err)
		}
		device.ReleaseBuffer(buffer)

		want := recording.Call{Kind: recording.CallKindReleaseBuffer, Arguments: buffer}
		if call := lastCall(h); call != want {
			t.Errorf("last call = %+v, want %+v", call, want)
		}
		if got := device.BufferDescriptor(buffer); got != (gfx.BufferDescriptor{}) {
			t.Errorf("BufferDescriptor of a released buffer = %+v, want the zero descriptor", got)
		}
	})
}

func TestSecondReleaseBufferLogsErrReleasedAndDoesNothing(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		buffer, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 16})
		if err != nil {
			t.Fatal(err)
		}
		device.ReleaseBuffer(buffer)
		before := len(h.instance.Recording().Calls)
		device.ReleaseBuffer(buffer)

		if after := len(h.instance.Recording().Calls); after != before {
			t.Errorf("the second release reached the backend: %v", h.instance.Recording().Calls[before:])
		}
	})
	if logged := h.takeLogged(); len(logged) != 1 || !errors.Is(logged[0], gfx.ErrReleased) {
		t.Fatalf("logged %v, want one ErrReleased", logged)
	}
}

func TestAReleasedIndexIsCreatedAgainAfterItsRelease(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		first, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 16})
		if err != nil {
			t.Fatal(err)
		}
		device.ReleaseBuffer(first)
		second, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 32})
		if err != nil {
			t.Fatalf("creating at the released index answered %v", err)
		}
		if second.Index() != first.Index() || second == first {
			t.Errorf("second = %+v, want index %d under a new generation than %+v", second, first.Index(), first)
		}
		if got := device.BufferDescriptor(first); got != (gfx.BufferDescriptor{}) {
			t.Errorf("the first handle answers %+v, want the zero descriptor", got)
		}
	})
	for _, call := range h.instance.Recording().Calls {
		if call.Error != nil {
			t.Errorf("%v answered %v", call.Kind, call.Error)
		}
	}
}

func TestCreateBufferAnswersTheBackendsError(t *testing.T) {
	outOfMemory := errors.New("out of memory")
	h := newHarness(t)
	h.mustSpawn(nil)
	h.instance.AnswerNext(recording.CallKindCreateBuffer, recording.Answer{Error: outOfMemory})

	h.readDevice(func(device gfx.Device) {
		buffer, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 16})
		if !errors.Is(err, outOfMemory) || buffer != (gfx.Buffer{}) {
			t.Fatalf("CreateBuffer answered %+v, %v, want the zero handle and %v", buffer, err, outOfMemory)
		}
		created, err := device.CreateBuffer(gfx.BufferDescriptor{Size: 16})
		if err != nil {
			t.Fatal(err)
		}
		if created.Index() != 0 {
			t.Errorf("the next buffer is at index %d, want the failed one's 0", created.Index())
		}
	})
}

func TestCreateShaderModuleAnswersTheBackendsErrorForInvalidWGSL(t *testing.T) {
	invalid := errors.New("invalid WGSL")
	h := newHarness(t)
	h.mustSpawn(nil)
	h.instance.AnswerNext(recording.CallKindCreateShaderModule, recording.Answer{Error: invalid})

	h.readDevice(func(device gfx.Device) {
		module, err := device.CreateShaderModule(gfx.ShaderModuleDescriptor{Label: "broken", WGSL: "@vertex fn vs( {"})
		if !errors.Is(err, invalid) || module != (gfx.ShaderModule{}) {
			t.Errorf("CreateShaderModule answered %+v, %v, want the zero handle and %v", module, err, invalid)
			return
		}
		created, err := device.CreateShaderModule(gfx.ShaderModuleDescriptor{WGSL: triangleWGSL})
		mustSucceed(t, err)
		if created.Index() != 0 {
			t.Errorf("the next module is at index %d, want the refused one's 0", created.Index())
		}
	})
}

func TestPreprocessShaderReachesTheBackendAndAnswersItsText(t *testing.T) {
	source := "//#include \"common.wgsl\"\n" + triangleWGSL
	files := fstest.MapFS{"shaders/lit.wgsl": {Data: []byte(source)}}
	defines := []gfx.ShaderDefine{{Name: "SKIN"}, {Name: "N", Value: "8"}}
	h := newHarness(t)
	h.mustSpawn(nil)
	callCount := len(h.instance.Recording().Calls)

	h.readDevice(func(device gfx.Device) {
		text, err := device.PreprocessShader(files, "shaders/lit.wgsl", defines)
		mustSucceed(t, err)
		if text != source {
			t.Errorf("PreprocessShader answered %q, want the recording adapter's %q", text, source)
		}
	})

	want := []recording.Call{{
		Kind:      recording.CallKindPreprocessShader,
		Arguments: recording.PreprocessShaderArguments{Path: "shaders/lit.wgsl", Defines: defines},
	}}
	if calls := h.instance.Recording().Calls[callCount:]; !reflect.DeepEqual(calls, want) {
		t.Errorf("recorded %+v, want %+v", calls, want)
	}
}

func TestTextureIsCreatedAndReleasedWithItsDescriptor(t *testing.T) {
	descriptor := gfx.TextureDescriptor{
		Label:         "albedo",
		Size:          gfx.Extent3D{Width: 64, Height: 64, DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     gfx.TextureDimension2D,
		Format:        gfx.TextureFormatRGBA8Unorm,
		Usage:         gfx.TextureUsageFlagTextureBinding,
		ViewFormats:   []gfx.TextureFormat{gfx.TextureFormatRGBA8UnormSrgb},
	}
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		texture, err := device.CreateTexture(descriptor)
		if err != nil {
			t.Fatalf("CreateTexture answered %v", err)
		}
		if got := device.TextureDescriptor(texture); !reflect.DeepEqual(got, descriptor) {
			t.Errorf("TextureDescriptor = %+v, want %+v", got, descriptor)
		}
		created := recording.Call{Kind: recording.CallKindCreateTexture, Arguments: recording.CreateTextureArguments{Texture: texture, Descriptor: descriptor}}
		if call := lastCall(h); !reflect.DeepEqual(call, created) {
			t.Errorf("last call = %+v, want %+v", call, created)
		}

		device.ReleaseTexture(texture)
		want := recording.Call{Kind: recording.CallKindReleaseTexture, Arguments: texture}
		if call := lastCall(h); call != want {
			t.Errorf("last call = %+v, want %+v", call, want)
		}
		if got := device.TextureDescriptor(texture); !reflect.DeepEqual(got, gfx.TextureDescriptor{}) {
			t.Errorf("TextureDescriptor of a released texture = %+v, want the zero descriptor", got)
		}
		device.ReleaseTexture(texture)
	})
	if logged := h.takeLogged(); len(logged) != 1 || !errors.Is(logged[0], gfx.ErrReleased) {
		t.Fatalf("logged %v, want one ErrReleased", logged)
	}
}

func TestTextureDescriptorsViewFormatsAreCopied(t *testing.T) {
	viewFormats := []gfx.TextureFormat{gfx.TextureFormatRGBA8UnormSrgb}
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		texture, err := device.CreateTexture(gfx.TextureDescriptor{
			Size:          gfx.Extent3D{Width: 4, Height: 4, DepthOrArrayLayers: 1},
			MipLevelCount: 1,
			SampleCount:   1,
			Format:        gfx.TextureFormatRGBA8Unorm,
			ViewFormats:   viewFormats,
		})
		if err != nil {
			t.Fatal(err)
		}
		viewFormats[0] = gfx.TextureFormatBGRA8Unorm
		device.TextureDescriptor(texture).ViewFormats[0] = gfx.TextureFormatBGRA8Unorm

		want := []gfx.TextureFormat{gfx.TextureFormatRGBA8UnormSrgb}
		if got := device.TextureDescriptor(texture).ViewFormats; !slices.Equal(got, want) {
			t.Errorf("TextureDescriptor's ViewFormats = %v, want %v", got, want)
		}
		created, _ := lastCall(h).Arguments.(recording.CreateTextureArguments)
		if !slices.Equal(created.Descriptor.ViewFormats, want) {
			t.Errorf("the recorded ViewFormats = %v, want %v", created.Descriptor.ViewFormats, want)
		}
	})
}

func TestEachTypeIsCreatedAndReleasedAndRecorded(t *testing.T) {
	cases := []struct {
		name   string
		create func(t *testing.T, device gfx.Device, live fixture) lifecycle
	}{
		{"texture view", func(t *testing.T, device gfx.Device, live fixture) lifecycle {
			descriptor := gfx.TextureViewDescriptor{Label: "albedo", Dimension: gfx.TextureViewDimension2D, MipLevelCount: 1, ArrayLayerCount: 1}
			view, err := device.CreateTextureView(live.texture, descriptor)
			mustSucceed(t, err)
			return lifecycle{
				created:  recording.Call{Kind: recording.CallKindCreateTextureView, Arguments: recording.CreateTextureViewArguments{View: view, Texture: live.texture, Descriptor: descriptor}},
				release:  func() { device.ReleaseTextureView(view) },
				released: recording.Call{Kind: recording.CallKindReleaseTextureView, Arguments: view},
			}
		}},
		{"sampler", func(t *testing.T, device gfx.Device, live fixture) lifecycle {
			descriptor := gfx.SamplerDescriptor{Label: "linear", MagFilter: gfx.FilterModeLinear, AddressModeU: gfx.AddressModeRepeat}
			sampler, err := device.CreateSampler(descriptor)
			mustSucceed(t, err)
			return lifecycle{
				created:  recording.Call{Kind: recording.CallKindCreateSampler, Arguments: recording.CreateSamplerArguments{Sampler: sampler, Descriptor: descriptor}},
				release:  func() { device.ReleaseSampler(sampler) },
				released: recording.Call{Kind: recording.CallKindReleaseSampler, Arguments: sampler},
			}
		}},
		{"shader module", func(t *testing.T, device gfx.Device, live fixture) lifecycle {
			descriptor := gfx.ShaderModuleDescriptor{Label: "triangle", WGSL: triangleWGSL}
			module, err := device.CreateShaderModule(descriptor)
			mustSucceed(t, err)
			return lifecycle{
				created:  recording.Call{Kind: recording.CallKindCreateShaderModule, Arguments: recording.CreateShaderModuleArguments{Module: module, Descriptor: descriptor}},
				release:  func() { device.ReleaseShaderModule(module) },
				released: recording.Call{Kind: recording.CallKindReleaseShaderModule, Arguments: module},
			}
		}},
		{"bind group layout", func(t *testing.T, device gfx.Device, live fixture) lifecycle {
			descriptor := uniformLayoutDescriptor()
			layout, err := device.CreateBindGroupLayout(descriptor)
			mustSucceed(t, err)
			return lifecycle{
				created:  recording.Call{Kind: recording.CallKindCreateBindGroupLayout, Arguments: recording.CreateBindGroupLayoutArguments{Layout: layout, Descriptor: descriptor}},
				release:  func() { device.ReleaseBindGroupLayout(layout) },
				released: recording.Call{Kind: recording.CallKindReleaseBindGroupLayout, Arguments: layout},
			}
		}},
		{"bind group", func(t *testing.T, device gfx.Device, live fixture) lifecycle {
			descriptor := gfx.BindGroupDescriptor{Label: "camera", Layout: live.groupLayout, Entries: []gfx.BindGroupEntry{{Binding: 0, Buffer: live.buffer}}}
			group, err := device.CreateBindGroup(descriptor)
			mustSucceed(t, err)
			return lifecycle{
				created:  recording.Call{Kind: recording.CallKindCreateBindGroup, Arguments: recording.CreateBindGroupArguments{Group: group, Descriptor: descriptor}},
				release:  func() { device.ReleaseBindGroup(group) },
				released: recording.Call{Kind: recording.CallKindReleaseBindGroup, Arguments: group},
			}
		}},
		{"pipeline layout", func(t *testing.T, device gfx.Device, live fixture) lifecycle {
			descriptor := gfx.PipelineLayoutDescriptor{Label: "main", BindGroupLayouts: []gfx.BindGroupLayout{live.groupLayout}}
			layout, err := device.CreatePipelineLayout(descriptor)
			mustSucceed(t, err)
			return lifecycle{
				created:  recording.Call{Kind: recording.CallKindCreatePipelineLayout, Arguments: recording.CreatePipelineLayoutArguments{Layout: layout, Descriptor: descriptor}},
				release:  func() { device.ReleasePipelineLayout(layout) },
				released: recording.Call{Kind: recording.CallKindReleasePipelineLayout, Arguments: layout},
			}
		}},
		{"render pipeline", func(t *testing.T, device gfx.Device, live fixture) lifecycle {
			descriptor := renderPipelineDescriptor(live)
			pipeline, err := device.CreateRenderPipeline(descriptor)
			mustSucceed(t, err)
			return lifecycle{
				created:  recording.Call{Kind: recording.CallKindCreateRenderPipeline, Arguments: recording.CreateRenderPipelineArguments{Pipeline: pipeline, Descriptor: descriptor}},
				release:  func() { device.ReleaseRenderPipeline(pipeline) },
				released: recording.Call{Kind: recording.CallKindReleaseRenderPipeline, Arguments: pipeline},
			}
		}},
		{"compute pipeline", func(t *testing.T, device gfx.Device, live fixture) lifecycle {
			descriptor := computePipelineDescriptor(live)
			pipeline, err := device.CreateComputePipeline(descriptor)
			mustSucceed(t, err)
			return lifecycle{
				created:  recording.Call{Kind: recording.CallKindCreateComputePipeline, Arguments: recording.CreateComputePipelineArguments{Pipeline: pipeline, Descriptor: descriptor}},
				release:  func() { device.ReleaseComputePipeline(pipeline) },
				released: recording.Call{Kind: recording.CallKindReleaseComputePipeline, Arguments: pipeline},
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustSpawn(nil)

			h.readDevice(func(device gfx.Device) {
				resources := c.create(t, device, createFixture(t, device))
				if reflect.ValueOf(resources.released.Arguments).IsZero() {
					t.Error("the create answered the zero handle")
				}
				if call := lastCall(h); !reflect.DeepEqual(call, resources.created) {
					t.Errorf("last call = %+v, want %+v", call, resources.created)
				}
				resources.release()
				if call := lastCall(h); call != resources.released {
					t.Errorf("last call = %+v, want %+v", call, resources.released)
				}
				before := len(h.instance.Recording().Calls)
				resources.release()
				if after := len(h.instance.Recording().Calls); after != before {
					t.Errorf("the second release reached the backend: %v", h.instance.Recording().Calls[before:])
				}
			})
			if logged := h.takeLogged(); len(logged) != 1 || !errors.Is(logged[0], gfx.ErrReleased) {
				t.Fatalf("logged %v, want one ErrReleased", logged)
			}
		})
	}
}

func TestTextureViewTextureAnswersTheViewsTexture(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		live := createFixture(t, device)
		if got := device.TextureViewTexture(live.view); got != live.texture {
			t.Errorf("TextureViewTexture = %+v, want %+v", got, live.texture)
		}
		device.ReleaseTextureView(live.view)
		if got := device.TextureViewTexture(live.view); got != (gfx.Texture{}) {
			t.Errorf("TextureViewTexture of a released view = %+v, want the zero texture", got)
		}
	})
}

func TestStaleHandlesInDescriptorsAnswerErrReleasedBeforeTheBackend(t *testing.T) {
	cases := []struct {
		name   string
		create func(device gfx.Device, live, stale fixture) error
	}{
		{"a view's texture", func(device gfx.Device, live, stale fixture) error {
			_, err := device.CreateTextureView(stale.texture, gfx.TextureViewDescriptor{})
			return err
		}},
		{"a view of the zero texture", func(device gfx.Device, live, stale fixture) error {
			_, err := device.CreateTextureView(gfx.Texture{}, gfx.TextureViewDescriptor{})
			return err
		}},
		{"a bind group's layout", func(device gfx.Device, live, stale fixture) error {
			_, err := device.CreateBindGroup(gfx.BindGroupDescriptor{Layout: stale.groupLayout, Entries: []gfx.BindGroupEntry{{Buffer: live.buffer}}})
			return err
		}},
		{"a bind group's buffer", func(device gfx.Device, live, stale fixture) error {
			_, err := device.CreateBindGroup(gfx.BindGroupDescriptor{Layout: live.groupLayout, Entries: []gfx.BindGroupEntry{{Binding: 1, Sampler: live.sampler}, {Buffer: stale.buffer}}})
			return err
		}},
		{"a bind group's sampler", func(device gfx.Device, live, stale fixture) error {
			_, err := device.CreateBindGroup(gfx.BindGroupDescriptor{Layout: live.groupLayout, Entries: []gfx.BindGroupEntry{{Sampler: stale.sampler}}})
			return err
		}},
		{"a bind group's texture view", func(device gfx.Device, live, stale fixture) error {
			_, err := device.CreateBindGroup(gfx.BindGroupDescriptor{Layout: live.groupLayout, Entries: []gfx.BindGroupEntry{{TextureView: stale.view}}})
			return err
		}},
		{"a pipeline layout's bind group layout", func(device gfx.Device, live, stale fixture) error {
			_, err := device.CreatePipelineLayout(gfx.PipelineLayoutDescriptor{BindGroupLayouts: []gfx.BindGroupLayout{live.groupLayout, stale.groupLayout}})
			return err
		}},
		{"a render pipeline's layout", func(device gfx.Device, live, stale fixture) error {
			descriptor := renderPipelineDescriptor(live)
			descriptor.Layout = stale.pipelineLayout
			_, err := device.CreateRenderPipeline(descriptor)
			return err
		}},
		{"a render pipeline's vertex module", func(device gfx.Device, live, stale fixture) error {
			descriptor := renderPipelineDescriptor(live)
			descriptor.Vertex.Module = stale.module
			_, err := device.CreateRenderPipeline(descriptor)
			return err
		}},
		{"a render pipeline's fragment module", func(device gfx.Device, live, stale fixture) error {
			descriptor := renderPipelineDescriptor(live)
			descriptor.Fragment.Module = stale.module
			_, err := device.CreateRenderPipeline(descriptor)
			return err
		}},
		{"an auto render pipeline's vertex module", func(device gfx.Device, live, stale fixture) error {
			descriptor := renderPipelineDescriptor(live)
			descriptor.Layout = gfx.PipelineLayout{}
			descriptor.Vertex.Module = stale.module
			_, err := device.CreateRenderPipeline(descriptor)
			return err
		}},
		{"an auto compute pipeline's module", func(device gfx.Device, live, stale fixture) error {
			descriptor := computePipelineDescriptor(live)
			descriptor.Layout = gfx.PipelineLayout{}
			descriptor.Module = stale.module
			_, err := device.CreateComputePipeline(descriptor)
			return err
		}},
		{"a compute pipeline's layout", func(device gfx.Device, live, stale fixture) error {
			descriptor := computePipelineDescriptor(live)
			descriptor.Layout = stale.pipelineLayout
			_, err := device.CreateComputePipeline(descriptor)
			return err
		}},
		{"a compute pipeline's module", func(device gfx.Device, live, stale fixture) error {
			descriptor := computePipelineDescriptor(live)
			descriptor.Module = stale.module
			_, err := device.CreateComputePipeline(descriptor)
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustSpawn(nil)

			h.readDevice(func(device gfx.Device) {
				stale := createFixture(t, device)
				releaseFixture(device, stale)
				live := createFixture(t, device)
				before := len(h.instance.Recording().Calls)

				if err := c.create(device, live, stale); !errors.Is(err, gfx.ErrReleased) {
					t.Errorf("the create answered %v, want ErrReleased", err)
				}
				if after := len(h.instance.Recording().Calls); after != before {
					t.Errorf("the create reached the backend: %v", h.instance.Recording().Calls[before:])
				}
			})
		})
	}
}

func TestOptionalZeroHandlesInDescriptorsAreNotChecked(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		live := createFixture(t, device)
		descriptor := renderPipelineDescriptor(live)
		descriptor.Fragment = gfx.FragmentState{}
		if _, err := device.CreateRenderPipeline(descriptor); err != nil {
			t.Errorf("a render pipeline with no fragment stage answered %v", err)
		}
		if _, err := device.CreateBindGroup(gfx.BindGroupDescriptor{Layout: live.groupLayout, Entries: []gfx.BindGroupEntry{{Buffer: live.buffer}}}); err != nil {
			t.Errorf("a bind group of a buffer alone answered %v", err)
		}
	})
}

func TestDescriptorSlicesAreCopied(t *testing.T) {
	cases := []struct {
		name string
		// create creates an object from build's descriptor, then changes
		// that descriptor's slices and maps, and answers build's again.
		create func(t *testing.T, device gfx.Device, live fixture) any
	}{
		{"bind group layout", func(t *testing.T, device gfx.Device, live fixture) any {
			descriptor := uniformLayoutDescriptor()
			_, err := device.CreateBindGroupLayout(descriptor)
			mustSucceed(t, err)
			descriptor.Entries[0].Binding = 9
			return uniformLayoutDescriptor()
		}},
		{"bind group", func(t *testing.T, device gfx.Device, live fixture) any {
			build := func() gfx.BindGroupDescriptor {
				return gfx.BindGroupDescriptor{Layout: live.groupLayout, Entries: []gfx.BindGroupEntry{{Buffer: live.buffer}}}
			}
			descriptor := build()
			_, err := device.CreateBindGroup(descriptor)
			mustSucceed(t, err)
			descriptor.Entries[0].Binding = 9
			return build()
		}},
		{"pipeline layout", func(t *testing.T, device gfx.Device, live fixture) any {
			build := func() gfx.PipelineLayoutDescriptor {
				return gfx.PipelineLayoutDescriptor{BindGroupLayouts: []gfx.BindGroupLayout{live.groupLayout}}
			}
			descriptor := build()
			_, err := device.CreatePipelineLayout(descriptor)
			mustSucceed(t, err)
			descriptor.BindGroupLayouts[0] = gfx.BindGroupLayout{}
			return build()
		}},
		{"render pipeline", func(t *testing.T, device gfx.Device, live fixture) any {
			descriptor := renderPipelineDescriptor(live)
			_, err := device.CreateRenderPipeline(descriptor)
			mustSucceed(t, err)
			descriptor.Vertex.Buffers[0].ArrayStride = 4
			descriptor.Vertex.Buffers[0].Attributes[0].ShaderLocation = 9
			descriptor.Fragment.Targets[0].Format = gfx.TextureFormatRGBA8Unorm
			return renderPipelineDescriptor(live)
		}},
		{"compute pipeline", func(t *testing.T, device gfx.Device, live fixture) any {
			descriptor := computePipelineDescriptor(live)
			_, err := device.CreateComputePipeline(descriptor)
			mustSucceed(t, err)
			descriptor.Constants["scale"] = 2
			return computePipelineDescriptor(live)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustSpawn(nil)

			h.readDevice(func(device gfx.Device) {
				want := c.create(t, device, createFixture(t, device))
				got := reflect.ValueOf(lastCall(h).Arguments).FieldByName("Descriptor").Interface()
				if !reflect.DeepEqual(got, want) {
					t.Errorf("the recorded descriptor = %+v, want %+v", got, want)
				}
			})
		})
	}
}

func TestWritesReachTheBackendRecordedWithTheirDataCopied(t *testing.T) {
	h := newHarness(t)
	h.mustSpawn(nil)

	h.readDevice(func(device gfx.Device) {
		live := createFixture(t, device)
		data := []byte{1, 2, 3, 4, 5, 6, 7, 8}
		destination := gfx.ImageCopyTexture{Texture: live.texture, Origin: gfx.Origin3D{X: 1}}
		layout := gfx.ImageDataLayout{BytesPerRow: 8, RowsPerImage: 1}
		size := gfx.Extent3D{Width: 2, Height: 1, DepthOrArrayLayers: 1}

		mustSucceed(t, device.WriteBuffer(live.buffer, 16, data))
		data[0] = 9
		wantBuffer := recording.Call{Kind: recording.CallKindWriteBuffer, Arguments: recording.WriteBufferArguments{
			Buffer: live.buffer,
			Offset: 16,
			Data:   []byte{1, 2, 3, 4, 5, 6, 7, 8},
		}}
		if call := lastCall(h); !reflect.DeepEqual(call, wantBuffer) {
			t.Errorf("WriteBuffer recorded %+v, want %+v", call, wantBuffer)
		}

		mustSucceed(t, device.WriteTexture(destination, data, layout, size))
		data[0] = 1
		wantTexture := recording.Call{Kind: recording.CallKindWriteTexture, Arguments: recording.WriteTextureArguments{
			Destination: destination,
			Data:        []byte{9, 2, 3, 4, 5, 6, 7, 8},
			Layout:      layout,
			Size:        size,
		}}
		if call := lastCall(h); !reflect.DeepEqual(call, wantTexture) {
			t.Errorf("WriteTexture recorded %+v, want %+v", call, wantTexture)
		}
	})
}

func TestAWriteToAStaleHandleAnswersErrReleasedBeforeTheBackend(t *testing.T) {
	for _, c := range []struct {
		name  string
		write func(device gfx.Device, stale fixture) error
	}{
		{"a buffer", func(device gfx.Device, stale fixture) error {
			return device.WriteBuffer(stale.buffer, 0, []byte{1, 2, 3, 4})
		}},
		{"the zero buffer", func(device gfx.Device, _ fixture) error {
			return device.WriteBuffer(gfx.Buffer{}, 0, []byte{1, 2, 3, 4})
		}},
		{"a texture", func(device gfx.Device, stale fixture) error {
			return device.WriteTexture(gfx.ImageCopyTexture{Texture: stale.texture}, []byte{1, 2, 3, 4}, gfx.ImageDataLayout{BytesPerRow: 4, RowsPerImage: 1}, gfx.Extent3D{Width: 1, Height: 1, DepthOrArrayLayers: 1})
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustSpawn(nil)

			h.readDevice(func(device gfx.Device) {
				stale := createFixture(t, device)
				releaseFixture(device, stale)
				before := len(h.instance.Recording().Calls)

				if err := c.write(device, stale); !errors.Is(err, gfx.ErrReleased) {
					t.Errorf("the write answered %v, want ErrReleased", err)
				}
				if after := len(h.instance.Recording().Calls); after != before {
					t.Errorf("the write reached the backend: %v", h.instance.Recording().Calls[before:])
				}
			})
		})
	}
}

// fixture is one live object of each type a descriptor names.
type fixture struct {
	buffer         gfx.Buffer
	texture        gfx.Texture
	view           gfx.TextureView
	sampler        gfx.Sampler
	module         gfx.ShaderModule
	groupLayout    gfx.BindGroupLayout
	pipelineLayout gfx.PipelineLayout
}

// lifecycle is one object as a case of
// TestEachTypeIsCreatedAndReleasedAndRecorded created it: the call its
// creation should have recorded, its release, and the call that should record.
type lifecycle struct {
	created  recording.Call
	release  func()
	released recording.Call
}

// triangleWGSL is a valid shader module with an entry point of each stage.
const triangleWGSL = "@vertex fn vs() -> @builtin(position) vec4f { return vec4f(); }\n" +
	"@fragment fn fs() -> @location(0) vec4f { return vec4f(1.0); }\n" +
	"@compute @workgroup_size(1) fn cs() {}\n"

// createFixture creates one object of each type a descriptor names, the
// buffer and the texture with every usage recordEveryCommand puts them to.
func createFixture(t *testing.T, device gfx.Device) fixture {
	t.Helper()
	var live fixture
	var err error
	live.buffer, err = device.CreateBuffer(gfx.BufferDescriptor{
		Size: 256,
		Usage: gfx.BufferUsageFlagUniform | gfx.BufferUsageFlagVertex | gfx.BufferUsageFlagIndex |
			gfx.BufferUsageFlagIndirect | gfx.BufferUsageFlagCopySrc | gfx.BufferUsageFlagCopyDst,
	})
	mustSucceed(t, err)
	live.texture, err = device.CreateTexture(gfx.TextureDescriptor{
		Size:          gfx.Extent3D{Width: 4, Height: 4, DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     gfx.TextureDimension2D,
		Format:        gfx.TextureFormatRGBA8Unorm,
		Usage: gfx.TextureUsageFlagTextureBinding | gfx.TextureUsageFlagRenderAttachment |
			gfx.TextureUsageFlagCopySrc | gfx.TextureUsageFlagCopyDst,
	})
	mustSucceed(t, err)
	live.view, err = device.CreateTextureView(live.texture, gfx.TextureViewDescriptor{})
	mustSucceed(t, err)
	live.sampler, err = device.CreateSampler(gfx.SamplerDescriptor{})
	mustSucceed(t, err)
	live.module, err = device.CreateShaderModule(gfx.ShaderModuleDescriptor{WGSL: triangleWGSL})
	mustSucceed(t, err)
	live.groupLayout, err = device.CreateBindGroupLayout(uniformLayoutDescriptor())
	mustSucceed(t, err)
	live.pipelineLayout, err = device.CreatePipelineLayout(gfx.PipelineLayoutDescriptor{BindGroupLayouts: []gfx.BindGroupLayout{live.groupLayout}})
	mustSucceed(t, err)
	return live
}

// releaseFixture releases every object of f, leaving its handles stale.
func releaseFixture(device gfx.Device, f fixture) {
	device.ReleasePipelineLayout(f.pipelineLayout)
	device.ReleaseBindGroupLayout(f.groupLayout)
	device.ReleaseShaderModule(f.module)
	device.ReleaseSampler(f.sampler)
	device.ReleaseTextureView(f.view)
	device.ReleaseTexture(f.texture)
	device.ReleaseBuffer(f.buffer)
}

// uniformLayoutDescriptor answers a layout of one uniform buffer, seen by the
// vertex stage.
func uniformLayoutDescriptor() gfx.BindGroupLayoutDescriptor {
	return gfx.BindGroupLayoutDescriptor{
		Label: "camera",
		Entries: []gfx.BindGroupLayoutEntry{
			{Binding: 0, Visibility: gfx.ShaderStageFlagVertex, Buffer: gfx.BufferBindingLayout{Type: gfx.BufferBindingTypeUniform}},
		},
	}
}

// renderPipelineDescriptor answers a pipeline of live's module and layout,
// with one vertex buffer and one color target.
func renderPipelineDescriptor(live fixture) gfx.RenderPipelineDescriptor {
	return gfx.RenderPipelineDescriptor{
		Label:  "triangle",
		Layout: live.pipelineLayout,
		Vertex: gfx.VertexState{
			Module:     live.module,
			EntryPoint: "vs",
			Buffers: []gfx.VertexBufferLayout{{
				ArrayStride: 12,
				StepMode:    gfx.VertexStepModeVertex,
				Attributes:  []gfx.VertexAttribute{{Format: gfx.VertexFormatFloat32x3}},
			}},
		},
		Primitive: gfx.PrimitiveState{Topology: gfx.PrimitiveTopologyTriangleList},
		Fragment: gfx.FragmentState{
			Module:     live.module,
			EntryPoint: "fs",
			Targets:    []gfx.ColorTargetState{{Format: gfx.TextureFormatBGRA8Unorm, WriteMask: gfx.ColorWriteFlagAll}},
		},
	}
}

// computePipelineDescriptor answers a pipeline of live's module and layout,
// with one override constant.
func computePipelineDescriptor(live fixture) gfx.ComputePipelineDescriptor {
	return gfx.ComputePipelineDescriptor{
		Label:      "cull",
		Layout:     live.pipelineLayout,
		Module:     live.module,
		EntryPoint: "cs",
		Constants:  map[string]float64{"scale": 1},
	}
}

// mustSucceed fails t when err is not nil.
func mustSucceed(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// lastCall answers the last call h's instance recorded.
func lastCall(h *harness) recording.Call {
	calls := h.instance.Recording().Calls
	return calls[len(calls)-1]
}
