package recording_test

import (
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/gfx/recording"
	"github.com/dvoyni/cog/platform"
)

func TestACreateOrReflectionNamingAMissingHandleAnswersAnError(t *testing.T) {
	for _, test := range []struct {
		name string
		call func(device gfx.BackendDevice, live, missing objects) error
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
		{"a reflection of a missing vertex module", func(device gfx.BackendDevice, live, missing objects) error {
			descriptor := renderPipelineDescriptor(live)
			descriptor.Layout = gfx.PipelineLayout{}
			descriptor.Vertex.Module = missing.module
			_, err := device.ReflectRenderPipelineLayout(descriptor)
			return err
		}},
		{"a reflection of a missing fragment module", func(device gfx.BackendDevice, live, missing objects) error {
			descriptor := renderPipelineDescriptor(live)
			descriptor.Layout = gfx.PipelineLayout{}
			descriptor.Fragment.Module = missing.module
			_, err := device.ReflectRenderPipelineLayout(descriptor)
			return err
		}},
		{"a reflection of a missing compute module", func(device gfx.BackendDevice, _, missing objects) error {
			_, err := device.ReflectComputePipelineLayout(gfx.ComputePipelineDescriptor{Module: missing.module, EntryPoint: "cs"})
			return err
		}},
		{"a vertex reflection of a missing module", func(device gfx.BackendDevice, _, missing objects) error {
			_, err := device.ReflectVertexInputs(missing.module, "vs")
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance, device := requestDevice(t)
			live := createObjects(t, device, everyBufferUsage, everyTextureUsage)

			if err := test.call(device, live, missingObjects()); err == nil {
				t.Fatal("the call answered no error")
			}
			calls := instance.Recording().Calls
			if last := calls[len(calls)-1]; last.Error == nil {
				t.Errorf("the call was recorded with no error: %+v", last)
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
	if _, err := device.AcquireFramebuffer(window, handleAt[gfx.Texture](5), framebuffer); err != nil {
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

func TestReleasingTheDeviceDropsItsFramebufferAndLeavesItsSurfaceToConfigureAgain(t *testing.T) {
	instance, device := requestDevice(t)
	window := platform.Window{}
	mustSucceed(t, instance.CreateSurface(window, platform.NativeHandle{}))
	mustSucceed(t, device.ConfigureSurface(window, gfx.SurfaceConfiguration{}))
	if _, err := device.AcquireFramebuffer(window, handleAt[gfx.Texture](5), handleAt[gfx.TextureView](5)); err != nil {
		t.Fatalf("AcquireFramebuffer answered %v", err)
	}

	device.Release()
	adapter, err := instance.RequestAdapter(gfx.RequestAdapterOptions{})
	mustSucceed(t, err)
	recovered, err := adapter.RequestDevice(gfx.DeviceDescriptor{})
	mustSucceed(t, err)

	if _, err := recovered.AcquireFramebuffer(window, handleAt[gfx.Texture](6), handleAt[gfx.TextureView](6)); err == nil {
		t.Error("AcquireFramebuffer on a surface configured with the released device answered no error")
	}
	mustSucceed(t, recovered.ConfigureSurface(window, gfx.SurfaceConfiguration{}))
	if _, err := recovered.AcquireFramebuffer(window, handleAt[gfx.Texture](6), handleAt[gfx.TextureView](6)); err != nil {
		t.Errorf("AcquireFramebuffer once configured again answered %v", err)
	}
}

func TestAnAcquiredFramebufferIsACopySourceWhenItsSurfaceIsCapturableUntilItsPresent(t *testing.T) {
	for _, test := range []struct {
		name       string
		capturable bool
	}{
		{"capturable", true},
		{"not capturable", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance, device := requestDevice(t)
			window := platform.Window{}
			framebuffer := handleAt[gfx.Texture](5)
			buffer := createBuffer(t, device, 0, gfx.BufferUsageFlagCopyDst|gfx.BufferUsageFlagMapRead)
			mustSucceed(t, instance.CreateSurface(window, platform.NativeHandle{}))
			mustSucceed(t, device.ConfigureSurface(window, gfx.SurfaceConfiguration{
				Format: gfx.TextureFormatBGRA8Unorm, Width: 1, Height: 1, Capturable: test.capturable,
			}))
			if _, err := device.AcquireFramebuffer(window, framebuffer, handleAt[gfx.TextureView](5)); err != nil {
				t.Fatalf("AcquireFramebuffer answered %v", err)
			}
			regions := []gfx.BufferTextureCopy{{
				BufferLayout: gfx.ImageDataLayout{BytesPerRow: 256, RowsPerImage: 1},
				TextureBase:  gfx.ImageCopyTexture{Texture: framebuffer},
				Size:         gfx.Extent3D{Width: 1, Height: 1, DepthOrArrayLayers: 1},
			}}

			acquired := createEncoder(t, device)
			acquired.CopyTextureToBuffer(framebuffer, buffer, regions)
			if _, err := acquired.Finish(); (err == nil) != test.capturable {
				t.Fatalf("a copy out of the acquired framebuffer answered %v at Finish, want an error only when not capturable", err)
			}
			mustSucceed(t, device.Present(window))
			presented := createEncoder(t, device)
			presented.CopyTextureToBuffer(framebuffer, buffer, regions)
			if _, err := presented.Finish(); err == nil {
				t.Fatal("a copy out of the presented framebuffer answered no error at Finish")
			}
		})
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

func TestACopyOrClearNeedsItsAlignment(t *testing.T) {
	for _, test := range []struct {
		name      string
		alignment uint64
		record    func(encoder gfx.CommandEncoder, live objects, value uint64)
	}{
		{"a buffer copy's source offset", 4, func(encoder gfx.CommandEncoder, live objects, value uint64) {
			encoder.CopyBufferToBuffer(live.buffer, value, live.buffer, 128, 4)
		}},
		{"a buffer copy's destination offset", 4, func(encoder gfx.CommandEncoder, live objects, value uint64) {
			encoder.CopyBufferToBuffer(live.buffer, 0, live.buffer, 128+value, 4)
		}},
		{"a buffer copy's size", 4, func(encoder gfx.CommandEncoder, live objects, value uint64) {
			encoder.CopyBufferToBuffer(live.buffer, 0, live.buffer, 128, value)
		}},
		{"a clear's offset", 4, func(encoder gfx.CommandEncoder, live objects, value uint64) {
			encoder.ClearBuffer(live.buffer, value, 4)
		}},
		{"a clear's size", 4, func(encoder gfx.CommandEncoder, live objects, value uint64) {
			encoder.ClearBuffer(live.buffer, 0, value)
		}},
		{"a buffer-to-texture copy's rows", 256, func(encoder gfx.CommandEncoder, live objects, value uint64) {
			encoder.CopyBufferToTexture(live.buffer, live.texture, regionsWithRowsOf(live, value))
		}},
		{"a texture-to-buffer copy's rows", 256, func(encoder gfx.CommandEncoder, live objects, value uint64) {
			encoder.CopyTextureToBuffer(live.texture, live.buffer, regionsWithRowsOf(live, value))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, value := range []uint64{test.alignment, test.alignment / 2} {
				_, device := requestDevice(t)
				live := createObjects(t, device, everyBufferUsage, everyTextureUsage)
				encoder := createEncoder(t, device)

				test.record(encoder, live, value)

				_, err := encoder.Finish()
				if aligned := value == test.alignment; aligned && err != nil {
					t.Errorf("%d answered %v at Finish", value, err)
				} else if !aligned && err == nil {
					t.Errorf("%d answered no error at Finish", value)
				}
			}
		})
	}
}

func TestAWriteNeedsItsAlignment(t *testing.T) {
	for _, test := range []struct {
		name  string
		write func(device gfx.BackendDevice, live objects, value uint64) error
	}{
		{"its offset", func(device gfx.BackendDevice, live objects, value uint64) error {
			return device.WriteBuffer(live.buffer, value, make([]byte, 4))
		}},
		{"its data's length", func(device gfx.BackendDevice, live objects, value uint64) error {
			return device.WriteBuffer(live.buffer, 0, make([]byte, value))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, value := range []uint64{4, 2} {
				_, device := requestDevice(t)
				live := createObjects(t, device, everyBufferUsage, everyTextureUsage)

				err := test.write(device, live, value)

				if aligned := value == 4; aligned && err != nil {
					t.Errorf("%d answered %v", value, err)
				} else if !aligned && err == nil {
					t.Errorf("%d answered no error", value)
				}
			}
		})
	}
}

func TestAWriteOrMapAnswersAnErrorFromTheCall(t *testing.T) {
	someBytes := []byte{1, 2, 3, 4}
	oneTexel := gfx.Extent3D{Width: 1, Height: 1, DepthOrArrayLayers: 1}
	oneRow := gfx.ImageDataLayout{BytesPerRow: 4, RowsPerImage: 1}
	for _, test := range []struct {
		name string
		// call makes the call: live has every usage, lacking none, missing
		// is at an index holding nothing.
		call func(device gfx.BackendDevice, live, lacking, missing objects) error
	}{
		{"a write to a missing buffer", func(device gfx.BackendDevice, _, _, missing objects) error {
			return device.WriteBuffer(missing.buffer, 0, someBytes)
		}},
		{"a write to a buffer without CopyDst", func(device gfx.BackendDevice, _, lacking, _ objects) error {
			return device.WriteBuffer(lacking.buffer, 0, someBytes)
		}},
		{"a write ending past the buffer", func(device gfx.BackendDevice, live, _, _ objects) error {
			return device.WriteBuffer(live.buffer, 252, make([]byte, 8))
		}},
		{"a write to a missing texture", func(device gfx.BackendDevice, _, _, missing objects) error {
			return device.WriteTexture(gfx.ImageCopyTexture{Texture: missing.texture}, someBytes, oneRow, oneTexel)
		}},
		{"a write to a texture without CopyDst", func(device gfx.BackendDevice, _, lacking, _ objects) error {
			return device.WriteTexture(gfx.ImageCopyTexture{Texture: lacking.texture}, someBytes, oneRow, oneTexel)
		}},
		{"a map of a missing buffer", func(device gfx.BackendDevice, _, _, missing objects) error {
			return device.MapBuffer(missing.buffer, gfx.MapModeRead, 0, 16)
		}},
		{"a read map of a buffer without MapRead", func(device gfx.BackendDevice, _, lacking, _ objects) error {
			return device.MapBuffer(lacking.buffer, gfx.MapModeRead, 0, 16)
		}},
		{"a write map of a buffer without MapWrite", func(device gfx.BackendDevice, _, lacking, _ objects) error {
			return device.MapBuffer(lacking.buffer, gfx.MapModeWrite, 0, 16)
		}},
		{"a map ending past the buffer", func(device gfx.BackendDevice, live, _, _ objects) error {
			return device.MapBuffer(live.buffer, gfx.MapModeRead, 248, 16)
		}},
		{"a map of a mapped buffer", func(device gfx.BackendDevice, live, _, _ objects) error {
			mustSucceed(t, device.MapBuffer(live.buffer, gfx.MapModeRead, 0, 16))
			return device.MapBuffer(live.buffer, gfx.MapModeRead, 16, 16)
		}},
		{"the status of an unmapped buffer", func(device gfx.BackendDevice, live, _, _ objects) error {
			_, err := device.MapStatus(live.buffer)
			return err
		}},
		{"the range of an unmapped buffer", func(device gfx.BackendDevice, live, _, _ objects) error {
			_, err := device.MappedRange(live.buffer, 0, 16)
			return err
		}},
		{"a range before the map", func(device gfx.BackendDevice, live, _, _ objects) error {
			mustSucceed(t, device.MapBuffer(live.buffer, gfx.MapModeRead, 16, 16))
			_, err := device.MappedRange(live.buffer, 8, 16)
			return err
		}},
		{"a range ending past the map", func(device gfx.BackendDevice, live, _, _ objects) error {
			mustSucceed(t, device.MapBuffer(live.buffer, gfx.MapModeRead, 16, 16))
			_, err := device.MappedRange(live.buffer, 16, 32)
			return err
		}},
		{"an unmap of an unmapped buffer", func(device gfx.BackendDevice, live, _, _ objects) error {
			return device.UnmapBuffer(live.buffer)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, device := requestDevice(t)
			live := createObjects(t, device, everyBufferUsage, everyTextureUsage)
			lacking := objects{buffer: createBuffer(t, device, 2, gfx.BufferUsageFlagNone), texture: handleAt[gfx.Texture](2)}
			mustSucceed(t, device.CreateTexture(lacking.texture, gfx.TextureDescriptor{Size: oneTexel, MipLevelCount: 1, SampleCount: 1, Format: gfx.TextureFormatRGBA8Unorm}))

			if err := test.call(device, live, lacking, missingObjects()); err == nil {
				t.Error("the call answered no error")
			}
		})
	}
}

func TestACopyOrClearEndingPastItsBufferFailsAtFinish(t *testing.T) {
	for _, test := range []struct {
		name   string
		record func(encoder gfx.CommandEncoder, live objects)
	}{
		{"a copy's source", func(encoder gfx.CommandEncoder, live objects) {
			encoder.CopyBufferToBuffer(live.buffer, 248, live.buffer, 0, 16)
		}},
		{"a copy's destination", func(encoder gfx.CommandEncoder, live objects) {
			encoder.CopyBufferToBuffer(live.buffer, 0, live.buffer, 248, 16)
		}},
		{"a clear", func(encoder gfx.CommandEncoder, live objects) {
			encoder.ClearBuffer(live.buffer, 248, 16)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, device := requestDevice(t)
			live := createObjects(t, device, everyBufferUsage, everyTextureUsage)
			encoder := createEncoder(t, device)

			test.record(encoder, live)

			if _, err := encoder.Finish(); err == nil {
				t.Error("Finish answered no error")
			}
		})
	}
}

func TestPassesInSequenceDrawingAfterTheirPipelineFinish(t *testing.T) {
	_, device := requestDevice(t)
	live := createObjects(t, device, everyBufferUsage, everyTextureUsage)
	encoder := createEncoder(t, device)

	inRenderPass(encoder, live, func(pass gfx.RenderPassEncoder) {
		pass.SetVertexBuffer(0, live.buffer, 0)
		pass.SetIndexBuffer(live.buffer, gfx.IndexFormatUint16, 0)
		pass.Draw(gfx.DrawArgs{VertexCount: 3, InstanceCount: 1})
		pass.DrawIndexed(gfx.DrawIndexedArgs{IndexCount: 3, InstanceCount: 1})
		pass.DrawIndirect(live.buffer, 0)
		pass.DrawIndexedIndirect(live.buffer, 0)
		pass.MultiDrawIndirect(live.buffer, 0, 2)
		pass.MultiDrawIndexedIndirect(live.buffer, 0, 2)
	})
	inComputePass(encoder, live, func(pass gfx.ComputePassEncoder) {
		pass.Dispatch(1, 1, 1)
		pass.DispatchIndirect(live.buffer, 0)
	})
	inRenderPass(encoder, live, func(pass gfx.RenderPassEncoder) { pass.Draw(gfx.DrawArgs{VertexCount: 3, InstanceCount: 1}) })

	if _, err := encoder.Finish(); err != nil {
		t.Fatalf("Finish answered %v", err)
	}
}

func TestABrokenPassSequenceFailsAtFinish(t *testing.T) {
	for _, test := range []struct {
		name   string
		record func(encoder gfx.CommandEncoder, live objects)
	}{
		{"a draw with no pipeline", func(encoder gfx.CommandEncoder, live objects) {
			renderPassOn(encoder, live).Draw(gfx.DrawArgs{VertexCount: 3, InstanceCount: 1})
		}},
		{"an indexed draw with no pipeline", func(encoder gfx.CommandEncoder, live objects) {
			renderPassOn(encoder, live).DrawIndexed(gfx.DrawIndexedArgs{IndexCount: 3, InstanceCount: 1})
		}},
		{"an indirect draw with no pipeline", func(encoder gfx.CommandEncoder, live objects) {
			renderPassOn(encoder, live).DrawIndirect(live.buffer, 0)
		}},
		{"an indexed indirect draw with no pipeline", func(encoder gfx.CommandEncoder, live objects) {
			renderPassOn(encoder, live).DrawIndexedIndirect(live.buffer, 0)
		}},
		{"indirect draws with no pipeline", func(encoder gfx.CommandEncoder, live objects) {
			renderPassOn(encoder, live).MultiDrawIndirect(live.buffer, 0, 2)
		}},
		{"indexed indirect draws with no pipeline", func(encoder gfx.CommandEncoder, live objects) {
			renderPassOn(encoder, live).MultiDrawIndexedIndirect(live.buffer, 0, 2)
		}},
		{"a dispatch with no pipeline", func(encoder gfx.CommandEncoder, _ objects) {
			encoder.BeginComputePass(gfx.ComputePassDescriptor{}).Dispatch(1, 1, 1)
		}},
		{"an indirect dispatch with no pipeline", func(encoder gfx.CommandEncoder, live objects) {
			encoder.BeginComputePass(gfx.ComputePassDescriptor{}).DispatchIndirect(live.buffer, 0)
		}},
		{"a draw after the pipeline of an earlier pass", func(encoder gfx.CommandEncoder, live objects) {
			inRenderPass(encoder, live, func(gfx.RenderPassEncoder) {})
			pass := renderPassOn(encoder, live)
			pass.Draw(gfx.DrawArgs{VertexCount: 3, InstanceCount: 1})
			pass.End()
		}},
		{"a render pass open at Finish", func(encoder gfx.CommandEncoder, live objects) {
			renderPassOn(encoder, live)
		}},
		{"a compute pass open at Finish", func(encoder gfx.CommandEncoder, _ objects) {
			encoder.BeginComputePass(gfx.ComputePassDescriptor{})
		}},
		{"a render pass begun inside a render pass", func(encoder gfx.CommandEncoder, live objects) {
			outer := renderPassOn(encoder, live)
			inRenderPass(encoder, live, func(gfx.RenderPassEncoder) {})
			outer.End()
		}},
		{"a compute pass begun inside a render pass", func(encoder gfx.CommandEncoder, live objects) {
			outer := renderPassOn(encoder, live)
			inComputePass(encoder, live, func(gfx.ComputePassEncoder) {})
			outer.End()
		}},
		{"a render pass begun inside a compute pass", func(encoder gfx.CommandEncoder, live objects) {
			outer := encoder.BeginComputePass(gfx.ComputePassDescriptor{})
			inRenderPass(encoder, live, func(gfx.RenderPassEncoder) {})
			outer.End()
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, device := requestDevice(t)
			live := createObjects(t, device, everyBufferUsage, everyTextureUsage)
			encoder := createEncoder(t, device)

			test.record(encoder, live)

			if _, err := encoder.Finish(); err == nil {
				t.Fatal("Finish answered no error")
			}
		})
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
		inRenderPass(encoder, live, func(pass gfx.RenderPassEncoder) { pass.SetPipeline(tested.renderPipeline) })
	}},
	{name: "a render pass's bind group", record: func(encoder gfx.CommandEncoder, live, tested objects) {
		inRenderPass(encoder, live, func(pass gfx.RenderPassEncoder) { pass.SetBindGroup(0, tested.group, nil) })
	}},
	{name: "a vertex buffer", bufferUsage: gfx.BufferUsageFlagVertex, record: func(encoder gfx.CommandEncoder, live, tested objects) {
		inRenderPass(encoder, live, func(pass gfx.RenderPassEncoder) { pass.SetVertexBuffer(0, tested.buffer, 0) })
	}},
	{name: "an index buffer", bufferUsage: gfx.BufferUsageFlagIndex, record: func(encoder gfx.CommandEncoder, live, tested objects) {
		inRenderPass(encoder, live, func(pass gfx.RenderPassEncoder) { pass.SetIndexBuffer(tested.buffer, gfx.IndexFormatUint16, 0) })
	}},
	{name: "an indirect draw", record: func(encoder gfx.CommandEncoder, live, tested objects) {
		inRenderPass(encoder, live, func(pass gfx.RenderPassEncoder) { pass.DrawIndirect(tested.buffer, 0) })
	}},
	{name: "an indexed indirect draw", record: func(encoder gfx.CommandEncoder, live, tested objects) {
		inRenderPass(encoder, live, func(pass gfx.RenderPassEncoder) { pass.DrawIndexedIndirect(tested.buffer, 0) })
	}},
	{name: "indirect draws", record: func(encoder gfx.CommandEncoder, live, tested objects) {
		inRenderPass(encoder, live, func(pass gfx.RenderPassEncoder) { pass.MultiDrawIndirect(tested.buffer, 0, 2) })
	}},
	{name: "indexed indirect draws", record: func(encoder gfx.CommandEncoder, live, tested objects) {
		inRenderPass(encoder, live, func(pass gfx.RenderPassEncoder) { pass.MultiDrawIndexedIndirect(tested.buffer, 0, 2) })
	}},
	{name: "a compute pipeline", record: func(encoder gfx.CommandEncoder, live, tested objects) {
		inComputePass(encoder, live, func(pass gfx.ComputePassEncoder) { pass.SetPipeline(tested.computePipeline) })
	}},
	{name: "a compute pass's bind group", record: func(encoder gfx.CommandEncoder, live, tested objects) {
		inComputePass(encoder, live, func(pass gfx.ComputePassEncoder) { pass.SetBindGroup(0, tested.group, nil) })
	}},
	{name: "an indirect dispatch", record: func(encoder gfx.CommandEncoder, live, tested objects) {
		inComputePass(encoder, live, func(pass gfx.ComputePassEncoder) { pass.DispatchIndirect(tested.buffer, 0) })
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

// inRenderPass records a render pass on live's view, live's pipeline set,
// with record's commands, and ends it.
func inRenderPass(encoder gfx.CommandEncoder, live objects, record func(pass gfx.RenderPassEncoder)) {
	pass := encoder.BeginRenderPass(gfx.RenderPassDescriptor{ColorAttachments: []gfx.RenderPassColorAttachment{{View: live.view}}})
	pass.SetPipeline(live.renderPipeline)
	record(pass)
	pass.End()
}

// inComputePass records a compute pass, live's pipeline set, with record's
// commands, and ends it.
func inComputePass(encoder gfx.CommandEncoder, live objects, record func(pass gfx.ComputePassEncoder)) {
	pass := encoder.BeginComputePass(gfx.ComputePassDescriptor{})
	pass.SetPipeline(live.computePipeline)
	record(pass)
	pass.End()
}

// renderPassOn begins a render pass on live's view, with no pipeline set.
func renderPassOn(encoder gfx.CommandEncoder, live objects) gfx.RenderPassEncoder {
	return encoder.BeginRenderPass(gfx.RenderPassDescriptor{ColorAttachments: []gfx.RenderPassColorAttachment{{View: live.view}}})
}

// regionsWithRowsOf answers one region of live's texture, its buffer rows
// bytesPerRow apart.
func regionsWithRowsOf(live objects, bytesPerRow uint64) []gfx.BufferTextureCopy {
	return []gfx.BufferTextureCopy{{
		BufferLayout: gfx.ImageDataLayout{BytesPerRow: uint32(bytesPerRow), RowsPerImage: 1},
		TextureBase:  gfx.ImageCopyTexture{Texture: live.texture},
		Size:         gfx.Extent3D{Width: 4, Height: 1, DepthOrArrayLayers: 1},
	}}
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

// submit submits buffers to device, failing the test on an error.
func submit(t *testing.T, device gfx.BackendDevice, buffers ...gfx.CommandBuffer) {
	t.Helper()
	_, err := device.Submit(buffers...)
	mustSucceed(t, err)
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
